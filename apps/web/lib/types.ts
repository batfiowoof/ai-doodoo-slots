import type { components } from "./api-schema";

// DTOs aligned with the openapi contract (generated types in api-schema.ts).
export type User = components["schemas"]["User"];
export type Me = components["schemas"]["Me"];

// The Game paytable is game-specific and untyped in the contract; this is
// the shape emitted by the Go config engine.
export interface SlotSymbol {
  name: string;
  weight: number;
  pays: Record<string, number>;
}

export interface SlotsPaytable {
  symbols: SlotSymbol[];
  betSteps: number[];
  reels: number;
  rows: number;
  paylines: number;
  lines: number[][];
  icons: string[];
  mode: "lines" | "scatter";
}

export interface GameInfo {
  id: string;
  name: string;
  theoreticalRtp: number;
  paytable?: SlotsPaytable | null;
}

export interface ScatterWin {
  symbol: number;
  count: number;
  pay: number;
}

export interface SlotsOutcome {
  grid: number[][];
  winningLines: number[] | null;
  scatterWins?: ScatterWin[] | null;
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
