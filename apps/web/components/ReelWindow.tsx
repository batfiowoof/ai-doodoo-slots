"use client";

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import PixelSymbol from "./PixelSymbol";
import { sound } from "@/lib/sound";

// The spin is theater played over a server-known result. Per reel: a strip of
// [current window, filler, target window]; a two-stage motion — constant
// linear travel stopping one window short, then a short eased settle onto the
// result. Anticipation holds stretch the linear stage of later reels; the
// hold is announced by the landing of the previous reel, never a timer.

const GAP = 6;
const STAGGER = 150;
const LINEAR_MS = 550;
const SETTLE_MS = 340;
const FILLER = 16;

export interface SpinSpec {
  id: number;
  /** Per column: the rows target symbols delivered by the server. */
  targets: number[][];
  /** Per column: anticipation hold in ms (0 = none). */
  holds: number[];
  /** Per column: already-landed cells to pulse hot, keyed "col:row". */
  hotFor: Record<string, Record<string, boolean>>;
}

export interface Anticipation {
  reel: number;
  level: number;
  hot: Record<string, boolean>;
}

interface ReelState {
  strip: number[];
  len: number;
  phase: 0 | 1 | 2;
  y: number;
  trans: string;
}

export default function ReelWindow({
  cols,
  rows,
  cell,
  sprite,
  icons,
  symbolCount,
  mode,
  lines,
  spec,
  skipToken,
  ant,
  winCells,
  paylineIdx,
  error,
  winBanner,
  onAnticipation,
  onAllSettled,
}: {
  cols: number;
  rows: number;
  cell: number;
  sprite: number;
  icons: (string | undefined)[];
  symbolCount: number;
  mode: "lines" | "scatter";
  /** Payline row table for this game (rows per reel). */
  lines: number[][];
  spec: SpinSpec | null;
  /** Bump to request a skip of the in-flight spin. */
  skipToken: number;
  ant: Anticipation | null;
  /** Winning cells keyed "col:row", shown during the celebration only. */
  winCells: Record<string, boolean> | null;
  /** Winning payline indices, line games only. */
  paylineIdx: number[];
  error: string | null;
  winBanner: ReactNode;
  onAnticipation: (ant: Anticipation | null) => void;
  onAllSettled: () => void;
}) {
  const reduced = useMemo(
    () =>
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    [],
  );

  const [reels, setReelsState] = useState<ReelState[]>(() =>
    Array.from({ length: cols }, (_, c) => {
      const strip = Array.from(
        { length: rows },
        (_, r) => (c * 2 + r * 3 + 1) % symbolCount,
      );
      return { strip, len: rows, phase: 0 as const, y: 0, trans: "none" };
    }),
  );
  const reelsRef = useRef(reels);
  const setReels = (updater: (prev: ReelState[]) => ReelState[]) => {
    setReelsState((prev) => {
      const next = updater(prev);
      reelsRef.current = next;
      return next;
    });
  };

  const specRef = useRef<SpinSpec | null>(null);
  const settledRef = useRef<Set<number>>(new Set());
  const skippedRef = useRef(false);
  const liveRef = useRef(false);
  const antRef = useRef<Anticipation | null>(null);
  const safetyRef = useRef<number | null>(null);
  const skipSeenRef = useRef(0);

  const settle = (c: number) => {
    const sp = specRef.current;
    if (!sp || !liveRef.current) return;
    if (settledRef.current.has(c)) return;
    settledRef.current.add(c);
    sound.reelStop(c);
    setReels((prev) =>
      prev.map((r, i) =>
        i === c
          ? { ...r, strip: sp.targets[c], len: rows, phase: 0, y: 0, trans: "none" }
          : r,
      ),
    );
    let nextAnt =
      antRef.current && antRef.current.reel === c ? null : antRef.current;
    const next = c + 1;
    if (!skippedRef.current && next < cols && sp.holds[next] > 0) {
      nextAnt = {
        reel: next,
        level: mode === "scatter" ? 3 : next - 1,
        hot: sp.hotFor[next] ?? {},
      };
      sound.anticipate(nextAnt.level, sp.holds[next]);
    }
    if (nextAnt !== antRef.current) {
      antRef.current = nextAnt;
      onAnticipation(nextAnt);
    }
    if (settledRef.current.size === cols) {
      liveRef.current = false;
      onAllSettled();
    }
  };

  // Spin launch: build strips, then (two frames in) start the two-stage move.
  useEffect(() => {
    if (!spec || spec.id === 0) return;
    specRef.current = spec;
    settledRef.current = new Set();
    skippedRef.current = false;
    liveRef.current = true;
    antRef.current = null;
    onAnticipation(null);

    const holds = reduced ? spec.holds.map(() => 0) : spec.holds;
    const linear = reduced ? 300 : LINEAR_MS;
    const settleMs = reduced ? 120 : SETTLE_MS;

    setReels((prev) =>
      prev.map((r, c) => {
        const count = FILLER + Math.round(holds[c] / 42);
        const filler = Array.from(
          { length: count },
          () => Math.floor(Math.random() * symbolCount), // decorative only, never a bet path
        );
        const strip = [...r.strip.slice(0, rows), ...filler, ...spec.targets[c]];
        return { strip, len: strip.length, phase: 0 as const, y: 0, trans: "none" };
      }),
    );

    const starts: number[] = [];
    let acc = 0;
    let worst = 0;
    for (let c = 0; c < cols; c++) {
      starts[c] = c * STAGGER + acc;
      acc += holds[c];
      worst = Math.max(worst, starts[c] + linear + holds[c] + settleMs);
    }

    if (safetyRef.current !== null) clearTimeout(safetyRef.current);
    const id = spec.id;
    // Safety net derived from the slowest reel: force-land everything if a
    // transitionend is ever missed. Cancelled by the next spin.
    safetyRef.current = window.setTimeout(() => {
      if (specRef.current?.id !== id) return;
      for (let c = 0; c < cols; c++) settle(c);
    }, worst + 500);

    let raf2 = 0;
    const raf1 = requestAnimationFrame(() => {
      raf2 = requestAnimationFrame(() => {
        if (specRef.current?.id !== id) return;
        setReels((prev) =>
          prev.map((r, c) => ({
            ...r,
            phase: 1,
            y: -(r.len - 2 * rows) * cell,
            trans: `transform ${linear + holds[c]}ms linear ${starts[c]}ms`,
          })),
        );
      });
    });
    return () => {
      cancelAnimationFrame(raf1);
      cancelAnimationFrame(raf2);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [spec?.id]);

  // Skip: drop the anticipation hold and re-aim every unsettled reel at its
  // result on a short, fast curve, staggered in reel order.
  useEffect(() => {
    if (skipToken === 0 || skipSeenRef.current === skipToken) return;
    skipSeenRef.current = skipToken;
    if (!liveRef.current || skippedRef.current) return;
    skippedRef.current = true;
    sound.click();
    if (antRef.current) {
      antRef.current = null;
      onAnticipation(null);
    }
    let n = 0;
    setReels((prev) =>
      prev.map((r, c) => {
        if (settledRef.current.has(c)) return r;
        const delay = n++ * 55;
        return {
          ...r,
          phase: 2,
          y: -(r.len - rows) * cell,
          trans: `transform 190ms cubic-bezier(.25,.9,.3,1.04) ${delay}ms`,
        };
      }),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [skipToken]);

  useEffect(() => {
    return () => {
      if (safetyRef.current !== null) clearTimeout(safetyRef.current);
    };
  }, []);

  const onReelEnd = (c: number) => (e: React.TransitionEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget || e.propertyName !== "transform") return;
    const r = reelsRef.current[c];
    if (!r || settledRef.current.has(c)) return;
    if (r.phase === 1) {
      setReels((prev) =>
        prev.map((x, i) =>
          i === c
            ? {
                ...x,
                phase: 2,
                y: -(x.len - rows) * cell,
                trans: `transform ${SETTLE_MS}ms cubic-bezier(.16,.84,.3,1.02)`,
              }
            : x,
        ),
      );
      return;
    }
    settle(c);
  };

  // ---- celebration geometry ----

  const gridW = cols * cell + (cols - 1) * GAP;
  const gridH = rows * cell;
  const cx = (c: number) => c * (cell + GAP) + cell / 2;
  const cy = (r: number) => r * cell + cell / 2;
  const paylines =
    mode === "lines"
      ? paylineIdx.map((li, i) => ({
          key: li,
          points: (lines[li] ?? []).map((r, c) => `${cx(c)},${cy(r)}`).join(" "),
          stroke: i % 2 === 0 ? "#22e8ff" : "#ff2d95",
          delay: `${i * 160}ms`,
        }))
      : [];

  return (
    <div style={{ position: "relative", overflow: "hidden" }}>
      <div style={{ display: "flex", gap: GAP }}>
        {reels.map((r, c) => (
          <div
            key={c}
            style={{
              overflow: "hidden",
              height: rows * cell,
              animation:
                ant && ant.reel === c ? "teaseWobble .14s steps(1) infinite" : "none",
            }}
          >
            <div
              className="reel-strip"
              style={{
                transform: `translateY(${r.y}px)`,
                transition: r.trans,
              }}
              onTransitionEnd={onReelEnd(c)}
            >
              {r.strip.map((sym, i) => {
                const isWin =
                  !!winCells && winCells[`${c}:${i}`] === true;
                const isHot =
                  !!ant && !!ant.hot[`${c}:${i}`] && c < ant.reel;
                return (
                  <div
                    key={i}
                    style={{
                      position: "relative",
                      width: cell,
                      height: cell,
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "center",
                      background: "#0c0718",
                      boxShadow: "inset 0 0 0 1px #241640",
                    }}
                  >
                    <PixelSymbol
                      index={sym}
                      icon={icons[sym]}
                      scale={sprite / 32}
                    />
                    {isWin && (
                      <div
                        style={{
                          position: "absolute",
                          inset: 0,
                          animation: "cellWin .34s steps(1) infinite",
                          mixBlendMode: "screen",
                        }}
                      />
                    )}
                    {isHot && (
                      <div
                        style={{
                          position: "absolute",
                          inset: 0,
                          animation: "cellHot .3s steps(1) infinite",
                          pointerEvents: "none",
                        }}
                      />
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>

      {paylines.length > 0 && (
        <svg
          viewBox={`0 0 ${gridW} ${gridH}`}
          style={{
            position: "absolute",
            inset: 0,
            width: "100%",
            height: "100%",
            pointerEvents: "none",
          }}
          aria-hidden
        >
          {paylines.map((l) => (
            <polyline
              key={l.key}
              points={l.points}
              fill="none"
              stroke={l.stroke}
              strokeWidth={4}
              strokeLinejoin="round"
              style={{
                filter: `drop-shadow(0 0 6px ${l.stroke})`,
                strokeDasharray: 900,
                strokeDashoffset: 900,
                animation: `paylineDraw .55s steps(11) forwards ${l.delay}`,
              }}
            />
          ))}
        </svg>
      )}

      <div
        style={{
          position: "absolute",
          inset: 0,
          pointerEvents: "none",
          background:
            "repeating-linear-gradient(to bottom, rgba(0,0,0,.34) 0 2px, rgba(0,0,0,0) 2px 4px)",
        }}
      />
      <div
        style={{
          position: "absolute",
          inset: 0,
          pointerEvents: "none",
          background:
            "linear-gradient(rgba(34,232,255,.10), transparent 40%, transparent 60%, rgba(255,45,149,.10))",
        }}
      />

      {ant && (
        <>
          <div
            style={{
              position: "absolute",
              inset: 0,
              pointerEvents: "none",
              animation: `antRing ${
                ant.level >= 3 ? ".26s" : ant.level === 2 ? ".34s" : ".44s"
              } steps(1) infinite`,
            }}
          />
          <div
            style={{
              position: "absolute",
              left: 0,
              right: 0,
              top: 0,
              padding: 8,
              textAlign: "center",
              pointerEvents: "none",
            }}
          >
            <span
              style={{
                display: "inline-block",
                background: "rgba(6,4,13,.86)",
                padding: "6px 14px",
                fontFamily: "var(--font-display)",
                fontSize: 15,
                letterSpacing: 3,
                color: "#ff8a1f",
                textShadow: "0 0 14px rgba(255,138,31,.9)",
                animation: "antLabel .3s steps(1) infinite",
              }}
            >
              {mode === "scatter"
                ? `SCATTER PENDING · REEL ${ant.reel + 1}`
                : `${ant.level + 1} MATCHING · REEL ${ant.reel + 1}`}
            </span>
          </div>
        </>
      )}

      {winBanner}

      {error && (
        <div
          role="alert"
          style={{
            position: "absolute",
            left: 0,
            right: 0,
            top: 0,
            padding: 10,
            textAlign: "center",
            background: "rgba(6,4,13,.88)",
            borderBottom: "2px solid #ff8a1f",
          }}
        >
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 16,
              color: "#ff8a1f",
            }}
          >
            {error}
          </span>
        </div>
      )}
    </div>
  );
}
