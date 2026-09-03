package cards

import (
	"testing"

	"github.com/ai-doodoo-slots/services/backend/internal/fair"
)

func TestNewDeckHas52UniqueCards(t *testing.T) {
	deck := NewDeck()
	if len(deck) != 52 {
		t.Fatalf("deck size = %d, want 52", len(deck))
	}
	seen := make(map[Card]bool, 52)
	for _, c := range deck {
		if seen[c] {
			t.Fatalf("duplicate card %s", c)
		}
		seen[c] = true
	}
}

func TestCardRoundTrip(t *testing.T) {
	deck := NewDeck()
	encoded := CardsString(deck)
	decoded, err := ParseCards(encoded)
	if err != nil {
		t.Fatalf("ParseCards: %v", err)
	}
	if CardsString(decoded) != encoded {
		t.Fatal("round trip changed the deck")
	}
	if _, err := ParseCard("Xz"); err == nil {
		t.Fatal("ParseCard accepted invalid code")
	}
}

func TestShuffleDeterministic(t *testing.T) {
	serverSeed, err := fair.GenerateSeed()
	if err != nil {
		t.Fatalf("GenerateSeed: %v", err)
	}
	deck1 := NewDeck()
	Shuffle(deck1, fair.NewPersonalStream(serverSeed, "client-a", 7))
	deck2 := NewDeck()
	Shuffle(deck2, fair.NewPersonalStream(serverSeed, "client-a", 7))
	if CardsString(deck1) != CardsString(deck2) {
		t.Fatal("same seed material produced different decks")
	}
	deck3 := NewDeck()
	Shuffle(deck3, fair.NewPersonalStream(serverSeed, "client-b", 7))
	if CardsString(deck1) == CardsString(deck3) {
		t.Fatal("different client seed produced identical deck")
	}
	// Permutation invariant: same 52 cards, all present.
	seen := make(map[Card]bool, 52)
	for _, c := range deck1 {
		seen[c] = true
	}
	if len(seen) != 52 {
		t.Fatalf("shuffled deck has %d unique cards, want 52", len(seen))
	}
}

func TestShuffleRoughlyUniform(t *testing.T) {
	// A weak smoke test: every position should receive a spread of ranks
	// across many shuffles (catches gross bias like a stuck index).
	serverSeed, err := fair.GenerateSeed()
	if err != nil {
		t.Fatalf("GenerateSeed: %v", err)
	}
	const rounds = 200
	dist := make([]map[Rank]int, 4) // suit of the first card, per position 0
	for i := range dist {
		dist[i] = make(map[Rank]int)
	}
	for n := 0; n < rounds; n++ {
		deck := NewDeck()
		Shuffle(deck, fair.NewPersonalStream(serverSeed, "client", int64(n)))
		for pos := 0; pos < 4; pos++ {
			dist[pos][deck[pos].Rank]++
		}
	}
	for pos := 0; pos < 4; pos++ {
		// Any single rank dominating (>60%) signals a broken shuffle.
		for _, count := range dist[pos] {
			if count > rounds*60/100 {
				t.Fatalf("position %d has a rank appearing %d/%d times", pos, count, rounds)
			}
		}
	}
}
