# AGENTS.md — Retro Casino

Play-money arcade casino: **Go backend owns all game logic and money; Next.js only renders outcomes.** Credits have no cash value — no deposit path, no cash-out. Never add real-money features.

## Layout

- `apps/web/` — Next.js 16 + React 19 + Tailwind 4 frontend (BFF proxies `/api/v1/*` to the Go api; auth via Keycloak OIDC + PKCE in httpOnly cookies)
- `services/backend/` — one Go module (`github.com/ai-doodoo-slots/services/backend`) with multiple `cmd/` entrypoints: `api` (stateless HTTP), `gameserver` (round games + WebSocket social layer), `migrate`, plus check bots (`wscheck`, `socialcheck`)
- `services/backend/db/migrations/` — numbered SQL migrations, run automatically before the api starts (`migrate up`)
- `keycloak/` — realm import (test users: `player/player123`, etc. — see README)
- `openapi.yaml` — API contract

## Commands

```sh
docker compose up -d --build   # full stack: web :3000, api :8080, keycloak :8081, gameserver :8082, postgres :55432
```
- Web: `npx tsc --noEmit` (typecheck), `npm run build`, `npm run lint` (run in `apps/web/`)
- Backend: `go vet ./...`, `go test ./...` (in `services/backend/`; tests using `internal/testdb` need the Postgres container up)

**Backend changes require rebuilding the api/gameserver containers; web changes require rebuilding the web container** (`docker compose up -d --build <service>`). Docker Desktop must be running. Do not commit unless explicitly asked — the owner usually commits their own work.

## Architecture rules (do not break these)

- **Every outcome is decided server-side** from the provably-fair stream (`internal/fair`: server seed hash + client seed + nonce, one nonce per bet). The client only animates what the server recorded — "what you watch is what paid" is a hard invariant.
- **Money moves only in `internal/play` service transactions**: wallet row locked `FOR UPDATE`, idempotency-key replay check, nonce increment, engine call, ledger entries, materialized balance — one commit. Bets send an `idempotencyKey` (uuid) and are rate-limited (60 plays / 10 s / user).
- **Game split**: instant games (slots, dice, plinko, mines start) settle synchronously over HTTP via `internal/play`; round/stateful games (crash, roulette, poker, blackjack) live in the `gameserver` process with WebSocket (`internal/ws`, social/big-win events ride pg_notify → hub → `TopicWins`).
- **Instant game = engine + params pattern**: add `internal/game/<name>` with `PlayWithParams`, seed catalog/metadata, and mirror any multiplier tables on BOTH sides (Go engine is authoritative; e.g. `TABLES` in `apps/web/components/PlinkoScreen.tsx` is a copy — values must match exactly).
- **Animations must land exactly on the paid outcome** (see `apps/web/lib/plinkoPhysics.ts` for the steered-physics contract: free physics biased at each step toward the server's recorded path).
- Auth: guest sessions auto-created with 1000 credits; Keycloak login upgrades the guest row in place. Bans/self-exclusion are checked in the play path.

## Conventions

- UI is a retro-neon pixel casino: canvas + `requestAnimationFrame` for games (`lib/plinkoPhysics.ts`, `lib/crashScene.ts`), CSS keyframe juice, sounds through `lib/sound.ts` (games should be sound-backed), `Silkscreen` display font via `var(--font-display)`. Keep effects large and readable; favor board/console layouts.
- React Query is the client cache; `usePlay` (lib/api.ts) is the single bet path and patches balance/fairness/history caches from authoritative responses.
- `.zcode/` and `.serena/` are gitignored local tooling state (plans, sim harness `sim-run.mjs`, Serena MCP config + memories). Do not commit them.
