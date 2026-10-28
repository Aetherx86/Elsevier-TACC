package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

func GenerateKeyPair() (*ecdsa.PrivateKey, string) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	xb := priv.PublicKey.X.Bytes()
	yb := priv.PublicKey.Y.Bytes()
	xpad := make([]byte, 32-len(xb))
	ypad := make([]byte, 32-len(yb))
	pubBytes := append(append(xpad, xb...), append(ypad, yb...)...)
	addr := hex.EncodeToString(pubBytes)
	return priv, addr
}

func TxSigningMessage(sender, receiver string, amount uint64, data string, gasPrice uint64) string {
	return fmt.Sprintf("%s|%s|%d|%s|%d", sender, receiver, amount, data, gasPrice)
}

func SignTransaction(priv *ecdsa.PrivateKey, msg string) (string, error) {
	h := sha256.Sum256([]byte(msg))
	r, s, err := ecdsa.Sign(rand.Reader, priv, h[:])
	if err != nil {
		return "", err
	}
	rb := r.Bytes()
	sb := s.Bytes()
	rpad := make([]byte, 32-len(rb))
	spad := make([]byte, 32-len(sb))
	sig := append(append(rpad, rb...), append(spad, sb...)...)
	return hex.EncodeToString(sig), nil
}

func VerifySignature(address, msg, sigHex string) bool {
	pubBytes, err := hex.DecodeString(address)
	if err != nil || len(pubBytes) != 64 {
		return false
	}
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil || len(sigBytes) != 64 {
		return false
	}

	x := new(big.Int).SetBytes(pubBytes[:32])
	y := new(big.Int).SetBytes(pubBytes[32:])
	pub := ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}

	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	h := sha256.Sum256([]byte(msg))
	return ecdsa.Verify(&pub, h[:], r, s)
}
