package cards

// BJTotal returns the blackjack hand total and whether it is soft (one
// ace counted as 11). Aces start at 11 and are downgraded to 1 as needed;
// the best (highest non-bust) total is returned.
func BJTotal(cs []Card) (total int, soft bool) {
	aces := 0
	total = 0
	for _, c := range cs {
		switch {
		case c.Rank == Ace:
			aces++
			total += 11
			soft = true
		case c.Rank >= Ten:
			total += 10
		default:
			total += int(c.Rank)
		}
	}
	for total > 21 && aces > 0 {
		total -= 10
		aces--
		if aces == 0 {
			soft = false
		}
	}
	if aces == 0 {
		soft = false
	}
	return total, soft
}

// IsBlackjack reports whether the two-card hand is a natural 21.
func IsBlackjack(cs []Card) bool {
	if len(cs) != 2 {
		return false
	}
	total, _ := BJTotal(cs)
	return total == 21
}
