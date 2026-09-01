// Package fair implements provably-fair outcome derivation.
//
// # Byte stream rule (the contract verification depends on)
//
// A Stream consumes an HMAC-SHA256 digest as a byte stream. Block 0 is
//
//	HMAC-SHA256(key, base)
//
// and block i (i >= 1) is
//
//	HMAC-SHA256(key, base + ":" + i)
//
// where base and key depend on the constructor:
//
//   - Personal rounds: key = serverSeed (raw bytes), base = clientSeed + ":" + nonce.
//   - Shared rounds:   key = chainSeed (raw bytes), base = salt.
//
// Bytes are consumed big-endian, four at a time, as uint32. Floats are
// uint32 / 2^32, i.e. uniform in [0, 1). When a 32-byte block is exhausted
// the stream extends by emitting block i+1. Every game draws from this one
// Stream, so the rule is shared and verifiable by anyone with the inputs.
package fair

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
)

// SeedSize is the length of generated server and chain seeds.
const SeedSize = 32

// MaxClientSeedLen bounds user-supplied client seeds.
const MaxClientSeedLen = 256

// GenerateSeed returns SeedSize cryptographically random bytes. Never
// math/rand anywhere near a bet.
func GenerateSeed() ([]byte, error) {
	buf := make([]byte, SeedSize)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("crypto/rand: %w", err)
	}
	return buf, nil
}

// HashSeed returns the hex-encoded SHA-256 of a seed. Only the hash of an
// unrevealed seed is published to clients.
func HashSeed(seed []byte) string {
	sum := sha256.Sum256(seed)
	return hex.EncodeToString(sum[:])
}

// Stream derives deterministic random values from seed material. It is pure:
// no clocks, no globals, no math/rand. The same inputs always produce the
// same values, which is what makes RTP tests and client-side verification
// possible.
type Stream struct {
	key      []byte
	base     string
	block    []byte
	consumed int // bytes consumed from the current block
	next     int // index of the next block to emit
}

// NewPersonalStream returns the stream for a single-player bet. The same
// (serverSeed, clientSeed, nonce) triple always yields the same values.
func NewPersonalStream(serverSeed []byte, clientSeed string, nonce int64) *Stream {
	return &Stream{
		key:  serverSeed,
		base: clientSeed + ":" + strconv.FormatInt(nonce, 10),
	}
}

// NewChainStream returns the stream for a shared round. chainSeed comes from
// the commit-reveal hash chain and salt is agreed before betting closes
// (e.g. a concatenation of participants' client seeds), so no single player
// can grind the outcome.
func NewChainStream(chainSeed []byte, salt string) *Stream {
	return &Stream{key: chainSeed, base: salt}
}

// emit produces block s.next and advances the counter.
func (s *Stream) emit() {
	msg := s.base
	if s.next > 0 {
		msg = s.base + ":" + strconv.Itoa(s.next)
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(msg))
	s.block = mac.Sum(nil)
	s.consumed = 0
	s.next++
}

func (s *Stream) ensure(n int) {
	for len(s.block)-s.consumed < n {
		s.emit()
	}
}

// read returns the next n bytes from the stream.
func (s *Stream) read(n int) []byte {
	s.ensure(n)
	out := s.block[s.consumed : s.consumed+n]
	s.consumed += n
	return out
}

// Uint32 returns the next 4 bytes as a big-endian uint32.
func (s *Stream) Uint32() uint32 {
	return binary.BigEndian.Uint32(s.read(4))
}

// Float returns the next uniform float in [0, 1): uint32 / 2^32.
func (s *Stream) Float() float64 {
	return float64(s.Uint32()) / float64(1<<32)
}

// WeightedPick walks a cumulative weight table with the next float and
// returns the selected index. Deterministic and reproducible by anyone with
// the three inputs (or the chain seed and salt, for shared rounds).
func (s *Stream) WeightedPick(weights []int64) int {
	var total int64
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		panic("fair: WeightedPick requires positive total weight")
	}
	target := int64(s.Float() * float64(total))
	var cumulative int64
	for i, w := range weights {
		cumulative += w
		if target < cumulative {
			return i
		}
	}
	return len(weights) - 1
}
