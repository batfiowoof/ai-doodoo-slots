// Package mines implements the stateful single-player mines game: a 5×5
// field hides M mines; the player reveals safe tiles and cashes out before
// hitting one. Mine positions are drawn once from the personal fair stream
// at start, so a finished round replays exactly from the fairness triple
// plus the reveal log.
package mines

import (
	"fmt"
	"math"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

// GameID is the registry and routes identifier.
const GameID = "mines"

const (
	// Tiles is the field size (5×5).
	Tiles = 25
	// MinBet / MaxBet bound the stake.
	MinBet = 1
	MaxBet = 10000
	// EdgeComplement scales every multiplier: the return is 99% at any
	// cashout point.
	EdgeComplement = 0.99
	// MaxMines keeps at least one safe tile on the field.
	MaxMines = Tiles - 1
)

// Round lifecycle statuses.
const (
	StatusActive = "active"
	StatusCashed = "cashed"
	StatusBusted = "busted"
)

// ValidateMineCount accepts any field with at least one safe tile.
func ValidateMineCount(n int) error {
	if n < 1 || n > MaxMines {
		return fmt.Errorf("mine count must be 1-%d", MaxMines)
	}
	return nil
}

// ValidateBet bounds the stake.
func ValidateBet(credits int64) error {
	if credits < MinBet || credits > MaxBet {
		return fmt.Errorf("bet must be between %d and %d credits", MinBet, MaxBet)
	}
	return nil
}

// BetLimits exposes the accepted stake range for the games listing.
func BetLimits() (int64, int64) { return MinBet, MaxBet }

// DrawMines selects mineCount distinct positions from the stream: a partial
// Fisher-Yates over the identity permutation, recorded by index so it
// replays byte-for-byte.
func DrawMines(s *fair.Stream, mineCount int) []int {
	order := make([]int, Tiles)
	for i := range order {
		order[i] = i
	}
	for i := 0; i < mineCount; i++ {
		j := i + int(s.Uint32()%uint32(Tiles-i))
		order[i], order[j] = order[j], order[i]
	}
	return order[:mineCount]
}

// comb returns C(n, k) as float.
func comb(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	r := 1.0
	for i := 0; i < k; i++ {
		r = r * float64(n-i) / float64(i+1)
	}
	return r
}

// Multiplier is the cashout value after k safe reveals with M mines:
// 0.99 · C(25,k) / C(25−M,k), floored to two decimals. It is the inverse of
// the probability that k specific tiles are all safe, times the payout
// complement — so the expected return is EdgeComplement whatever k the
// player stops at.
func Multiplier(mineCount, revealed int) float64 {
	if revealed <= 0 {
		return 1
	}
	m := EdgeComplement * comb(Tiles, revealed) / comb(Tiles-mineCount, revealed)
	return math.Floor(m*100) / 100
}

// Payout is the credited amount for a cashout at k reveals.
func Payout(betCredits int64, mineCount, revealed int) int64 {
	return int64(math.Floor(float64(betCredits) * Multiplier(mineCount, revealed)))
}
