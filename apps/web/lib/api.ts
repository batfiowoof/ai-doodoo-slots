"use client";

import { useQuery } from "@tanstack/react-query";
import type { BetRow, FairCurrent, GameInfo, Me } from "./types";

// All client traffic goes through the same-origin BFF; the client never
// talks to the Go service directly and never computes an outcome.

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) throw new Error(`${path} failed: ${res.status}`);
  return res.json() as Promise<T>;
}

/**
 * Session bootstrap: read /me; on 401 create a guest session (1000 credits,
 * seed pair) then re-read. The cookie is httpOnly, set by the BFF.
 */
export function useSession() {
  return useQuery({
    queryKey: ["me"],
    queryFn: async (): Promise<Me> => {
      let res = await fetch("/api/v1/me");
      if (res.status === 401) {
        const guest = await fetch("/api/v1/auth/guest", { method: "POST" });
        if (!guest.ok) throw new Error("guest bootstrap failed");
        res = await fetch("/api/v1/me");
      }
      if (!res.ok) throw new Error(`/api/v1/me failed: ${res.status}`);
      return res.json() as Promise<Me>;
    },
    staleTime: 10_000,
  });
}

export function useGames() {
  return useQuery({
    queryKey: ["games"],
    queryFn: () => getJSON<GameInfo[]>("/api/v1/games"),
    staleTime: Infinity,
  });
}

export function useBets() {
  return useQuery({
    queryKey: ["bets"],
    queryFn: () => getJSON<{ bets: BetRow[]; nextCursor: string | null }>("/api/v1/bets"),
  });
}

export function useFairCurrent(enabled: boolean) {
  return useQuery({
    queryKey: ["fair"],
    queryFn: () => getJSON<FairCurrent>("/api/v1/fair/current"),
    enabled,
  });
}
