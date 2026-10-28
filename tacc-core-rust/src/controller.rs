use crate::security::SecurityEvaluator;
use crate::types::{RoundResult, TACCParams};

pub struct AdaptiveController {
    pub params: TACCParams,
    pub history: Vec<RoundResult>,
    pub cooldown: usize,
    pub safe_checkpoint: TACCParams,
    pub evaluator: SecurityEvaluator,
    pub last_note: String,
}

impl AdaptiveController {
    pub fn new(mode: &str, rho: f64, epsilon: f64) -> Self {
        let evaluator = SecurityEvaluator::new(500, rho, epsilon);
        let (k_min, _) = evaluator.find_k_min(4, 256);

        let initial_params = TACCParams {
            k: k_min + 4,
            k_min,
            k_max: 256,
            delta_ms: 450,
            min_delta_ms: 150,
            max_delta_ms: 5000,
            b: 100,
            min_b: 20,
            max_b: 500,
            epsilon,
            rho,
            mode: mode.to_string(),
            safe_mode: false,
            rollbacks: 0,
        };

        Self {
            safe_checkpoint: initial_params.clone(),
            params: initial_params,
            history: Vec::new(),
            cooldown: 0,
            evaluator,
            last_note: "initialized".to_string(),
        }
    }

    pub fn update(&mut self, res: RoundResult) -> String {
        self.history.push(res);
        if self.history.len() > 16 {
            self.history.remove(0);
        }

        if self.params.mode != "tacc" {
            self.last_note = "static mode".to_string();
            return self.last_note.clone();
        }

        if self.cooldown > 0 {
            self.cooldown -= 1;
            self.last_note = format!("cooldown active ({} left)", self.cooldown);
            return self.last_note.clone();
        }

        if self.history.len() < 4 {
            self.last_note = "warming up history".to_string();
            return self.last_note.clone();
        }

        let timeouts = self.history.iter().filter(|h| h.is_timeout).count();
        let timeout_rate = timeouts as f64 / self.history.len() as f64;

        let mut lats: Vec<u64> = self.history.iter().map(|h| h.latency_ms).collect();
        lats.sort_unstable();
        let p99 = *lats.last().unwrap_or(&self.params.delta_ms);

        // Emergency Rollback & Safe Mode
        if timeout_rate >= 0.50 {
            self.params.safe_mode = true;
            self.params.rollbacks += 1;
            self.params.k = (self.safe_checkpoint.k + 8).min(self.params.k_max);
            self.params.delta_ms = ((p99 as f64 * 2.5) as u64)
                .clamp(self.params.min_delta_ms, self.params.max_delta_ms);
            self.params.b = (self.params.b / 2).max(self.params.min_b);
            self.cooldown = 6;
            self.last_note = format!(
                "EMERGENCY ROLLBACK: timeout_rate={:.2}! SafeMode triggered",
                timeout_rate
            );
            return self.last_note.clone();
        }

        let mut new_p = self.params.clone();
        let note;

        // Dynamic Delta Tuning: Delta = gamma * P99
        let gamma = if timeout_rate > 0.10 { 2.0 } else { 1.6 };
        let target_delta = ((p99 as f64 * gamma) as u64)
            .clamp(self.params.min_delta_ms, self.params.max_delta_ms);

        // Hysteresis on Delta: ignore jitter < 8%
        let delta_diff = (target_delta as f64 - self.params.delta_ms as f64).abs();
        if delta_diff / (self.params.delta_ms as f64) > 0.08 {
            new_p.delta_ms = target_delta;
        }

        // Dynamic Committee Size k Tuning
        if timeout_rate >= 0.25 {
            new_p.k = (self.params.k + 4).min(self.params.k_max);
            new_p.b = (self.params.b.saturating_sub(25)).max(self.params.min_b);
            note = format!(
                "congestion mitigation: timeout_rate={:.2}, expand k={}, shrink B={}",
                timeout_rate, new_p.k, new_p.b
            );
        } else if timeout_rate > 0.05 {
            new_p.k = (self.params.k + 1).min(self.params.k_max);
            note = format!("slight jitter: expanding k={}", new_p.k);
        } else if (p99 as f64) < (self.params.delta_ms as f64) * 0.45 {
            if self.params.k > self.params.k_min {
                new_p.k = (self.params.k - 1).max(self.params.k_min);
            }
            if new_p.b < self.params.max_b {
                new_p.b = (self.params.b + 20).min(self.params.max_b);
            }
            note = format!(
                "stable low-latency: shrinking k toward k_min={}, expand B={}",
                self.params.k_min, new_p.b
            );
        } else {
            note = format!("equilibrium: p99={}ms", p99);
        }

        // STRICT ENFORCEMENT OF SECURITY BOUND
        if new_p.k < self.params.k_min {
            new_p.k = self.params.k_min;
        }
        if new_p.k > self.params.k_max {
            new_p.k = self.params.k_max;
        }

        if self.params.safe_mode && timeout_rate == 0.0 {
            new_p.safe_mode = false;
        }

        let changed = new_p.k != self.params.k
            || new_p.delta_ms != self.params.delta_ms
            || new_p.b != self.params.b;
        if changed {
            self.safe_checkpoint = self.params.clone();
            self.params = new_p;
            self.cooldown = 4;
        }

        self.last_note = note;
        self.last_note.clone()
    }
}
