package network

import (
	"encoding/csv"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Region represents a simulated geographic datacenter region.
type Region string

const (
	RegionUSWest Region = "us-west"
	RegionUSEast Region = "us-east"
	RegionEurope Region = "europe"
	RegionAsia   Region = "asia"
	RegionAfrica Region = "africa"
)

// NetworkFaultScenario defines stress conditions injected into the testbed.
type NetworkFaultScenario struct {
	DelayBurstActive bool    // 10x RTT burst up to 3000ms
	BurstMultiplier  float64 // e.g. 10.0
	PacketLossRate   float64 // e.g. 0.10 (10% drop)
	DoSTargetActive  bool    // Targeted DoS attack against leader
	JitterVarianceMs int     // Jitter variance
}

// WANNetem simulates realistic geo-distributed WAN network conditions.
type WANNetem struct {
	mu           sync.RWMutex
	RTTMatrix    map[Region]map[Region]time.Duration
	NodeRegions  map[string]Region
	Fault        NetworkFaultScenario
	RandomSource *rand.Rand
}

// LoadWANMatrixFromCSV dynamically loads the empirical WAN latency matrix from data/wan_latency_matrix.csv.
// Sourced from AWS Global Infrastructure ping measurements across 5 regions:
// us-west-2 (Oregon), us-east-1 (Virginia), eu-central-1 (Frankfurt), ap-northeast-1 (Tokyo), af-south-1 (Cape Town).
func LoadWANMatrixFromCSV() (map[Region]map[Region]time.Duration, error) {
	candidatePaths := []string{
		"data/wan_latency_matrix.csv",
		"../data/wan_latency_matrix.csv",
		"../../data/wan_latency_matrix.csv",
	}

	var file *os.File
	var err error
	for _, p := range candidatePaths {
		file, err = os.Open(p)
		if err == nil {
			break
		}
	}
	if file == nil {
		return nil, fmt.Errorf("could not find data/wan_latency_matrix.csv")
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	regionMap := map[string]Region{
		"US-West": RegionUSWest,
		"US-East": RegionUSEast,
		"Europe":  RegionEurope,
		"Asia":    RegionAsia,
		"Africa":  RegionAfrica,
	}

	rttMatrix := make(map[Region]map[Region]time.Duration)
	for _, r := range []Region{RegionUSWest, RegionUSEast, RegionEurope, RegionAsia, RegionAfrica} {
		rttMatrix[r] = make(map[Region]time.Duration)
	}

	header := records[0]
	cols := make([]Region, len(header)-1)
	for i := 1; i < len(header); i++ {
		cols[i-1] = regionMap[strings.TrimSpace(header[i])]
	}

	for _, row := range records[1:] {
		if len(row) < len(header) {
			continue
		}
		fromRegion := regionMap[strings.TrimSpace(row[0])]
		for cIdx, toRegion := range cols {
			val, parseErr := strconv.Atoi(strings.TrimSpace(row[cIdx+1]))
			if parseErr == nil {
				rttMatrix[fromRegion][toRegion] = time.Duration(val) * time.Millisecond
			}
		}
	}

	return rttMatrix, nil
}

// NewWANNetem initializes a 5-region WAN latency matrix dynamically from the empirical dataset on disk.
func NewWANNetem() *WANNetem {
	rttMatrix, err := LoadWANMatrixFromCSV()
	if err != nil {
		panic(fmt.Sprintf("Critical: failed to load WAN latency matrix from CSV: %v", err))
	}

	return &WANNetem{
		RTTMatrix:    rttMatrix,
		NodeRegions:  make(map[string]Region),
		RandomSource: rand.New(rand.NewSource(time.Now().UnixNano())),
		Fault: NetworkFaultScenario{
			BurstMultiplier:  1.0,
			PacketLossRate:   0.0,
			JitterVarianceMs: 25,
		},
	}
}

// AssignRegion assigns a node to a region.
func (n *WANNetem) AssignRegion(nodeID string, region Region) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.NodeRegions[nodeID] = region
}

// SetFaultConfig configures fault injection on the fly.
func (n *WANNetem) SetFaultConfig(fault NetworkFaultScenario) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Fault = fault
}

// GetFaultConfig returns current fault config.
func (n *WANNetem) GetFaultConfig() NetworkFaultScenario {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.Fault
}

// SimulateLink evaluates network latency and packet drop between sender and receiver.
// Returns (simulatedDelay, isDropped).
func (n *WANNetem) SimulateLink(sender, receiver string, isLeader bool, round int) (time.Duration, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// 1. Packet Loss check
	if n.Fault.PacketLossRate > 0 && n.RandomSource.Float64() < n.Fault.PacketLossRate {
		return 0, true // Dropped
	}

	// 2. Base RTT lookup from dynamically loaded dataset
	senderRegion, sOk := n.NodeRegions[sender]
	if !sOk {
		senderRegion = RegionUSWest
	}
	receiverRegion, rOk := n.NodeRegions[receiver]
	if !rOk {
		receiverRegion = RegionUSEast
	}

	baseRTT := n.RTTMatrix[senderRegion][receiverRegion]
	if baseRTT == 0 {
		baseRTT = 75 * time.Millisecond
	}

	// 3. Jitter calculation (Uniform [-JitterVariance, +JitterVariance])
	jitter := time.Duration(n.RandomSource.Intn(2*n.Fault.JitterVarianceMs+1)-n.Fault.JitterVarianceMs) * time.Millisecond
	effectiveDelay := baseRTT + jitter
	if effectiveDelay < 5*time.Millisecond {
		effectiveDelay = 5 * time.Millisecond
	}

	// 4. Delay Burst Injection
	isBurst := (round >= 12 && round <= 15) || (round >= 28 && round <= 31) || (round%10 >= 3 && round%10 <= 5)
	if n.Fault.DelayBurstActive && isBurst {
		// Transient burst spikes up to 3000ms
		burstRatio := n.Fault.BurstMultiplier
		if burstRatio <= 1.0 {
			burstRatio = 8.0
		}
		burstMs := float64(effectiveDelay.Milliseconds()) * burstRatio
		if burstMs > 3000 {
			burstMs = 3000
		}
		effectiveDelay = time.Duration(burstMs) * time.Millisecond
	}

	// 5. Adaptive DoS attack targeted at leader
	if n.Fault.DoSTargetActive && isLeader {
		dosDelay := time.Duration(350+n.RandomSource.Intn(300)) * time.Millisecond
		effectiveDelay += dosDelay
	}

	return effectiveDelay, false
}
