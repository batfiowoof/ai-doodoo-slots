package cards

import "testing"

func mustCards(t *testing.T, s string) []Card {
	t.Helper()
	cs, err := ParseCards(s)
	if err != nil {
		t.Fatalf("ParseCards(%q): %v", s, err)
	}
	return cs
}

func TestBJTotal(t *testing.T) {
	cases := []struct {
		hand  string
		total int
		soft  bool
	}{
		{"AcJd", 21, true},
		{"Ac9d", 20, true},   // A=11 + 9
		{"Ac9d5h", 15, false}, // A downgraded: 11+9+5=25 -> 1+9+5=15
		{"AcAh", 12, true},    // 11+1
		{"AcAhTd", 12, false}, // 11+11+10=32 -> 1+1+10=12
		{"KcQd", 20, false},
		{"KcQd2h", 22, false},
		{"2c5d", 7, false},
		{"AcTd", 21, true},
		{"AcAhTdTh", 22, false}, // 1+1+10+10: both aces downgraded, still bust
	}
	for _, tc := range cases {
		hand := mustCards(t, tc.hand)
		got, soft := BJTotal(hand)
		if got != tc.total || soft != tc.soft {
			t.Errorf("BJTotal(%s) = %d, soft=%v; want %d, %v", tc.hand, got, soft, tc.total, tc.soft)
		}
	}
}

func TestBJTotalQuadAces(t *testing.T) {
	// A+A+A+A = 11+1+1+1 = 14 soft.
	hand := mustCards(t, "AsAhAdAc")
	got, soft := BJTotal(hand)
	if got != 14 || !soft {
		t.Fatalf("quad aces = %d soft=%v; want 14 soft", got, soft)
	}
}

func TestIsBlackjack(t *testing.T) {
	if !IsBlackjack(mustCards(t, "AcTd")) {
		t.Fatal("AT should be blackjack")
	}
	if IsBlackjack(mustCards(t, "AcAhTd")) {
		t.Fatal("three cards cannot be blackjack")
	}
	if IsBlackjack(mustCards(t, "2c2d")) {
		t.Fatal("22 is not blackjack")
	}
}
