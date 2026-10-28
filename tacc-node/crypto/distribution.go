package crypto

import (
	"math"
)

// DistributionModel defines the statistical model used for security bounds.
type DistributionModel string

const (
	ModelBinomial       DistributionModel = "binomial"
	ModelHypergeometric DistributionModel = "hypergeometric"
)

// LogFactorial returns ln(n!) using math.Lgamma.
func LogFactorial(n int) float64 {
	if n <= 1 {
		return 0
	}
	lg, _ := math.Lgamma(float64(n + 1))
	return lg
}

// LogCombination returns ln(n choose k).
func LogCombination(n, k int) float64 {
	if k < 0 || k > n {
		return math.Inf(-1)
	}
	if k == 0 || k == n {
		return 0
	}
	return LogFactorial(n) - LogFactorial(k) - LogFactorial(n-k)
}

// BinomialPMF computes P(X = m) for X ~ Binomial(k, rho) in log space to avoid overflow.
func BinomialPMF(k, m int, rho float64) float64 {
	if m < 0 || m > k || rho <= 0 || rho >= 1 {
		if rho == 0 && m == 0 {
			return 1.0
		}
		if rho == 1 && m == k {
			return 1.0
		}
		return 0.0
	}
	logComb := LogCombination(k, m)
	logProb := logComb + float64(m)*math.Log(rho) + float64(k-m)*math.Log(1.0-rho)
	return math.Exp(logProb)
}

// BinomialBadProbability calculates P(Byzantine >= ceil(k/3)) under Binomial(k, rho).
func BinomialBadProbability(k int, rho float64) float64 {
	if k <= 0 || rho <= 0 {
		return 0.0
	}
	if rho >= 1.0/3.0 {
		// When rho >= 1/3, as k grows law of large numbers concentrates on >= 1/3
		if rho >= 1.0 {
			return 1.0
		}
	}

	minBad := int(math.Ceil(float64(k) / 3.0))
	sum := 0.0
	for m := minBad; m <= k; m++ {
		sum += BinomialPMF(k, m, rho)
	}

	if sum > 1.0 {
		return 1.0
	}
	return sum
}

// HypergeometricPMF computes P(X = m) for without-replacement sampling from total nodes N with M Byzantine nodes.
// P(X = m) = (M choose m) * ((N - M) choose (k - m)) / (N choose k)
func HypergeometricPMF(N, M, k, m int) float64 {
	if m < 0 || m > k || m > M || (k-m) > (N-M) || k > N {
		return 0.0
	}
	logNumerator := LogCombination(M, m) + LogCombination(N-M, k-m)
	logDenominator := LogCombination(N, k)
	logProb := logNumerator - logDenominator
	return math.Exp(logProb)
}

// HypergeometricBadProbability computes P(Byzantine >= ceil(k/3)) under Hypergeometric(N, M=floor(rho*N), k).
func HypergeometricBadProbability(N int, k int, rho float64) float64 {
	if k <= 0 || rho <= 0 || N <= 0 {
		return 0.0
	}
	M := int(math.Floor(rho * float64(N)))
	if M <= 0 {
		return 0.0
	}

	minBad := int(math.Ceil(float64(k) / 3.0))
	sum := 0.0
	for m := minBad; m <= k && m <= M; m++ {
		sum += HypergeometricPMF(N, M, k, m)
	}

	if sum > 1.0 {
		return 1.0
	}
	return sum
}

// SecurityBoundEvaluator computes the security failure probability and minimum committee size k_min.
type SecurityBoundEvaluator struct {
	TotalNodes int
	Rho        float64
	Epsilon    float64
	Model      DistributionModel
}

// NewSecurityBoundEvaluator initializes an evaluator. If totalNodes is large and k > 0.1 N, it prefers Hypergeometric.
func NewSecurityBoundEvaluator(totalNodes int, rho, epsilon float64) *SecurityBoundEvaluator {
	model := ModelBinomial
	if totalNodes > 0 {
		model = ModelHypergeometric
	}
	return &SecurityBoundEvaluator{
		TotalNodes: totalNodes,
		Rho:        rho,
		Epsilon:    epsilon,
		Model:      model,
	}
}

// CalculatePBad computes the security failure probability for a given committee size k.
func (e *SecurityBoundEvaluator) CalculatePBad(k int) float64 {
	if e.Model == ModelHypergeometric && e.TotalNodes > 0 && float64(k) > 0.1*float64(e.TotalNodes) {
		return HypergeometricBadProbability(e.TotalNodes, k, e.Rho)
	}
	return BinomialBadProbability(k, e.Rho)
}

// FindKMin finds the smallest k in [minK, maxK] such that P(bad) <= epsilon.
func (e *SecurityBoundEvaluator) FindKMin(minK, maxK int) (int, float64) {
	for k := minK; k <= maxK; k++ {
		p := e.CalculatePBad(k)
		if p <= e.Epsilon {
			return k, p
		}
	}
	return maxK, e.CalculatePBad(maxK)
}
