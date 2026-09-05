// Roulette domain shared by the room, the board and the lobby card: the
// European wheel, pocket colors, and the classic betting menu. The server is
// the only payout authority — these tables drive rendering and highlighting.

// WheelOrder is the European single-zero wheel clockwise from the zero, and
// must mirror services/backend/internal/game/roulette.
export const EUROPEAN_ORDER: number[] = [
  0, 32, 15, 19, 4, 21, 2, 25, 17, 34, 6, 27, 13, 36, 11, 30, 8, 23, 10, 5,
  24, 16, 33, 1, 20, 14, 31, 9, 22, 18, 29, 7, 28, 12, 35, 3, 26,
];

export const POCKET_COUNT = EUROPEAN_ORDER.length;

export const RED_SET = new Set([
  1, 3, 5, 7, 9, 12, 14, 16, 18, 19, 21, 23, 25, 27, 30, 32, 34, 36,
]);

export type PocketColor = "red" | "black" | "green";

export function pocketColor(pocket: number): PocketColor {
  if (pocket === 0) return "green";
  return RED_SET.has(pocket) ? "red" : "black";
}

// Retro-palette swatches for pockets — authentic casino tones: racing red,
// true black (lifted just enough to separate from the void), felt green.
export const POCKET_COLORS: Record<PocketColor, string> = {
  red: "#d0342c",
  black: "#101018",
  green: "#17954c",
};

export type SpotGroup = "straight" | "dozen" | "column" | "even";

export interface RouletteSpot {
  /** Wire id, mirroring the backend spot catalog. */
  id: string;
  label: string;
  /** Total return per credit staked, stake included (36 = 35:1 + stake). */
  payout: number;
  group: SpotGroup;
  hits: (pocket: number) => boolean;
}

const straight = (n: number): RouletteSpot => ({
  id: `n${n}`,
  label: String(n),
  payout: 36,
  group: "straight",
  hits: (p) => p === n,
});

const dozen = (n: number): RouletteSpot => ({
  id: `d${n}`,
  label: `${["1ST", "2ND", "3RD"][n - 1]} 12`,
  payout: 3,
  group: "dozen",
  hits: (p) => p >= (n - 1) * 12 + 1 && p <= n * 12,
});

// Column n holds the numbers ≡ n (mod 3); the zero belongs to no column.
const column = (n: number): RouletteSpot => ({
  id: `c${n}`,
  label: `${["1ST", "2ND", "3RD"][n - 1]} COL`,
  payout: 3,
  group: "column",
  hits: (p) => p > 0 && p % 3 === n % 3,
});

const even = (
  id: string,
  label: string,
  hits: (p: number) => boolean,
): RouletteSpot => ({ id, label, payout: 2, group: "even", hits });

/** The full betting menu in board order: straights, dozens, columns, even-money. */
export const SPOTS: RouletteSpot[] = [
  ...Array.from({ length: 37 }, (_, n) => straight(n)),
  dozen(1),
  dozen(2),
  dozen(3),
  column(1),
  column(2),
  column(3),
  even("red", "RED", (p) => pocketColor(p) === "red"),
  even("black", "BLACK", (p) => pocketColor(p) === "black"),
  even("odd", "ODD", (p) => p > 0 && p % 2 === 1),
  even("even", "EVEN", (p) => p > 0 && p % 2 === 0),
  even("low", "1-18", (p) => p >= 1 && p <= 18),
  even("high", "19-36", (p) => p >= 19 && p <= 36),
];

const SPOT_INDEX = new Map(SPOTS.map((s) => [s.id, s]));

export function spotOf(id: string): RouletteSpot | undefined {
  return SPOT_INDEX.get(id);
}

/** Human payout odds for a spot's total-return multiplier: 36 → "35:1". */
export function payoutOdds(payout: number): string {
  return `${payout - 1}:1`;
}

/** Client-side projection of a pocket onto a spot, for board highlighting. */
export function spotWins(spotId: string, pocket: number): boolean {
  return SPOT_INDEX.get(spotId)?.hits(pocket) ?? false;
}

/** All winning spot ids for a pocket, in board order. */
export function winningSpots(pocket: number): string[] {
  return SPOTS.filter((s) => s.hits(pocket)).map((s) => s.id);
}

// The tableau's number layout: three rows ordered exactly like the cloth —
// top row 3,6,…,36; middle 2,5,…,35; bottom 1,4,…,34.
export const BOARD_ROWS: number[][] = [2, 1, 0].map((row) =>
  Array.from({ length: 12 }, (_, col) => col * 3 + (3 - row)),
);
