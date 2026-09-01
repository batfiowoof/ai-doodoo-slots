"use client";

import {
  GLYPH_PATTERNS,
  GLYPH_SIZE,
  SYMBOL_COLORS,
  SYMBOL_ACCENTS,
  HIGHLIGHT,
  SHADE,
} from "@/lib/symbols";
import { useTheme } from "@/lib/theme";
import { spriteDataUrl } from "@/lib/spriteRenderer";

interface PixelSymbolProps {
  index: number;
  /** Integer scale factor only — fractional scaling kills the effect. */
  scale?: number;
}

/** tone → fill color for the placeholder glyphs. */
function toneColor(char: string, index: number): string | null {
  switch (char) {
    case "#":
      return SYMBOL_COLORS[index % SYMBOL_COLORS.length];
    case "o":
      return SYMBOL_ACCENTS[index % SYMBOL_ACCENTS.length];
    case "+":
      return HIGHLIGHT;
    case "-":
      return SHADE;
    default:
      return null;
  }
}

export default function PixelSymbol({ index, scale = 4 }: PixelSymbolProps) {
  const theme = useTheme();
  const themePalette = theme?.palette;
  const themeSprite = theme?.sprites[index];

  // Generated theme sprite, drawn once at 1:1 and scaled by integers.
  if (themePalette && themeSprite && themeSprite.rows.length === 16) {
    const url = spriteDataUrl(theme.id, index, themePalette, themeSprite.rows);
    if (url) {
      return (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={url}
          width={16 * scale}
          height={16 * scale}
          className="pixelated shrink-0"
          alt={themeSprite.name}
          draggable={false}
        />
      );
    }
  }

  // Placeholder glyph: 16x16, four tones, crisp edges.
  const pattern = GLYPH_PATTERNS[index % GLYPH_PATTERNS.length];
  const cells: Array<{ x: number; y: number; fill: string }> = [];
  pattern.forEach((row, y) => {
    for (let x = 0; x < GLYPH_SIZE; x++) {
      const ch = row[x] ?? ".";
      const fill = toneColor(ch, index);
      if (fill) cells.push({ x, y, fill });
    }
  });

  return (
    <svg
      width={GLYPH_SIZE * scale}
      height={GLYPH_SIZE * scale}
      viewBox={`0 0 ${GLYPH_SIZE} ${GLYPH_SIZE}`}
      shapeRendering="crispEdges"
      className="pixelated shrink-0"
      aria-hidden
    >
      {cells.map(({ x, y, fill }) => (
        <rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} fill={fill} />
      ))}
    </svg>
  );
}
