package mt5

// SRP-6a implementation for MetaTrader 5.
// Uses RFC 3526 Group 14 (2048-bit prime), SHA-256 hash.
// Reference: RFC 2945 and RFC 5054, SRP-6a variant.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
)

// RFC 3526 Group 14 — 2048-bit safe prime (exactly 256 bytes / 512 hex chars).
const group14Hex = "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1" +
	"29024E088A67CC74020BBEA63B139B22514A08798E3404DD" +
	"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245" +
	"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
	"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D" +
	"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F" +
	"83655D23DCA3AD961C62F356208552BB9ED5290770966D67" +
	"0C354E4ABC9804F1746C08CA18217C32905E462E36CE3BE3" +
	"9E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9DE" +
	"2BCBF6955817183995497CEA956AE515D2261898FA051015" +
	"728E5A8AACAA68FFFFFFFFFFFFFFFF"

var (
	srpN *big.Int // 2048-bit prime
	srpG *big.Int // generator = 2
	srpK *big.Int // k = H(N, g)
)

func init() {
	b, err := hex.DecodeString(group14Hex)
	if err != nil {
		panic("srp6a: invalid prime hex: " + err.Error())
	}
	srpN = new(big.Int).SetBytes(b)
	srpG = big.NewInt(2)

	// k = H(N || g), both padded to 256 bytes
	h := sha256.New()
	h.Write(srpPad(srpN, 256))
	h.Write(srpPad(srpG, 256))
	srpK = new(big.Int).SetBytes(h.Sum(nil))
}

// srpPad returns x as a big-endian byte slice padded to n bytes.
func srpPad(x *big.Int, n int) []byte {
	b := x.Bytes()
	if len(b) >= n {
		return b[len(b)-n:]
	}
	out := make([]byte, n)
	copy(out[n-len(b):], b)
	return out
}

// SRPClient holds the client-side SRP-6a state.
type SRPClient struct {
	login    string
	password string
	a        *big.Int // private ephemeral
	A        *big.Int // public ephemeral = g^a mod N
}

// NewSRPClient creates a new SRP-6a client and generates the client ephemeral.
func NewSRPClient(login, password string) (*SRPClient, error) {
	// Generate 256-bit random a
	ab := make([]byte, 32)
	if _, err := rand.Read(ab); err != nil {
		return nil, err
	}
	a := new(big.Int).SetBytes(ab)
	A := new(big.Int).Exp(srpG, a, srpN)
	return &SRPClient{
		login:    login,
		password: password,
		a:        a,
		A:        A,
	}, nil
}

// ABytes returns the 256-byte big-endian encoding of the client public value A.
func (s *SRPClient) ABytes() []byte {
	return srpPad(s.A, 256)
}

// ComputeProof computes M1 (client proof) and K (session key) from the server's
// salt and public value B.
// Returns M1 (32 bytes), K (32 bytes), and any error.
func (s *SRPClient) ComputeProof(salt, bBytes []byte) (M1, K []byte, err error) {
	B := new(big.Int).SetBytes(bBytes)

	// Verify B > 0
	if B.Sign() == 0 {
		return nil, nil, errors.New("srp6a: B is zero")
	}

	// u = H(A || B), both padded to 256 bytes
	hU := sha256.New()
	hU.Write(srpPad(s.A, 256))
	hU.Write(srpPad(B, 256))
	u := new(big.Int).SetBytes(hU.Sum(nil))

	// x = H(salt || H(login ":" password))
	hID := sha256.New()
	hID.Write([]byte(s.login + ":" + s.password))
	idHash := hID.Sum(nil)

	hX := sha256.New()
	hX.Write(salt)
	hX.Write(idHash)
	x := new(big.Int).SetBytes(hX.Sum(nil))

	// S = (B - k*g^x)^(a + u*x) mod N
	gx := new(big.Int).Exp(srpG, x, srpN)
	kgx := new(big.Int).Mul(srpK, gx)
	kgx.Mod(kgx, srpN)

	base := new(big.Int).Sub(B, kgx)
	base.Mod(base, srpN)
	if base.Sign() < 0 {
		base.Add(base, srpN)
	}

	ux := new(big.Int).Mul(u, x)
	exp := new(big.Int).Add(s.a, ux)

	S := new(big.Int).Exp(base, exp, srpN)

	// K = H(S)
	hK := sha256.New()
	hK.Write(srpPad(S, 256))
	K = hK.Sum(nil)

	// M1 = H( H(N) XOR H(g)  ||  H(login)  ||  salt  ||  A  ||  B  ||  K )
	hN := sha256.Sum256(srpPad(srpN, 256))
	hG := sha256.Sum256(srpPad(srpG, 256))
	xorNG := make([]byte, 32)
	for i := range xorNG {
		xorNG[i] = hN[i] ^ hG[i]
	}

	hLogin := sha256.Sum256([]byte(s.login))

	hM := sha256.New()
	hM.Write(xorNG)
	hM.Write(hLogin[:])
	hM.Write(salt)
	hM.Write(srpPad(s.A, 256))
	hM.Write(srpPad(B, 256))
	hM.Write(K)
	M1 = hM.Sum(nil)

	return M1, K, nil
}

// VerifyServerProof checks that the server's M2 = H(A || M1 || K).
func (s *SRPClient) VerifyServerProof(M1, K, M2 []byte) bool {
	h := sha256.New()
	h.Write(srpPad(s.A, 256))
	h.Write(M1)
	h.Write(K)
	expected := h.Sum(nil)
	if len(expected) != len(M2) {
		return false
	}
	for i := range expected {
		if expected[i] != M2[i] {
			return false
		}
	}
	return true
}
