# Retro Casino

A play-money arcade casino with a pixel-art presentation. Go backend owns
all game logic and money; Next.js renders outcomes. Slots ships first.

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

## Architecture

```
apps/web            Next.js 16 (App Router, Tailwind v4), thin BFF proxy
services/backend    one Go module
  cmd/api           stateless HTTP (:8080)
  cmd/gameserver    stateful round loops (crash, later)
  cmd/migrate       goose migrations (embedded)
  internal/
    auth            sessions (opaque tokens), passwords (argon2id), accounts
    wallet          append-only ledger, FOR UPDATE locking, idempotency
    fair            provably-fair byte stream (personal + chain modes)
    game, game/slots  engine interface, registry, 3x3 five-payline slots
    play            the single transaction every bet runs inside
    theme           OpenRouter client, strict sprite validation
    admin           RBAC, ban/self-exclusion, audit log
    store           sqlc output (pgx/v5)
    clock           injectable time; no time.Now anywhere else (enforced by test)
```

### Provably fair

Every bet derives from `HMAC-SHA256(serverSeed, clientSeed + ":" + nonce)`
consumed as a byte stream (block extension rule documented in the `fair`
package doc). Only the sha256 of the server seed is published; rotation
reveals it. The /verify page recomputes outcomes entirely in the browser.

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

## API contract

`openapi.yaml` is the source of truth; the web client's types are generated
from it (`npm run -w apps/web gen:api` or `npx openapi-typescript
../../openapi.yaml -o lib/api-schema.ts`).

## Configuration

| Variable | Where | Purpose |
|---|---|---|
| `DATABASE_URL` | api | Postgres DSN (default: compose network) |
| `COOKIE_SECURE` | api | Set `true` behind HTTPS |
| `ADMIN_EMAILS` | api | Emails granted the admin role |
| `OPENROUTER_API_KEY` | api | Enables AI theme generation |
| `API_URL` | web | Upstream Go API for the BFF proxy |
