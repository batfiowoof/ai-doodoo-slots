package cards

import "github.com/ai-doodoo-slots/services/backend/internal/fair"

// Shuffle performs an in-place Fisher-Yates shuffle driven entirely by the
// fair stream, using deterministic rejection sampling so the draw is
// uniform (no modulo bias). The same seed material always produces the
// same deck order, which is what makes replay and client-side
// verification possible.
//
// Rejection rule (part of the verification contract): to pick a uniform
// value in [0, n), draw v = stream.Uint32() and accept it when
// v < 2^32 - (2^32 mod n); otherwise draw again. j = v mod n.
func Shuffle(deck []Card, s *fair.Stream) {
	for i := len(deck) - 1; i > 0; i-- {
		j := uniformBelow(s, uint32(i+1))
		deck[i], deck[j] = deck[j], deck[i]
	}
}

// uniformBelow returns a uniform value in [0, n) via rejection sampling.
// n must be > 0 and < 2^32.
func uniformBelow(s *fair.Stream, n uint32) uint32 {
	// Values at or above the largest multiple of n that fits in 32 bits
	// are rejected, cancelling modulo bias. The rejection sequence itself
	// is deterministic given the stream, so replay stays exact.
	const twoPow32 = uint64(1) << 32
	threshold := twoPow32 - (twoPow32 % uint64(n))
	for {
		v := s.Uint32()
		if uint64(v) < threshold {
			return v % n
		}
	}
}
