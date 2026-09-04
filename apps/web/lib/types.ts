import type { components } from "./api-schema";

// DTOs aligned with the openapi contract (generated types in api-schema.ts).
export type User = components["schemas"]["User"];
export type Me = components["schemas"]["Me"];
export type PublicProfile = components["schemas"]["PublicProfile"];
export type AdminUserRow = components["schemas"]["AdminUserRow"];
export type SessionInfo = components["schemas"]["SessionInfo"];

/** Live profile change broadcast on the room/lobby socket. */
export interface ProfileUpdatedEvent {
  userId: number;
  displayName: string;
  avatarPreset: string;
  avatarVersion: number;
}

/**
 * Curated avatar presets: keys match the server's avatar_preset charset and
 * map onto the shared slot-sprite sheet, so avatars inherit the cabinet art.
 */
export const AVATAR_PRESETS: { key: string; src: string }[] = [
  { key: "crown", src: "/sprites/crown.png" },
  { key: "seven", src: "/sprites/seven.png" },
  { key: "diamond", src: "/sprites/diamond-blue.png" },
  { key: "star", src: "/sprites/star.png" },
  { key: "bell", src: "/sprites/bell.png" },
  { key: "clover", src: "/sprites/clover.png" },
  { key: "cherries", src: "/sprites/cherries.png" },
  { key: "horseshoe", src: "/sprites/horseshoe.png" },
  { key: "dice", src: "/sprites/dice.png" },
  { key: "ruby", src: "/sprites/gem-ruby.png" },
  { key: "key", src: "/sprites/key.png" },
  { key: "chip", src: "/sprites/poker-chip.png" },
  { key: "trophy", src: "/sprites/trophy.png" },
  { key: "spade", src: "/sprites/spade.png" },
  { key: "heart-gem", src: "/sprites/heart-gem.png" },
  { key: "money-bag", src: "/sprites/money-bag.png" },
];

/** Public avatar URL for an uploaded image (only valid when preset = ""). */
export function avatarUploadURL(userId: number, version?: number): string {
  return `/api/v1/users/${userId}/avatar${version ? `?v=${version}` : ""}`;
}

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
  kind?: "instant" | "stateful";
  betSteps?: number[] | null;
  paytable?: SlotsPaytable | null;
}

/** Blackjack hand view; dealerCards hides the hole card while active. */
export type BlackjackHandView = components["schemas"]["BlackjackHand"];

export interface HandResponse {
  hand: BlackjackHandView;
  balanceCredits: number;
  fairness: FairCurrent;
  replay: boolean;
}

export interface BlackjackOutcome {
  status?: string;
  outcome?: string;
  playerCards?: string;
  dealerCards?: string;
  playerTotal?: number;
  dealerTotal?: number | null;
  betCredits?: number;
  payoutCredits?: number;
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
  outcome?: SlotsOutcome | BlackjackOutcome | null;
  createdAt: string;
}

export interface FairCurrent {
  serverSeedHash: string;
  clientSeed: string;
  nonce: number;
}
