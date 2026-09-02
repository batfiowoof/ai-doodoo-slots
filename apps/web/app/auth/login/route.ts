import { NextRequest, NextResponse } from "next/server";
import {
  KC_STATE_COOKIE,
  authorizationURL,
  kcCookieOptions,
  keycloakConfigured,
  pkcePair,
  randomState,
  safeNext,
} from "@/lib/oidc";

// GET /auth/login — starts the OIDC authorization-code flow with PKCE.
export async function GET(req: NextRequest) {
  if (!keycloakConfigured()) {
    return NextResponse.redirect(new URL("/?auth=unconfigured", req.url));
  }

  const next = safeNext(req.nextUrl.searchParams.get("next"));
  const { verifier, challenge } = pkcePair();
  const state = randomState();

  const res = NextResponse.redirect(authorizationURL({ state, challenge }));
  res.cookies.set(KC_STATE_COOKIE, JSON.stringify({ state, verifier, next }), kcCookieOptions(600));
  return res;
}
