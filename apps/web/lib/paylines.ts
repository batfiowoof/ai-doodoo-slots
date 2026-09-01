// Payline row table, mirroring the Go engine's paylines exactly.
// Each line is the grid row (0-2) crossed at each of the 5 reels.
export const PAYLINE_ROWS: number[][] = [
  [1, 1, 1, 1, 1], // middle row
  [0, 0, 0, 0, 0], // top row
  [2, 2, 2, 2, 2], // bottom row
  [0, 1, 2, 1, 0], // V
  [2, 1, 0, 1, 2], // A
  [1, 0, 1, 0, 1], // zigzag upper
  [1, 2, 1, 2, 1], // zigzag lower
  [0, 0, 1, 0, 0], // top bump
  [2, 2, 1, 2, 2], // bottom bump
];

export const PAYLINE_NAMES = [
  "MIDDLE",
  "TOP",
  "BOTTOM",
  "V",
  "A",
  "ZIG TOP",
  "ZIG BOTTOM",
  "TOP BUMP",
  "BOTTOM BUMP",
];
