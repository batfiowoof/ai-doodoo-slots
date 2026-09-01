"use client";

import { useEffect, useRef, useState } from "react";
import PixelSymbol from "./PixelSymbol";

// Physical reels have momentum, so the reel spin is the one place the design
// system allows real easing: one CSS transition per reel, staggered. The
// outcome arrives before the animation starts — the spin is theater played
// over a known result.
const CELL_PX = 64; // 8 multiple
const WINDOW_CELLS = 3;
const RANDOM_CELLS = 27;
const STAGGER_MS = 350;
const SETTLE_GRACE_MS = 600;

export interface SpinRequest {
  id: number;
  /** 3x3 grid, rows as delivered by the server. */
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
  /** Rows (0-2) in this column that belong to a winning payline. */
  winRows: Set<number>;
  onSettled: () => void;
}

function Reel({ start, target, spinId, delayMs, winRows, onSettled }: ReelProps) {
  const [strip, setStrip] = useState<number[]>(start);
  const [offset, setOffset] = useState(0);
  const [animate, setAnimate] = useState(false);
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
        setOffset(-(full.length - WINDOW_CELLS) * CELL_PX);
      });
    });
    return () => {
      cancelAnimationFrame(raf1);
      cancelAnimationFrame(raf2);
    };
  }, [spinId]);

  return (
    <div
      className="overflow-hidden"
      style={{ height: WINDOW_CELLS * CELL_PX }}
    >
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
          onSettled();
        }}
      >
        {strip.map((symbolIndex, i) => {
          const committedRow = i; // after settle, strip = 3 target cells
          const isWinner = !animate && winRows.has(committedRow);
          return (
            <div
              key={i}
              className={`pixelated flex items-center justify-center border-4 bg-ink ${
                isWinner ? "cell-win" : "border-slate"
              }`}
              style={{ width: CELL_PX, height: CELL_PX }}
            >
              <PixelSymbol index={symbolIndex} scale={8} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

// Payline geometry: cells are 64px with 8px gaps inside an 8px-padded
// container, so cell centers sit at 40 + 72c. Strokes are hard-edged
// (crispEdges) and draw on with steps() — decorative motion lands on frames.
const LINE_POINTS = [
  "12,32 212,32", // top row
  "12,104 212,104", // middle row
  "12,176 212,176", // bottom row
  "40,40 112,112 184,184", // diagonal down
  "40,184 112,112 184,40", // diagonal up
];

function winRowsByColumn(lines: number[]): Set<number>[] {
  const sets = [new Set<number>(), new Set<number>(), new Set<number>()];
  for (const l of lines) {
    if (l <= 2) {
      for (let c = 0; c < 3; c++) sets[c].add(l);
    } else if (l === 3) {
      sets[0].add(0);
      sets[1].add(1);
      sets[2].add(2);
    } else {
      sets[0].add(2);
      sets[1].add(1);
      sets[2].add(0);
    }
  }
  return sets;
}

export default function ReelWindow({
  spin,
  winningLines,
  onAllSettled,
}: {
  spin: SpinRequest | null;
  /** Winning paylines to stroke, shown during the win celebration only. */
  winningLines: number[] | null;
  onAllSettled: () => void;
}) {
  // Columns: committed[c] = top-to-bottom symbols of reel c.
  const [committed, setCommitted] = useState<number[][]>([
    [0, 3, 6],
    [1, 4, 7],
    [2, 5, 0],
  ]);
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
    const worstCase = 1600 + 2 * STAGGER_MS + SETTLE_GRACE_MS;
    const timer = setTimeout(() => {
      if (settledCount.current < WINDOW_CELLS) {
        settledCount.current = WINDOW_CELLS;
        setCommitted(spin.grid.map((_, c) => spin.grid.map((row) => row[c])));
        onAllSettled();
      }
    }, worstCase);
    return () => clearTimeout(timer);
  }, [spin, onAllSettled]);

  const handleSettled = () => {
    settledCount.current += 1;
    if (settledCount.current === WINDOW_CELLS && spinRef.current) {
      const grid = spinRef.current.grid;
      setCommitted(grid.map((_, c) => grid.map((row) => row[c])));
      onAllSettled();
    }
  };

  const winRows = winningLines ? winRowsByColumn(winningLines) : null;

  return (
    <div className="my-4 border-8 border-stone bg-plum p-2">
      <div className="relative flex gap-2 bg-slate p-2">
        {committed.map((column, c) => (
          <Reel
            key={c}
            start={column}
            target={spin ? spin.grid.map((row) => row[c]) : column}
            spinId={spin?.id ?? 0}
            delayMs={c * STAGGER_MS}
            winRows={winRows ? winRows[c] : new Set<number>()}
            onSettled={handleSettled}
          />
        ))}
        {winningLines && winningLines.length > 0 && (
          <svg
            viewBox="0 0 224 224"
            className="pointer-events-none absolute inset-0 h-full w-full"
            shapeRendering="crispEdges"
            aria-hidden
          >
            {winningLines.map((l) => (
              <polyline
                key={l}
                points={LINE_POINTS[l]}
                fill="none"
                stroke="var(--color-cyan)"
                strokeWidth={6}
                className="payline-stroke"
              />
            ))}
          </svg>
        )}
      </div>
    </div>
  );
}
