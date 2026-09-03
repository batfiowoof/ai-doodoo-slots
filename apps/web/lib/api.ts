"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  BetRow,
  BlackjackHandView,
  FairCurrent,
  GameInfo,
  HandResponse,
  Me,
  SlotsOutcome,
} from "./types";

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

export interface PlayResponse {
  betId: number;
  gameId: string;
  payoutCredits: number;
  balanceCredits: number;
  outcome: SlotsOutcome;
  fairness: FairCurrent;
  replay: boolean;
}

export class PlayError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export interface PlayInput {
  gameId: string;
  betCredits: number;
  clientSeed: string;
}

/** Places the bet; the server decides everything. On success, patches the
 * balance, fairness, and history caches from the authoritative response. */
export function usePlay() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ gameId, betCredits, clientSeed }: PlayInput): Promise<PlayResponse> => {
      const res = await fetch(`/api/v1/games/${gameId}/play`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          betCredits,
          clientSeed,
          idempotencyKey: crypto.randomUUID(),
        }),
      });
      if (!res.ok) {
        throw new PlayError(res.status, `play failed: ${res.status}`);
      }
      return res.json() as Promise<PlayResponse>;
    },
    onSuccess: (data, variables) => {
      qc.setQueryData<Me>(["me"], (old) =>
        old ? { ...old, balanceCredits: data.balanceCredits } : old,
      );
      qc.setQueryData<FairCurrent>(["fair"], data.fairness);
      qc.setQueryData<{ bets: BetRow[]; nextCursor: string | null }>(
        ["bets"],
        (old) =>
          old
            ? {
                bets: [
                  {
                    id: data.betId,
                    gameId: data.gameId,
                    roundId: 0,
                    betCredits: variables.betCredits,
                    payoutCredits: data.payoutCredits,
                    clientSeed: data.fairness.clientSeed,
                    nonce: data.fairness.nonce,
                    outcome: data.outcome,
                    createdAt: new Date().toISOString(),
                  },
                  ...old.bets,
                ],
                nextCursor: old.nextCursor,
              }
            : old,
      );
    },
    onError: () => {
      // Balance may have drifted (e.g. another tab); re-sync.
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });
}

export interface DepositResponse {
  balanceCredits: number;
  claimed: boolean;
  amountCredits: number;
}

/** Tops up credits: +1000, once per UTC hour (server-enforced). */
export function useDeposit() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<DepositResponse> => {
      const res = await fetch("/api/v1/me/deposit", { method: "POST" });
      if (!res.ok) throw new Error(`deposit failed: ${res.status}`);
      return res.json() as Promise<DepositResponse>;
    },
    onSuccess: (data) => {
      qc.setQueryData<Me>(["me"], (old) =>
        old ? { ...old, balanceCredits: data.balanceCredits } : old,
      );
    },
  });
}

// ---- Blackjack (stateful deal/action flow) ----

/** The caller's in-progress hand, if any — picked up on page load. */
export function useActiveHand(enabled: boolean) {
  return useQuery({
    queryKey: ["hand"],
    queryFn: async (): Promise<BlackjackHandView | null> => {
      const res = await fetch("/api/v1/hands/active");
      if (!res.ok) throw new Error(`/api/v1/hands/active failed: ${res.status}`);
      const data = (await res.json()) as { hand: BlackjackHandView | null };
      return data.hand;
    },
    enabled,
    staleTime: 0,
  });
}

function applyHandResponse(qc: ReturnType<typeof useQueryClient>, data: HandResponse) {
  qc.setQueryData<Me>(["me"], (old) =>
    old ? { ...old, balanceCredits: data.balanceCredits } : old,
  );
  qc.setQueryData<FairCurrent>(["fair"], {
    serverSeedHash: data.fairness.serverSeedHash,
    clientSeed: data.fairness.clientSeed,
    nonce: data.fairness.nonce,
  });
  if (data.hand.status === "complete") {
    // The settled bet row (final stake + payout) only exists server-side
    // once the hand completes; refetch history.
    void qc.invalidateQueries({ queryKey: ["bets"] });
  }
}

export interface DealInput {
  betCredits: number;
  clientSeed: string;
}

/** Deals a blackjack hand; the server shuffles and holds the deck. */
export function useDeal() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ betCredits, clientSeed }: DealInput): Promise<HandResponse> => {
      const res = await fetch("/api/v1/games/blackjack/deal", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ betCredits, clientSeed, idempotencyKey: crypto.randomUUID() }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null;
        throw new PlayError(res.status, body?.message ?? `deal failed: ${res.status}`);
      }
      return res.json() as Promise<HandResponse>;
    },
    onSuccess: (data) => applyHandResponse(qc, data),
    onError: () => {
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });
}

export type HandAction = "hit" | "stand" | "double";

/** Applies hit/stand/double; completion credits the payout server-side. */
export function useHandAction() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      handId,
      action,
    }: {
      handId: number;
      action: HandAction;
    }): Promise<HandResponse> => {
      const res = await fetch(`/api/v1/hands/${handId}/action`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ action, idempotencyKey: crypto.randomUUID() }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null;
        throw new PlayError(res.status, body?.message ?? `action failed: ${res.status}`);
      }
      return res.json() as Promise<HandResponse>;
    },
    onSuccess: (data) => applyHandResponse(qc, data),
    onError: () => {
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
  });
}
