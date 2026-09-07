"use client";

import { useCallback, useEffect, useRef, useState } from "react";

// One shared lobby-mode socket for the social dock: chat, emotes, big-win
// banners, rain and presence all ride subscribe_lobby. Same URL resolution
// and reconnect cadence as useLobby; the latest onMessage is kept in a ref
// so callers can re-render without the socket ever reconnecting.

export interface CasinoEnvelope {
  type: string;
  payload?: unknown;
}

export function wsUrl(): string {
  return (
    process.env.NEXT_PUBLIC_WS_URL ??
    (location.port === "3000"
      ? "ws://localhost:8082/api/v1/ws"
      : `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/ws`)
  );
}

export function useCasinoSocket(
  onMessage: (msg: CasinoEnvelope) => void,
  enabled = true
): { status: "connecting" | "open" | "closed"; send: (type: string, payload?: unknown) => void } {
  const [status, setStatus] = useState<"connecting" | "open" | "closed">("connecting");
  const handlerRef = useRef(onMessage);
  useEffect(() => {
    handlerRef.current = onMessage;
  });
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let closed = false;
    let retry: number | undefined;
    let sock: WebSocket | null = null;

    const connect = () => {
      if (closed) return;
      setStatus("connecting");
      sock = new WebSocket(wsUrl());
      wsRef.current = sock;
      sock.onopen = () => {
        setStatus("open");
        sock?.send(JSON.stringify({ type: "subscribe_lobby" }));
      };
      sock.onclose = () => {
        setStatus("closed");
        if (!closed) retry = window.setTimeout(connect, 2000);
      };
      sock.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data) as CasinoEnvelope;
          handlerRef.current(msg);
        } catch {
          // Malformed frame: ignore, the server never sends one.
        }
      };
    };
    connect();

    return () => {
      closed = true;
      if (retry) window.clearTimeout(retry);
      sock?.close();
      wsRef.current = null;
    };
  }, [enabled]);

  const send = useCallback((type: string, payload?: unknown) => {
    wsRef.current?.send(JSON.stringify({ type, payload }));
  }, []);

  return { status, send };
}
