import type { components } from "./api-schema";

// DTOs aligned with the openapi contract (generated types in api-schema.ts).
export type User = components["schemas"]["User"];
export type Me = components["schemas"]["Me"];

// The Game paytable is game-specific and untyped in the contract; this is
// the slots shape as emitted by the Go engine.
export interface SlotsPaytable {
  symbols: { name: string; weight: number; pay: number }[];
  betSteps: number[];
  reels: number;
  rows: number;
  paylines: number;
}

export interface GameInfo {
  id: string;
  name: string;
  theoreticalRtp: number;
  paytable?: SlotsPaytable | null;
}

export interface SlotsOutcome {
  grid: number[][];
  winningLines: number[] | null;
}

export interface BetRow {
  id: number;
  gameId: string;
  roundId: number;
  betCredits: number;
  payoutCredits: number;
  clientSeed: string;
  nonce: number;
  outcome?: SlotsOutcome | null;
  createdAt: string;
}

export interface FairCurrent {
  serverSeedHash: string;
  clientSeed: string;
  nonce: number;
}
