"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import PixelSymbol from "./PixelSymbol";
import { PAYLINE_ROWS } from "@/lib/paylines";
import { sound } from "@/lib/sound";

// Physical reels have momentum, so the reel spin is the one place the design
// system allows real easing: one CSS transition per reel, staggered. The
// outcome arrives before the animation starts — the spin is theater played
// over a known result.
const WINDOW_CELLS = 3;
const RANDOM_CELLS = 27;
const SPIN_MS = 1600;
const STAGGER_MS = 350;
const SETTLE_GRACE_MS = 600;
const REELS = 5;
const ROWS = 3;

export interface SpinRequest {
  id: number;
  /** 3x5 grid, rows as delivered by the server. */
  grid: number[][];
  winningLines: number[];
  payout: number;
}

interface ReelProps {
  /** Column currently committed on screen (before this spin). */
  start: number[];
  /** Column this spin must land on. */
  target: number[];
  spinId: number;
  delayMs: number;
  cellPx: number;
  /** Rows (0-2) in this column that belong to a winning payline. */
  winRows: Set<number>;
  onSettled: () => void;
}

function Reel({
  start,
  target,
  spinId,
  delayMs,
  cellPx,
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

    // Current view on top so there is no jump, then random filler, then the
    // known result at the bottom of the strip.
    const filler = Array.from(
      { length: RANDOM_CELLS },
      () => Math.floor(Math.random() * 8), // decorative only, never a bet path
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
  }, [spinId, cellPx]);

  return (
    <div className="overflow-hidden" style={{ height: WINDOW_CELLS * cellPx }}>
      <div
        className={`reel-strip ${animate ? "animate" : ""}`}
        style={{
          transform: `translateY(${offset}px)`,
          transitionDelay: animate ? `${delayMs}ms` : "0ms",
        }}
        onTransitionEnd={(e) => {
          // transitionend bubbles and can fire per property; guard both.
          if (e.target !== e.currentTarget) return;
          if (e.propertyName !== "transform") return;
          if (settledRef.current) return; // double-fire guard
          settledRef.current = true;
          // Rebuild with the result on top; reset transform un-transitioned.
          setStrip(targetRef.current);
          setOffset(0);
          setAnimate(false);
          setStopFlash(true);
          window.setTimeout(() => setStopFlash(false), 350);
          onSettled();
        }}
      >
        {strip.map((symbolIndex, i) => {
          const committedRow = i; // after settle, strip = target cells
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
              <PixelSymbol index={symbolIndex} scale={cellPx === 96 ? 5 : 4} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ---- payline overlay geometry ----

export function overlayGeometry(cellPx: number) {
  const gap = 8;
  const pad = 8;
  const width = REELS * cellPx + (REELS - 1) * gap + 2 * pad;
  const height = ROWS * cellPx + (ROWS - 1) * gap + 2 * pad;
  const cx = (c: number) => pad + c * (cellPx + gap) + cellPx / 2;
  const cy = (r: number) => pad + r * (cellPx + gap) + cellPx / 2;
  const points = (rows: number[]) =>
    rows.map((r, c) => `${cx(c)},${cy(r)}`).join(" ");
  const strokeLine = (rows: number[]) => {
    if (rows.every((r) => r === rows[0])) {
      return `8,${cy(rows[0])} ${width - 8},${cy(rows[0])}`;
    }
    return rows.map((r, c) => `${cx(c)},${cy(r)}`).join(" ");
  };
  return { width, height, points, strokeLine };
}

function winRowsByColumn(lines: number[]): Set<number>[] {
  const sets: Set<number>[] = [];
  for (let c = 0; c < REELS; c++) sets.push(new Set<number>());
  for (const l of lines) {
    const rows = PAYLINE_ROWS[l];
    rows.forEach((r, c) => sets[c].add(r));
  }
  return sets;
}

export default function ReelWindow({
  spin,
  winningLines,
  cellPx,
  onAllSettled,
}: {
  spin: SpinRequest | null;
  /** Winning paylines to stroke, shown during the win celebration only. */
  winningLines: number[] | null;
  cellPx: number;
  onAllSettled: () => void;
}) {
  // Columns: committed[c] = top-to-bottom symbols of reel c.
  const [committed, setCommitted] = useState<number[][]>(
    Array.from({ length: REELS }, (_, c) => [(0 + c) % 8, (3 + c) % 8, (6 + c) % 8]),
  );
  const settledCount = useRef(0);
  const spinRef = useRef<SpinRequest | null>(null);

  useEffect(() => {
    spinRef.current = spin;
  }, [spin]);

  // Reset the settle counter per spin, plus a safety net in case a
  // transitionend is ever missed: commit anyway after the worst-case time.
  useEffect(() => {
    if (!spin) return;
    settledCount.current = 0;
    const worstCase = SPIN_MS + (REELS - 1) * STAGGER_MS + SETTLE_GRACE_MS;
    const timer = setTimeout(() => {
      if (settledCount.current < REELS) {
        settledCount.current = REELS;
        setCommitted(spin.grid.map((_, c) => spin.grid.map((row) => row[c])));
        onAllSettled();
      }
    }, worstCase);
    return () => clearTimeout(timer);
  }, [spin, onAllSettled]);

  const handleSettled = () => {
    sound.reelStop(settledCount.current);
    settledCount.current += 1;
    if (settledCount.current === REELS && spinRef.current) {
      const grid = spinRef.current.grid;
      setCommitted(grid.map((_, c) => grid.map((row) => row[c])));
      onAllSettled();
    }
  };

  const winRows = winningLines ? winRowsByColumn(winningLines) : null;
  const geo = useMemo(() => overlayGeometry(cellPx), [cellPx]);

  // Coin rain: deterministic per celebration (index-derived offsets).
  const coins = useMemo(() => {
    if (!winningLines || winningLines.length === 0) return [];
    return Array.from({ length: 28 }, (_, i) => ({
      left: ((i * 137 + 41) % 100) + "%",
      delay: ((i * 97) % 500) + "ms",
      dur: 420 + ((i * 61) % 260) + "ms",
    }));
  }, [winningLines]);

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
              winRows={winRows ? winRows[c] : new Set<number>()}
              onSettled={handleSettled}
            />
          ))}
        </div>
        {winningLines && winningLines.length > 0 && (
          <>
            <svg
              viewBox={`0 0 ${geo.width} ${geo.height}`}
              className="pointer-events-none absolute inset-0 h-full w-full"
              shapeRendering="crispEdges"
              aria-hidden
            >
              {winningLines.map((l, i) => (
                <polyline
                  key={l}
                  points={geo.strokeLine(PAYLINE_ROWS[l])}
                  fill="none"
                  stroke={i % 2 === 0 ? "var(--color-cyan)" : "var(--color-magenta)"}
                  strokeWidth={6}
                  className="payline-stroke"
                  style={{ animationDelay: `${i * 200}ms` }}
                />
              ))}
            </svg>
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
    </div>
  );
}
