"use client";

import { GLYPHS, GLYPH_SIZE } from "@/lib/symbols";
import { useTheme } from "@/lib/theme";
import { spriteDataUrl } from "@/lib/spriteRenderer";

interface PixelSymbolProps {
  index: number;
  /** Integer scale factor only — fractional scaling kills the effect. */
  scale?: number;
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

  // Shipped icon set: per-symbol palette, crisp edges.
  const glyph = GLYPHS[index % GLYPHS.length];
  const cells: Array<{ x: number; y: number; fill: string }> = [];
  glyph.rows.forEach((row, y) => {
    for (let x = 0; x < GLYPH_SIZE; x++) {
      const ch = row[x] ?? ".";
      const fill = glyph.palette[ch];
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
