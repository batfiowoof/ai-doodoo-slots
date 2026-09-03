package cards

import "testing"

func TestEvaluateCategories(t *testing.T) {
	cases := []struct {
		name     string
		hand     string
		category int
	}{
		{"royal flush", "AsKsQsJsTs", CatStFlush},
		{"wheel straight flush", "Ad2d3d4d5d", CatStFlush},
		{"steel wheel ace-low", "As2s3s4s5s", CatStFlush},
		{"quads", "7s7h7d7cKs", CatQuads},
		{"full house", "7s7h7dKcKs", CatFullHouse},
		{"flush", "2s4s6s8sTs", CatFlush},
		{"broadway straight", "AsKsQdJsTc", CatStraight},
		{"wheel straight", "Ah2d3c4s5h", CatStraight},
		{"trips", "7s7h7dKc2s", CatTrips},
		{"two pair", "7s7hKdKc2s", CatTwoPair},
		{"pair", "7s7hAdKc2s", CatPair},
		{"high card", "7s4hAdKc2s", CatHighCard},
	}
	for _, tc := range cases {
		hand := mustCards(t, tc.hand)
		got := Evaluate(hand)
		if got.Category != tc.category {
			t.Errorf("%s (%s): category = %d (%s), want %d", tc.name, tc.hand, got.Category, got.Name, tc.category)
		}
	}
}

func TestEvaluateTiebreaks(t *testing.T) {
	// Higher straight flush beats lower.
	sfHigh := Evaluate(mustCards(t, "9s8s7s6s5s"))
	sfLow := Evaluate(mustCards(t, "8s7s6s5s4s"))
	if !sfHigh.Better(sfLow) || sfLow.Better(sfHigh) {
		t.Fatal("straight flush ordering broken")
	}

	// Wheel straight (high 5) loses to 6-high straight.
	wheel := Evaluate(mustCards(t, "Ah2d3c4s5h"))
	six := Evaluate(mustCards(t, "2d3c4s5h6h"))
	if wheel.Better(six) || wheel.Tiebreak[0] != 5 {
		t.Fatalf("wheel misvalued: %+v", wheel)
	}

	// Kicker: pair of aces, king kicker beats queen kicker.
	aK := Evaluate(mustCards(t, "AsAhKd7c2s"))
	aQ := Evaluate(mustCards(t, "AsAhQd7c2s"))
	if !aK.Better(aQ) {
		t.Fatal("kicker ordering broken")
	}

	// Two pair: aces up beats kings up; equal pairs fall to kicker.
	twoPairHigh := Evaluate(mustCards(t, "AsAhKdKc2s"))
	twoPairLow := Evaluate(mustCards(t, "KsKhQdQcAs"))
	if !twoPairHigh.Better(twoPairLow) {
		t.Fatal("two pair ordering broken")
	}

	// Full house: trips rank dominates.
	fhHigh := Evaluate(mustCards(t, "AsAhAdKcKs"))
	fhLow := Evaluate(mustCards(t, "KsKhKdAcAs"))
	if !fhHigh.Better(fhLow) {
		t.Fatal("full house ordering broken")
	}
}

func TestEvaluateBestOfSeven(t *testing.T) {
	// Board+hole: the best five must be the full house, not the flush on
	// the board.
	hand := Evaluate(mustCards(t, "AsAh5s5d5cKs2s"))
	if hand.Category != CatFullHouse {
		t.Fatalf("best of 7 = %d (%s), want full house", hand.Category, hand.Name)
	}
	// Six-card input: flush over pair (five spades present).
	hand = Evaluate(mustCards(t, "AsKs2s9s5s5d"))
	if hand.Category != CatFlush {
		t.Fatalf("best of 6 = %d (%s), want flush", hand.Category, hand.Name)
	}
}

func TestEvaluateSevenVsFiveConsistency(t *testing.T) {
	// Adding genuinely dead cards (no rank improves the made hand) must
	// not change the score: quad aces with a king kicker.
	base := mustCards(t, "AsAhAdAcKd")
	if Evaluate(base) != Evaluate(append(append([]Card{}, base...), mustCards(t, "2c3c")...)) {
		t.Fatal("7-card evaluation changed a made 5-card hand")
	}
	// Sanity: the made hand is quads with a king kicker.
	e := Evaluate(base)
	if e.Category != CatQuads || e.Tiebreak != [5]Rank{Ace, King, 0, 0, 0} {
		t.Fatalf("quads misvalued: %+v", e)
	}
}

func TestEqualHands(t *testing.T) {
	a := Evaluate(mustCards(t, "AsAhKd7c2s"))
	b := Evaluate(mustCards(t, "AdAcKh7s2c"))
	if a.Better(b) || b.Better(a) {
		t.Fatal("identical-strength hands must tie")
	}
}
