"use client";

import { useQuery } from "@tanstack/react-query";
import type { ChatMessage, Leaderboard, LeaderboardEntry, PublicProfile } from "./types";

// Social surface REST half: chat history replay and leaderboard snapshots.
// The live half (chat_message / emote / big_win / rain / roster) rides the
// dock's socket — see SocialDock + useCasinoSocket.

export type { ChatMessage, Leaderboard, LeaderboardEntry };

/** 1234567 → "1,234,567" for credit displays. */
export function fmtCredits(n: number): string {
  const neg = n < 0;
  const s = Math.abs(Math.trunc(n)).toString();
  let out = "";
  for (let i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 === 0) out += ",";
    out += s[i];
  }
  return (neg ? "-" : "") + out;
}

export function useChatHistory(enabled: boolean) {
  return useQuery({
    queryKey: ["chat"],
    queryFn: async (): Promise<ChatMessage[]> => {
      const res = await fetch("/api/v1/chat/messages?limit=50");
      if (!res.ok) throw new Error(`chat history failed: ${res.status}`);
      const data = (await res.json()) as { messages: ChatMessage[] };
      return data.messages;
    },
    enabled,
    staleTime: Infinity, // live updates arrive over the socket
  });
}

export type LeaderboardMetric = "biggest_win" | "net_profit" | "wagered";
export type LeaderboardWindow = "daily" | "weekly" | "all";

export interface LeaderboardView {
  metric: LeaderboardMetric;
  window: LeaderboardWindow;
  entries: LeaderboardEntry[];
  me: LeaderboardEntry | null;
}

export function useLeaderboard(
  metric: LeaderboardMetric,
  window: LeaderboardWindow,
  enabled: boolean
) {
  return useQuery({
    queryKey: ["leaderboard", metric, window],
    queryFn: async (): Promise<LeaderboardView> => {
      const res = await fetch(`/api/v1/leaderboard?metric=${metric}&window=${window}`);
      if (!res.ok) throw new Error(`leaderboard failed: ${res.status}`);
      return res.json() as Promise<LeaderboardView>;
    },
    enabled,
    refetchInterval: 30_000,
  });
}

export interface PlayerProfile extends PublicProfile {
  stats?: { biggestWin?: number };
}

export function usePlayerProfile(userId: number | null) {
  return useQuery({
    queryKey: ["player", userId],
    queryFn: async (): Promise<PlayerProfile> => {
      const res = await fetch(`/api/v1/users/${userId}/profile`);
      if (!res.ok) throw new Error(`profile failed: ${res.status}`);
      return res.json() as Promise<PlayerProfile>;
    },
    enabled: userId != null,
    staleTime: 30_000,
  });
}
