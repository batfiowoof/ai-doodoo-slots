// Placeholder pixel glyphs until phase 9 ships generated 16x16 sprites.
// Each symbol is 6 rows of 6 chars; '.' is transparent. Rendered as SVG
// rects with shape-rendering=crispEdges, scaled by an integer factor.
export const GLYPH_ROWS = 6;
export const GLYPH_COLS = 6;

export const GLYPH_PATTERNS: string[][] = [
  [ // 0 plum — round fruit
    "..##..",
    ".####.",
    "######",
    "######",
    ".####.",
    "..##..",
  ],
  [ // 1 cherry — pair on a stem
    "....#.",
    "...#..",
    ".##...",
    "###...",
    ".###..",
    "..##..",
  ],
  [ // 2 bell
    "..##..",
    ".####.",
    ".####.",
    ".####.",
    "######",
    "..##..",
  ],
  [ // 3 clover
    ".##.##",
    "######",
    "######",
    ".####.",
    "..##..",
    "......",
  ],
  [ // 4 star
    "...#..",
    "..###.",
    "######",
    ".####.",
    "..##..",
    ".#..#.",
  ],
  [ // 5 diamond
    "..##..",
    ".####.",
    "######",
    "######",
    ".####.",
    "..##..",
  ],
  [ // 6 seven
    "######",
    "....##",
    "...##.",
    "..##..",
    "..##..",
    "..##..",
  ],
  [ // 7 crown
    "#....#",
    "##..##",
    "######",
    "######",
    "######",
    "......",
  ],
];

// One palette color per symbol, ordered to match GLYPH_PATTERNS. Phase 9
// theme generation overrides these; chrome colors stay fixed.
export const SYMBOL_COLORS = [
  "#6b5f9e", // haze
  "#f2643d", // ember
  "#ffb13b", // amber
  "#5fe08a", // mint
  "#a94ee6", // violet
  "#4fd6e0", // cyan
  "#e63f8c", // magenta
  "#c78b3c", // brass
];

export const SYMBOL_NAMES = [
  "plum",
  "cherry",
  "bell",
  "clover",
  "star",
  "diamond",
  "seven",
  "crown",
];
