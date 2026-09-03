"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";

// The lobby: initial state from GET /api/v1/lobby, upgraded to live via
// the socket's subscribe_lobby. The idle lobby receives ~1 message per
// second (summaries only — round ticks never fan out to it).

export interface LobbyRoom {
  id: number;
  slug: string;
  name: string;
  gameId: string;
  minBet: number;
  maxBet: number;
  capacity: number;
  playerCount: number;
  state?: string;
  multiplier?: number;
  recentCrashes?: number[];
}

export interface LobbySummary {
  rooms: Record<string, { players: number; state?: string; multiplier?: number; recentCrashes?: number[] }>;
  connectedPlayers: number;
}

async function fetchLobby(): Promise<{ rooms: LobbyRoom[] }> {
  const res = await fetch("/api/v1/lobby");
  if (!res.ok) throw new Error("lobby fetch failed");
  return res.json();
}

export function useLobby(enabled = true) {
  const initial = useQuery({
    queryKey: ["lobby"],
    queryFn: fetchLobby,
    enabled,
    staleTime: 5_000,
    refetchInterval: 10_000,
  });

  const [summary, setSummary] = useState<LobbySummary | null>(null);

  useEffect(() => {
    if (!enabled) return;
    const wsURL =
      process.env.NEXT_PUBLIC_WS_URL ??
      (location.port === "3000"
        ? "ws://localhost:8080/api/v1/ws"
        : `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/ws`);

    let closed = false;
    let retry: number | undefined;
    let ws: WebSocket | null = null;

    const connect = () => {
      if (closed) return;
      ws = new WebSocket(wsURL);
      ws.onopen = () => ws?.send(JSON.stringify({ type: "subscribe_lobby" }));
      ws.onclose = () => {
        if (!closed) retry = window.setTimeout(connect, 2000);
      };
      ws.onmessage = (ev) => {
        const msg = JSON.parse(ev.data) as { type: string; payload?: LobbySummary };
        // The lobby only ever receives summaries; round ticks are filtered
        // server-side, never fanned out here.
        if (msg.type === "lobby_summary" && msg.payload) setSummary(msg.payload);
      };
    };
    connect();

    return () => {
      closed = true;
      if (retry) window.clearTimeout(retry);
      ws?.close();
    };
  }, [enabled]);

  // Merge live counts into the initial room list.
  const rooms: LobbyRoom[] = (initial.data?.rooms ?? []).map((r) => {
    const live = summary?.rooms?.[r.slug];
    return live
      ? { ...r, playerCount: live.players, state: live.state, multiplier: live.multiplier, recentCrashes: live.recentCrashes ?? r.recentCrashes }
      : r;
  });

  return { rooms, connectedPlayers: summary?.connectedPlayers, isLoading: initial.isLoading };
}
