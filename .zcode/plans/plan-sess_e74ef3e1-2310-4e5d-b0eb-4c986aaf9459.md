# Casino expansion: single crash, custom bet inputs, Dice + Plinko + Mines

## Finding on the crash "shared websocket" (corrects the premise)

Results are **not** cross-broadcast between crash rooms — each room has its own runner and `hub.BroadcastRoom` (`services/backend/internal/ws/hub.go:343`) targets only that room's subscribers; each browser page opens its own socket joined to one slug (max 1 room/connection). What **is** shared: one `/api/v1/ws` endpoint whose single `Hub.Run` goroutine consumes one `"rooms"` bus subscription (`hub.go:135-137`) backed by a **128-event drop-on-full buffer** (`internal/bus/bus.go:59`) — a common fan-in for all 9 rooms where slow consumption silently drops events.

Per your choice we consolidate to **one crash room** (removes the redundant rooms and most of the fan-in pressure) plus a buffer hardening.

## Stream A — Single crash room (small)

1. **Migration `00012_single_crash.sql`**: `UPDATE rooms SET is_active = false WHERE slug IN ('crash-2','crash-3')` (keeps round/bet history intact) and `UPDATE rooms SET min_bet = 5, max_bet = 10000 WHERE slug = 'crash-1'` (absorbs the removed tiers; server-side tier enforcement at `internal/round/intake.go:222-224` picks this up automatically).
2. **Bus hardening**: `subBuffer` 128 → 1024 in `internal/bus/bus.go:59` (events are dropped, never blocked — give the single rooms consumer headroom).
3. **Lobby root ring** (`apps/web/app/page.tsx:195-197`): groups with exactly one child render that child directly on the root ring (keeping the group's LIVE badge) instead of a pointless drill-down; drill-down stays for 2+ table groups. The crash room card then sits on the ring itself.

Verify: `docker compose` up, `wscheck sync` + `lobby` modes against crash-1, lobby shows one CRASH card with `BETS 5–10,000`.

## Stream B — Custom input bets (medium)

Server already accepts arbitrary integer amounts for crash/roulette within the room tier (`intake.go:213-224`); only slots/blackjack hard-code step membership, and no game except poker has a free-form field.

**Backend**
1. `game.Listing` (game.go:44-51) gains `MinBet/MaxBet` (json `minBet`/`maxBet`), populated in `Listings()` via an optional `BetLimits() (int64,int64)` interface on engines, and set on the blackjack listing (`httpapi/server.go:127`) — so the client can clamp everywhere.
2. Slots: `Config` gains `MinBet/MaxBet` (set per machine in `internal/game/slots/games.go`, e.g. 5..10,000); `ValidateBet` (slots.go:62-69) becomes a range check. `BetSteps` remain as UI presets.
3. Blackjack engine: same range-check change (`internal/game/blackjack` `ValidateBet`, called from `internal/hand/service.go:135`).

**Frontend**
4. New shared `apps/web/components/BetInput.tsx` (beside `Chip.tsx`): preset chip row (steps), free-form numeric field (digit-filtered like the poker raise input), ½ / 2× / MAX buttons, clamp to [min,max] on blur, shake + error sound when out of range, `accent` prop so it adopts each game's theme, `data-testid`s.
5. Integrate into all bet surfaces: CrashRoom "MISSION CONTROL" (replace the `BET_STEPS`-only row, ~844-873), RouletteRoom chip console (typed amount sets the chip denomination, ~976-989), Cabinet bet-per-spin (~641-671), BlackjackTable rail (~649-666). Poker already free-form.

Verify: engine tests for new validation; GUI pass placing odd amounts (e.g. 37 CR) in every game; out-of-tier bets on rooms rejected with `out_of_tier`; RTP unaffected (payouts are bet multipliers).

## Stream C — New games: Dice, Plinko, Mines

### C0. Player-parameter instant games (enabler, small)
Dice and Plinko need player choices, but `Game.Play(stream, bet)` has no params channel. Add an optional interface in `internal/game/game.go`:

```go
type ParamGame interface {
    Game
    ValidateParams(raw json.RawMessage) error
    PlayWithParams(s *fair.Stream, betCredits int64, params json.RawMessage) (Outcome, error)
}
```

`play/service.go:71` `Play()` accepts `params json.RawMessage` (handler `handlers_play.go:37` adds the field): if the game implements `ParamGame`, params are required + validated and `PlayWithParams` runs; otherwise classic `Play`. The accepted params go into the outcome payload so every play stays verifiable.

### C1. Dice (instant, small)
- `internal/game/dice/dice.go`: roll = `floor(stream.Float()×10000)/100` ∈ [0,100). Params `{direction:"under"|"over", target:2..98}`. Win: under → `roll < target`, over → `roll > target`; chance% = `target` (under) or `100−target` (over); payout = `floor(bet × 99/chance)` → 1% edge. `TheoreticalRTP() = 0.99`. Register at `httpapi/server.go:115`. Tests: param edges, payout table, RTP simulation gate (same pattern as slots).
- **DiceScreen** at `/play/dice` (new branch in `app/play/[id]/page.tsx`): neon target slider (2–98), UNDER/OVER toggle, live CHANCE × MULTIPLIER × PROFIT readouts, big ROLL button, recent-rolls strip, win pop + existing sounds (`winTick`/`bigWin`/`error`). Accent gold `#ffd21f`. BetInput integrated.

### C2. Plinko (instant, medium)
- `internal/game/plinko/plinko.go`: rows ∈ {8,12,16} × risk ∈ {low,medium,high}; one stream bit per row decides left/right, bucket = count of rights; multiplier tables (Stake-style, e.g. rows 8 high: `[29,4,1.5,0.3,0.2,0.3,1.5,4,29]`); payout = `floor(bet × mult)`. `TheoreticalRTP()` exact via binomial Σ C(n,k)/2ⁿ·multₖ, tables normalized to 0.99; simulation test gates every rows/risk combo.
- **PlinkoScreen**: canvas peg board (rAF, patterns from `lib/crashScene.ts` / roulette wheel), ball drop with pitch-rising peg ticks, bucket glow + payout pop, rows/risk selectors, recent-buckets strip. Accent violet `#b18cff`. BetInput integrated.

### C3. Mines (stateful, blackjack pattern, largest)
- **Migration `00013_mines.sql`**: `mines_rounds` (user, bet, mine_count, mine_positions JSONB, revealed JSONB, state active/cashed/busted, payout, fairness columns) — modeled on `00008_blackjack_hands.sql`.
- Engine `internal/game/mines/mines.go`: 25 tiles, mine_count ∈ {1,3,5,10,24}, positions drawn from the fairness stream; multiplier after k safe reveals = `0.99 × C(25,k)/C(25−M,k)` (2dp) → 0.99 RTP at any cashout point.
- Service + handlers (pattern: `internal/hand/service.go`, `handlers_hand.go`): `POST /api/v1/games/mines/start {betCredits, mineCount, idempotencyKey}` (range-validated debit), `.../reveal {tile, actionKey}`, `.../cashout {actionKey}`. `RegisterListing("mines")`. On round end, insert a settled round + bet row with revealed positions for verification.
- **MinesScreen**: 5×5 neon tile grid, mine-count selector, flip animations on reveal, multiplier ladder (current/next), live CASH OUT button, explosion + shake on bust, big-win celebration on cashout. Accent red `#f2643d`. BetInput on start.

### Lobby wiring for the new games
Dice/plinko/mines have no paytable, so they appear on the root ring automatically via `tableGames` → `gameNode` (`page.tsx:530-554`); extend `gameNode` with a per-game override map giving each its own accent, status line, and mini-art (`MiniDice`, `MiniPeg`, `MiniMine` CSS/SVG components in the same file, like `MiniWheel`/`MiniFelt`). Root ring grows to 8 nodes; `gameEllipse(n)` already scales radii by count.

## Out of scope / later
- Refactor the 4 duplicated ws clients (~350 lines) into a shared `useRoomSocket` hook — regression risk across 3 working rooms, do separately.
- Baccarat / keno / hi-lo candidates, Redis bus swap when a second gameserver exists.

## Rollout order & verification
A (crash) → B (custom bets) → C0+C1 (Dice) → C2 (Plinko) → C3 (Mines), each independently verifiable.
- Backend: `go build ./... && go test ./...` (RTP sims gate each engine; params/mines service tests).
- Live: docker compose up; `wscheck sync`/`lobby`; browser GUI pass (dispatch `element.click()` via evaluate, measure viewport via `evaluate` — known quirks) covering: single crash card, custom bets in all four games, full rounds of dice/plinko/mines, root-ring navigation.