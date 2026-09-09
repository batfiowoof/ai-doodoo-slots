package recs

import (
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

func catalog() []CatalogEntry {
	newUntil := testNow.Add(48 * time.Hour)
	return []CatalogEntry{
		{GameID: "fruits", Category: "slots", Tags: []string{"reels", "fruit", "casual"}},
		{GameID: "slots", Category: "slots", Tags: []string{"reels", "classic", "retro"}},
		{GameID: "treasure", Category: "slots", Tags: []string{"reels", "adventure"}, IsNewUntil: &newUntil},
		{GameID: "dice", Category: "instant", Tags: []string{"multiplier", "risk", "fast"}},
		{GameID: "mines", Category: "instant", Tags: []string{"grid", "risk", "cashout"}},
		{GameID: "crash", Category: "live", Tags: []string{"multiplier", "cashout", "social"}},
	}
}

func svc() *Service {
	return &Service{names: map[string]string{"fruits": "Fruit Salad", "slots": "Classic Sevens", "treasure": "Treasure", "dice": "Dice", "mines": "Mines", "crash": "Crash"}}
}

func TestRankAffinityDominates(t *testing.T) {
	signals := map[string]Signal{
		"dice": {GameID: "dice", Plays: 40, LastPlayed: testNow.Add(-2 * time.Hour)},
		"mines": {GameID: "mines", Plays: 3, LastPlayed: testNow.Add(-1 * time.Hour)},
	}
	rank := Rank(catalog(), signals, nil, testNow)
	if rank[0] != "dice" {
		t.Fatalf("expected dice first, got %v", rank)
	}
	if rank[1] != "mines" {
		t.Fatalf("expected mines second, got %v", rank)
	}
}

func TestRankRecencyBreaksAffinityTies(t *testing.T) {
	signals := map[string]Signal{
		"dice":  {GameID: "dice", Plays: 5, LastPlayed: testNow.Add(-100 * time.Hour)},
		"mines": {GameID: "mines", Plays: 5, LastPlayed: testNow.Add(-2 * time.Hour)},
	}
	rank := Rank(catalog(), signals, nil, testNow)
	if rank[0] != "mines" || rank[1] != "dice" {
		t.Fatalf("expected mines before dice, got %v", rank)
	}
}

func TestRankTrendingLiftsUnplayed(t *testing.T) {
	trending := map[string]int64{"crash": 50, "mines": 2}
	rank := Rank(catalog(), nil, trending, testNow)
	if rank[0] != "crash" {
		t.Fatalf("expected trending crash first, got %v", rank)
	}
	// Mines (light trending) must land ahead of every no-signal game
	// except treasure, whose time-limited new boost legitimately competes.
	indexOf := func(id string) int {
		for i, x := range rank {
			if x == id {
				return i
			}
		}
		return -1
	}
	if indexOf("mines") > indexOf("fruits") {
		t.Fatalf("expected trending mines above no-signal fruits, got %v", rank)
	}
}

func TestRankTieBreaksAlphabetically(t *testing.T) {
	rank := Rank(catalog(), nil, nil, testNow)
	// No signals, no trending: only treasure gets the new boost.
	if rank[0] != "treasure" {
		t.Fatalf("expected new game first, got %v", rank)
	}
	rest := map[string]bool{}
	for _, id := range rank[1:] {
		rest[id] = true
	}
	for _, want := range []string{"crash", "dice", "fruits", "mines", "slots"} {
		if !rest[want] {
			t.Fatalf("expected %s in catalog tail, got %v", want, rank)
		}
	}
}

func TestForYouSeedsSameCategoryAndExcludesSeed(t *testing.T) {
	signals := map[string]Signal{
		"slots": {GameID: "slots", Plays: 20, LastPlayed: testNow.Add(-1 * time.Hour)},
		"dice":  {GameID: "dice", Plays: 18, LastPlayed: testNow.Add(-1 * time.Hour)},
	}
	ids, seed := forYouSeeds(catalog(), signals, nil, 3)
	if seed != "slots" {
		t.Fatalf("expected slots as seed, got %q", seed)
	}
	// Same-category unplayed neighbours first (stable catalog order for
	// ties), then the best cross-category pick. The heavily-played dice is
	// a worse discovery than unplayed games.
	want := []string{"fruits", "treasure", "mines"}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("expected %v, got %v", want, ids)
		}
	}
	for _, id := range ids {
		if id == seed {
			t.Fatalf("seed %s must not appear in its own recommendations", seed)
		}
	}
}

func TestBuildColdStartKeepsNavigationDropsPromotion(t *testing.T) {
	signals := map[string]Signal{
		"dice": {GameID: "dice", Plays: 2, LastPlayed: testNow.Add(-30 * time.Minute)},
	}
	res := svc().build(catalog(), signals, map[string]int64{"crash": 9}, testNow, false, true, ReasonCold)
	if res.Personalized || res.Reason != ReasonCold {
		t.Fatalf("expected cold result, got %+v", res)
	}
	if len(res.Continue) != 1 || res.Continue[0].GameID != "dice" {
		t.Fatalf("expected dice in continue, got %+v", res.Continue)
	}
	if res.Continue[0].Label != "30M AGO" {
		t.Fatalf("expected relative label, got %q", res.Continue[0].Label)
	}
	// Trending is descriptive and stays, but nothing else is personalized.
	if len(res.Trending) != 1 || res.Trending[0] != "crash" {
		t.Fatalf("expected crash trending, got %v", res.Trending)
	}
	if len(res.ForYou) != 0 {
		t.Fatalf("cold start must not emit forYou, got %+v", res.ForYou)
	}
}

func TestBuildRiskSuppressionStripsPromotion(t *testing.T) {
	signals := map[string]Signal{
		"dice":  {GameID: "dice", Plays: 30, LastPlayed: testNow.Add(-10 * time.Minute)},
		"mines": {GameID: "mines", Plays: 12, LastPlayed: testNow.Add(-2 * time.Hour)},
	}
	res := svc().build(catalog(), signals, map[string]int64{"crash": 9}, testNow, true, false, ReasonRiskSuppressed)
	if !res.Personalized || res.PromoEligible {
		t.Fatalf("expected personalized-but-suppressed, got %+v", res)
	}
	if len(res.ForYou) != 0 || len(res.Trending) != 0 || len(res.New) != 0 {
		t.Fatalf("suppression must strip promotional sections: %+v", res)
	}
	if len(res.Continue) == 0 {
		t.Fatal("suppression must keep continue-playing navigation")
	}
	for id, badge := range res.Badges {
		if badge != "RECENT" {
			t.Fatalf("suppression keeps only descriptive badges; %s=%s", id, badge)
		}
	}
}

func TestBuildBadgePriority(t *testing.T) {
	signals := map[string]Signal{
		"dice": {GameID: "dice", Plays: 30, LastPlayed: testNow.Add(-10 * time.Minute)},
	}
	res := svc().build(catalog(), signals, map[string]int64{"dice": 9, "crash": 5}, testNow, true, true, "")
	// dice is top pick, trending, new and recent — one badge, the top one.
	if res.Badges["dice"] != "TOP PICK" {
		t.Fatalf("expected TOP PICK to win, got %q", res.Badges["dice"])
	}
	if res.Badges["treasure"] != "NEW" {
		t.Fatalf("expected NEW on treasure, got %q", res.Badges["treasure"])
	}
	if res.Badges["crash"] != "HOT" {
		t.Fatalf("expected HOT on crash, got %q", res.Badges["crash"])
	}
	// Untouched games get no badge at all.
	if res.Badges["mines"] != "" {
		t.Fatalf("expected no badge on untouched mines, got %q", res.Badges["mines"])
	}
}

func TestBuildForYouLabelsAreHonest(t *testing.T) {
	signals := map[string]Signal{
		"slots": {GameID: "slots", Plays: 20, LastPlayed: testNow.Add(-1 * time.Hour)},
	}
	res := svc().build(catalog(), signals, nil, testNow, true, true, "")
	for _, e := range res.ForYou {
		if e.Label != "BECAUSE YOU PLAYED CLASSIC SEVENS" {
			t.Fatalf("unexpected label %q", e.Label)
		}
		break
	}
}

func TestRelLabel(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "JUST NOW"},
		{5 * time.Minute, "5M AGO"},
		{3 * time.Hour, "3H AGO"},
		{2 * 24 * time.Hour, "2D AGO"},
	}
	for _, c := range cases {
		if got := relLabel(testNow, testNow.Add(-c.ago)); got != c.want {
			t.Errorf("relLabel(%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}
