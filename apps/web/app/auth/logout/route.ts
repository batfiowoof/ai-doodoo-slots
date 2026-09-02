import { NextRequest, NextResponse } from "next/server";
import { KC_COOKIE, decodeTokenSet, endSessionURL, kcCookieOptions } from "@/lib/oidc";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

// GET /auth/logout — clears the local token cookie, revokes any guest
// session, and redirects through Keycloak's end-session endpoint so the SSO
// session dies too (without an id_token hint Keycloak asks for confirmation).
export async function GET(req: NextRequest) {
  // Best-effort guest session revocation (no-op for Keycloak users).
  const guest = req.cookies.get("retro_session");
  if (guest) {
    try {
      await fetch(`${API_URL}/api/v1/auth/logout`, {
        method: "POST",
        headers: { cookie: `retro_session=${guest.value}` },
        cache: "no-store",
      });
    } catch {
      // Ignore: logout proceeds regardless.
    }
  }

  const tokens = decodeTokenSet(req.cookies.get(KC_COOKIE)?.value ?? "");
  const res = NextResponse.redirect(endSessionURL(tokens?.idToken));
  res.cookies.set(KC_COOKIE, "", { ...kcCookieOptions(0), maxAge: 0 });
  return res;
}
