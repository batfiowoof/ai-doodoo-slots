package recs

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Pure scoring logic: no I/O, injected clock, deterministic tie-breaks so
// tests can assert exact orderings.

// Signal is one player's behavioral record for a single game.
type Signal struct {
	GameID     string
	Plays      int64 // settled bets in the affinity window
	Net        int64 // payout - stakes
	LastPlayed time.Time
	LastLaunch time.Time // page open, feeds cold-start "continue"
}

// CatalogEntry is the metadata slice of one game the scorer needs.
type CatalogEntry struct {
	GameID     string
	Category   string // slots | instant | table | live
	Collection string
	Tags       []string
	IsNewUntil *time.Time
}

// Scoring weights. Affinity dominates; the rest spreads across recency,
// trending and new-ness. All copy the payload emits is descriptive — the
// score may reorder the wheel, never nag about it.
const (
	wAffinity = 0.5
	wRecency  = 0.2
	wTrending = 0.2
	wNew      = 0.1

	// Hours over which a play's recency contribution decays to ~e^-1.
	recencyTauHours = 72.0
)

func scoreGame(sig Signal, trend float64, isNew bool, maxPlays int64, now time.Time) float64 {
	// Affinity scales with play volume (log-damped so one mega-session can't
	// flatten the rest of the floor); recency decays exponentially.
	var affinity, recency float64
	if sig.Plays > 0 && maxPlays > 0 && !sig.LastPlayed.IsZero() {
		affinity = math.Log1p(float64(sig.Plays)) / math.Log1p(float64(maxPlays))
		recency = math.Exp(-now.Sub(sig.LastPlayed).Hours() / recencyTauHours)
	}
	newness := 0.0
	if isNew {
		newness = 1
	}
	return wAffinity*affinity + wRecency*recency + wTrending*trend + wNew*newness
}

// Rank orders the whole catalog for one player, best first. Games with no
// signal still rank by trending/new so an unplayed floor stays discoverable.
// Ties break by game id for determinism.
func Rank(catalog []CatalogEntry, signals map[string]Signal, trending map[string]int64, now time.Time) []string {
	maxTrend := int64(1)
	for _, h := range trending {
		if h > maxTrend {
			maxTrend = h
		}
	}
	maxPlays := int64(1)
	for _, sig := range signals {
		if sig.Plays > maxPlays {
			maxPlays = sig.Plays
		}
	}
	type scored struct {
		id    string
		score float64
	}
	all := make([]scored, 0, len(catalog))
	for _, c := range catalog {
		trend := float64(trending[c.GameID]) / float64(maxTrend)
		isNew := c.IsNewUntil != nil && c.IsNewUntil.After(now)
		all = append(all, scored{c.GameID, scoreGame(signals[c.GameID], trend, isNew, maxPlays, now)})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].id < all[j].id
	})
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, s.id)
	}
	return out
}

// forYouSeeds picks "because you played…" neighbours of the player's top
// game: same category first, then tag overlap, then trending. Returns at
// most n ids, never the seed itself.
func forYouSeeds(catalog []CatalogEntry, signals map[string]Signal, trending map[string]int64, n int) ([]string, string) {
	var seed Signal
	var seedSet bool
	for _, s := range signals {
		if s.Plays > 0 && (!seedSet || s.Plays > seed.Plays || (s.Plays == seed.Plays && s.LastPlayed.After(seed.LastPlayed))) {
			seed = s
			seedSet = true
		}
	}
	if !seedSet {
		return nil, ""
	}
	seedMeta := findMeta(catalog, seed.GameID)
	type cand struct {
		id    string
		cat   bool
		tags  int
		trend int64
		plays int64
	}
	var cands []cand
	for _, c := range catalog {
		if c.GameID == seed.GameID {
			continue
		}
		cands = append(cands, cand{
			id:    c.GameID,
			cat:   seedMeta != nil && c.Category == seedMeta.Category,
			tags:  overlap(seedMeta, &c),
			trend: trending[c.GameID],
			plays: signals[c.GameID].Plays,
		})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].cat != cands[j].cat {
			return cands[i].cat
		}
		if cands[i].tags != cands[j].tags {
			return cands[i].tags > cands[j].tags
		}
		// Lightly-played games make better discoveries than ones already
		// in rotation; trending breaks the remaining ties.
		if (cands[i].plays == 0) != (cands[j].plays == 0) {
			return cands[i].plays == 0
		}
		return cands[i].trend > cands[j].trend
	})
	out := make([]string, 0, n)
	for _, c := range cands {
		if len(out) == n {
			break
		}
		out = append(out, c.id)
	}
	return out, seed.GameID
}

func findMeta(catalog []CatalogEntry, id string) *CatalogEntry {
	for i := range catalog {
		if catalog[i].GameID == id {
			return &catalog[i]
		}
	}
	return nil
}

func overlap(a, b *CatalogEntry) int {
	if a == nil || b == nil {
		return 0
	}
	n := 0
	for _, t := range a.Tags {
		for _, u := range b.Tags {
			if strings.EqualFold(t, u) {
				n++
			}
		}
	}
	return n
}

// relLabel renders a compact age for node small print ("4M AGO", "2H AGO",
// "3D AGO"); the granularity the radial badges can fit.
func relLabel(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "JUST NOW"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "M AGO"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "H AGO"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "D AGO"
	}
}
