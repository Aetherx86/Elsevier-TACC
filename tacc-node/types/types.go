package types

import "time"

// Transaction represents a transfer or state transition.
type Transaction struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Receiver  string `json:"receiver"`
	Amount    uint64 `json:"amount"`
	Data      string `json:"data"`
	GasPrice  uint64 `json:"gas_price"`
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
}

// QuorumCertificate (QC) represents a threshold cryptographic proof formed by a quorum of committee votes.
type QuorumCertificate struct {
	View          int      `json:"view"`
	BlockHash     string   `json:"block_hash"`
	Signers       []string `json:"signers"`
	AggregatedSig string   `json:"aggregated_sig"`
	Timestamp     int64    `json:"timestamp"`
}

// Block represents a HotStuff-2 pipelined block.
type Block struct {
	Hash         string            `json:"hash"`
	ParentHash   string            `json:"parent_hash"`
	View         int               `json:"view"`
	Round        int               `json:"round"`
	Proposer     string            `json:"proposer"`
	VRFProof     string            `json:"vrf_proof"`
	VRFScore     uint64            `json:"vrf_score"`
	Transactions []Transaction     `json:"transactions"`
	JustifyQC    QuorumCertificate `json:"justify_qc"`
	Timestamp    int64             `json:"timestamp"`
}

// Vote represents a vote on a proposed block in HotStuff-2.
type Vote struct {
	View      int    `json:"view"`
	BlockHash string `json:"block_hash"`
	Voter     string `json:"voter"`
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
}

// ViewChangeMessage is sent when pacemaker timer Δ expires.
type ViewChangeMessage struct {
	View      int               `json:"view"`
	NodeID    string            `json:"node_id"`
	HighestQC QuorumCertificate `json:"highest_qc"`
	Signature string            `json:"signature"`
	Timestamp int64             `json:"timestamp"`
}

// TACCParams holds adaptive consensus control parameters.
type TACCParams struct {
	K          int           `json:"k"`
	KMin       int           `json:"k_min"`
	KMax       int           `json:"k_max"`
	Delta      time.Duration `json:"delta"`
	MinDelta   time.Duration `json:"min_delta"`
	MaxDelta   time.Duration `json:"max_delta"`
	B          int           `json:"b"`
	MinB       int           `json:"min_b"`
	MaxB       int           `json:"max_b"`
	Epsilon    float64       `json:"epsilon"`
	Rho        float64       `json:"rho"`
	Mode       string        `json:"mode"`
	SafeMode   bool          `json:"safe_mode"`
	Rollbacks  int           `json:"rollbacks"`
	Updated    time.Time     `json:"updated_at"`
}

// Telemetry captures comprehensive real-time consensus network metrics.
type Telemetry struct {
	Round          int           `json:"round"`
	View           int           `json:"view"`
	Mode           string        `json:"mode"`
	Latency        time.Duration `json:"latency"`
	LatencyMs      int64         `json:"latency_ms"`
	P50Ms          int64         `json:"p50_ms"`
	P90Ms          int64         `json:"p90_ms"`
	P99Ms          int64         `json:"p99_ms"`
	CommitteeSize  int           `json:"committee_size"`
	DeltaMs        int64         `json:"delta_ms"`
	BlockSizeB     int           `json:"block_size_b"`
	Timeout        bool          `json:"timeout"`
	ViewChanged    bool          `json:"view_changed"`
	FinalizedCount int           `json:"finalized_count"`
	FinalizedHash  string        `json:"finalized_hash"`
	TPS            float64       `json:"tps"`
	MempoolSize    int           `json:"mempool_size"`
	AppliedTx      int           `json:"applied_tx"`
	DroppedTx      int           `json:"dropped_tx"`
	SecurityPBad   float64       `json:"security_p_bad"`
	Leader         string        `json:"leader"`
	Committee      []string      `json:"committee"`
	SafeMode       bool          `json:"safe_mode"`
	ControllerNote string        `json:"controller_note"`
	Timestamp      time.Time     `json:"timestamp"`
}

// RoundResult feeds back execution outcomes into the adaptive controller.
type RoundResult struct {
	Round       int
	View        int
	Latency     time.Duration
	IsTimeout   bool
	ViewChanged bool
	TPS         float64
	Committed   bool
}

// FullStatus represents the complete node status returned via RPC.
type FullStatus struct {
	Round          int                 `json:"round"`
	View           int                 `json:"view"`
	Balances       map[string]uint64   `json:"balances"`
	Params         TACCParams          `json:"params"`
	Committee      []string            `json:"committee"`
	LastLeader     string              `json:"last_leader"`
	LastLatency    int64               `json:"last_latency_ms"`
	LastTimeout    bool                `json:"last_timeout"`
	LastFinalized  string              `json:"last_finalized"`
	FinalizedChain []string            `json:"finalized_chain"`
	MempoolSize    int                 `json:"mempool_size"`
	MetricsCount   int                 `json:"metrics_count"`
	SafeMode       bool                `json:"safe_mode"`
	LastUpdate     time.Time           `json:"last_update"`
}

// NetMessage is a gossip envelope for p2p communication.
type NetMessage struct {
	Kind      string `json:"kind"`
	NodeID    string `json:"node_id"`
	Round     int    `json:"round"`
	View      int    `json:"view"`
	Timestamp int64  `json:"timestamp"`
	Payload   any    `json:"payload"`
}
