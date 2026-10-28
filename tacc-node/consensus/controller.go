package consensus

import (
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"tacc-node/crypto"
	"tacc-node/types"
)

const (
	DefaultMinK            = 4
	DefaultMaxK            = 256
	DefaultMinDelta        = 150 * time.Millisecond
	DefaultMaxDelta        = 5000 * time.Millisecond
	DefaultMinB            = 20
	DefaultMaxB            = 500
	DefaultHistoryWindow   = 16
	DefaultCooldownRounds  = 4
	HighTimeoutThreshold   = 0.25
	EmergencyDropThreshold = 0.50
	HysteresisDeltaRatio   = 0.08 // 8% change threshold to prevent oscillation
)

// TACCController implements a closed-loop adaptive feedback controller:
// - P99 tail latency synchronized timeout Delta
// - Redundancy-seeking committee size k bound by mathematical constraint k >= k_min
// - Congestion-aware block sizing B
// - Hysteresis band and cooldown counters
// - Anomaly detection with rollback to safe mode
type TACCController struct {
	Params         types.TACCParams
	History        []types.RoundResult
	Cooldown       int
	SafeCheckpoint types.TACCParams
	RollbackCount  int
	LastController string
	Evaluator      *crypto.SecurityBoundEvaluator
	mu             sync.RWMutex
}

// NewController initializes the TACC adaptive controller with mathematical bounds.
func NewController(mode string, rho, epsilon float64) *TACCController {
	eval := crypto.NewSecurityBoundEvaluator(500, rho, epsilon)
	kMin, pBad := eval.FindKMin(DefaultMinK, DefaultMaxK)

	if kMin < DefaultMinK {
		kMin = DefaultMinK
	}
	if kMin > DefaultMaxK {
		kMin = DefaultMaxK
	}

	initialK := kMin + 4
	if initialK > DefaultMaxK {
		initialK = DefaultMaxK
	}

	initialParams := types.TACCParams{
		K:         initialK,
		KMin:      kMin,
		KMax:      DefaultMaxK,
		Delta:     450 * time.Millisecond,
		MinDelta:  DefaultMinDelta,
		MaxDelta:  DefaultMaxDelta,
		B:         100,
		MinB:      DefaultMinB,
		MaxB:      DefaultMaxB,
		Epsilon:   epsilon,
		Rho:       rho,
		Mode:      mode,
		SafeMode:  false,
		Rollbacks: 0,
		Updated:   time.Now(),
	}

	ctrl := &TACCController{
		Params:         initialParams,
		SafeCheckpoint: initialParams,
		History:        make([]types.RoundResult, 0, DefaultHistoryWindow),
		Evaluator:      eval,
		LastController: "initialized",
	}

	log.Printf(
		"[INIT] [TACC Controller Init] mode=%s rho=%.4f epsilon=%g K=%d KMin=%d (pBad=%.4e)",
		mode, rho, epsilon, ctrl.Params.K, ctrl.Params.KMin, pBad,
	)

	return ctrl
}

// Update feeds a round execution result into the feedback loop.
func (c *TACCController) Update(res types.RoundResult) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.History = append(c.History, res)
	if len(c.History) > DefaultHistoryWindow {
		c.History = c.History[1:]
	}

	if c.Params.Mode != "tacc" {
		c.LastController = "static mode: fixed parameters"
		return c.LastController
	}

	if c.Cooldown > 0 {
		c.Cooldown--
		c.LastController = fmt.Sprintf("cooldown active (%d rounds remaining)", c.Cooldown)
		return c.LastController
	}

	if len(c.History) < 4 {
		c.LastController = "accumulating baseline history"
		return c.LastController
	}

	// Calculate statistics
	var lats []float64
	timeouts := 0
	viewChanges := 0
	avgTPS := 0.0

	for _, h := range c.History {
		if h.IsTimeout {
			timeouts++
		}
		if h.ViewChanged {
			viewChanges++
		}
		lats = append(lats, float64(h.Latency.Milliseconds()))
		avgTPS += h.TPS
	}
	avgTPS /= float64(len(c.History))

	sort.Float64s(lats)
	p50 := lats[len(lats)/2]
	p90 := lats[int(float64(len(lats))*0.9)]
	p99 := lats[len(lats)-1]

	timeoutRate := float64(timeouts) / float64(len(c.History))

	// Anomaly Detection: Surge in timeouts triggers Rollback & Safe Mode
	if timeoutRate >= EmergencyDropThreshold {
		c.Params.SafeMode = true
		c.RollbackCount++
		c.Params.Rollbacks = c.RollbackCount
		c.Params.K = min(c.SafeCheckpoint.K+10, c.Params.KMax)
		c.Params.Delta = clampDuration(
			time.Duration(p99*2.5)*time.Millisecond,
			c.Params.MinDelta,
			c.Params.MaxDelta,
		)
		c.Params.B = max(c.Params.MinB, c.Params.B/2)
		c.Cooldown = DefaultCooldownRounds + 2
		c.LastController = fmt.Sprintf("EMERGENCY ROLLBACK: timeout_rate=%.2f! SafeMode active", timeoutRate)
		log.Printf("[EMERGENCY] [TACC] %s", c.LastController)
		return c.LastController
	}

	newP := c.Params
	var note string

	// Adaptive Timeout (Delta) Tuning: Delta = gamma * P99
	gamma := 1.6
	if timeoutRate > 0.10 {
		gamma = 2.0
	}
	targetDelta := clampDuration(
		time.Duration(p99*gamma)*time.Millisecond,
		c.Params.MinDelta,
		c.Params.MaxDelta,
	)

	// Apply Hysteresis to Delta
	deltaDiff := math.Abs(float64(targetDelta - c.Params.Delta))
	if deltaDiff/float64(c.Params.Delta) > HysteresisDeltaRatio {
		newP.Delta = targetDelta
	}

	// Adaptive Committee Size (k) Tuning
	if timeoutRate >= HighTimeoutThreshold {
		// WAN congestion/jitter: expand committee to gain quorum redundancy
		newP.K = min(c.Params.K+4, c.Params.KMax)
		// Reduce block size to alleviate bandwidth bottleneck
		newP.B = max(c.Params.MinB, c.Params.B-25)
		note = fmt.Sprintf("congestion mitigation: timeout_rate=%.2f, expand k=%d, shrink B=%d", timeoutRate, newP.K, newP.B)
	} else if timeoutRate > 0.05 {
		newP.K = min(c.Params.K+1, c.Params.KMax)
		note = fmt.Sprintf("slight jitter: expanding k=%d", newP.K)
	} else if p99 < float64(c.Params.Delta.Milliseconds())*0.45 {
		// Network is performing well: shrink k towards k_min to lower crypto overhead
		if c.Params.K > c.Params.KMin {
			newP.K = max(c.Params.KMin, c.Params.K-1)
			note = fmt.Sprintf("stable low-latency network: shrinking k towards k_min=%d", c.Params.KMin)
		}
		// Expand block size B to boost throughput
		if newP.B < c.Params.MaxB {
			newP.B = min(c.Params.MaxB, c.Params.B+20)
		}
	} else {
		note = fmt.Sprintf("equilibrium: p50=%.0fms p99=%.0fms", p50, p99)
	}

	// STRICT ENFORCEMENT: k CANNOT BE LESS THAN k_min
	if newP.K < c.Params.KMin {
		newP.K = c.Params.KMin
	}
	if newP.K > c.Params.KMax {
		newP.K = c.Params.KMax
	}

	// Clear SafeMode if network stabilized for consecutive rounds
	if c.Params.SafeMode && timeoutRate == 0 {
		c.Params.SafeMode = false
		note += " (SafeMode recovered)"
	}

	// Detect if parameters changed
	changed := newP.K != c.Params.K || newP.Delta != c.Params.Delta || newP.B != c.Params.B
	if changed {
		c.SafeCheckpoint = c.Params
		c.Params = newP
		c.Params.Updated = time.Now()
		c.Cooldown = DefaultCooldownRounds
		log.Printf(
			"[CTRL] [TACC Feedback] K=%d (KMin=%d) Delta=%v B=%d | P50=%.0f P90=%.0f P99=%.0f | %s",
			newP.K, newP.KMin, newP.Delta, newP.B, p50, p90, p99, note,
		)
	}

	c.LastController = note
	return note
}

// CurrentParams returns the latest parameters safely.
func (c *TACCController) CurrentParams() types.TACCParams {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Params
}

// SetMode allows dynamically switching between "tacc" and "static".
func (c *TACCController) SetMode(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Params.Mode = mode
}

// Helper functions
func clampDuration(v, lo, hi time.Duration) time.Duration {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
