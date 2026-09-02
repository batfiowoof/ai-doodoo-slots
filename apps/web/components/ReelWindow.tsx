"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import PixelSymbol from "./PixelSymbol";
import { sound } from "@/lib/sound";

// Physical reels have momentum, so the reel spin is the one place the design
// system allows real easing: one CSS transition per reel, staggered. The
// outcome arrives before the animation starts ÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Â ÃƒÂ¢Ã¢â€šÂ¬Ã¢â€žÂ¢ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã‚Â ÃƒÆ’Ã‚Â¢ÃƒÂ¢Ã¢â‚¬Å¡Ã‚Â¬ÃƒÂ¢Ã¢â‚¬Å¾Ã‚Â¢ÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã‚Â¢ÃƒÂ¢Ã¢â‚¬Å¡Ã‚Â¬Ãƒâ€¦Ã‚Â¡ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¢ÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Â ÃƒÂ¢Ã¢â€šÂ¬Ã¢â€žÂ¢ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¢ÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¢ÃƒÆ’Ã†â€™Ãƒâ€šÃ‚Â¢ÃƒÆ’Ã‚Â¢ÃƒÂ¢Ã¢â‚¬Å¡Ã‚Â¬Ãƒâ€¦Ã‚Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¬ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã‚Â¦ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¡ÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã‚Â¢ÃƒÂ¢Ã¢â‚¬Å¡Ã‚Â¬Ãƒâ€¦Ã‚Â¡ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¬ÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Â ÃƒÂ¢Ã¢â€šÂ¬Ã¢â€žÂ¢ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¢ÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¢ÃƒÆ’Ã†â€™Ãƒâ€šÃ‚Â¢ÃƒÆ’Ã‚Â¢ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡Ãƒâ€šÃ‚Â¬ÃƒÆ’Ã¢â‚¬Â¦Ãƒâ€šÃ‚Â¡ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â¬ÃƒÆ’Ã†â€™Ãƒâ€ Ã¢â‚¬â„¢ÃƒÆ’Ã‚Â¢ÃƒÂ¢Ã¢â‚¬Å¡Ã‚Â¬Ãƒâ€¦Ã‚Â¡ÃƒÆ’Ã†â€™ÃƒÂ¢Ã¢â€šÂ¬Ã…Â¡ÃƒÆ’Ã¢â‚¬Å¡Ãƒâ€šÃ‚Â the spin is theater played
// over a known result.
const WINDOW_CELLS = 3;
const RANDOM_CELLS = 27;
const SPIN_MS = 1600;
const STAGGER_MS = 350;
const SETTLE_GRACE_MS = 600;

export interface SpinRequest {
  id: number;
  /** rows x cols grid as delivered by the server. */
  grid: number[][];
  winningLines: number[];
  scatterSymbols: number[];
  payout: number;
}

interface ReelProps {
	symbolCount: number;
  start: number[];
  target: number[];
  spinId: number;
  delayMs: number;
  cellPx: number;
  scale: number;
  iconFor: (symbolIndex: number) => string | undefined;
  winRows: Set<number>;
  onSettled: () => void;
}

function Reel({
  start,
  target,
  spinId,
  delayMs,
  cellPx,
  scale,
  iconFor,
	symbolCount,
  winRows,
  onSettled,
}: ReelProps) {
  const [strip, setStrip] = useState<number[]>(start);
  const [offset, setOffset] = useState(0);
  const [animate, setAnimate] = useState(false);
  const [stopFlash, setStopFlash] = useState(false);
  const settledRef = useRef(true);
  const startRef = useRef(start);
  const targetRef = useRef(target);

  useEffect(() => {
    startRef.current = start;
    targetRef.current = target;
  }, [start, target]);

  useEffect(() => {
    if (spinId === 0) return; // nothing to animate yet
    settledRef.current = false;

    const filler = Array.from(
      { length: RANDOM_CELLS },
      () => Math.floor(Math.random() * symbolCount), // decorative only, never a bet path
    );
    const full = [...startRef.current, ...filler, ...targetRef.current];

    setStrip(full);
    setOffset(0);
    setAnimate(false);

    let raf2 = 0;
    const raf1 = requestAnimationFrame(() => {
      raf2 = requestAnimationFrame(() => {
        setAnimate(true);
        setOffset(-(full.length - WINDOW_CELLS) * cellPx);
      });
    });
    return () => {
      cancelAnimationFrame(raf1);
      cancelAnimationFrame(raf2);
    };
  }, [spinId, cellPx, symbolCount]);

  return (
    <div className="overflow-hidden" style={{ height: WINDOW_CELLS * cellPx }}>
      <div
        className={`reel-strip ${animate ? "animate" : ""}`}
        style={{
          transform: `translateY(${offset}px)`,
          transitionDelay: animate ? `${delayMs}ms` : "0ms",
        }}
        onTransitionEnd={(e) => {
          if (e.target !== e.currentTarget) return;
          if (e.propertyName !== "transform") return;
          if (settledRef.current) return; // double-fire guard
          settledRef.current = true;
          setStrip(targetRef.current);
          setOffset(0);
          setAnimate(false);
          setStopFlash(true);
          window.setTimeout(() => setStopFlash(false), 350);
          onSettled();
        }}
      >
        {strip.map((symbolIndex, i) => {
          const committedRow = i;
          const isWinner = !animate && winRows.has(committedRow);
          return (
            <div
              key={i}
              className={`flex items-center justify-center border-4 bg-ink ${
                isWinner
                  ? "cell-win"
                  : stopFlash
                    ? "cell-stop"
                    : "border-slate"
              }`}
              style={{ width: cellPx, height: cellPx }}
            >
              <PixelSymbol index={symbolIndex} icon={iconFor(symbolIndex)} scale={scale} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ---- payline overlay geometry ----

export function overlayGeometry(cellPx: number, cols: number) {
  const gap = 8;
  const pad = 8;
  const width = cols * cellPx + (cols - 1) * gap + 2 * pad;
  const height = WINDOW_CELLS * cellPx + (WINDOW_CELLS - 1) * gap + 2 * pad;
  const cx = (c: number) => pad + c * (cellPx + gap) + cellPx / 2;
  const cy = (r: number) => pad + r * (cellPx + gap) + cellPx / 2;
  const strokeLine = (rows: number[]) => {
    if (rows.every((r) => r === rows[0])) {
      return `8,${cy(rows[0])} ${width - 8},${cy(rows[0])}`;
    }
    return rows.map((r, c) => `${cx(c)},${cy(r)}`).join(" ");
  };
  return { width, height, strokeLine };
}

export default function ReelWindow({
  spin,
  winningLines,
  scatterSymbols,
  cellPx,
  cols,
  lines,
  iconFor,
  symbolCount,
  onAllSettled,
}: {
  spin: SpinRequest | null;
  /** Winning payline indices, shown during the win celebration only. */
  winningLines: number[] | null;
  /** Symbols that won as scatters, highlighted during celebration. */
  scatterSymbols: number[] | null;
  cellPx: number;
  cols: number;
  /** Payline row table for this game (rows per reel). */
  lines: number[][];
  iconFor: (symbolIndex: number) => string | undefined;
  symbolCount: number;
  onAllSettled: () => void;
}) {
  const [committed, setCommitted] = useState<number[][]>(() =>
    Array.from({ length: cols }, (_, c) => [(c * 2) % symbolCount, (c * 3 + 1) % symbolCount, (c * 5 + 2) % symbolCount]),
  );
  const settledCount = useRef(0);
  const spinRef = useRef<SpinRequest | null>(null);

  useEffect(() => {
    spinRef.current = spin;
  }, [spin]);
	useEffect(() => {
	if (!spin) return;
    settledCount.current = 0;
    const worstCase = SPIN_MS + (cols - 1) * STAGGER_MS + SETTLE_GRACE_MS;
    const timer = setTimeout(() => {
      if (settledCount.current < cols) {
        settledCount.current = cols;
        setCommitted(spin.grid.map((_, c) => spin.grid.map((row) => row[c])));
        onAllSettled();
      }
    }, worstCase);
    return () => clearTimeout(timer);
  }, [spin, onAllSettled, cols]);

  const handleSettled = () => {
    sound.reelStop(settledCount.current);
    settledCount.current += 1;
    if (settledCount.current === cols && spinRef.current) {
      const grid = spinRef.current.grid;
      setCommitted(grid.map((_, c) => grid.map((row) => row[c])));
      onAllSettled();
    }
  };

  // Per-column winning rows: payline cells + scatter-matching cells.
  const winRows = useMemo(() => {
    const sets: Set<number>[] = [];
    for (let c = 0; c < cols; c++) sets.push(new Set<number>());
    if (winningLines) {
      for (const l of winningLines) {
        const rows = lines[l];
        if (!rows) continue;
        rows.forEach((r, c) => sets[c].add(r));
      }
    }
    if (scatterSymbols) {
      for (let c = 0; c < cols; c++) {
        for (let r = 0; r < WINDOW_CELLS; r++) {
          const sym = committed[r]?.[c];
          if (sym !== undefined && scatterSymbols.includes(sym)) sets[c].add(r);
        }
      }
    }
    return sets;
  }, [winningLines, scatterSymbols, lines, cols, committed]);

  const geo = useMemo(() => overlayGeometry(cellPx, cols), [cellPx, cols]);

  const coins = useMemo(() => {
    if ((!winningLines || winningLines.length === 0) && (!scatterSymbols || scatterSymbols.length === 0)) return [];
    return Array.from({ length: 28 }, (_, i) => ({
      left: ((i * 137 + 41) % 100) + "%",
      delay: ((i * 97) % 500) + "ms",
      dur: 420 + ((i * 61) % 260) + "ms",
    }));
  }, [winningLines, scatterSymbols]);

  const showCelebration =
    (winningLines && winningLines.length > 0) ||
    (scatterSymbols && scatterSymbols.length > 0);

  return (
    <div className="my-4 border-8 border-stone bg-plum p-2">
      <div className="relative bg-slate" style={{ padding: 8 }}>
        <div className="flex" style={{ gap: 8 }}>
          {committed.map((column, c) => (
            <Reel
              key={c}
              start={column}
              target={spin ? spin.grid.map((row) => row[c]) : column}
              spinId={spin?.id ?? 0}
              delayMs={c * STAGGER_MS}
              cellPx={cellPx}
              scale={cellPx >= 112 ? 3 : 2}
              iconFor={iconFor}
	symbolCount={symbolCount}
              winRows={winRows[c]}
              onSettled={handleSettled}
            />
          ))}
        </div>
        {showCelebration && (
          <>
            {winningLines && winningLines.length > 0 && (
              <svg
                viewBox={`0 0 ${geo.width} ${geo.height}`}
                className="pointer-events-none absolute inset-0 h-full w-full"
                shapeRendering="crispEdges"
                aria-hidden
              >
                {winningLines.map((l, i) => {
                  const rows = lines[l];
                  if (!rows) return null;
                  return (
                    <polyline
                      key={l}
                      points={geo.strokeLine(rows)}
                      fill="none"
                      stroke={i % 2 === 0 ? "var(--color-cyan)" : "var(--color-magenta)"}
                      strokeWidth={6}
                      className="payline-stroke"
                      style={{ animationDelay: `${i * 200}ms` }}
                    />
                  );
                })}
              </svg>
            )}
            <div className="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden>
              {coins.map((coin, i) => (
                <span
                  key={i}
                  className="coin absolute"
                  style={{
                    left: coin.left,
                    top: 0,
                    animationDelay: coin.delay,
                    animationDuration: coin.dur,
                  }}
                />
              ))}
            </div>
          </>
        )}
      </div>
      <span className="sr-only">{symbolCount} symbols on this machine</span>
    </div>
  );
}
