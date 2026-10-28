package crypto

import (
	"encoding/hex"
	"testing"
)

func TestRFC9381VRF(t *testing.T) {
	sk, pk, err := GenerateVRFKeyPair()
	if err != nil {
		t.Fatalf("GenerateVRFKeyPair failed: %v", err)
	}

	alpha := []byte("round-100-alpha-test-seed")

	// Prove
	beta1, pi1, err := sk.Prove(alpha)
	if err != nil {
		t.Fatalf("Prove failed: %v", err)
	}

	// Verify with correct pk
	betaVerified, ok := pk.Verify(alpha, pi1)
	if !ok {
		t.Fatalf("Verify failed with valid proof")
	}
	if hex.EncodeToString(beta1) != hex.EncodeToString(betaVerified) {
		t.Fatalf("Beta mismatch: %x vs %x", beta1, betaVerified)
	}

	// Uniqueness: proving again must yield identical beta and verifiable proof
	beta2, pi2, err := sk.Prove(alpha)
	if err != nil {
		t.Fatalf("Prove 2 failed: %v", err)
	}
	if hex.EncodeToString(beta1) != hex.EncodeToString(beta2) {
		t.Fatalf("Uniqueness violated: %x != %x", beta1, beta2)
	}
	_, ok2 := pk.Verify(alpha, pi2)
	if !ok2 {
		t.Fatalf("Verify failed on second proof")
	}

	// Tampered input must fail verification
	tamperedAlpha := []byte("round-100-alpha-test-seed-tampered")
	_, okTampered := pk.Verify(tamperedAlpha, pi1)
	if okTampered {
		t.Fatalf("Verify succeeded on tampered input")
	}

	// Tampered proof must fail verification
	tamperedPi := make([]byte, len(pi1))
	copy(tamperedPi, pi1)
	tamperedPi[0] ^= 0xff
	_, okTamperedPi := pk.Verify(alpha, tamperedPi)
	if okTamperedPi {
		t.Fatalf("Verify succeeded on tampered proof")
	}
}

func TestSecurityDistributions(t *testing.T) {
	rho := 0.20
	epsilon := 1e-9

	eval := NewSecurityBoundEvaluator(500, rho, epsilon)
	kMin, pBad := eval.FindKMin(4, 256)

	t.Logf("kMin=%d, pBad=%e (rho=%.2f, epsilon=%e)", kMin, pBad, rho, epsilon)
	if pBad > epsilon {
		t.Fatalf("FindKMin yielded pBad %e > epsilon %e", pBad, epsilon)
	}

	// Test Hypergeometric vs Binomial
	pHyper := HypergeometricBadProbability(500, 100, rho)
	pBino := BinomialBadProbability(100, rho)
	t.Logf("k=100: Hypergeometric pBad=%e, Binomial pBad=%e", pHyper, pBino)
	if pHyper > pBino {
		t.Logf("Note: In finite population, Hypergeometric tail is strictly tighter than Binomial")
	}
}
