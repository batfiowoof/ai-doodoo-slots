import { NextRequest, NextResponse } from "next/server";
import {
  KC_COOKIE,
  KC_STATE_COOKIE,
  encodeTokenSet,
  exchangeCode,
  kcCookieOptions,
  safeNext,
} from "@/lib/oidc";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

// GET /auth/callback — completes the OIDC flow: validates state, exchanges
// the code (PKCE), upgrades the guest in place via the Go API, and stores
// the token set in an httpOnly cookie.
export async function GET(req: NextRequest) {
  const url = req.nextUrl;
  const error = url.searchParams.get("error");
  if (error) {
    return NextResponse.redirect(new URL(`/?auth=${encodeURIComponent(error)}`, req.url));
  }

  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");
  const stored = req.cookies.get(KC_STATE_COOKIE)?.value;
  if (!code || !state || !stored) {
    return NextResponse.redirect(new URL("/?auth=missing_state", req.url));
  }

  let pending: { state: string; verifier: string; next: string };
  try {
    pending = JSON.parse(stored);
  } catch {
    return NextResponse.redirect(new URL("/?auth=bad_state", req.url));
  }
  if (pending.state !== state) {
    return NextResponse.redirect(new URL("/?auth=state_mismatch", req.url));
  }

  let tokens;
  try {
    tokens = await exchangeCode({ code, verifier: pending.verifier });
  } catch {
    return NextResponse.redirect(new URL("/?auth=token_exchange_failed", req.url));
  }

  // Establish the app-side identity. The guest session cookie (if any) is
  // forwarded so the Go API upgrades that user row in place — wallet, bets,
  // history, and seeds are retained.
  try {
    await fetch(`${API_URL}/api/v1/auth/keycloak/session`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        ...(req.cookies.get("retro_session")
          ? { cookie: `retro_session=${req.cookies.get("retro_session")!.value}` }
          : {}),
      },
      body: JSON.stringify({ accessToken: tokens.accessToken }),
      cache: "no-store",
    });
  } catch {
    // Non-fatal: the identity resolves lazily on the next Bearer request.
  }

  const res = NextResponse.redirect(new URL(safeNext(pending.next), req.url));
  const maxAge = Math.max(60, Math.floor((tokens.expiresAtMs - Date.now()) / 1000) + 86_400);
  res.cookies.set(KC_COOKIE, encodeTokenSet(tokens), kcCookieOptions(maxAge));
  res.cookies.set(KC_STATE_COOKIE, "", { ...kcCookieOptions(0), maxAge: 0 });
  // The guest session was consumed by the upgrade (or is stale); drop it.
  res.cookies.set("retro_session", "", { ...kcCookieOptions(0), maxAge: 0 });
  return res;
}
