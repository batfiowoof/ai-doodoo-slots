// Package cards provides the shared card primitives for card games:
// deck construction, provably-fair shuffling, blackjack hand totals, and
// poker hand evaluation. Everything here is pure — no clocks, no globals,
// no math/rand — so every deal is reproducible from its seed material.
package cards

import "fmt"

// Rank is a card rank: 2..10 numeric, then Jack=11, Queen=12, King=13,
// Ace=14.
type Rank uint8

// Suit is a card suit, 0..3.
type Suit uint8

const (
	Spade   Suit = 0
	Heart   Suit = 1
	Diamond Suit = 2
	Club    Suit = 3
)

const (
	Ace   Rank = 14
	King  Rank = 13
	Queen Rank = 12
	Jack  Rank = 11
	Ten   Rank = 10
)

var rankChars = map[Rank]byte{
	2: '2', 3: '3', 4: '4', 5: '5', 6: '6', 7: '7', 8: '8', 9: '9',
	Ten: 'T', Jack: 'J', Queen: 'Q', King: 'K', Ace: 'A',
}

var suitChars = map[Suit]byte{Spade: 's', Heart: 'h', Diamond: 'd', Club: 'c'}

// Card is one playing card.
type Card struct {
	Rank Rank
	Suit Suit
}

// String returns the two-character code, e.g. "As", "Td", "7c".
func (c Card) String() string {
	r, ok := rankChars[c.Rank]
	if !ok {
		return fmt.Sprintf("?%d%d", c.Rank, c.Suit)
	}
	s := suitChars[c.Suit]
	return string(r) + string(s)
}

// ParseCard decodes a two-character code produced by String.
func ParseCard(s string) (Card, error) {
	if len(s) != 2 {
		return Card{}, fmt.Errorf("card code %q must be two characters", s)
	}
	var rank Rank
	switch s[0] {
	case '2', '3', '4', '5', '6', '7', '8', '9':
		rank = Rank(s[0] - '0')
	case 'T':
		rank = Ten
	case 'J':
		rank = Jack
	case 'Q':
		rank = Queen
	case 'K':
		rank = King
	case 'A':
		rank = Ace
	default:
		return Card{}, fmt.Errorf("card code %q has invalid rank", s)
	}
	var suit Suit
	switch s[1] {
	case 's':
		suit = Spade
	case 'h':
		suit = Heart
	case 'd':
		suit = Diamond
	case 'c':
		suit = Club
	default:
		return Card{}, fmt.Errorf("card code %q has invalid suit", s)
	}
	return Card{Rank: rank, Suit: suit}, nil
}

// ParseCards decodes a whitespace-free concatenation of two-character
// codes, e.g. "AsKd7c2hTd".
func ParseCards(s string) ([]Card, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("card list %q has odd length", s)
	}
	out := make([]Card, 0, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		c, err := ParseCard(s[i : i+2])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// CardsString encodes cards back to the compact list form.
func CardsString(cs []Card) string {
	buf := make([]byte, 0, len(cs)*2)
	for _, c := range cs {
		buf = append(buf, c.String()...)
	}
	return string(buf)
}

// NewDeck returns a fresh 52-card deck in canonical order.
func NewDeck() []Card {
	deck := make([]Card, 0, 52)
	for suit := Suit(0); suit <= 3; suit++ {
		for rank := Rank(2); rank <= Ace; rank++ {
			deck = append(deck, Card{Rank: rank, Suit: suit})
		}
	}
	return deck
}

// Ranks returns the distinct ranks present, highest first.
func Ranks(cs []Card) []Rank {
	seen := [15]bool{}
	for _, c := range cs {
		seen[c.Rank] = true
	}
	out := make([]Rank, 0, 5)
	for r := Ace; r >= 2; r-- {
		if seen[r] {
			out = append(out, r)
		}
	}
	return out
}
