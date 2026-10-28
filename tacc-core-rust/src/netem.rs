#![allow(dead_code)]

use rand::Rng;
use std::fs::File;
use std::io::{BufRead, BufReader};
use std::path::PathBuf;

#[derive(Clone, Debug)]
pub struct FaultScenario {
    pub delay_burst_active: bool,
    pub burst_multiplier: f64,
    pub packet_loss_rate: f64,
    pub dos_target_active: bool,
    pub jitter_variance_ms: i32,
}

impl Default for FaultScenario {
    fn default() -> Self {
        Self {
            delay_burst_active: true,
            burst_multiplier: 8.0,
            packet_loss_rate: 0.10,
            dos_target_active: true,
            jitter_variance_ms: 15,
        }
    }
}

pub struct NetemSimulator {
    pub fault: FaultScenario,
    pub node_regions: Vec<usize>,
    pub rtt_matrix: [[f64; 5]; 5],
}

impl NetemSimulator {
    /// Loads the empirical 5-region WAN latency matrix dynamically from data/wan_latency_matrix.csv.
    /// The matrix is derived from AWS Global Infrastructure inter-region RTT telemetry:
    /// Region 0: US-West (us-west-2, Oregon)
    /// Region 1: US-East (us-east-1, Virginia)
    /// Region 2: Europe (eu-central-1, Frankfurt)
    /// Region 3: Asia (ap-northeast-1, Tokyo)
    /// Region 4: Africa (af-south-1, Cape Town)
    pub fn load_rtt_matrix_from_csv() -> [[f64; 5]; 5] {
        let candidates = [
            PathBuf::from("data/wan_latency_matrix.csv"),
            PathBuf::from("../data/wan_latency_matrix.csv"),
            PathBuf::from("../../data/wan_latency_matrix.csv"),
        ];

        for path in &candidates {
            if path.exists() {
                if let Ok(file) = File::open(path) {
                    let reader = BufReader::new(file);
                    let mut matrix = [[0.0f64; 5]; 5];
                    let mut row_idx = 0;

                    for line in reader.lines().skip(1) {
                        if let Ok(l) = line {
                            let parts: Vec<&str> = l.split(',').collect();
                            if parts.len() >= 6 && row_idx < 5 {
                                for col in 0..5 {
                                    if let Ok(val) = parts[col + 1].trim().parse::<f64>() {
                                        matrix[row_idx][col] = val;
                                    }
                                }
                                row_idx += 1;
                            }
                        }
                    }
                    if row_idx == 5 {
                        return matrix;
                    }
                }
            }
        }

        panic!("Critical: data/wan_latency_matrix.csv could not be found or parsed");
    }

    pub fn new(fault: FaultScenario, total_nodes: usize) -> Self {
        // Dynamically load the empirical WAN RTT matrix from research data file on disk
        let rtt_matrix = Self::load_rtt_matrix_from_csv();
        
        // Distribute nodes equally across the 5 global geographic regions
        let node_regions = (0..total_nodes).map(|i| i % 5).collect();
        Self {
            fault,
            node_regions,
            rtt_matrix,
        }
    }

    /// Determines if the current round falls into a transient WAN congestion burst window
    pub fn is_burst_round(&self, round: u64) -> bool {
        self.fault.delay_burst_active
            && ((12..=15).contains(&round)
                || (28..=31).contains(&round)
                || (43..=44).contains(&round))
    }

    /// Simulates real physical message propagation for a 2-chain consensus round:
    /// 1. Leader broadcasts proposal to committee.
    /// 2. Committee members sign and return votes to leader.
    /// 3. Returns the arrival time of the supermajority (2/3 + 1) quorum of votes.
    pub fn simulate_quorum_latency(
        &self,
        round: u64,
        leader_id: usize,
        committee: &[usize],
        is_static_dos: bool,
    ) -> f64 {
        let mut rng = rand::thread_rng();
        let leader_reg = self.node_regions[leader_id % self.node_regions.len()];
        let is_burst = self.is_burst_round(round);

        let mut vote_arrival_times = Vec::with_capacity(committee.len());

        for &member_id in committee {
            let member_reg = self.node_regions[member_id % self.node_regions.len()];
            // Lookup dynamically loaded empirical RTT between leader and member regions
            let base_rtt = self.rtt_matrix[leader_reg][member_reg];

            // Real-world network jitter (Uniform +/- jitter variance)
            let jitter = rng.gen_range(
                -(self.fault.jitter_variance_ms as f64)..=(self.fault.jitter_variance_ms as f64),
            );
            
            // 2-Chain finality round-trip transit time (proposal broadcast + vote aggregation)
            let mut transit = (base_rtt + jitter).max(10.0) * 1.5 + 20.0;

            // Congestion burst injection on cross-continental links
            if is_burst {
                transit *= rng.gen_range(2.2..3.2);
            }

            // Packet drop and retransmission
            if self.fault.packet_loss_rate > 0.0 && rng.gen_bool(self.fault.packet_loss_rate) {
                transit += base_rtt * 1.2;
            }

            // Predictable leader targeted DoS queueing delay (Static HotStuff only)
            if is_static_dos {
                transit += rng.gen_range(320.0..450.0);
            }

            vote_arrival_times.push(transit);
        }

        // Sort vote arrival times to determine when the supermajority quorum is formed
        vote_arrival_times.sort_by(|a, b| a.partial_cmp(b).unwrap());

        // 2/3 Supermajority threshold for HotStuff-2 QC formation
        let quorum_idx = (2 * committee.len()) / 3;
        vote_arrival_times[quorum_idx]
    }
}
