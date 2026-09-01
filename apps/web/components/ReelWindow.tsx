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
const SPIN_MS = 1600;
const STAGGER_MS = 350;
const SETTLE_GRACE_MS = 600;

export interface SpinRequest {
  id: number;
  /** 3x3 grid, rows as delivered by the server. */
  grid: number[][];
}

interface ReelProps {
  /** Column currently committed on screen (before this spin). */
  start: number[];
  /** Column this spin must land on. */
  target: number[];
  spinId: number;
  delayMs: number;
  onSettled: () => void;
}

function Reel({ start, target, spinId, delayMs, onSettled }: ReelProps) {
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
        {strip.map((symbolIndex, i) => (
          <div
            key={i}
            className="pixelated flex items-center justify-center border-4 border-slate bg-ink"
            style={{ height: CELL_PX }}
          >
            <PixelSymbol index={symbolIndex} scale={8} />
          </div>
        ))}
      </div>
    </div>
  );
}

export default function ReelWindow({
  spin,
  onAllSettled,
}: {
  spin: SpinRequest | null;
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
    const worstCase =
      SPIN_MS + 2 * STAGGER_MS + SETTLE_GRACE_MS;
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

  return (
    <div className="my-4 border-8 border-stone bg-plum p-2">
      <div className="flex gap-2 bg-slate p-2">
        {committed.map((column, c) => (
          <Reel
            key={c}
            start={column}
            target={spin ? spin.grid.map((row) => row[c]) : column}
            spinId={spin?.id ?? 0}
            delayMs={c * STAGGER_MS}
            onSettled={handleSettled}
          />
        ))}
      </div>
    </div>
  );
}
