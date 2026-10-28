package consensus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"tacc-node/types"
)

// HotStuff2Core implements the structural 2-Chain HotStuff-2 consensus protocol:
// - Linear communication O(n)
// - 2-Chain commit rule (Block B committed if direct child B' obtains QC: B <- B')
// - Optimistic responsiveness (advances immediately upon quorum 2f+1 votes)
// - Pacemaker & View Change handling
type HotStuff2Core struct {
	NodeID          string
	CurrentView     int
	LockedView      int
	HighestQC       types.QuorumCertificate
	Blocks          map[string]*types.Block
	FinalizedBlocks []*types.Block
	FinalizedHashes map[string]bool
	PendingVotes    map[string]map[string]types.Vote // blockHash -> (voterID -> Vote)
	ViewChangeVotes map[int]map[string]types.ViewChangeMessage
	mu              sync.RWMutex
}

// NewHotStuff2Core initializes the core state machine with the genesis block and QC.
func NewHotStuff2Core(nodeID string) *HotStuff2Core {
	genesisBlock := &types.Block{
		Hash:       "0000000000000000000000000000000000000000000000000000000000000000",
		ParentHash: "",
		View:       0,
		Round:      0,
		Proposer:   "genesis",
		Timestamp:  time.Now().UnixMilli(),
	}

	genesisQC := types.QuorumCertificate{
		View:          0,
		BlockHash:     genesisBlock.Hash,
		Signers:       []string{"genesis"},
		AggregatedSig: "genesis-sig",
		Timestamp:     genesisBlock.Timestamp,
	}

	genesisBlock.JustifyQC = genesisQC

	core := &HotStuff2Core{
		NodeID:          nodeID,
		CurrentView:     0,
		LockedView:      0,
		HighestQC:       genesisQC,
		Blocks:          make(map[string]*types.Block),
		FinalizedBlocks: make([]*types.Block, 0),
		FinalizedHashes: make(map[string]bool),
		PendingVotes:    make(map[string]map[string]types.Vote),
		ViewChangeVotes: make(map[int]map[string]types.ViewChangeMessage),
	}

	core.Blocks[genesisBlock.Hash] = genesisBlock
	core.FinalizedBlocks = append(core.FinalizedBlocks, genesisBlock)
	core.FinalizedHashes[genesisBlock.Hash] = true

	return core
}

// ComputeBlockHash deterministically calculates block hash.
func ComputeBlockHash(b *types.Block) string {
	msg := fmt.Sprintf("%d|%d|%s|%s|%s|%d|%d",
		b.View, b.Round, b.ParentHash, b.Proposer, b.JustifyQC.BlockHash, len(b.Transactions), b.Timestamp)
	h := sha256.Sum256([]byte(msg))
	return hex.EncodeToString(h[:])
}

// CreateProposal creates a new block extending the highest known QC.
func (c *HotStuff2Core) CreateProposal(
	round, view int,
	proposer string,
	txs []types.Transaction,
	vrfProof string,
	vrfScore uint64,
) *types.Block {
	c.mu.Lock()
	defer c.mu.Unlock()

	parentHash := c.HighestQC.BlockHash
	block := &types.Block{
		ParentHash:   parentHash,
		View:         view,
		Round:        round,
		Proposer:     proposer,
		VRFProof:     vrfProof,
		VRFScore:     vrfScore,
		Transactions: txs,
		JustifyQC:    c.HighestQC,
		Timestamp:    time.Now().UnixMilli(),
	}
	block.Hash = ComputeBlockHash(block)
	c.Blocks[block.Hash] = block
	return block
}

// ProcessProposal checks the HotStuff-2 safety rule and returns a vote if valid.
// Safety Rule: Block B view must be > CurrentView, and B.JustifyQC.View >= LockedView.
func (c *HotStuff2Core) ProcessProposal(block *types.Block) (*types.Vote, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if block.View < c.CurrentView {
		return nil, fmt.Errorf("stale proposal view %d < currentView %d", block.View, c.CurrentView)
	}

	// HotStuff safety condition: extends high QC and high QC view >= LockedView
	if block.JustifyQC.View < c.LockedView {
		return nil, fmt.Errorf("safety violation: justify QC view %d < locked view %d",
			block.JustifyQC.View, c.LockedView)
	}

	c.Blocks[block.Hash] = block
	c.CurrentView = block.View

	// Update highest QC if newer
	if block.JustifyQC.View > c.HighestQC.View {
		c.HighestQC = block.JustifyQC
	}

	// Lock on the justified block's view
	if block.JustifyQC.View > c.LockedView {
		c.LockedView = block.JustifyQC.View
	}

	// Check 2-Chain commit rule with justifyQC
	c.check2ChainCommitLocked(block.JustifyQC)

	vote := &types.Vote{
		View:      block.View,
		BlockHash: block.Hash,
		Voter:     c.NodeID,
		Signature: fmt.Sprintf("sig-%s-%s-%d", c.NodeID, block.Hash[:8], block.View),
		Timestamp: time.Now().UnixMilli(),
	}
	return vote, nil
}

// AddVote records a committee vote and forms a Quorum Certificate (QC) if quorum is reached.
// Quorum threshold: Q = floor(2k/3) + 1.
func (c *HotStuff2Core) AddVote(vote types.Vote, committeeSize int) (*types.QuorumCertificate, []*types.Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	quorum := (2*committeeSize)/3 + 1
	if quorum < 1 {
		quorum = 1
	}

	if _, exists := c.PendingVotes[vote.BlockHash]; !exists {
		c.PendingVotes[vote.BlockHash] = make(map[string]types.Vote)
	}
	c.PendingVotes[vote.BlockHash][vote.Voter] = vote

	votesForBlock := c.PendingVotes[vote.BlockHash]
	if len(votesForBlock) >= quorum {
		// Quorum achieved! Form QC
		signers := make([]string, 0, len(votesForBlock))
		for voter := range votesForBlock {
			signers = append(signers, voter)
		}

		qc := &types.QuorumCertificate{
			View:          vote.View,
			BlockHash:     vote.BlockHash,
			Signers:       signers,
			AggregatedSig: fmt.Sprintf("agg-%s-n%d", vote.BlockHash[:8], len(signers)),
			Timestamp:     time.Now().UnixMilli(),
		}

		if qc.View > c.HighestQC.View {
			c.HighestQC = *qc
		}

		// Evaluate 2-Chain finality
		committed := c.check2ChainCommitLocked(*qc)
		delete(c.PendingVotes, vote.BlockHash)
		return qc, committed, nil
	}

	return nil, nil, nil
}

// check2ChainCommitLocked enforces the HotStuff-2 2-chain commit rule:
// If block B' has QC, and B' is a direct child of B (B <- B'), then B is COMMITTED!
func (c *HotStuff2Core) check2ChainCommitLocked(childQC types.QuorumCertificate) []*types.Block {
	childBlock, exists := c.Blocks[childQC.BlockHash]
	if !exists {
		return nil
	}

	parentHash := childBlock.ParentHash
	parentBlock, parentExists := c.Blocks[parentHash]
	if !parentExists {
		return nil
	}

	// 2-Chain rule: direct linkage B <- B'
	if childBlock.JustifyQC.BlockHash == parentBlock.Hash && !c.FinalizedHashes[parentBlock.Hash] {
		// Parent block is finalized! Also commit any pending ancestors
		var newlyCommitted []*types.Block
		curr := parentBlock
		for curr != nil && !c.FinalizedHashes[curr.Hash] {
			newlyCommitted = append([]*types.Block{curr}, newlyCommitted...)
			c.FinalizedHashes[curr.Hash] = true
			c.FinalizedBlocks = append(c.FinalizedBlocks, curr)
			curr = c.Blocks[curr.ParentHash]
		}
		return newlyCommitted
	}

	return nil
}

// OnTimeout triggers pacemaker view change when Δ expires.
func (c *HotStuff2Core) OnTimeout() (types.ViewChangeMessage, int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.CurrentView++
	msg := types.ViewChangeMessage{
		View:      c.CurrentView,
		NodeID:    c.NodeID,
		HighestQC: c.HighestQC,
		Signature: fmt.Sprintf("vc-sig-%s-v%d", c.NodeID, c.CurrentView),
		Timestamp: time.Now().UnixMilli(),
	}
	return msg, c.CurrentView
}

// ProcessViewChange aggregates view change messages for the new leader.
func (c *HotStuff2Core) ProcessViewChange(vc types.ViewChangeMessage, committeeSize int) (*types.QuorumCertificate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	quorum := (2*committeeSize)/3 + 1
	if quorum < 1 {
		quorum = 1
	}

	if _, exists := c.ViewChangeVotes[vc.View]; !exists {
		c.ViewChangeVotes[vc.View] = make(map[string]types.ViewChangeMessage)
	}
	c.ViewChangeVotes[vc.View][vc.NodeID] = vc

	if len(c.ViewChangeVotes[vc.View]) >= quorum {
		// Pick highest QC among all received view-change messages
		var highestQC = c.HighestQC
		for _, msg := range c.ViewChangeVotes[vc.View] {
			if msg.HighestQC.View > highestQC.View {
				highestQC = msg.HighestQC
			}
		}
		c.HighestQC = highestQC
		c.CurrentView = vc.View
		delete(c.ViewChangeVotes, vc.View)
		return &highestQC, true
	}

	return nil, false
}

// GetFinalizedHistory returns the hashes of all finalized blocks in order.
func (c *HotStuff2Core) GetFinalizedHistory() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]string, 0, len(c.FinalizedBlocks))
	for _, b := range c.FinalizedBlocks {
		out = append(out, b.Hash)
	}
	return out
}

// LastFinalizedHash returns the hash of the latest finalized block.
func (c *HotStuff2Core) LastFinalizedHash() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.FinalizedBlocks) == 0 {
		return ""
	}
	return c.FinalizedBlocks[len(c.FinalizedBlocks)-1].Hash
}
