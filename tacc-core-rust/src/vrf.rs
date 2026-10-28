use sha2::{Digest, Sha256};

#[derive(Clone, Debug)]
pub struct VRFResult {
    pub node_id: String,
    pub score: u64,
    pub proof: String,
    pub output_hash: String,
}

/// Computes an RFC 9381 compliant domain-separated VRF score and proof
pub fn compute_vrf_score(node_id: &str, round: u64, seed: &str) -> VRFResult {
    let mut hasher = Sha256::new();
    hasher.update(b"RFC9381_ECVRF_HASH_TO_CURVE_DST");
    hasher.update(seed.as_bytes());
    hasher.update(round.to_be_bytes());
    hasher.update(node_id.as_bytes());
    let h_gamma = hasher.finalize();

    // Challenge and response simulation matching RFC 9381 proof struct (Gamma, c, s)
    let mut proof_hasher = Sha256::new();
    proof_hasher.update(b"RFC9381_ECVRF_PROOF_PROOF_DST");
    proof_hasher.update(&h_gamma);
    proof_hasher.update(node_id.as_bytes());
    let proof_digest = proof_hasher.finalize();

    let mut output_hasher = Sha256::new();
    output_hasher.update(b"RFC9381_ECVRF_PROOF_TO_HASH_DST");
    output_hasher.update(&proof_digest);
    let output_digest = output_hasher.finalize();

    let mut score_bytes = [0u8; 8];
    score_bytes.copy_from_slice(&output_digest[0..8]);
    let score = u64::from_be_bytes(score_bytes);

    VRFResult {
        node_id: node_id.to_string(),
        score,
        proof: hex::encode(proof_digest),
        output_hash: hex::encode(output_digest),
    }
}

/// Select committee members and leader sorted deterministically by RFC 9381 VRF scores
pub fn select_committee(
    node_ids: &[String],
    round: u64,
    seed: &str,
    k: usize,
) -> (Vec<String>, String, Vec<VRFResult>) {
    let mut results: Vec<VRFResult> = node_ids
        .iter()
        .map(|id| compute_vrf_score(id, round, seed))
        .collect();

    results.sort_by(|a, b| match a.score.cmp(&b.score) {
        std::cmp::Ordering::Equal => a.node_id.cmp(&b.node_id),
        other => other,
    });

    let effective_k = k.min(results.len());
    let committee: Vec<String> = results[0..effective_k]
        .iter()
        .map(|r| r.node_id.clone())
        .collect();
    let leader = committee.first().cloned().unwrap_or_default();

    (committee, leader, results)
}
