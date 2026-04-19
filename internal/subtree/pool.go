// Package subtree manages a fixed pool of 32-byte subtree identifiers for
// load generation. Derivation is deterministic from a user-supplied seed
// so test scenarios are reproducible across runs and machines.
package subtree

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// ID is a 32-byte subtree identifier (matches frame.Frame.SubtreeID).
type ID = [32]byte

// Pool is a read-only collection of subtree IDs. Safe for concurrent use.
type Pool struct {
	ids []ID
}

// New builds a Pool of n deterministic IDs derived from seed.
// If n == 0 the pool is empty and [Pick] returns the zero ID (meaning
// "no subtree assigned").
//
// Derivation: id[i] = SHA256(seed || uint64_be(i)).
func New(n int, seed []byte) *Pool {
	if n <= 0 {
		return &Pool{}
	}
	p := &Pool{ids: make([]ID, n)}
	var idxBuf [8]byte
	for i := 0; i < n; i++ {
		h := sha256.New()
		_, _ = h.Write(seed)
		binary.BigEndian.PutUint64(idxBuf[:], uint64(i))
		_, _ = h.Write(idxBuf[:])
		h.Sum(p.ids[i][:0])
	}
	return p
}

// Len returns the number of IDs in the pool.
func (p *Pool) Len() int { return len(p.ids) }

// At returns the i-th ID. Panics if i is out of range.
func (p *Pool) At(i int) ID { return p.ids[i] }

// Pick returns the i-th ID modulo len(pool). If the pool is empty, returns
// the zero ID.
func (p *Pool) Pick(i uint64) ID {
	if len(p.ids) == 0 {
		return ID{}
	}
	return p.ids[int(i%uint64(len(p.ids)))]
}

// HexAt returns the i-th ID as lowercase hex.
func (p *Pool) HexAt(i int) string {
	return hex.EncodeToString(p.ids[i][:])
}

// String returns a short summary for logging.
func (p *Pool) String() string {
	if len(p.ids) == 0 {
		return "subtree.Pool{empty}"
	}
	return fmt.Sprintf("subtree.Pool{n=%d first=%s…}", len(p.ids), p.HexAt(0)[:16])
}
