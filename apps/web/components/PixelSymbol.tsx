import { GLYPH_PATTERNS, GLYPH_ROWS, GLYPH_COLS, SYMBOL_COLORS } from "@/lib/symbols";

interface PixelSymbolProps {
  index: number;
  /** Integer scale factor only — fractional scaling kills the effect. */
  scale?: 2 | 4 | 8;
}

export default function PixelSymbol({ index, scale = 4 }: PixelSymbolProps) {
  const pattern = GLYPH_PATTERNS[index % GLYPH_PATTERNS.length];
  const color = SYMBOL_COLORS[index % SYMBOL_COLORS.length];
  const cells: Array<{ x: number; y: number }> = [];
  pattern.forEach((row, y) => {
    for (let x = 0; x < GLYPH_COLS; x++) {
      if (row[x] === "#") cells.push({ x, y });
    }
  });

  return (
    <svg
      width={GLYPH_COLS * scale}
      height={GLYPH_ROWS * scale}
      viewBox={`0 0 ${GLYPH_COLS} ${GLYPH_ROWS}`}
      shapeRendering="crispEdges"
      className="pixelated shrink-0"
      aria-hidden
    >
      {cells.map(({ x, y }) => (
        <rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} fill={color} />
      ))}
    </svg>
  );
}
