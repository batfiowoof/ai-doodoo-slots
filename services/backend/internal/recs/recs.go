// Package recs computes the personalized lobby: per-game affinity from the
// bets ledger plus lightweight launch events, blended into one deterministic
// ranking. Responsible-gambling gates run before scoring: a suppressed
// player keeps neutral navigation (continue playing, stable order) but every
// promotional surface — new-game pushes, hot streaks, "picked for you" —
// drops out of the payload entirely.
package recs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-doodoo-slots/services/backend/internal/clock"
	"github.com/ai-doodoo-slots/services/backend/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Result is the personalized-lobby payload (see openapi.yaml).
type Result struct {
	Personalized  bool              `json:"personalized"`
	PromoEligible bool              `json:"promoEligible"`
	Reason        string            `json:"reason,omitempty"`
	GameRank      []string          `json:"gameRank"`
	Continue      []Entry           `json:"continue"`
	ForYou        []Entry           `json:"forYou"`
	Trending      []string          `json:"trending"`
	New           []string          `json:"new"`
	Badges        map[string]string `json:"badges"`
}

// Entry is one game in a curated section; Label is the honest small-print
// reason the section claims it ("2H AGO", "BECAUSE YOU PLAYED FRUITS").
type Entry struct {
	GameID string `json:"gameId"`
	Label  string `json:"label,omitempty"`
}

// Suppression reasons, mirrored by the frontend for the neutral chip.
const (
	ReasonAccountRestricted = "account_restricted"
	ReasonToggleOff         = "toggle_off"
	ReasonCold              = "cold"
	ReasonRiskSuppressed    = "risk_suppressed"
)

// Safer-play gate defaults: a player who lost past the floor in 24h AND is
// staking meaningfully above their weekly norm loses promotional content
// until the pattern eases. Env-overridable for tuning without a rebuild.
const (
	defaultRiskLossFloor  = int64(1500)
	defaultRiskStakeRatio = 1.5

	affinitySpan  = "30 days"
	launchSpan    = "7 days"
	resultTTL     = 30 * time.Second
	metadataTTL   = 5 * time.Minute
	recentWindow  = 24 * time.Hour
	continueLimit = 3
	forYouLimit   = 3
	trendingLimit = 3
	coldMinBets   = 5
	coldMinGames  = 2
)

type cacheEntry struct {
	at  time.Time
	res Result
}

// Service computes and caches per-player lobby results.
type Service struct {
	pool   *pgxpool.Pool
	clock  clock.Clock
	logger *slog.Logger
	names  map[string]string

	mu    sync.Mutex
	cache map[int64]cacheEntry

	metaMu sync.Mutex
	metas  []store.GameMetadatum
	metaAt time.Time

	riskLossFloor  int64
	riskStakeRatio float64
}

// NewService builds the recommendation service. names maps game ids to
// display names for the "because you played" labels (live games are rooms,
// not registry listings, so they arrive via the map).
func NewService(pool *pgxpool.Pool, clk clock.Clock, logger *slog.Logger, names map[string]string) *Service {
	s := &Service{
		pool:           pool,
		clock:          clk,
		logger:         logger,
		names:          names,
		cache:          make(map[int64]cacheEntry),
		riskLossFloor:  defaultRiskLossFloor,
		riskStakeRatio: defaultRiskStakeRatio,
	}
	if v, err := strconv.ParseInt(os.Getenv("REC_RISK_LOSS_FLOOR"), 10, 64); err == nil && v > 0 {
		s.riskLossFloor = v
	}
	if v, err := strconv.ParseFloat(os.Getenv("REC_RISK_STAKE_RATIO"), 64); err == nil && v > 1 {
		s.riskStakeRatio = v
	}
	return s
}

// Invalidate drops a player's cached result (called after pref changes).
func (s *Service) Invalidate(userID int64) {
	s.mu.Lock()
	delete(s.cache, userID)
	s.mu.Unlock()
}

// MetadataMap returns the catalog metadata keyed by game id, best-effort:
// a failed load serves the last known snapshot so /games enrichment degrades
// gracefully on a not-yet-migrated database.
func (s *Service) MetadataMap(ctx context.Context) map[string]Meta {
	s.metaMu.Lock()
	fresh := s.clock.Now().Sub(s.metaAt) < metadataTTL
	s.metaMu.Unlock()
	if !fresh {
		if rows, err := s.repo().ListGameMetadata(ctx); err == nil {
			s.metaMu.Lock()
			s.metas = rows
			s.metaAt = s.clock.Now()
			s.metaMu.Unlock()
		} else {
			s.logger.Warn("recs: metadata load failed; serving snapshot", "err", err)
		}
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	out := make(map[string]Meta, len(s.metas))
	for _, m := range s.metas {
		out[m.GameID] = Meta{
			Category:   m.Category,
			Collection: m.Collection,
			Tags:       append([]string(nil), m.Tags...),
			IsNewUntil: m.IsNewUntil,
			Blurb:      m.Blurb,
		}
	}
	return out
}

// Player is the identity slice the engine may use. Status gates everything.
type Player struct {
	ID     int64
	Status string
}

// For computes (or replays) the lobby payload for one player.
func (s *Service) For(ctx context.Context, p Player) (Result, error) {
	if p.Status != "active" {
		// Banned or self-excluded: read-only floor, zero personalization,
		// zero promotion. Nothing in the payload nudges play.
		return neutralResult(nil, ReasonAccountRestricted), nil
	}
	now := s.clock.Now()
	s.mu.Lock()
	if c, ok := s.cache[p.ID]; ok && now.Sub(c.at) < resultTTL {
		s.mu.Unlock()
		return c.res, nil
	}
	s.mu.Unlock()

	res, err := s.compute(ctx, p, now)
	if err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	s.cache[p.ID] = cacheEntry{at: now, res: res}
	s.mu.Unlock()
	return res, nil
}

func (s *Service) compute(ctx context.Context, p Player, now time.Time) (Result, error) {
	repo := store.New(s.pool)

	enabled := true
	if v, err := repo.GetUserPrefs(ctx, p.ID); err == nil {
		enabled = v
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	if !enabled {
		return neutralResult(s.catalog(ctx), ReasonToggleOff), nil
	}

	affinity, err := repo.UserGameAffinity(ctx, store.UserGameAffinityParams{UserID: p.ID, Span: affinitySpan})
	if err != nil {
		return Result{}, err
	}
	launches, err := repo.UserRecentLaunches(ctx, store.UserRecentLaunchesParams{UserID: p.ID, Span: launchSpan})
	if err != nil {
		return Result{}, err
	}
	trendL, err := repo.TrendingLaunches(ctx)
	if err != nil {
		return Result{}, err
	}
	trendB, err := repo.TrendingBets(ctx)
	if err != nil {
		return Result{}, err
	}

	catalog := s.catalog(ctx)
	signals := s.mergeSignals(affinity, launches)
	trending := mergeTrending(trendL, trendB)

	// Safer-play gate: a deep 24h loss plus stakes ramping above the
	// player's weekly norm suppresses promotion, not navigation.
	stakes, err := repo.UserStakeStats(ctx, p.ID)
	if err != nil {
		return Result{}, err
	}
	promoOK := true
	if stakes.Net24h <= -s.riskLossFloor && stakes.AvgBet1h > 0 &&
		float64(stakes.AvgBet1h) > s.riskStakeRatio*float64(stakes.AvgBet7d) {
		promoOK = false
	}

	// Cold start: too little history to rank on. The lobby stays neutral,
	// but continue/trending still help orientation (and are descriptive).
	var totalBets int64
	gamesPlayed := make(map[string]struct{})
	for _, a := range affinity {
		totalBets += a.BetCount
		gamesPlayed[a.GameID] = struct{}{}
	}
	personalized := totalBets >= coldMinBets && len(gamesPlayed) >= coldMinGames
	reason := ""
	if !personalized {
		reason = ReasonCold
	} else if !promoOK {
		reason = ReasonRiskSuppressed
	}
	return s.build(catalog, signals, trending, now, personalized, promoOK, reason), nil
}

// build assembles the payload from signals. Kept side-effect free so tests
// pin the exact section/badge outcomes per gate combination.
func (s *Service) build(catalog []CatalogEntry, signals map[string]Signal, trending map[string]int64, now time.Time, personalized, promoOK bool, reason string) Result {
	res := Result{
		Personalized:  personalized,
		PromoEligible: promoOK,
		Reason:        reason,
		GameRank:      Rank(catalog, signals, trending, now),
		Continue:      []Entry{},
		ForYou:        []Entry{},
		Trending:      []string{},
		New:           []string{},
		Badges:        map[string]string{},
	}

	// Continue: most recent touch (bet or launch) first, regardless of
	// personalization — it is navigation, not promotion.
	type touch struct {
		id string
		at time.Time
	}
	var touches []touch
	for id, sig := range signals {
		at := sig.LastPlayed
		if sig.LastLaunch.After(at) {
			at = sig.LastLaunch
		}
		if !at.IsZero() {
			touches = append(touches, touch{id, at})
		}
	}
	sort.SliceStable(touches, func(i, j int) bool { return touches[i].at.After(touches[j].at) })
	for _, t := range touches {
		if len(res.Continue) == continueLimit {
			break
		}
		res.Continue = append(res.Continue, Entry{GameID: t.id, Label: relLabel(now, t.at)})
	}

	// Trending and new: promotional surfaces, gated.
	var trendingIDs, newIDs []string
	for _, c := range catalog {
		if c.IsNewUntil != nil && c.IsNewUntil.After(now) {
			newIDs = append(newIDs, c.GameID)
		}
	}
	for id := range trending {
		trendingIDs = append(trendingIDs, id)
	}
	sort.SliceStable(trendingIDs, func(i, j int) bool { return trendingIDs[i] < trendingIDs[j] })
	sort.SliceStable(trendingIDs, func(i, j int) bool { return trending[trendingIDs[i]] > trending[trendingIDs[j]] })
	if len(trendingIDs) > trendingLimit {
		trendingIDs = trendingIDs[:trendingLimit]
	}
	sort.SliceStable(newIDs, func(i, j int) bool { return newIDs[i] < newIDs[j] })

	taken := map[string]bool{}
	for _, e := range res.Continue {
		taken[e.GameID] = true
	}
	if promoOK {
		if personalized {
			seedIDs, seed := forYouSeeds(catalog, signals, trending, forYouLimit)
			for _, id := range seedIDs {
				if taken[id] {
					continue
				}
				label := ""
				if seed != "" {
					label = "BECAUSE YOU PLAYED " + s.displayName(seed)
				}
				res.ForYou = append(res.ForYou, Entry{GameID: id, Label: label})
				taken[id] = true
			}
		}
		for _, id := range trendingIDs {
			if taken[id] {
				continue
			}
			res.Trending = append(res.Trending, id)
			taken[id] = true
		}
		for _, id := range newIDs {
			if taken[id] {
				continue
			}
			res.New = append(res.New, id)
			taken[id] = true
		}
	}

	// Badges: one per game, most informative wins. When promotion is
	// suppressed only the descriptive RECENT badge survives.
	if personalized && promoOK && len(res.GameRank) > 0 {
		res.Badges[res.GameRank[0]] = "TOP PICK"
	}
	if promoOK {
		for _, id := range newIDs {
			res.Badges[id] = "NEW"
		}
		for i, id := range trendingIDs {
			if i < 3 {
				if _, has := res.Badges[id]; !has {
					res.Badges[id] = "HOT"
				}
			}
		}
	}
	for _, t := range touches {
		if now.Sub(t.at) <= recentWindow {
			if _, has := res.Badges[t.id]; !has {
				res.Badges[t.id] = "RECENT"
			}
		}
	}
	return res
}

// neutralResult serves the pre-personalization lobby: catalog order, no
// sections, no badges. With a catalog it still lists ids in stable order.
func neutralResult(catalog []CatalogEntry, reason string) Result {
	rank := make([]string, 0, len(catalog))
	for _, c := range catalog {
		rank = append(rank, c.GameID)
	}
	return Result{
		Personalized:  false,
		PromoEligible: false,
		Reason:        reason,
		GameRank:      rank,
		Continue:      []Entry{},
		ForYou:        []Entry{},
		Trending:      []string{},
		New:           []string{},
		Badges:        map[string]string{},
	}
}

func (s *Service) catalog(ctx context.Context) []CatalogEntry {
	s.MetadataMap(ctx)
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	out := make([]CatalogEntry, 0, len(s.metas))
	for _, m := range s.metas {
		out = append(out, CatalogEntry{
			GameID:     m.GameID,
			Category:   m.Category,
			Collection: m.Collection,
			Tags:       append([]string(nil), m.Tags...),
			IsNewUntil: m.IsNewUntil,
		})
	}
	return out
}

func (s *Service) repo() *store.Queries { return store.New(s.pool) }

func (s *Service) displayName(id string) string {
	if n, ok := s.names[id]; ok && n != "" {
		return strings.ToUpper(n)
	}
	return strings.ToUpper(id)
}

func (s *Service) mergeSignals(affinity []store.UserGameAffinityRow, launches []store.UserRecentLaunchesRow) map[string]Signal {
	out := make(map[string]Signal, len(affinity)+len(launches))
	for _, a := range affinity {
		sig := out[a.GameID]
		sig.GameID = a.GameID
		sig.Plays = a.BetCount
		sig.Net = a.Net
		if t, ok := a.LastPlayed.(time.Time); ok {
			sig.LastPlayed = t
		}
		out[a.GameID] = sig
	}
	for _, l := range launches {
		sig := out[l.GameID]
		sig.GameID = l.GameID
		if t, ok := l.LastAt.(time.Time); ok {
			sig.LastLaunch = t
		}
		out[l.GameID] = sig
	}
	return out
}

func mergeTrending(launches []store.TrendingLaunchesRow, bets []store.TrendingBetsRow) map[string]int64 {
	out := make(map[string]int64, len(launches)+len(bets))
	for _, r := range launches {
		out[r.GameID] += r.Hits
	}
	for _, r := range bets {
		out[r.GameID] += r.Hits
	}
	return out
}

// Meta is the public metadata shape (also used for /games enrichment).
type Meta struct {
	Category   string
	Collection string
	Tags       []string
	IsNewUntil *time.Time
	Blurb      string
}

// Event is one inbound behavioral event (validated by the handler).
type Event struct {
	Type    string          `json:"type"`
	GameID  string          `json:"gameId"`
	Context json.RawMessage `json:"context,omitempty"`
}

// RecordLaunch persists a batch of validated events. best-effort per row:
// a duplicated or unknown game must not sink the rest.
func (s *Service) RecordLaunch(ctx context.Context, userID, sessionID int64, events []Event) (int, error) {
	q := s.repo()
	accepted := 0
	for _, e := range events {
		var ctxArg []byte
		if len(e.Context) > 0 {
			ctxArg = e.Context
		}
		err := q.InsertPlayerEvent(ctx, store.InsertPlayerEventParams{
			UserID:    userID,
			SessionID: pgtype.Int8{Int64: sessionID, Valid: sessionID > 0},
			EventType: "launch",
			GameID:    e.GameID,
			Context:   ctxArg,
		})
		if err != nil {
			s.logger.Warn("recs: event insert", "err", err, "game_id", e.GameID)
			continue
		}
		accepted++
	}
	return accepted, nil
}

// SetPersonalize persists the lobby personalization preference.
func (s *Service) SetPersonalize(ctx context.Context, userID int64, enabled bool) (bool, error) {
	v, err := s.repo().UpsertUserPrefs(ctx, store.UpsertUserPrefsParams{UserID: userID, PersonalizeEnabled: enabled})
	if err != nil {
		return false, err
	}
	s.Invalidate(userID)
	return v, nil
}
