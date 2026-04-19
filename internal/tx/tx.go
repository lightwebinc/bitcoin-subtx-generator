// Package tx builds random BSV-shaped transaction payloads for load
// generation. Output layout matches the BSV P2P "tx" message body:
// version (4 LE) | vin_count (varint) | vin[] | vout_count (varint) | vout[] | locktime (4 LE).
//
// This is not consensus-valid — scripts are random bytes. It is shape-correct
// so any downstream parser that walks the structure sees plausible data.
package tx

import (
	"encoding/binary"
	"math/rand/v2"
)

// Builder generates transaction payloads of a target byte length using a
// per-instance PRNG. Not safe for concurrent use; give each worker its own
// Builder.
type Builder struct {
	rng *rand.ChaCha8
}

// New creates a Builder seeded from the provided 32-byte seed.
func New(seed [32]byte) *Builder {
	return &Builder{rng: rand.NewChaCha8(seed)}
}

// Build writes a random transaction into dst[:targetSize] and returns the
// slice. dst must have cap >= targetSize. A minimum of 10 bytes is enforced
// (version + 1-byte vin count + 1-byte vout count + locktime).
func (b *Builder) Build(dst []byte, targetSize int) []byte {
	const minSize = 10
	if targetSize < minSize {
		targetSize = minSize
	}
	if cap(dst) < targetSize {
		dst = make([]byte, targetSize)
	}
	dst = dst[:targetSize]

	// version
	binary.LittleEndian.PutUint32(dst[0:4], 2)

	// Reserve 4 trailing bytes for locktime.
	body := dst[4 : targetSize-4]

	// Split remaining bytes: 1-byte vin_count, ~50% inputs, 1-byte vout_count, rest outputs.
	// Each input: 32-byte prev_hash + 4-byte prev_index + 1-byte script_len + script + 4-byte sequence (41+script).
	// Each output: 8-byte value + 1-byte script_len + script (9+script).
	if len(body) < 2 {
		// Degenerate: empty tx shell.
		body[0] = 0
		body[len(body)-1] = 0
		binary.LittleEndian.PutUint32(dst[targetSize-4:], 0)
		return dst
	}

	// Fill body with random bytes deterministically from our PRNG.
	// Use Uint64 chunks for speed.
	fillRand(b.rng, body)

	// Override count bytes so they parse as "one input, one output".
	body[0] = 1 // vin_count = 1
	// Position of vout_count: choose roughly halfway, but ensure the "input"
	// block before it has the fixed 41-byte minimum (0 script). We collapse
	// to a single 41-byte input followed by one output filling the remainder.
	if len(body) >= 42 {
		// Input: body[1:1+41] — prev_hash(32) + prev_index(4) + script_len=0 (1) + sequence(4)
		body[1+32+4] = 0 // script_len = 0
		// vout_count at body[1+41] = body[42]
		body[42] = 1
		// Output starts at body[43]: value(8) + script_len + script
		if len(body) >= 43+9 {
			// script_len fits the remaining bytes (sans locktime already reserved).
			scriptBytes := len(body) - 43 - 9
			if scriptBytes < 0 {
				scriptBytes = 0
			}
			if scriptBytes > 252 {
				scriptBytes = 252 // keep it a 1-byte varint for simplicity
			}
			body[43+8] = byte(scriptBytes)
		}
	}

	// locktime = 0 (random would also be fine).
	binary.LittleEndian.PutUint32(dst[targetSize-4:], 0)
	return dst
}

// fillRand writes deterministic pseudo-random bytes using the ChaCha8 stream.
func fillRand(r *rand.ChaCha8, buf []byte) {
	for len(buf) >= 8 {
		binary.LittleEndian.PutUint64(buf, r.Uint64())
		buf = buf[8:]
	}
	if len(buf) > 0 {
		var tmp [8]byte
		binary.LittleEndian.PutUint64(tmp[:], r.Uint64())
		copy(buf, tmp[:])
	}
}
