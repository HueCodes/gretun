package disco

import "crypto/sha256"

// sha256Sum returns a SHA-256 digest of b. Extracted so the client signing
// path doesn't pull crypto/sha256 into the main client.go imports explicitly.
func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
