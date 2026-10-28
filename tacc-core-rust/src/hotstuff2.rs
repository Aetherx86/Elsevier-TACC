use std::collections::{HashMap, HashSet};
use sha2::{Digest, Sha256};
use crate::types::{Block, QuorumCertificate, Vote};

pub struct HotStuff2Core {
    pub node_id: String,
    pub current_view: u64,
    pub locked_view: u64,
    pub highest_qc: QuorumCertificate,
    pub blocks: HashMap<String, Block>,
    pub finalized_blocks: Vec<Block>,
    pub finalized_hashes: HashSet<String>,
    pub pending_votes: HashMap<String, HashMap<String, Vote>>,
}

impl HotStuff2Core {
    pub fn new(node_id: String) -> Self {
        let genesis_block = Block {
            hash: "0000000000000000000000000000000000000000000000000000000000000000".to_string(),
            parent_hash: String::new(),
            view: 0,
            round: 0,
            proposer: "genesis".to_string(),
            vrf_proof: "genesis-proof".to_string(),
            vrf_score: 0,
            transactions: Vec::new(),
            justify_qc: QuorumCertificate {
                view: 0,
                block_hash: "0000000000000000000000000000000000000000000000000000000000000000".to_string(),
                signers: vec!["genesis".to_string()],
                aggregated_sig: "genesis-sig".to_string(),
                timestamp: 0,
            },
            timestamp: 0,
        };

        let genesis_qc = genesis_block.justify_qc.clone();
        let mut blocks = HashMap::new();
        blocks.insert(genesis_block.hash.clone(), genesis_block.clone());

        let mut finalized_hashes = HashSet::new();
        finalized_hashes.insert(genesis_block.hash.clone());

        Self {
            node_id,
            current_view: 0,
            locked_view: 0,
            highest_qc: genesis_qc,
            blocks,
            finalized_blocks: vec![genesis_block],
            finalized_hashes,
            pending_votes: HashMap::new(),
        }
    }

    pub fn compute_block_hash(b: &Block) -> String {
        let mut hasher = Sha256::new();
        hasher.update(b.view.to_be_bytes());
        hasher.update(b.round.to_be_bytes());
        hasher.update(b.parent_hash.as_bytes());
        hasher.update(b.proposer.as_bytes());
        hasher.update(b.justify_qc.block_hash.as_bytes());
        hasher.update((b.transactions.len() as u64).to_be_bytes());
        hasher.update(b.timestamp.to_be_bytes());
        hex::encode(hasher.finalize())
    }

    pub fn create_proposal(
        &mut self,
        round: u64,
        view: u64,
        proposer: String,
        txs: Vec<crate::types::Transaction>,
        vrf_proof: String,
        vrf_score: u64,
    ) -> Block {
        let parent_hash = self.highest_qc.block_hash.clone();
        let mut block = Block {
            hash: String::new(),
            parent_hash,
            view,
            round,
            proposer,
            vrf_proof,
            vrf_score,
            transactions: txs,
            justify_qc: self.highest_qc.clone(),
            timestamp: chrono::Utc::now().timestamp_millis(),
        };
        block.hash = Self::compute_block_hash(&block);
        self.blocks.insert(block.hash.clone(), block.clone());
        block
    }

    pub fn process_proposal(&mut self, block: &Block) -> Result<Vote, String> {
        if block.view < self.current_view {
            return Err(format!("Stale view {} < {}", block.view, self.current_view));
        }
        if block.justify_qc.view < self.locked_view {
            return Err("Safety violation: justify QC view < locked view".to_string());
        }

        self.blocks.insert(block.hash.clone(), block.clone());
        self.current_view = block.view;

        if block.justify_qc.view > self.highest_qc.view {
            self.highest_qc = block.justify_qc.clone();
        }
        if block.justify_qc.view > self.locked_view {
            self.locked_view = block.justify_qc.view;
        }

        // Check 2-Chain commit rule with justifyQC
        self.check_2chain_commit(&block.justify_qc);

        Ok(Vote {
            view: block.view,
            block_hash: block.hash.clone(),
            voter: self.node_id.clone(),
            signature: format!("sig-{}-v{}", self.node_id, block.view),
            timestamp: chrono::Utc::now().timestamp_millis(),
        })
    }

    pub fn add_vote(
        &mut self,
        vote: Vote,
        committee_size: usize,
    ) -> (Option<QuorumCertificate>, Vec<Block>) {
        let quorum = (2 * committee_size) / 3 + 1;
        let votes = self
            .pending_votes
            .entry(vote.block_hash.clone())
            .or_default();
        votes.insert(vote.voter.clone(), vote.clone());

        if votes.len() >= quorum {
            let signers: Vec<String> = votes.keys().cloned().collect();
            let qc = QuorumCertificate {
                view: vote.view,
                block_hash: vote.block_hash.clone(),
                signers,
                aggregated_sig: format!("agg-sig-{}", &vote.block_hash[0..8]),
                timestamp: chrono::Utc::now().timestamp_millis(),
            };

            if qc.view > self.highest_qc.view {
                self.highest_qc = qc.clone();
            }

            let newly_committed = self.check_2chain_commit(&qc);
            self.pending_votes.remove(&vote.block_hash);
            (Some(qc), newly_committed)
        } else {
            (None, Vec::new())
        }
    }

    // HotStuff-2 2-Chain commit rule: B committed if B <- B' and B' has QC
    fn check_2chain_commit(&mut self, child_qc: &QuorumCertificate) -> Vec<Block> {
        let child_block = match self.blocks.get(&child_qc.block_hash) {
            Some(b) => b.clone(),
            None => return Vec::new(),
        };

        let parent_hash = child_block.parent_hash.clone();
        let parent_block = match self.blocks.get(&parent_hash) {
            Some(b) => b.clone(),
            None => return Vec::new(),
        };

        if child_block.justify_qc.block_hash == parent_block.hash
            && !self.finalized_hashes.contains(&parent_block.hash)
        {
            let mut newly_committed = Vec::new();
            let mut curr_hash = parent_block.hash.clone();

            while !curr_hash.is_empty() && !self.finalized_hashes.contains(&curr_hash) {
                if let Some(b) = self.blocks.get(&curr_hash).cloned() {
                    self.finalized_hashes.insert(curr_hash.clone());
                    newly_committed.insert(0, b.clone());
                    self.finalized_blocks.push(b.clone());
                    curr_hash = b.parent_hash;
                } else {
                    break;
                }
            }
            newly_committed
        } else {
            Vec::new()
        }
    }

    pub fn last_finalized_hash(&self) -> String {
        self.finalized_blocks
            .last()
            .map(|b| b.hash.clone())
            .unwrap_or_default()
    }
}
