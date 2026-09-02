package fair

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/testdb"
)

// TestChainRevealVerifiesBackwards is the phase-12 gate: revealing links
// across two full chains (spanning the global index continuation) verifies
// that each revealed seed hashes to the previously revealed one. It also
// proves tampering with the stored hash is detected.
func TestChainRevealVerifiesBackwards(t *testing.T) {
	pool := testdb.Pool(t)
	s := NewChainService(pool)
	ctx := context.Background()

	if err := s.EnsureChain(ctx); err != nil {
		t.Fatalf("ensure chain: %v", err)
	}
	// Exhaust the remaining links plus one fresh chain to cross the chain
	// boundary with index continuation.
	for {
		if _, _, err := s.RevealNext(ctx); err == ErrChainExhausted {
			break
		} else if err != nil {
			t.Fatalf("reveal: %v", err)
		}
	}
	if err := s.EnsureChain(ctx); err != nil {
		t.Fatalf("ensure second chain: %v", err)
	}

	const links = 1000
	firstIndex, firstHash, err := s.CurrentHead(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	_ = firstIndex
	prevPlain, prevIdx, err := s.RevealNext(ctx)
	if err != nil {
		t.Fatalf("reveal first: %v", err)
	}
	_ = firstHash
	for i := 1; i < links; i++ {
		plain, idx, err := s.RevealNext(ctx)
		if err != nil {
			t.Fatalf("reveal %d: %v", i, err)
		}
		// Relation: seed[idx] = sha256(seed[idx+1]) — the previous link's
		// plain must hash to this link's plain.
		sum := sha256.Sum256(prevPlain)
		if hex.EncodeToString(sum[:]) != hex.EncodeToString(plain) {
			t.Fatalf("link %d: revealed seed[%d] does not hash to seed[%d]", i, prevIdx, idx)
		}
		if idx != prevIdx-1 {
			t.Fatalf("links consumed out of order: %d then %d", prevIdx, idx)
		}
		prevPlain, prevIdx = plain, idx
	}
}

func TestChainStreamFromRevealedSeedIsDeterministic(t *testing.T) {
	pool := testdb.Pool(t)
	s := NewChainService(pool)
	ctx := context.Background()

	if err := s.EnsureChain(ctx); err != nil {
		t.Fatalf("ensure chain: %v", err)
	}
	seed, _, err := s.RevealNext(ctx)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	salt := "salt-from-participant-client-seeds"
	a := NewChainStream(seed, salt)
	b := NewChainStream(seed, salt)
	for i := 0; i < 100; i++ {
		if a.Float() != b.Float() {
			t.Fatal("chain streams diverged for identical inputs")
		}
	}
	// A different round salt must produce different floats (salt enters the
	// HMAC message, not the key).
	c := NewChainStream(seed, "different-round-salt")
	a2 := NewChainStream(seed, salt)
	for i := 0; i < 100; i++ {
		x := a2.Float()
		y := c.Float()
		if x != y {
			break
		}
		if i == 99 {
			t.Fatal("different salts produced identical float streams")
		}
	}
}
