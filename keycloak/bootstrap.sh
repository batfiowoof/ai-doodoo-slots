#!/usr/bin/env bash
# Post-import bootstrap for the retro-casino realm. `--import-realm` skips
# existing objects, so parts of the config cannot live in the realm JSON:
# the service-account user is auto-created by the client import (the JSON
# user entry is skipped), and KC 26 manages custom user attributes through
# the User Profile API. Run this once after `docker compose up keycloak`
# on a fresh volume. Idempotent: safe to re-run.
#
#   KEYCLOAK_HOST=http://localhost:8081 KEYCLOAK_ADMIN=admin \
#   KEYCLOAK_ADMIN_PASSWORD=admin bash keycloak/bootstrap.sh
set -euo pipefail

KC_HOST="${KEYCLOAK_HOST:-http://localhost:8081}"
KC_ADMIN="${KEYCLOAK_ADMIN:-admin}"
KC_ADMIN_PASSWORD="${KEYCLOAK_ADMIN_PASSWORD:-admin}"
REALM="retro-casino"

command -v python >/dev/null || { echo "python is required" >&2; exit 1; }

MTOK=$(curl -sf -X POST "$KC_HOST/realms/master/protocol/openid-connect/token" \
  -d "client_id=admin-cli" -d "grant_type=password" \
  -d "username=$KC_ADMIN" -d "password=$KC_ADMIN_PASSWORD" |
  python -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
AUTH="Authorization: Bearer $MTOK"

# 1. Declare the profile attributes the API writes back (KC 26 drops
#    attributes that are not in the User Profile schema).
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
curl -sf "$KC_HOST/admin/realms/$REALM/users/profile" -H "$AUTH" -o "$TMP/profile.json"
python - "$TMP/profile.json" <<'PY'
import json, sys
p = sys.argv[1]
d = json.load(open(p, encoding="utf-8"))
names = [a["name"] for a in d["attributes"]]
changed = False
for name in ("displayName", "avatarPreset", "avatarVersion"):
    if name not in names:
        d["attributes"].append({
            "name": name,
            "displayName": name,
            "multivalued": False,
            "annotations": {},
            "validations": {},
            "permissions": {"view": ["admin", "user"], "edit": ["admin", "user"]},
        })
        changed = True
if changed:
    json.dump(d, open(p, "w", encoding="utf-8"))
print("profile schema:", "updated" if changed else "already declared")
PY
curl -sf -X PUT "$KC_HOST/admin/realms/$REALM/users/profile" -H "$AUTH" \
  -H "content-type: application/json" --data-binary "@$TMP/profile.json" \
  -o /dev/null -w "profile schema put: %{http_code}\n"

# 2. Give the retro-api service account its realm-management roles (the
#    client import auto-creates the service user, so realm JSON misses it).
RMID=$(curl -sf "$KC_HOST/admin/realms/$REALM/clients?clientId=realm-management" -H "$AUTH" |
  python -c "import json,sys; print(json.load(sys.stdin)[0]['id'])")
SAID=$(curl -sf "$KC_HOST/admin/realms/$REALM/users?username=service-account-retro-api&exact=true" -H "$AUTH" |
  python -c "import json,sys; print(json.load(sys.stdin)[0]['id'])")
ROLES=$(curl -sf "$KC_HOST/admin/realms/$REALM/clients/$RMID/roles" -H "$AUTH" |
  python -c "import json,sys; rs=json.load(sys.stdin); print(json.dumps([{'id':r['id'],'name':r['name']} for r in rs if r['name'] in ('view-users','manage-users')]))")
curl -sf -X POST "$KC_HOST/admin/realms/$REALM/users/$SAID/role-mappings/clients/$RMID" \
  -H "$AUTH" -H "content-type: application/json" -d "$ROLES" \
  -o /dev/null -w "service roles put: %{http_code}\n"

echo "bootstrap done"
