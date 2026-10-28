/// Exact Binomial and Hypergeometric probability calculation with log-gamma space
/// to prevent factorial overflow for large N and k.

// Stirling approximation / Lanczos approximation of ln(Gamma(z))
pub fn ln_gamma(x: f64) -> f64 {
    if x <= 0.0 {
        return f64::INFINITY;
    }
    // Lanczos coefficients (g=7, N=9)
    let p = [
        0.99999999999980993,
        676.5203681218851,
        -1259.1392167224028,
        771.32342877765313,
        -176.61502916214059,
        12.507343278686905,
        -0.13857109526572012,
        9.9843695780195716e-6,
        1.5056327351493116e-7,
    ];
    let z = x - 1.0;
    let mut a = p[0];
    let t = z + 7.5;
    for (i, val) in p.iter().enumerate().skip(1) {
        a += val / (z + i as f64);
    }
    0.5 * (2.0 * std::f64::consts::PI).ln() + (z + 0.5) * t.ln() - t + a.ln()
}

pub fn log_factorial(n: usize) -> f64 {
    if n <= 1 {
        0.0
    } else {
        ln_gamma((n + 1) as f64)
    }
}

pub fn log_combination(n: usize, k: usize) -> f64 {
    if k > n {
        f64::NEG_INFINITY
    } else if k == 0 || k == n {
        0.0
    } else {
        log_factorial(n) - log_factorial(k) - log_factorial(n - k)
    }
}

pub fn binomial_pmf(k: usize, m: usize, rho: f64) -> f64 {
    if m > k || rho <= 0.0 || rho >= 1.0 {
        if rho == 0.0 && m == 0 {
            return 1.0;
        }
        if rho == 1.0 && m == k {
            return 1.0;
        }
        return 0.0;
    }
    let log_comb = log_combination(k, m);
    let log_prob = log_comb + (m as f64) * rho.ln() + ((k - m) as f64) * (1.0 - rho).ln();
    log_prob.exp()
}

pub fn binomial_bad_probability(k: usize, rho: f64) -> f64 {
    if k == 0 || rho <= 0.0 {
        return 0.0;
    }
    let min_bad = ((k as f64) / 3.0).ceil() as usize;
    let mut sum = 0.0;
    for m in min_bad..=k {
        sum += binomial_pmf(k, m, rho);
    }
    sum.min(1.0)
}

pub fn hypergeometric_pmf(n: usize, m_total: usize, k: usize, m: usize) -> f64 {
    if m > k || m > m_total || (k - m) > (n - m_total) || k > n {
        return 0.0;
    }
    let log_num = log_combination(m_total, m) + log_combination(n - m_total, k - m);
    let log_den = log_combination(n, k);
    (log_num - log_den).exp()
}

pub fn hypergeometric_bad_probability(n: usize, k: usize, rho: f64) -> f64 {
    if k == 0 || rho <= 0.0 || n == 0 {
        return 0.0;
    }
    let m_total = (rho * n as f64).floor() as usize;
    let min_bad = ((k as f64) / 3.0).ceil() as usize;
    let mut sum = 0.0;
    for m in min_bad..=k.min(m_total) {
        sum += hypergeometric_pmf(n, m_total, k, m);
    }
    sum.min(1.0)
}

pub struct SecurityEvaluator {
    pub total_nodes: usize,
    pub rho: f64,
    pub epsilon: f64,
}

impl SecurityEvaluator {
    pub fn new(total_nodes: usize, rho: f64, epsilon: f64) -> Self {
        Self {
            total_nodes,
            rho,
            epsilon,
        }
    }

    pub fn calculate_p_bad(&self, k: usize) -> f64 {
        if self.total_nodes > 0 && (k as f64) > 0.1 * (self.total_nodes as f64) {
            hypergeometric_bad_probability(self.total_nodes, k, self.rho)
        } else {
            binomial_bad_probability(k, self.rho)
        }
    }

    pub fn find_k_min(&self, min_k: usize, max_k: usize) -> (usize, f64) {
        for k in min_k..=max_k {
            let p = self.calculate_p_bad(k);
            if p <= self.epsilon {
                return (k, p);
            }
        }
        (max_k, self.calculate_p_bad(max_k))
    }
}
