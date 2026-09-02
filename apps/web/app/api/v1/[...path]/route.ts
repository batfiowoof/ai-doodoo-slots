import { NextRequest, NextResponse } from "next/server";
import { KC_COOKIE, decodeTokenSet, encodeTokenSet, kcCookieOptions, refreshTokens } from "@/lib/oidc";

// Thin BFF: forwards /api/v1/* to the Go service same-origin. Keycloak
// tokens stay in an httpOnly cookie here; the proxy attaches them as a
// Bearer credential upstream (and drops the guest cookie, so the two auth
// paths never mix on one request). The Go API never needs public CORS and
// no secrets live in the client bundle.
const API_URL = process.env.API_URL ?? "http://localhost:8080";

// Refresh when the access token expires within this window.
const refreshSlackMs = 30_000;

/**
 * Returns the Bearer value to attach, plus a possibly-updated token cookie.
 * Any failure falls back to no credential — the API then answers 401 and the
 * client re-bootstraps as a guest.
 */
async function resolveAuthorization(
  req: NextRequest,
): Promise<{ bearer: string | null; updated: { value: string; maxAge: number } | null }> {
  const raw = req.cookies.get(KC_COOKIE)?.value;
  if (!raw) return { bearer: null, updated: null };
  const tokens = decodeTokenSet(raw);
  if (!tokens) return { bearer: null, updated: null };

  if (tokens.expiresAtMs - refreshSlackMs > Date.now()) {
    return { bearer: tokens.accessToken, updated: null };
  }
  if (!tokens.refreshToken) return { bearer: null, updated: null };

  try {
    const fresh = await refreshTokens(tokens.refreshToken);
    const maxAge = Math.max(60, Math.floor((fresh.expiresAtMs - Date.now()) / 1000) + 86_400);
    return { bearer: fresh.accessToken, updated: { value: encodeTokenSet(fresh), maxAge } };
  } catch {
    return { bearer: null, updated: null };
  }
}

async function proxy(req: NextRequest): Promise<NextResponse> {
  const url = new URL(req.url);
  const suffix = url.pathname.slice("/api/v1".length);
  const upstream = `${API_URL}/api/v1${suffix}${url.search}`;

  const { bearer, updated } = await resolveAuthorization(req);

  const headers = new Headers();
  if (bearer) {
    headers.set("authorization", `Bearer ${bearer}`);
  } else {
    // No Keycloak credential: forward the guest session cookie if present.
    const cookie = req.headers.get("cookie");
    if (cookie) headers.set("cookie", cookie);
  }
  const contentType = req.headers.get("content-type");
  if (contentType) headers.set("content-type", contentType);

  const init: RequestInit = { method: req.method, headers };
  if (!["GET", "HEAD"].includes(req.method)) {
    init.body = await req.arrayBuffer();
  }

  const res = await fetch(upstream, init);

  const out = new NextResponse(res.body, { status: res.status });
  if (updated) {
    out.headers.append(
      "set-cookie",
      serializeCookie(KC_COOKIE, updated.value, updated.maxAge),
    );
  }
  for (const c of res.headers.getSetCookie()) {
    out.headers.append("set-cookie", c);
  }
  const resType = res.headers.get("content-type");
  if (resType) out.headers.set("content-type", resType);
  return out;
}

function serializeCookie(name: string, value: string, maxAge: number): string {
  const opts = kcCookieOptions(maxAge);
  const parts = [`${name}=${value}`, `Path=${opts.path}`, `Max-Age=${maxAge}`, "SameSite=Lax"];
  if (opts.httpOnly) parts.push("HttpOnly");
  if (opts.secure) parts.push("Secure");
  return parts.join("; ");
}

export {
  proxy as GET,
  proxy as POST,
  proxy as DELETE,
  proxy as PUT,
  proxy as PATCH,
};
