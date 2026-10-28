package consensus

import (
	"testing"
	"time"

	"tacc-node/network"
	"tacc-node/types"
)

func TestHotStuff2TwoChainFinality(t *testing.T) {
	core := NewHotStuff2Core("test-node-1")

	// Initial state: Genesis block is finalized
	if len(core.FinalizedBlocks) != 1 {
		t.Fatalf("Expected 1 genesis block finalized, got %d", len(core.FinalizedBlocks))
	}

	// 1. Propose Block 1 (View 1, extends Genesis)
	b1 := core.CreateProposal(1, 1, "test-node-1", nil, "proof1", 100)
	_, err := core.ProcessProposal(b1)
	if err != nil {
		t.Fatalf("ProcessProposal b1 failed: %v", err)
	}

	// Quorum votes for b1 -> forms QC1 (quorum of 4 is 3)
	var qc1 *types.QuorumCertificate
	var committed1 []*types.Block
	for _, voterID := range []string{"voter-1", "voter-2", "voter-3"} {
		v := types.Vote{
			View:      b1.View,
			BlockHash: b1.Hash,
			Voter:     voterID,
			Signature: "sig-" + voterID,
			Timestamp: time.Now().UnixMilli(),
		}
		q, c, err := core.AddVote(v, 4)
		if err != nil {
			t.Fatalf("AddVote b1 failed: %v", err)
		}
		if q != nil {
			qc1 = q
			committed1 = c
		}
	}
	if qc1 == nil {
		t.Fatalf("Expected QC1 to be formed")
	}
	// With only b1, 2-chain rule has not completed yet
	if len(committed1) != 0 {
		t.Fatalf("Expected 0 newly committed blocks on b1 QC, got %d", len(committed1))
	}

	// 2. Propose Block 2 (View 2, extends Block 1 and carries QC1)
	b2 := core.CreateProposal(2, 2, "test-node-1", nil, "proof2", 200)
	_, err = core.ProcessProposal(b2)
	if err != nil {
		t.Fatalf("ProcessProposal b2 failed: %v", err)
	}

	// Quorum votes for b2 -> forms QC2
	var qc2 *types.QuorumCertificate
	var committed2 []*types.Block
	for _, voterID := range []string{"voter-1", "voter-2", "voter-3"} {
		v := types.Vote{
			View:      b2.View,
			BlockHash: b2.Hash,
			Voter:     voterID,
			Signature: "sig-" + voterID,
			Timestamp: time.Now().UnixMilli(),
		}
		q, c, err := core.AddVote(v, 4)
		if err != nil {
			t.Fatalf("AddVote b2 failed: %v", err)
		}
		if q != nil {
			qc2 = q
			committed2 = c
		}
	}
	if qc2 == nil {
		t.Fatalf("Expected QC2 to be formed")
	}

	// 2-Chain Rule: Direct linkage b1 <- b2 with QC2 commits b1!
	if len(committed2) != 1 || committed2[0].Hash != b1.Hash {
		t.Fatalf("Expected b1 to be finalized by 2-chain rule, got: %v", committed2)
	}

	t.Logf("[PASS] HotStuff-2 2-Chain commit verified! Block %s finalized", b1.Hash[:12])
}

func TestAdaptiveController(t *testing.T) {
	ctrl := NewController("tacc", 0.20, 1e-9)

	initialK := ctrl.Params.K
	initialDelta := ctrl.Params.Delta

	_ = initialK
	t.Logf("Initial: K=%d, KMin=%d, Delta=%v", ctrl.Params.K, ctrl.Params.KMin, ctrl.Params.Delta)

	// Feed rounds with latency spikes
	for r := 1; r <= 8; r++ {
		ctrl.Update(types.RoundResult{
			Round:       r,
			View:        r,
			Latency:     750 * time.Millisecond,
			IsTimeout:   r%2 == 0,
			ViewChanged: r%2 == 0,
			TPS:         120.0,
		})
	}

	// After timeouts and high latency, Delta must have stretched and K expanded
	stretchedParams := ctrl.CurrentParams()
	t.Logf("After stress: K=%d, Delta=%v, SafeMode=%v", stretchedParams.K, stretchedParams.Delta, stretchedParams.SafeMode)

	if stretchedParams.Delta <= initialDelta {
		t.Errorf("Expected Delta to stretch under latency spike: %v <= %v", stretchedParams.Delta, initialDelta)
	}
	if stretchedParams.K < ctrl.Params.KMin {
		t.Errorf("K (%d) cannot drop below KMin (%d)", stretchedParams.K, ctrl.Params.KMin)
	}
}

func TestComparativeBenchmarkRun(t *testing.T) {
	fault := network.NetworkFaultScenario{
		DelayBurstActive: true,
		BurstMultiplier:  3.0,
		PacketLossRate:   0.05,
		JitterVarianceMs: 20,
	}

	report := RunComparativeBenchmark(10, fault)
	t.Logf("Benchmark Report: Static P99=%.1fms, TACC P99=%.1fms, P99 Imp=%.1f%%, TPS Imp=%.1f%%",
		report.StaticP99Latency, report.TACCP99Latency, report.P99Improvement, report.TPSImprovement)

	if report.TotalRounds != 10 {
		t.Errorf("Expected 10 rounds, got %d", report.TotalRounds)
	}
}
