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
  cmd/gameserver    stateful round loops (crash, later)
  cmd/migrate       goose migrations (embedded)
  internal/
    auth            guest sessions (opaque tokens), Keycloak OIDC (JWKS),
                    identity provisioning, guest upgrade in place
    wallet          append-only ledger, FOR UPDATE locking, idempotency
    fair            provably-fair byte stream (personal + chain modes),
                    commit-reveal chain generation and reveal
    game, game/slots  engine interface, registry, 3x3 five-payline slots
    game            RoundGame interface for shared-round games (crash next)
    bus             in-process event publisher (Redis swap later)
    ws              authenticated WebSocket hub, rooms, 1Hz lobby summaries,
                    bounded send buffers, session revocation closes sockets
    play            the single transaction every bet runs inside
    theme           OpenRouter client, strict sprite validation
    admin           RBAC, ban/self-exclusion, audit log
    store           sqlc output (pgx/v5)
    clock           injectable time; no time.Now anywhere else (enforced by test)
```

Process shape: `api` (stateless, :8080) and `gameserver` (round loops +
realtime, :8082) both speak the same surface; the WebSocket endpoint is
`GET /api/v1/ws`, authenticated by the exact same session/Bearer path as
HTTP. Bans, self-exclusion, and session revocation close open sockets.

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
| `COOKIE_SECURE` | api, web | Set `true` behind HTTPS |
| `KEYCLOAK_ISSUER` | api, web | Public realm URL, e.g. `http://localhost:8081/realms/retro-casino`; unset = guest-only mode |
| `KEYCLOAK_CLIENT_ID` | api, web | OIDC client (default `web`) |
| `KEYCLOAK_JWKS_URL` / `KEYCLOAK_TOKEN_URL` / `KEYCLOAK_AUTH_URL` / `KEYCLOAK_END_SESSION_URL` | api, web | Internal-network endpoint overrides (compose) |
| `KEYCLOAK_PUBLIC_AUTH_URL` / `KEYCLOAK_PUBLIC_END_SESSION_URL` | web | Browser-facing redirect targets (compose) |
| `OPENROUTER_API_KEY` | api | Enables AI theme generation |
| `API_URL` | web | Upstream Go API for the BFF proxy |
