# Retro Casino

A play-money arcade casino with a pixel-art presentation. Go backend owns
all game logic and money; Next.js renders outcomes. Slots, crash, stateful
blackjack, and multiplayer Texas Hold'em all ship behind one contract.

**This is a social casino: credits have no cash value. There is no deposit
path and no cash-out.**

## Run it (Docker)

```sh
cp .env.example .env        # set ADMIN_EMAILS / OPENROUTER_API_KEY (optional)
docker compose up -d --build
```

- Web: http://localhost:3000 — the machine, paytable, history, theme machine
- API: http://localhost:8080 — behind the Next.js BFF in normal use

Opening the page creates a guest session with 1000 credits automatically.

## Accounts

Guest play needs no account: opening the page creates a guest session with
1000 credits automatically. Registered users authenticate through **Keycloak**
(OpenID Connect, authorization code + PKCE). The first Keycloak login
upgrades the current guest **in place** — the same user row keeps its
wallet, bets, history, and seeds.

Login: the web BFF at `/auth/login` redirects to Keycloak and stores the
token set in an httpOnly cookie; the BFF proxy attaches the access token as
a Bearer credential to API calls. Roles (`player`, `moderator`, `admin`)
come from Keycloak realm roles and are synced onto the local user row;
bans and self-exclusion stay local.

Test users (auto-imported realm, sign-in at http://localhost:8081):

| User | Password | Role |
|---|---|---|
| `player` | `player123` | player |
| `moderator` | `mod123` | moderator |
| `admin` | `admin123` | admin |

## Architecture

```
apps/web            Next.js 16 (App Router, Tailwind v4), thin BFF proxy
services/backend    one Go module
  cmd/api           stateless HTTP (:8080)
  cmd/gameserver    stateful loops: crash rounds + poker tables (:8082)
  cmd/migrate       goose migrations (embedded)
  cmd/wscheck       live contract checks: crash sync, lobby, holdem tables
  internal/
    auth            guest sessions (opaque tokens), Keycloak OIDC (JWKS),
                    identity provisioning, guest upgrade in place
    wallet          append-only ledger, FOR UPDATE locking, idempotency
    fair            provably-fair byte stream (personal + chain modes),
                    commit-reveal chain generation and reveal
    cards           shared card primitives: deck, Fisher-Yates over the fair
                    stream (rejection sampling, no modulo bias), blackjack
                    totals, 7-card poker evaluator
    game, game/slots  engine interface, registry, 3x3 five-payline slots
    game/blackjack  stateful single-player engine (deal/hit/stand/double)
    game/poker      multiplayer Hold'em reducer: blinds, min-raise, side
                    pots, showdown — pure and replayable
    hand            the stateful blackjack transactions (hand spans several
                    requests; deck replays from the seed triple + action log)
    table           poker table runner (one goroutine per room), persister
                    (buy-in/cash-out/settlement), socket intake
    round           crash round runner + intake (shared-round pattern)
    bus             in-process event publisher (Redis swap later)
    ws              authenticated WebSocket hub, rooms, game-action routing,
                    1Hz lobby summaries, bounded send buffers, session
                    revocation closes sockets
    play            the single transaction every instant bet runs inside
    theme           OpenRouter client, strict sprite validation
    admin           RBAC, ban/self-exclusion, audit log
    store           sqlc output (pgx/v5)
    clock           injectable time; no time.Now anywhere else (enforced by test)
```

Process shape: `api` (stateless, :8080) and `gameserver` (round loops +
tables + realtime, :8082) both speak the same surface; the WebSocket
endpoint is `GET /api/v1/ws`, authenticated by the exact same session/Bearer
path as HTTP. Bans, self-exclusion, and session revocation close open
sockets.

## The games

| Game | Kind | Where | Fairness source |
|---|---|---|---|
| Slots (3 cabinets) | instant, single call | `POST /games/{id}/play` | personal seed pair |
| Crash (3 rooms) | shared round, realtime | rooms over WS | commit-reveal chain link |
| Blackjack | stateful, single player | `deal` + `hands/{id}/action` | personal seed pair |
| Texas Hold'em | shared table, realtime | rooms over WS | commit-reveal chain link |

### Blackjack

One hand spans several HTTP transactions, so the whole 52-card deck order is
fixed at deal time (Fisher-Yates over the personal stream) and every later
draw is the next card of that deck. The deck itself is never trusted from
storage — every action replays the hand from the seed triple plus the action
log, which is exactly what the verifier recomputes. Rules: dealer stands on
all 17s, naturals pay 3:2, double on the first two cards only, no split,
one active hand per player (idle hands auto-stand after 5 minutes). The
hole card is withheld from API responses until the hand completes.

### Texas Hold'em

Ring games (`holdem-1/2/3`, stakes mirror the crash tiers): buy in once and
chips live in house-held stacks; blinds and bets move inside the table;
leaving cashes the remaining stack back to the wallet. No-limit betting
with min-raise rules, proper side pots, 20-second turn timer (auto
check/fold), full reveal at settlement. Zero-sum, no rake — every hand's
payouts sum to its contributions. The whole deck is shuffled once per hand
from that hand's committed chain link; the persisted hand record (rounds
result) carries every seat's cards, the action log, and pot splits, so any
hand replays from public material once the link is revealed.

Socket protocol for table rooms (`game_action` messages, acked with
`game_ack` or `error`):

- `{action: "buy_in", amount, idempotencyKey}` — seat + debit (20 BB min,
  room max cap)
- `{action: "rebuy", amount, idempotencyKey}` — queued to hand start
- `{action: "fold" | "check" | "call"}` / `{action: "bet" | "raise", amount}`
  (raise-to semantics)
- `{action: "leave"}` — fold out of hand if needed, cash out at settlement
- `{action: "state"}` — personalized snapshot (your hole cards)

Room broadcasts: `table_state` (masked — no hole cards), `hand_started`,
`hand_result` (full reveal), `cash_out`. Crash rooms keep their
`round_tick`/`round_state`/`round_result` protocol; both route through the
same per-room handler registration on the hub.

### Provably fair

Every bet derives from `HMAC-SHA256(serverSeed, clientSeed + ":" + nonce)`
consumed as a byte stream (block extension rule documented in the `fair`
package doc). Only the sha256 of the server seed is published; rotation
reveals it. The /verify page recomputes outcomes entirely in the browser —
slot grids and blackjack deck order alike (the fixtures in
`apps/web/scripts/check-verify.ts` were generated by the Go engines and
must keep matching).

Shared-round games (crash, hold'em) derive from a commit-reveal hash chain
instead: `seed[i] = sha256(seed[i+1])`, one link committed per round at
open and revealed at settlement, so nobody — including the operator — can
reorder or skip outcomes. For hold'em, the stream salt is
`round:<roundId>`, binding each deck to its persisted round.

## Development

```sh
docker compose up -d postgres
cd services/backend
go run ./cmd/migrate up        # apply migrations
go run ./cmd/api               # API on :8080
cd ../../apps/web
npm install && npm run dev     # web on :3000 (API_URL defaults to :8080)
```

Backend tests (integration tests need the dockerized Postgres):

```sh
cd services/backend && go test ./...
cd ../apps/web && npm run verify:fixture   # verifier fixtures
```

Live contract checks against a running gameserver (:8082):

```sh
cd services/backend
go run ./cmd/wscheck sync    # crash: two sockets see identical ticks, rejoin snapshot
go run ./cmd/wscheck lobby   # lobby gets ~1 summary/sec, zero round ticks
go run ./cmd/wscheck holdem  # poker: identical table state, hand settles, masking + reconnect
```

For browser WebSocket access from the dev web server, start the gameserver
with `WS_ALLOWED_ORIGINS=http://localhost:3000`.

## API contract

`openapi.yaml` is the source of truth; the web client's types are generated
from it (`npm run -w apps/web gen:api` or `npx openapi-typescript
../../openapi.yaml -o lib/api-schema.ts`).

## Configuration

| Variable | Where | Purpose |
|---|---|---|
| `DATABASE_URL` | api | Postgres DSN (default: compose network) |
| `COOKIE_SECURE` | api, web | Set `true` behind HTTPS |
| `KEYCLOAK_ISSUER` | api, web | Public realm URL, e.g. `http://localhost:8081/realms/retro-casino`; unset = guest-only mode |
| `KEYCLOAK_CLIENT_ID` | api, web | OIDC client (default `web`) |
| `KEYCLOAK_JWKS_URL` / `KEYCLOAK_TOKEN_URL` / `KEYCLOAK_AUTH_URL` / `KEYCLOAK_END_SESSION_URL` | api, web | Internal-network endpoint overrides (compose) |
| `KEYCLOAK_PUBLIC_AUTH_URL` / `KEYCLOAK_PUBLIC_END_SESSION_URL` | web | Browser-facing redirect targets (compose) |
| `OPENROUTER_API_KEY` | api | Enables AI theme generation |
| `API_URL` | web | Upstream Go API for the BFF proxy |
| `WS_ALLOWED_ORIGINS` | gameserver | Comma-separated origins allowed to upgrade WebSockets (dev: `http://localhost:3000`) |
