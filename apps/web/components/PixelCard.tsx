"use client";

// Pixel-art playing card. One SVG per card, drawn from pixel bitmaps at a
// 20x28 base grid and scaled by an integer factor — fractional scaling kills
// the effect. The hole-card back is a diagonal weave in the house magenta.
// An equipped card skin (shop item id) swaps the palette; ids resolve in
// lib/cosmetics.ts and must match the shop_items seed.

import { cardSkinOf } from "@/lib/cosmetics";

const SUIT_BITMAPS: Record<string, string[]> = {
  // s = spade, h = heart, d = diamond, c = club; 9x9, "#" = ink.
  s: [
    "....#....",
    "...###...",
    "..#####..",
    ".#######.",
    "#########",
    "#########",
    "##.###.##",
    "....#....",
    "...###...",
  ],
  h: [
    ".##...##.",
    "####.####",
    "#########",
    "#########",
    "#########",
    ".#######.",
    "..#####..",
    "...###...",
    "....#....",
  ],
  d: [
    "....#....",
    "...###...",
    "..#####..",
    ".#######.",
    "#########",
    ".#######.",
    "..#####..",
    "...###...",
    "....#....",
  ],
  c: [
    "....#....",
    "...###...",
    "..#####..",
    "..#####..",
    ".#######.",
    "#########",
    "##.###.##",
    "...###...",
    "..#####..",
  ],
};

const RANK_CHARS: Record<string, string> = { T: "10" };

const CARD_W = 20;
const CARD_H = 28;

interface BitmapCell {
  x: number;
  y: number;
}

function suitCells(suit: string): BitmapCell[] {
  const rows = SUIT_BITMAPS[suit] ?? SUIT_BITMAPS.s;
  const cells: BitmapCell[] = [];
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) {
      if (row[x] === "#") cells.push({ x, y });
    }
  });
  return cells;
}

/** Back-of-card weave: every (x+y) % 4 < 2 pixel inside the border. */
function backCells(): BitmapCell[] {
  const cells: BitmapCell[] = [];
  for (let y = 2; y < CARD_H - 2; y++) {
    for (let x = 2; x < CARD_W - 2; x++) {
      if ((x + y) % 4 < 2) cells.push({ x, y });
    }
  }
  return cells;
}

const BACK_CELLS = backCells();

export interface PixelCardProps {
  /** Two-character code ("As", "Td", "7c") or "back" for the hole card. */
  code: string;
  /** Integer scale factor. */
  scale?: number;
  /** Dim face-down or losing cards at showdown. */
  dim?: boolean;
  /** Equipped card-skin item id (""/undefined = classic house card). */
  skin?: string;
}

export default function PixelCard({ code, scale = 3, dim = false, skin }: PixelCardProps) {
  const w = CARD_W * scale;
  const h = CARD_H * scale;
  const palette = cardSkinOf(skin || undefined);

  if (code === "back" || code.length !== 2) {
    return (
      <svg
        width={w}
        height={h}
        viewBox={`0 0 ${CARD_W} ${CARD_H}`}
        shapeRendering="crispEdges"
        className="pixelated shrink-0"
        style={{ opacity: dim ? 0.6 : 1 }}
        aria-hidden
      >
        <rect x={0} y={0} width={CARD_W} height={CARD_H} fill={palette.backBorder} />
        <rect x={1} y={1} width={CARD_W - 2} height={CARD_H - 2} fill={palette.back} />
        {BACK_CELLS.map(({ x, y }) => (
          <rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} fill={palette.backPip} />
        ))}
      </svg>
    );
  }

  const rankChar = code[0];
  const suit = code[1];
  // Classic cards keep the red/black ink split; skins are monochrome.
  const red = suit === "h" || suit === "d";
  const ink = !skin && red ? "#e04038" : palette.ink;
  const rankText = RANK_CHARS[rankChar] ?? rankChar;
  const cells = suitCells(suit);

  return (
    <svg
      width={w}
      height={h}
      viewBox={`0 0 ${CARD_W} ${CARD_H}`}
      shapeRendering="crispEdges"
      className="pixelated shrink-0"
      style={{ opacity: dim ? 0.6 : 1 }}
      aria-label={code}
      role="img"
    >
      <rect x={0} y={0} width={CARD_W} height={CARD_H} fill={palette.face} />
      <rect
        x={0.5}
        y={0.5}
        width={CARD_W - 1}
        height={CARD_H - 1}
        fill="none"
        stroke={palette.faceBorder}
        strokeWidth={1}
      />
      <text
        x={3}
        y={9}
        fill={ink}
        fontFamily="var(--font-display), monospace"
        fontSize={rankText.length > 1 ? 6 : 8}
        fontWeight="bold"
      >
        {rankText}
      </text>
      {/* Small suit under the rank, big suit centered below. */}
      {cells.map(({ x, y }) => (
        <rect key={`s${x}-${y}`} x={3 + x / 3} y={11 + y / 3} width={1 / 3} height={1 / 3} fill={ink} />
      ))}
      {cells.map(({ x, y }) => (
        <rect key={`b${x}-${y}`} x={5 + x} y={17 + y} width={1} height={1} fill={ink} />
      ))}
    </svg>
  );
}
