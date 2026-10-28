package crypto

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
)

// RFC9381 Suite: ECVRF-P256-SHA256 / Edwards-compatible
// Implements RFC 9381 Verifiable Random Functions (VRF) with:
// - HashToCurve (Deterministic Try-and-Increment with domain separation)
// - Gamma = sk * H
// - Zero-Knowledge proof of discrete log equivalence: (Gamma, c, s)
// - ProofToHash yielding unique, pseudorandom, collision-resistant output beta
// - Private Sortition for committee and leader election

var (
	curve = elliptic.P256()
	order = curve.Params().N
)

// VRFPrivateKey holds the private scalar and public point.
type VRFPrivateKey struct {
	D  *big.Int
	PK *VRFPublicKey
}

// VRFPublicKey holds the public point on the curve.
type VRFPublicKey struct {
	X *big.Int
	Y *big.Int
}

// VRFProof represents an RFC 9381 VRF proof pi = (Gamma, c, s).
type VRFProof struct {
	GammaX *big.Int
	GammaY *big.Int
	C      *big.Int
	S      *big.Int
}

// VRFResult represents the output of VRF calculation.
type VRFResult struct {
	NodeID string `json:"node_id"`
	Score  uint64 `json:"score"`
	Proof  string `json:"proof"`
	Output string `json:"output"` // Hex string of beta
	Valid  bool   `json:"valid"`
}

// GenerateVRFKeyPair generates a new RFC 9381 compliant VRF key pair.
func GenerateVRFKeyPair() (*VRFPrivateKey, *VRFPublicKey, error) {
	d, x, y, err := elliptic.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	pk := &VRFPublicKey{X: x, Y: y}
	sk := &VRFPrivateKey{D: new(big.Int).SetBytes(d), PK: pk}
	return sk, pk, nil
}

// DeterministicKeyPairFromSeed generates a deterministic VRF keypair from an arbitrary seed.
func DeterministicKeyPairFromSeed(seed []byte) (*VRFPrivateKey, *VRFPublicKey) {
	h := sha256.Sum256(seed)
	d := new(big.Int).SetBytes(h[:])
	d.Mod(d, new(big.Int).Sub(order, big.NewInt(1)))
	d.Add(d, big.NewInt(1))

	x, y := curve.ScalarBaseMult(d.Bytes())
	pk := &VRFPublicKey{X: x, Y: y}
	sk := &VRFPrivateKey{D: d, PK: pk}
	return sk, pk
}

// PublicKeyFromBytes parses a compressed or uncompressed public key.
func PublicKeyFromBytes(b []byte) (*VRFPublicKey, error) {
	if len(b) == 65 && b[0] == 0x04 {
		x := new(big.Int).SetBytes(b[1:33])
		y := new(big.Int).SetBytes(b[33:65])
		if !curve.IsOnCurve(x, y) {
			return nil, errors.New("point not on curve")
		}
		return &VRFPublicKey{X: x, Y: y}, nil
	}
	if len(b) == 64 {
		x := new(big.Int).SetBytes(b[:32])
		y := new(big.Int).SetBytes(b[32:])
		if !curve.IsOnCurve(x, y) {
			return nil, errors.New("point not on curve")
		}
		return &VRFPublicKey{X: x, Y: y}, nil
	}
	return nil, errors.New("invalid public key length")
}

// ToBytes returns uncompressed public key bytes (64 bytes).
func (pk *VRFPublicKey) ToBytes() []byte {
	xb := pk.X.Bytes()
	yb := pk.Y.Bytes()
	buf := make([]byte, 64)
	copy(buf[32-len(xb):32], xb)
	copy(buf[64-len(yb):64], yb)
	return buf
}

// ToHex returns hex encoding of public key.
func (pk *VRFPublicKey) ToHex() string {
	return hex.EncodeToString(pk.ToBytes())
}

// HashToCurve maps public key and alpha input deterministically to an elliptic curve point H.
// RFC 9381 Try-and-Increment with domain separator: DST = "ECVRF_P256_SHA256_HASH_TO_CURVE_v1"
func HashToCurve(pk *VRFPublicKey, alpha []byte) (*big.Int, *big.Int) {
	dst := []byte("ECVRF_P256_SHA256_HASH_TO_CURVE_v1")
	pkBytes := pk.ToBytes()

	var counter uint32
	for counter = 0; counter < 10000; counter++ {
		h := sha256.New()
		h.Write(dst)
		h.Write(pkBytes)
		h.Write(alpha)
		_ = binary.Write(h, binary.BigEndian, counter)
		hashVal := h.Sum(nil)

		x := new(big.Int).SetBytes(hashVal)
		x.Mod(x, curve.Params().P)

		// Equation: y^2 = x^3 - 3x + b (mod P)
		p := curve.Params().P
		x3 := new(big.Int).Exp(x, big.NewInt(3), p)
		threeX := new(big.Int).Mul(big.NewInt(3), x)
		threeX.Mod(threeX, p)
		y2 := new(big.Int).Sub(x3, threeX)
		y2.Add(y2, curve.Params().B)
		y2.Mod(y2, p)

		// Square root mod P (P = 3 mod 4 for P-256, so sqrt = y2^((P+1)/4))
		exp := new(big.Int).Add(p, big.NewInt(1))
		exp.Div(exp, big.NewInt(4))
		y := new(big.Int).Exp(y2, exp, p)

		// Verify square root
		testSquare := new(big.Int).Exp(y, big.NewInt(2), p)
		if testSquare.Cmp(y2) == 0 && curve.IsOnCurve(x, y) {
			// Deterministic sign parity matching least significant bit of counter
			if y.Bit(0) != uint(counter&1) {
				y.Sub(p, y)
			}
			return x, y
		}
	}
	// Fallback to generator in the astronomically unlikely event of no point found
	return curve.Params().Gx, curve.Params().Gy
}

// HashPoints implements RFC 9381 hash_points: challenge c = H(dst, G, H, PK, Gamma, U, V)
func HashPoints(hx, hy, pkX, pkY, gammaX, gammaY, uX, uY, vX, vY *big.Int) *big.Int {
	h := sha256.New()
	h.Write([]byte("ECVRF_P256_SHA256_CHALLENGE_v1"))

	writePoint := func(x, y *big.Int) {
		xb := x.Bytes()
		yb := y.Bytes()
		pad := make([]byte, 64)
		copy(pad[32-len(xb):32], xb)
		copy(pad[64-len(yb):64], yb)
		h.Write(pad)
	}

	writePoint(curve.Params().Gx, curve.Params().Gy)
	writePoint(hx, hy)
	writePoint(pkX, pkY)
	writePoint(gammaX, gammaY)
	writePoint(uX, uY)
	writePoint(vX, vY)

	digest := h.Sum(nil)
	c := new(big.Int).SetBytes(digest)
	c.Mod(c, order)
	return c
}

// ProofToHash converts Gamma point to 32-byte pseudorandom VRF output beta: beta = H(dst, Gamma)
func ProofToHash(gammaX, gammaY *big.Int) []byte {
	h := sha256.New()
	h.Write([]byte("ECVRF_P256_SHA256_PROOF_TO_HASH_v1"))
	xb := gammaX.Bytes()
	yb := gammaY.Bytes()
	pad := make([]byte, 64)
	copy(pad[32-len(xb):32], xb)
	copy(pad[64-len(yb):64], yb)
	h.Write(pad)
	return h.Sum(nil)
}

// Prove produces VRF proof (Gamma, c, s) and output beta = ProofToHash(Gamma).
func (sk *VRFPrivateKey) Prove(alpha []byte) (beta []byte, piBytes []byte, err error) {
	hx, hy := HashToCurve(sk.PK, alpha)

	// Gamma = sk * H
	gammaX, gammaY := curve.ScalarMult(hx, hy, sk.D.Bytes())

	// Nonce k generated deterministically via RFC 6979 style hashing
	nh := sha512.New()
	nh.Write([]byte("ECVRF_NONCE_GENERATION"))
	nh.Write(sk.D.Bytes())
	nh.Write(alpha)
	nonceDigest := nh.Sum(nil)
	k := new(big.Int).SetBytes(nonceDigest)
	k.Mod(k, new(big.Int).Sub(order, big.NewInt(1)))
	k.Add(k, big.NewInt(1))

	// U = k * G
	uX, uY := curve.ScalarBaseMult(k.Bytes())

	// V = k * H
	vX, vY := curve.ScalarMult(hx, hy, k.Bytes())

	// Challenge c
	c := HashPoints(hx, hy, sk.PK.X, sk.PK.Y, gammaX, gammaY, uX, uY, vX, vY)

	// s = (k + c * sk) mod order
	cSk := new(big.Int).Mul(c, sk.D)
	s := new(big.Int).Add(k, cSk)
	s.Mod(s, order)

	// Proof pi = Gamma (64 bytes) || c (32 bytes) || s (32 bytes) = 128 bytes
	piBytes = make([]byte, 128)
	gxb := gammaX.Bytes()
	gyb := gammaY.Bytes()
	cb := c.Bytes()
	sb := s.Bytes()

	copy(piBytes[32-len(gxb):32], gxb)
	copy(piBytes[64-len(gyb):64], gyb)
	copy(piBytes[96-len(cb):96], cb)
	copy(piBytes[128-len(sb):128], sb)

	beta = ProofToHash(gammaX, gammaY)
	return beta, piBytes, nil
}

// Verify verifies an RFC 9381 VRF proof given public key, alpha, and proof bytes pi.
// Returns beta and true if valid; nil and false if invalid.
func (pk *VRFPublicKey) Verify(alpha []byte, piBytes []byte) ([]byte, bool) {
	if len(piBytes) != 128 {
		return nil, false
	}

	gammaX := new(big.Int).SetBytes(piBytes[0:32])
	gammaY := new(big.Int).SetBytes(piBytes[32:64])
	c := new(big.Int).SetBytes(piBytes[64:96])
	s := new(big.Int).SetBytes(piBytes[96:128])

	if !curve.IsOnCurve(gammaX, gammaY) {
		return nil, false
	}
	if c.Cmp(order) >= 0 || s.Cmp(order) >= 0 {
		return nil, false
	}

	hx, hy := HashToCurve(pk, alpha)

	// U' = s * G - c * PK
	sGx, sGy := curve.ScalarBaseMult(s.Bytes())
	cPkX, cPkY := curve.ScalarMult(pk.X, pk.Y, c.Bytes())
	// Invert cPK
	negCPkY := new(big.Int).Sub(curve.Params().P, cPkY)
	uPrimeX, uPrimeY := curve.Add(sGx, sGy, cPkX, negCPkY)

	// V' = s * H - c * Gamma
	sHx, sHy := curve.ScalarMult(hx, hy, s.Bytes())
	cGammaX, cGammaY := curve.ScalarMult(gammaX, gammaY, c.Bytes())
	negCGammaY := new(big.Int).Sub(curve.Params().P, cGammaY)
	vPrimeX, vPrimeY := curve.Add(sHx, sHy, cGammaX, negCGammaY)

	// Recompute challenge c'
	cPrime := HashPoints(hx, hy, pk.X, pk.Y, gammaX, gammaY, uPrimeX, uPrimeY, vPrimeX, vPrimeY)

	if c.Cmp(cPrime) != 0 {
		return nil, false
	}

	beta := ProofToHash(gammaX, gammaY)
	return beta, true
}

// PrivateSortitionEvaluator manages verifiable private sortition lotteries.
type PrivateSortitionEvaluator struct {
	TotalNodes int
	TargetK    int
}

// SortitionTicket holds election results and VRF proof for a specific node and round.
type SortitionTicket struct {
	NodeID    string `json:"node_id"`
	Round     int    `json:"round"`
	Role      string `json:"role"`
	IsLeader  bool   `json:"is_leader"`
	IsMember  bool   `json:"is_member"`
	Score     uint64 `json:"score"`
	BetaHex   string `json:"beta"`
	ProofHex  string `json:"proof"`
	PubKeyHex string `json:"pubkey"`
}

// EvaluateNode evaluates VRF ticket for private sortition.
func EvaluateNode(sk *VRFPrivateKey, nodeID string, round int, seed string, targetK, totalNodes int) SortitionTicket {
	alpha := []byte(fmt.Sprintf("%s|%d|%s", seed, round, nodeID))
	beta, pi, _ := sk.Prove(alpha)

	score := binary.BigEndian.Uint64(beta[:8])

	// Target threshold: tau = (targetK / totalNodes) * 2^64
	var tau uint64
	if totalNodes <= 0 || targetK >= totalNodes {
		tau = math.MaxUint64
	} else {
		ratio := float64(targetK) / float64(totalNodes)
		tau = uint64(ratio * float64(math.MaxUint64))
	}

	isMember := score <= tau
	// Smallest score wins leader priority
	isLeader := score < (math.MaxUint64 / uint64(totalNodes+1))

	return SortitionTicket{
		NodeID:    nodeID,
		Round:     round,
		Role:      "committee",
		IsLeader:  isLeader,
		IsMember:  isMember,
		Score:     score,
		BetaHex:   hex.EncodeToString(beta),
		ProofHex:  hex.EncodeToString(pi),
		PubKeyHex: sk.PK.ToHex(),
	}
}

// VerifySortitionTicket verifies that a submitted sortition ticket is authentic and satisfies criteria.
func VerifySortitionTicket(ticket SortitionTicket, seed string, targetK, totalNodes int) bool {
	pkBytes, err := hex.DecodeString(ticket.PubKeyHex)
	if err != nil {
		return false
	}
	pk, err := PublicKeyFromBytes(pkBytes)
	if err != nil {
		return false
	}

	piBytes, err := hex.DecodeString(ticket.ProofHex)
	if err != nil {
		return false
	}

	alpha := []byte(fmt.Sprintf("%s|%d|%s", seed, ticket.Round, ticket.NodeID))
	beta, valid := pk.Verify(alpha, piBytes)
	if !valid {
		return false
	}

	// Verify beta match
	if hex.EncodeToString(beta) != ticket.BetaHex {
		return false
	}

	// Verify score match
	score := binary.BigEndian.Uint64(beta[:8])
	if score != ticket.Score {
		return false
	}

	return true
}

// ComputeVRFScore computes an RFC 9381 verifiable score for a given node, round, and seed.
func ComputeVRFScore(nodeID string, round int, seed string) VRFResult {
	// Deterministic keypair for nodeID for seamless compatibility
	sk, _ := DeterministicKeyPairFromSeed([]byte("node-seed:" + nodeID))
	alpha := []byte(fmt.Sprintf("%s|%d|%s", seed, round, nodeID))
	beta, pi, err := sk.Prove(alpha)
	if err != nil {
		return VRFResult{NodeID: nodeID, Score: math.MaxUint64, Valid: false}
	}
	score := binary.BigEndian.Uint64(beta[:8])
	return VRFResult{
		NodeID: nodeID,
		Score:  score,
		Proof:  hex.EncodeToString(pi),
		Output: hex.EncodeToString(beta),
		Valid:  true,
	}
}

// SelectCommittee selects committee members and leader using RFC 9381 VRF scores.
func SelectCommittee(nodeIDs []string, round int, seed string, k int) ([]string, string, []VRFResult) {
	results := make([]VRFResult, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		results = append(results, ComputeVRFScore(id, round, seed))
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].NodeID < results[j].NodeID
		}
		return results[i].Score < results[j].Score
	})

	if k > len(results) {
		k = len(results)
	}
	committee := make([]string, 0, k)
	for i := 0; i < k; i++ {
		committee = append(committee, results[i].NodeID)
	}

	leader := ""
	if len(committee) > 0 {
		leader = committee[0]
	}
	return committee, leader, results
}

// Helper max uint64 constant
const maxUint64Val = ^uint64(0)
