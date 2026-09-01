import { NextRequest, NextResponse } from "next/server";

// Thin BFF: forwards /api/v1/* to the Go service same-origin so the session
// cookie stays httpOnly and the Go API never needs public CORS. No API keys
// or secrets live in this bundle — the OpenRouter key stays in Go.
const API_URL = process.env.API_URL ?? "http://localhost:8080";

async function proxy(req: NextRequest): Promise<NextResponse> {
  const url = new URL(req.url);
  const suffix = url.pathname.slice("/api/v1".length);
  const upstream = `${API_URL}/api/v1${suffix}${url.search}`;

  const headers = new Headers();
  const contentType = req.headers.get("content-type");
  if (contentType) headers.set("content-type", contentType);
  const cookie = req.headers.get("cookie");
  if (cookie) headers.set("cookie", cookie);

  const init: RequestInit = { method: req.method, headers };
  if (!["GET", "HEAD"].includes(req.method)) {
    init.body = await req.arrayBuffer();
  }

  const res = await fetch(upstream, init);

  const out = new NextResponse(res.body, { status: res.status });
  for (const c of res.headers.getSetCookie()) {
    out.headers.append("set-cookie", c);
  }
  const resType = res.headers.get("content-type");
  if (resType) out.headers.set("content-type", resType);
  return out;
}

export {
  proxy as GET,
  proxy as POST,
  proxy as DELETE,
  proxy as PUT,
  proxy as PATCH,
};
