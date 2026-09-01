// Pixel symbol glyphs transcribed from the shipped icon set: 16x16, with a
// per-symbol palette. '.' = transparent; every other character indexes that
// symbol's palette. Rendered as SVG rects with shape-rendering=crispEdges
// at integer scale.
export const GLYPH_SIZE = 16;

export interface GlyphPattern {
  rows: string[];
  palette: Record<string, string>;
}

// Shared tones.
const WHITE = "#ffffff";
const GREEN = "#3db83a";
const GREEN_DARK = "#2a8f27";
const GREEN_LIGHT = "#8fe36a";
const GOLD = "#f6a921";
const GOLD_DARK = "#d9860d";
const RED = "#e8281e";
const RED_DARK = "#b01410";
const RED_LIGHT = "#ff7a6e";

export const GLYPHS: GlyphPattern[] = [
  { // 0 plum — purple fruit, curved stem, leaf
    palette: { a: "#8f35d1", b: "#6b1f9e", c: WHITE, d: GREEN, e: GREEN_DARK },
    rows: [
      "..........dd....",
      ".........ddd....",
      "........dd......",
      ".......dd.......",
      "......dd........",
      ".....aaa........",
      "....aaaaaa......",
      "...aaaaaaaa.....",
      "..aacaaaaaab....",
      ".aaccaaaaaaabb..",
      ".aacaaaaaaaabb..",
      ".aaaaaaaaaaaabb.",
      ".aaaaaaaaaaaabb.",
      "..aaaaaaaaaabb..",
      "..aaaaaaaabbb...",
      "...aaaaaabb.....",
    ],
  },
  { // 1 cherry — twin fruit, crossing stems, leaf
    palette: { a: RED, b: RED_DARK, c: RED_LIGHT, d: GREEN, e: GREEN_DARK },
    rows: [
      ".........dddd...",
      "....ddddd..dd...",
      ".....ddd...dd...",
      "......dd...dd...",
      ".......dd.dd....",
      ".......d.dd.....",
      "......dd.d......",
      "......d.dd......",
      ".....dd..d......",
      "..aaa....aaa....",
      ".acaaa..acaaa...",
      ".acaaa..acaaab..",
      ".aaaaa..aaaaab..",
      "..aaaa..aaaaa...",
      "...aa....aaa....",
      "................",
    ],
  },
  { // 2 bell — gold dome, white sheen, dark rim, clapper
    palette: { a: GOLD, b: GOLD_DARK, c: WHITE },
    rows: [
      ".......aa.......",
      "......aaaa......",
      ".....aaaaaa.....",
      ".....aacaaa.....",
      "....aacaaaaa....",
      "...aacaaaaaaa...",
      "...aacaaaaaab...",
      "..aacaaaaaaabb..",
      "..acaaaaaaaabb..",
      "..acaaaaaaaabb..",
      ".acaaaaaaaaaabb.",
      ".acaaaaaaaabbbb.",
      ".aaaaaaaaaabbbb.",
      ".aaaaaaaaaaaaaa.",
      "..bbbbbbbbbbbb..",
      "......aaaa......",
    ],
  },
  { // 3 clover — four leaves around a center
    palette: { a: GREEN, b: GREEN_DARK, c: GREEN_LIGHT },
    rows: [
      "..aaaa....aaaa..",
      ".acaaaa..acaaaa.",
      ".aaaaaa..aaaaaa.",
      ".aaaaaa..aaaaaa.",
      "..aaaaa..aaaaa..",
      "...aaaaaaaaaa...",
      "....aaaaaaaa....",
      ".....aaaaaa.....",
      "....aaaaaaaa....",
      "...aaaaaaaaaa...",
      "..aaaaa..aaaaa..",
      ".aaaaaa..aaaaaa.",
      ".aaaaaa..aaaaaa.",
      ".aabaaa..aabaaa.",
      "..bbbb....bbbb..",
      "................",
    ],
  },
  { // 4 star — five points, hot gold, white core highlight
    palette: { a: GOLD, b: GOLD_DARK, c: WHITE },
    rows: [
      ".......aa.......",
      ".......aa.......",
      "......aaaa......",
      "......aaaa......",
      ".....aacaaa.....",
      "....aaccaaa.....",
      "aaaaccaaaaaaaa..",
      ".acccaaaaaaaaa..",
      "..caaaaaaaaaa...",
      "...aaaaaaaaa....",
      "..aaaa...aaaa...",
      ".aaaa.....aaaa..",
      ".aaaa.....aaaa..",
      "aaaa.......aaaa.",
      "aaa.........aaa.",
      "aa...........aa.",
    ],
  },
  { // 5 diamond — faceted cyan gem
    palette: { a: "#37c5f0", b: "#1e9fd8", c: WHITE },
    rows: [
      "...aaaaaaaaaa...",
      "..aacccaaaaaaab.",
      ".aaccccaaaaaaabb",
      ".aaccaaaaaaaaabb",
      ".aacaaaaaaaabbbb",
      ".aaaaaaaaaaabbb.",
      "..aaaaaaaaabbb..",
      "...aaaaaaabbb...",
      "...aaaaaabbb....",
      "....aaaabbb.....",
      "....aaabbb......",
      "....aabbb.......",
      ".....abbb.......",
      ".....abb........",
      "......bb........",
      "................",
    ],
  },
  { // 6 seven — bold magenta numeral, sheen and dark edge
    palette: { a: "#f04fc0", b: "#c4189a", c: WHITE },
    rows: [
      "..aaaaaaaaaaab..",
      "..aaaaaaaaaaab..",
      "..aacccccaaab...",
      "......aaaab.....",
      ".....aaaab......",
      ".....aaaab......",
      "....aaaab.......",
      "....aaaab.......",
      "...aaaab........",
      "...aaaab........",
      "...aaaab........",
      "..aaaab.........",
      "..aaaab.........",
      "..aaaab.........",
      "..aaaab.........",
      "..aaaab.........",
    ],
  },
  { // 7 crown — gold with three spikes and a red jewel
    palette: { a: GOLD, b: GOLD_DARK, c: WHITE, r: RED, d: RED_DARK },
    rows: [
      ".a.....aa.....a.",
      ".a.....aa.....a.",
      ".aa....aa....aa.",
      ".aa...aaaa...aa.",
      ".aaa..aaaa..aaa.",
      ".aaa.aaaaaa.aaa.",
      ".aaaaaaaaaaaaaa.",
      ".aacaaaaaacaaaa.",
      ".aaaaaaaaaaaaaa.",
      ".aaaaarrrraaaa..",
      ".aaaarrrrrraaa..",
      ".aaaarrdrrraa...",
      ".aaaaarrrraaaa..",
      ".aaaaaarraaaaa..",
      ".aaaaaaaaaaaaaa.",
      "abbbbbbbbbbbbbba",
    ],
  },
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
