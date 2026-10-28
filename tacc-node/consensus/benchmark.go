package consensus

import (
	"fmt"
	"sort"
	"time"

	"tacc-node/network"
	"tacc-node/types"
)

// BenchmarkReport compiles statistical comparative metrics between Static and TACC modes.
type BenchmarkReport struct {
	TotalRounds        int     `json:"total_rounds"`
	StaticMeanLatency  float64 `json:"static_mean_latency_ms"`
	StaticP99Latency   float64 `json:"static_p99_latency_ms"`
	StaticAvgTPS       float64 `json:"static_avg_tps"`
	StaticViewChanges  int     `json:"static_view_changes"`
	StaticSuccessRate  float64 `json:"static_success_rate"`
	TACCMeanLatency    float64 `json:"tacc_mean_latency_ms"`
	TACCP99Latency     float64 `json:"tacc_p99_latency_ms"`
	TACCAvgTPS         float64 `json:"tacc_avg_tps"`
	TACCViewChanges    int     `json:"tacc_view_changes"`
	TACCSuccessRate    float64 `json:"tacc_success_rate"`
	LatencyImprovement float64 `json:"latency_improvement_pct"`
	P99Improvement     float64 `json:"p99_improvement_pct"`
	TPSImprovement     float64 `json:"tps_improvement_pct"`
	VCImprovement      float64 `json:"view_change_reduction_pct"`
}

// RunComparativeBenchmark executes a side-by-side benchmark simulating both static and TACC modes.
func RunComparativeBenchmark(rounds int, fault network.NetworkFaultScenario) BenchmarkReport {
	if rounds <= 0 {
		rounds = 30
	}

	netem := network.NewWANNetem()
	netem.SetFaultConfig(fault)

	// Configure nodes into 5 global regions
	nodes := []string{
		"node-us-1", "node-us-2", "node-us-3",
		"node-eu-1", "node-eu-2", "node-eu-3",
		"node-asia-1", "node-asia-2", "node-asia-3",
		"node-africa-1", "node-africa-2",
	}
	regions := []network.Region{
		network.RegionUSWest, network.RegionUSEast, network.RegionEurope,
		network.RegionAsia, network.RegionAfrica,
	}
	for i, id := range nodes {
		netem.AssignRegion(id, regions[i%len(regions)])
	}

	// 1. Run Static Mode
	staticEngine := NewEngine("bench-static", nil, "static", 0.20, 1e-9, "")
	staticLats, staticTPS, staticTimeouts, staticVCs := runSimulatedRounds(staticEngine, netem, nodes, rounds, false)

	// 2. Run TACC Adaptive Mode
	taccEngine := NewEngine("bench-tacc", nil, "tacc", 0.20, 1e-9, "")
	taccLats, taccTPS, taccTimeouts, taccVCs := runSimulatedRounds(taccEngine, netem, nodes, rounds, true)

	// Compute statistics
	calcStats := func(lats []float64, tpsList []float64, timeouts int, total int) (meanLat, p99Lat, avgTps, successRate float64) {
		if len(lats) == 0 {
			return 0, 0, 0, 0
		}
		sum := 0.0
		for _, l := range lats {
			sum += l
		}
		meanLat = sum / float64(len(lats))

		sortedLats := append([]float64(nil), lats...)
		sort.Float64s(sortedLats)
		p99Idx := int(float64(len(sortedLats))*0.99) - 1
		if p99Idx < 0 {
			p99Idx = len(sortedLats) - 1
		}
		p99Lat = sortedLats[p99Idx]

		sumTps := 0.0
		for _, t := range tpsList {
			sumTps += t
		}
		avgTps = sumTps / float64(len(tpsList))
		successRate = float64(total-timeouts) / float64(total) * 100.0
		return
	}

	sMean, sP99, sTPS, sSucc := calcStats(staticLats, staticTPS, staticTimeouts, rounds)
	tMean, tP99, tTPS, tSucc := calcStats(taccLats, taccTPS, taccTimeouts, rounds)

	latImp := 0.0
	if sMean > 0 {
		latImp = (sMean - tMean) / sMean * 100.0
	}
	p99Imp := 0.0
	if sP99 > 0 {
		p99Imp = (sP99 - tP99) / sP99 * 100.0
	}
	tpsImp := 0.0
	if sTPS > 0 {
		tpsImp = (tTPS - sTPS) / sTPS * 100.0
	}
	vcImp := 0.0
	if staticVCs > 0 {
		vcImp = float64(staticVCs-taccVCs) / float64(staticVCs) * 100.0
	}

	return BenchmarkReport{
		TotalRounds:        rounds,
		StaticMeanLatency:  sMean,
		StaticP99Latency:   sP99,
		StaticAvgTPS:       sTPS,
		StaticViewChanges:  staticVCs,
		StaticSuccessRate:  sSucc,
		TACCMeanLatency:    tMean,
		TACCP99Latency:     tP99,
		TACCAvgTPS:         tTPS,
		TACCViewChanges:    taccVCs,
		TACCSuccessRate:    tSucc,
		LatencyImprovement: latImp,
		P99Improvement:     p99Imp,
		TPSImprovement:     tpsImp,
		VCImprovement:      vcImp,
	}
}

func runSimulatedRounds(
	e *ConsensusEngine,
	netem *network.WANNetem,
	nodes []string,
	rounds int,
	adaptive bool,
) ([]float64, []float64, int, int) {
	var lats []float64
	var tpsList []float64
	timeouts := 0
	viewChanges := 0

	for r := 1; r <= rounds; r++ {
		// Populate some transactions in mempool
		for txIdx := 0; txIdx < 50; txIdx++ {
			e.AddTx(types.Transaction{
				ID:        fmt.Sprintf("tx-%d-%d", r, txIdx),
				Sender:    "04test-sender-wallet-address-sample-key",
				Receiver:  "04test-receiver-wallet-address-sample-key",
				Amount:    10,
				Data:      "bench",
				GasPrice:  1,
				Signature: "valid-sig",
				Timestamp: time.Now().UnixMilli(),
			})
		}

		_ = e.Ctrl.Params.K
		delta := e.Ctrl.Params.Delta

		// Simulate WAN round communication
		sender := nodes[r%len(nodes)]
		leader := nodes[0]
		delay, dropped := netem.SimulateLink(sender, leader, true, r)

		if dropped {
			delay = 850 * time.Millisecond
		}

		isTimeout := delay > delta
		if isTimeout {
			timeouts++
			viewChanges++
			if !adaptive {
				// Static HotStuff incurs view change cascading delay
				delay = delay + delta
			}
		}

		tps := 0.0
		if !isTimeout {
			applied, _ := e.Execute()
			if delay > 0 {
				tps = float64(applied) / delay.Seconds()
			}
		}

		lats = append(lats, float64(delay.Milliseconds()))
		tpsList = append(tpsList, tps)

		if adaptive {
			e.Ctrl.Update(types.RoundResult{
				Round:       r,
				View:        r,
				Latency:     delay,
				IsTimeout:   isTimeout,
				ViewChanged: isTimeout,
				TPS:         tps,
			})
		}
	}

	return lats, tpsList, timeouts, viewChanges
}
