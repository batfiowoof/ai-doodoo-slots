// Server-only OIDC helpers for the Keycloak integration. Tokens live in an
// httpOnly cookie; the browser never sees them. The BFF attaches the access
// token as a Bearer credential when proxying to the Go API.
import "server-only";

import crypto from "node:crypto";

const ISSUER = process.env.KEYCLOAK_ISSUER ?? "http://localhost:8081/realms/retro-casino";
const CLIENT_ID = process.env.KEYCLOAK_CLIENT_ID ?? "web";
const REDIRECT_URI = process.env.KEYCLOAK_REDIRECT_URI ?? "http://localhost:3000/auth/callback";
const POST_LOGOUT_URI = process.env.KEYCLOAK_POST_LOGOUT_URI ?? "http://localhost:3000/";
const COOKIE_SECURE = process.env.COOKIE_SECURE === "true" || process.env.HTTPS === "true";

// Internal (server-side) endpoints, overridable for compose deployments
// where the container cannot reach the public frontend URL.
const AUTH_URL = process.env.KEYCLOAK_AUTH_URL ?? `${ISSUER}/protocol/openid-connect/auth`;
const TOKEN_URL = process.env.KEYCLOAK_TOKEN_URL ?? `${ISSUER}/protocol/openid-connect/token`;
const END_SESSION_URL = process.env.KEYCLOAK_END_SESSION_URL ?? `${ISSUER}/protocol/openid-connect/logout`;

// Browser-facing endpoints used for redirects.
const PUBLIC_AUTH_URL = process.env.KEYCLOAK_PUBLIC_AUTH_URL ?? AUTH_URL;
const PUBLIC_END_SESSION_URL = process.env.KEYCLOAK_PUBLIC_END_SESSION_URL ?? END_SESSION_URL;

export const KC_COOKIE = "retro_kc";
export const KC_STATE_COOKIE = "retro_oidc";

export function keycloakConfigured(): boolean {
  return Boolean(process.env.KEYCLOAK_ISSUER);
}

export interface TokenSet {
  accessToken: string;
  refreshToken?: string;
  idToken?: string;
  expiresAtMs: number;
}

/**
 * Cookie payload. The idToken is deliberately excluded: three JWTs exceed
 * the ~4KB per-cookie limit browsers and HTTP clients enforce, and the id
 * token is only an optional logout hint. Access + refresh keeps the cookie
 * comfortably small.
 */
export function encodeTokenSet(t: TokenSet): string {
  return Buffer.from(
    JSON.stringify({
      accessToken: t.accessToken,
      refreshToken: t.refreshToken,
      expiresAtMs: t.expiresAtMs,
    }),
    "utf8",
  ).toString("base64url");
}

export function decodeTokenSet(raw: string): TokenSet | null {
  try {
    const t = JSON.parse(Buffer.from(raw, "base64url").toString("utf8"));
    if (typeof t.accessToken !== "string" || typeof t.expiresAtMs !== "number") return null;
    return t as TokenSet;
  } catch {
    return null;
  }
}

export function kcCookieOptions(maxAgeSeconds: number) {
  return {
    httpOnly: true,
    sameSite: "lax" as const,
    secure: COOKIE_SECURE,
    path: "/",
    maxAge: maxAgeSeconds,
  };
}

// PKCE --------------------------------------------------------------------

function b64url(buf: Buffer): string {
  return buf.toString("base64url");
}

export function pkcePair(): { verifier: string; challenge: string } {
  const verifier = b64url(crypto.randomBytes(48));
  const challenge = b64url(crypto.createHash("sha256").update(verifier).digest());
  return { verifier, challenge };
}

export function randomState(): string {
  return b64url(crypto.randomBytes(24));
}

// Authorization redirect ---------------------------------------------------

export function authorizationURL(params: { state: string; challenge: string }): string {
  const u = new URL(PUBLIC_AUTH_URL);
  u.searchParams.set("response_type", "code");
  u.searchParams.set("client_id", CLIENT_ID);
  u.searchParams.set("redirect_uri", REDIRECT_URI);
  u.searchParams.set("scope", "openid profile email");
  u.searchParams.set("state", params.state);
  u.searchParams.set("code_challenge", params.challenge);
  u.searchParams.set("code_challenge_method", "S256");
  return u.toString();
}

export function endSessionURL(idTokenHint?: string): string {
  const u = new URL(PUBLIC_END_SESSION_URL);
  if (idTokenHint) u.searchParams.set("id_token_hint", idTokenHint);
  u.searchParams.set("post_logout_redirect_uri", POST_LOGOUT_URI);
  return u.toString();
}

// Token endpoints ------------------------------------------------------------

interface TokenResponse {
  access_token: string;
  refresh_token?: string;
  id_token?: string;
  expires_in: number;
}

async function tokenRequest(body: Record<string, string>): Promise<TokenSet> {
  const res = await fetch(TOKEN_URL, {
    method: "POST",
    headers: { "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams(body).toString(),
    cache: "no-store",
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`token endpoint ${res.status}: ${text.slice(0, 200)}`);
  }
  const data = (await res.json()) as TokenResponse;
  return {
    accessToken: data.access_token,
    refreshToken: data.refresh_token,
    idToken: data.id_token,
    expiresAtMs: Date.now() + data.expires_in * 1000,
  };
}

export function exchangeCode(params: { code: string; verifier: string }): Promise<TokenSet> {
  return tokenRequest({
    grant_type: "authorization_code",
    code: params.code,
    redirect_uri: REDIRECT_URI,
    client_id: CLIENT_ID,
    code_verifier: params.verifier,
  });
}

export function refreshTokens(refreshToken: string): Promise<TokenSet> {
  return tokenRequest({
    grant_type: "refresh_token",
    refresh_token: refreshToken,
    client_id: CLIENT_ID,
  });
}

/** Returns true when the path is a safe relative redirect target. */
export function safeNext(raw: string | null | undefined): string {
  if (raw && raw.startsWith("/") && !raw.startsWith("//")) return raw;
  return "/";
}
