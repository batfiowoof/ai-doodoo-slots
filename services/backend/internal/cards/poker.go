package cards

import "sort"

// Poker hand categories, ordered worst to best.
const (
	CatHighCard  = 0
	CatPair      = 1
	CatTwoPair   = 2
	CatTrips     = 3
	CatStraight  = 4
	CatFlush     = 5
	CatFullHouse = 6
	CatQuads     = 7
	CatStFlush   = 8
)

// Eval is a scored poker hand: category plus tiebreak ranks, comparable
// lexicographically (category first, then tiebreaks high to low).
type Eval struct {
	Category int
	Tiebreak [5]Rank
	Name     string
}

// Better reports whether a strictly beats b.
func (a Eval) Better(b Eval) bool {
	if a.Category != b.Category {
		return a.Category > b.Category
	}
	for i := range a.Tiebreak {
		if a.Tiebreak[i] != b.Tiebreak[i] {
			return a.Tiebreak[i] > b.Tiebreak[i]
		}
	}
	return false
}

// Evaluate scores the best five-card hand from 5..7 cards.
func Evaluate(cs []Card) Eval {
	if len(cs) < 5 || len(cs) > 7 {
		panic("cards.Evaluate requires 5 to 7 cards")
	}
	// Choose 5 of n via index combinations (at most C(7,5)=21).
	idx := make([]int, 5)
	for i := range idx {
		idx[i] = i
	}
	best := eval5(combo(cs, idx))
	for {
		if !nextCombo(idx, len(cs)) {
			break
		}
		if e := eval5(combo(cs, idx)); e.Better(best) {
			best = e
		}
	}
	return best
}

func combo(cs []Card, idx []int) []Card {
	out := make([]Card, len(idx))
	for i, j := range idx {
		out[i] = cs[j]
	}
	return out
}

// nextCombo advances idx to the next strictly ascending combination.
func nextCombo(idx []int, n int) bool {
	k := len(idx)
	i := k - 1
	for i >= 0 && idx[i] == n-k+i {
		i--
	}
	if i < 0 {
		return false
	}
	idx[i]++
	for j := i + 1; j < k; j++ {
		idx[j] = idx[j-1] + 1
	}
	return true
}

// eval5 scores exactly five cards.
func eval5(cs []Card) Eval {
	counts := [15]int{}
	suits := [4]int{}
	for _, c := range cs {
		counts[c.Rank]++
		suits[c.Suit]++
	}
	flush := suits[cs[0].Suit] == 5

	// Straight detection from the distinct ranks present.
	straightHigh, ok := straightHigh(cs)
	if ok && flush {
		return Eval{CatStFlush, [5]Rank{straightHigh, 0, 0, 0, 0}, "straight flush"}
	}

	// Group ranks by count, then by rank, for the count-based categories.
	quads, trips, pairs := collectCounts(counts)
	switch {
	case len(quads) == 1:
		kicker := bestRanks(counts, 1, quads)
		return Eval{CatQuads, [5]Rank{quads[0], kicker[0], 0, 0, 0}, "four of a kind"}
	case len(trips) == 1 && len(pairs) >= 1:
		return Eval{CatFullHouse, [5]Rank{trips[0], pairs[0], 0, 0, 0}, "full house"}
	case flush:
		fr := make([]Rank, 0, 5)
		for _, c := range cs {
			fr = append(fr, c.Rank)
		}
		sort.Slice(fr, func(i, j int) bool { return fr[i] > fr[j] })
		return Eval{CatFlush, [5]Rank{fr[0], fr[1], fr[2], fr[3], fr[4]}, "flush"}
	case ok:
		return Eval{CatStraight, [5]Rank{straightHigh, 0, 0, 0, 0}, "straight"}
	case len(trips) == 1:
		kickers := bestRanks(counts, 2, trips)
		return Eval{CatTrips, [5]Rank{trips[0], kickers[0], kickers[1], 0, 0}, "three of a kind"}
	case len(pairs) >= 2:
		sort.Slice(pairs, func(i, j int) bool { return pairs[i] > pairs[j] })
		kicker := bestRanks(counts, 1, pairs)
		return Eval{CatTwoPair, [5]Rank{pairs[0], pairs[1], kicker[0], 0, 0}, "two pair"}
	case len(pairs) == 1:
		kickers := bestRanks(counts, 3, pairs)
		return Eval{CatPair, [5]Rank{pairs[0], kickers[0], kickers[1], kickers[2], 0}, "pair"}
	default:
		hr := bestRanks(counts, 5, nil)
		return Eval{CatHighCard, [5]Rank{hr[0], hr[1], hr[2], hr[3], hr[4]}, "high card"}
	}
}

// straightHigh returns the high rank if all five ranks are consecutive
// (A-2-3-4-5 has high rank 5). ok is false otherwise.
func straightHigh(cs []Card) (Rank, bool) {
	ranks := make([]Rank, 0, 5)
	for _, c := range cs {
		ranks = append(ranks, c.Rank)
	}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i] > ranks[j] })
	for i := 1; i < len(ranks); i++ {
		if ranks[i] == ranks[i-1] {
			return 0, false // duplicate ranks: not a straight
		}
	}
	for i := 1; i < len(ranks); i++ {
		if ranks[i-1]-ranks[i] != 1 {
			// Wheel check: A,5,4,3,2 in desc order is A,5,4,3,2.
			if ranks[0] == Ace && ranks[1] == 5 && ranks[2] == 4 && ranks[3] == 3 && ranks[4] == 2 {
				return 5, true
			}
			return 0, false
		}
	}
	return ranks[0], true
}

// collectCounts splits ranks by their multiplicity in counts.
func collectCounts(counts [15]int) (quads, trips, pairs []Rank) {
	for r := Ace; r >= 2; r-- {
		switch counts[r] {
		case 4:
			quads = append(quads, r)
		case 3:
			trips = append(trips, r)
		case 2:
			pairs = append(pairs, r)
		}
	}
	return quads, trips, pairs
}

// bestRanks returns the top n distinct ranks from counts, excluding the
// excluded list.
func bestRanks(counts [15]int, n int, excluded []Rank) []Rank {
	isExcluded := [15]bool{}
	for _, r := range excluded {
		isExcluded[r] = true
	}
	out := make([]Rank, 0, n)
	for r := Ace; r >= 2 && len(out) < n; r-- {
		if counts[r] > 0 && !isExcluded[r] {
			out = append(out, r)
		}
	}
	for len(out) < n {
		out = append(out, 0)
	}
	return out
}
