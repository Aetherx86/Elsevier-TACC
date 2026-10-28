use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Transaction {
    pub id: String,
    pub sender: String,
    pub receiver: String,
    pub amount: u64,
    pub data: String,
    pub gas_price: u64,
    pub signature: String,
    pub timestamp: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct QuorumCertificate {
    pub view: u64,
    pub block_hash: String,
    pub signers: Vec<String>,
    pub aggregated_sig: String,
    pub timestamp: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Block {
    pub hash: String,
    pub parent_hash: String,
    pub view: u64,
    pub round: u64,
    pub proposer: String,
    pub vrf_proof: String,
    pub vrf_score: u64,
    pub transactions: Vec<Transaction>,
    pub justify_qc: QuorumCertificate,
    pub timestamp: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Vote {
    pub view: u64,
    pub block_hash: String,
    pub voter: String,
    pub signature: String,
    pub timestamp: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TACCParams {
    pub k: usize,
    pub k_min: usize,
    pub k_max: usize,
    pub delta_ms: u64,
    pub min_delta_ms: u64,
    pub max_delta_ms: u64,
    pub b: usize,
    pub min_b: usize,
    pub max_b: usize,
    pub epsilon: f64,
    pub rho: f64,
    pub mode: String,
    pub safe_mode: bool,
    pub rollbacks: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Telemetry {
    pub round: u64,
    pub view: u64,
    pub mode: String,
    pub latency_ms: u64,
    pub p50_ms: u64,
    pub p90_ms: u64,
    pub p99_ms: u64,
    pub committee_size: usize,
    pub delta_ms: u64,
    pub block_size_b: usize,
    pub timeout: bool,
    pub view_changed: bool,
    pub finalized_count: usize,
    pub finalized_hash: String,
    pub tps: f64,
    pub mempool_size: usize,
    pub security_p_bad: f64,
    pub leader: String,
    pub safe_mode: bool,
    pub controller_note: String,
}

#[derive(Debug, Clone)]
pub struct RoundResult {
    pub round: u64,
    pub view: u64,
    pub latency_ms: u64,
    pub is_timeout: bool,
    pub view_changed: bool,
    pub tps: f64,
    pub committed: bool,
}
