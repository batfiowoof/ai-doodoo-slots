"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import ReelWindow, {
  type Anticipation,
  type SpinSpec,
} from "./ReelWindow";
import { PlayError, useFairCurrent, useGames, usePlay, useSession } from "@/lib/api";
import { sound } from "@/lib/sound";
import type { SlotsPaytable } from "@/lib/types";

const GAP = 6;

export interface OverlayState {
  spinKey: number;
  payout: number;
  winShown: number;
  summary: string;
  bigWin: boolean;
  coins: boolean;
}

type SpinPhase = "idle" | "lever" | "spinning" | "celebrating";

/** Cell size: fits the cabinet's 624px inner width and a 470px tall window. */
function metrics(cols: number, rows: number) {
  const cell = Math.min(
    Math.floor((624 - (cols - 1) * GAP) / cols),
    Math.floor(470 / rows),
  );
  const sprite = Math.max(32, Math.floor((cell - 16) / 32) * 32);
  return { cell, sprite };
}

/** Leading run length of a payline in a settled grid. */
function runLength(grid: number[][], lineRows: number[]): number {
  const sym = grid[lineRows[0]][0];
  let run = 1;
  for (let c = 1; c < lineRows.length; c++) {
    if (grid[lineRows[c]][c] !== sym) break;
    run++;
  }
  return run;
}

/** Which reels get held back, and which landed cells are already "hot". */
function anticipation(
  pt: SlotsPaytable,
  grid: number[][],
): Pick<SpinSpec, "holds" | "hotFor"> {
  const cols = pt.reels;
  const holds = Array.from({ length: cols }, () => 0);
  const hotFor: Record<string, Record<string, boolean>> = {};
  const last = cols - 1;

  if (pt.mode === "scatter") {
    // A paying symbol one short of its lowest tier, outside the last reel.
    let bestCount = -1;
    let bestCells: Record<string, boolean> | null = null;
    for (let si = 0; si < pt.symbols.length; si++) {
      const sym = pt.symbols[si];
      const tiers = Object.keys(sym.pays ?? {}).map(Number);
      if (tiers.length === 0) continue;
      const min = Math.min(...tiers);
      let count = 0;
      const cells: Record<string, boolean> = {};
      for (let r = 0; r < pt.rows; r++) {
        for (let c = 0; c < last; c++) {
          if (grid[r][c] === si) {
            count++;
            cells[`${c}:${r}`] = true;
          }
        }
      }
      if (count >= min - 1 && count > bestCount) {
        bestCount = count;
        bestCells = cells;
      }
    }
    if (bestCells) {
      holds[last] = 1350;
      hotFor[last] = bestCells;
    }
    return { holds, hotFor };
  }

  const runs = pt.lines.map((rows) => ({ rows, run: runLength(grid, rows) }));
  const from = Math.max(2, cols - 2);
  for (let c = from; c < cols; c++) {
    const live = runs.filter((r) => r.run >= c);
    if (live.length === 0) continue;
    holds[c] = c === cols - 1 ? 1350 : 1000;
    const hot: Record<string, boolean> = {};
    live.forEach((r) => {
      for (let k = 0; k < c; k++) hot[`${k}:${r.rows[k]}`] = true;
    });
    hotFor[c] = hot;
  }
  return { holds, hotFor };
}

export default function Cabinet({
  gameId,
  inert,
  onOverlay,
  bigWinDismissed,
}: {
  gameId: string;
  /** True while an overlay panel is open: the lever goes inert. */
  inert: boolean;
  onOverlay: (o: OverlayState | null) => void;
  /** The player tapped the big-win takeover away; fall back to the banner. */
  bigWinDismissed?: boolean;
}) {
  const session = useSession();
  const fair = useFairCurrent(session.isSuccess);
  const games = useGames();
  const play = usePlay();

  const info = games.data?.find((g) => g.id === gameId);
  const pt = info?.paytable ?? null;
  const cols = pt?.reels ?? 5;
  const rows = pt?.rows ?? 3;
  const mode = pt?.mode ?? "lines";
  const icons = pt?.icons ?? [];
  const symbolCount = pt?.symbols.length ?? 8;
  const betSteps = pt?.betSteps ?? [5, 10, 25, 50, 100];

  const { cell, sprite } = metrics(cols, rows);

  const [bet, setBet] = useState<number | null>(null);
  const effBet = bet ?? betSteps[1] ?? betSteps[0] ?? 10;
  const [phase, setPhase] = useState<SpinPhase>("idle");
  const [error, setError] = useState<string | null>(null);
  // Non-null only while a spin or celebration owns the display; otherwise the
  // credits odometer follows the server balance in the session cache.
  const [spinCredits, setSpinCredits] = useState<number | null>(null);
  const credits = spinCredits ?? session.data?.balanceCredits ?? null;
  const [ant, setAnt] = useState<Anticipation | null>(null);
  const [spec, setSpec] = useState<SpinSpec | null>(null);
  const [skipToken, setSkipToken] = useState(0);
  const [winShown, setWinShown] = useState(0);
  const [celebration, setCelebration] = useState<{
    payout: number;
    summary: string;
    bigWin: boolean;
    winCells: Record<string, boolean> | null;
    paylineIdx: number[];
  } | null>(null);

  const resultRef = useRef<{
    res: import("@/lib/api").PlayResponse;
    bet: number;
  } | null>(null);
  const overlayRef = useRef<OverlayState | null>(null);
  const timersRef = useRef<number[]>([]);
  const tickRef = useRef<number | null>(null);
  const spinIdRef = useRef(0);
  const pullRef = useRef<() => void>(() => {});

  const reduced = useMemo(
    () =>
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    [],
  );

  useEffect(() => {
    return () => {
      timersRef.current.forEach(clearTimeout);
      if (tickRef.current !== null) clearInterval(tickRef.current);
      sound.stopWhir();
    };
  }, []);

  const later = (fn: () => void, ms: number) => {
    const t = window.setTimeout(fn, ms);
    timersRef.current.push(t);
    return t;
  };

  const clearTimers = () => {
    timersRef.current.forEach(clearTimeout);
    timersRef.current = [];
  };

  const handleAnticipation = (a: Anticipation | null) => {
    setAnt(a);
  };

  const celebrate = () => {
    const r = resultRef.current;
    if (!r || !pt) return;
    const { res, bet: betAtSpin } = r;
    const payout = res.payoutCredits;
    if (payout <= 0) {
      setSpinCredits(res.balanceCredits);
      setPhase("idle");
      return;
    }

    const grid = res.outcome.grid;
    const lines = res.outcome.winningLines ?? [];
    const scatter = res.outcome.scatterWins ?? [];

    const winCells: Record<string, boolean> = {};
    if (mode === "scatter") {
      const wins = scatter.map((s) => s.symbol);
      for (let rr = 0; rr < pt.rows; rr++) {
        for (let cc = 0; cc < pt.reels; cc++) {
          if (wins.includes(grid[rr][cc])) winCells[`${cc}:${rr}`] = true;
        }
      }
    } else {
      lines.forEach((li) => {
        const lineRows = pt.lines[li];
        if (!lineRows) return;
        const run = runLength(grid, lineRows);
        for (let c = 0; c < run; c++) winCells[`${c}:${lineRows[c]}`] = true;
      });
    }

    const summary =
      mode === "scatter"
        ? scatter
            .map(
              (w) =>
                `${(pt.symbols[w.symbol]?.name ?? "?").toUpperCase()} ×${w.count}`,
            )
            .join(" · ")
        : `${lines.length} ${lines.length === 1 ? "LINE" : "LINES"}`;

    const big = payout >= betAtSpin * 20;
    const coins = big || payout >= betAtSpin * 6;

    setPhase("celebrating");
    setCelebration({ payout, summary, bigWin: big, winCells, paylineIdx: lines });
    setWinShown(0);
    sound.jackpot(lines.length + scatter.length);
    if (big) sound.bigWin();

    const overlay: OverlayState = {
      spinKey: res.betId,
      payout,
      winShown: 0,
      summary,
      bigWin: big,
      coins,
    };
    overlayRef.current = overlay;
    onOverlay(overlay);

    later(
      () => {
        setPhase("idle");
        setCelebration(null);
        setWinShown(0);
        setAnt(null);
        setSpinCredits(res.balanceCredits);
        overlayRef.current = null;
        onOverlay(null);
      },
      big ? 5200 : 3000,
    );

    if (reduced) {
      setWinShown(payout);
      setSpinCredits(res.balanceCredits + payout);
      overlayRef.current = { ...overlay, winShown: payout };
      onOverlay(overlayRef.current);
      return;
    }
    const step = Math.max(1, Math.ceil(payout / 18));
    let shown = 0;
    if (tickRef.current !== null) clearInterval(tickRef.current);
    tickRef.current = window.setInterval(() => {
      shown = Math.min(payout, shown + step);
      sound.winTick(shown);
      setWinShown(shown);
      setSpinCredits(res.balanceCredits + shown);
      if (overlayRef.current) {
        overlayRef.current = { ...overlayRef.current, winShown: shown };
        onOverlay(overlayRef.current);
      }
      if (shown >= payout && tickRef.current !== null) {
        clearInterval(tickRef.current);
        tickRef.current = null;
      }
    }, 70);
  };

  const handleAllSettled = () => {
    sound.stopWhir();
    celebrate();
  };

  const pull = () => {
    if (!pt || inert) return;
    if (phase === "spinning") {
      setSkipToken((t) => t + 1);
      return;
    }
    if (phase !== "idle" || play.isPending) return;
    sound.unlock();
    if (credits !== null && credits < effBet) {
      setError("INSUFFICIENT CREDITS");
      sound.error();
      later(() => setError(null), 1600);
      return;
    }
    if (!fair.data) {
      setError("CASINO UNREACHABLE");
      sound.error();
      later(() => setError(null), 1600);
      return;
    }
    sound.lever();
    clearTimers();
    setError(null);
    setCelebration(null);
    setWinShown(0);
    setAnt(null);
    if (credits !== null) setSpinCredits(credits - effBet);
    setPhase("lever");

    const clientSeed = fair.data.clientSeed;
    const betAtSpin = effBet;
    later(() => {
      play.mutate(
        { gameId, betCredits: betAtSpin, clientSeed },
        {
          onSuccess: (res) => {
            spinIdRef.current += 1;
            resultRef.current = { res, bet: betAtSpin };
            const grid = res.outcome.grid;
            const targets = grid[0].map((_, c) => grid.map((row) => row[c]));
            const { holds, hotFor } = anticipation(pt, grid);
            setSpec({ id: spinIdRef.current, targets, holds, hotFor });
            setSpinCredits(res.balanceCredits);
            setPhase("spinning");
            sound.startWhir();
          },
          onError: (err) => {
            setPhase("idle");
            sound.error();
            setSpinCredits(null);
            if (err instanceof PlayError) {
              switch (err.status) {
                case 402:
                  setError("INSUFFICIENT CREDITS");
                  break;
                case 429:
                  setError("SLOW DOWN");
                  break;
                case 403:
                  setError("BETTING BLOCKED");
                  break;
                default:
                  setError("SPIN REJECTED");
              }
            } else {
              setError("CASINO UNREACHABLE");
            }
            later(() => setError(null), 1600);
          },
        },
      );
    }, 340);
  };

  // Keep the key handler and lever column pointed at the latest pull.
  useEffect(() => {
    pullRef.current = pull;
  });

  // Spacebar pulls (or skips), exactly like clicking the lever.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.code === "Space") {
        e.preventDefault();
        pullRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const busy = phase !== "idle" || play.isPending;
  const celebrating = phase === "celebrating" && celebration !== null;

  const winBanner =
    celebrating &&
    celebration.payout > 0 &&
    !(celebration.bigWin && !bigWinDismissed) ? (
      <div
        style={{
          position: "absolute",
          left: 0,
          right: 0,
          bottom: 0,
          padding: 10,
          textAlign: "center",
          background: "rgba(6,4,13,.82)",
          borderTop: "2px solid #22e8ff",
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 26,
            color: "#22e8ff",
            textShadow: "0 0 14px rgba(34,232,255,.9)",
          }}
        >
          WIN {winShown.toLocaleString()}
        </span>
        <span style={{ fontSize: 19, color: "#ff2d95", marginLeft: 14 }}>
          {celebration.summary}
        </span>
      </div>
    ) : null;

  const digitStr = String(Math.max(0, credits ?? 0))
    .padStart(4, "0")
    .split("");
  const pulled = phase === "lever";
  const leverTrans = pulled
    ? "top 170ms cubic-bezier(.45,0,1,1), height 170ms cubic-bezier(.45,0,1,1)"
    : "top 560ms cubic-bezier(.34,1.5,.64,1), height 560ms cubic-bezier(.34,1.5,.64,1)";

  const statusLine = ant
    ? `HOLDING REEL ${ant.reel + 1}…`
    : phase === "spinning"
      ? "REELS IN MOTION"
      : phase === "lever"
        ? "LEVER RELEASED"
        : phase === "celebrating"
          ? "PAYING OUT"
          : `READY · BET ${effBet}`;

  return (
    <div
      style={{
        display: "flex",
        alignItems: "stretch",
        gap: 14,
        animation:
          celebrating && celebration.payout > 0
            ? "cabShake .28s steps(1) 4"
            : "none",
      }}
    >
      <div
        style={{
          width: 660,
          padding: 14,
          background: "linear-gradient(#170c2b,#0d0619)",
          border: "2px solid #35205c",
          boxShadow:
            "0 0 60px rgba(157,77,255,.28), inset 0 1px 0 rgba(236,230,255,.12)",
        }}
      >
        {/* Marquee */}
        <div
          style={{
            border: "2px solid #ff2d95",
            background: "#1a0d2e",
            boxShadow:
              "0 0 26px rgba(255,45,149,.45), inset 0 0 34px rgba(255,45,149,.14)",
            padding: "12px 14px 14px",
            textAlign: "center",
          }}
        >
          <div style={{ display: "flex", justifyContent: "center", gap: 9, marginBottom: 10 }}>
            {Array.from({ length: 22 }, (_, i) => (
              <span
                key={i}
                style={{
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  background: "#4a2350",
                  animation: "bulb 1.1s steps(1) infinite",
                  animationDelay: `${Math.round((i * 1100) / 22)}ms`,
                }}
              />
            ))}
          </div>
          <div
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 38,
              letterSpacing: 5,
              color: "#fff",
              animation: "titleGlow 2.4s ease-in-out infinite",
            }}
          >
            {(info?.name ?? "SLOTS").toUpperCase()}
          </div>
          <div
            style={{
              marginTop: 8,
              fontFamily: "var(--font-body)",
              fontSize: 19,
              letterSpacing: 4,
              color: "#22e8ff",
            }}
          >
            {cols} REELS ·{" "}
            {mode === "scatter" ? "SCATTER PAYS" : `${pt?.paylines ?? 0} LINES`} ·
            PROVABLY FAIR
          </div>
        </div>

        {/* Reel window */}
        <div
          style={{
            margin: "14px 0",
            padding: 10,
            background: "#06040d",
            border: "2px solid #22e8ff",
            boxShadow:
              "0 0 24px rgba(34,232,255,.3), inset 0 0 40px rgba(34,232,255,.08)",
            display: "flex",
            justifyContent: "center",
          }}
        >
          <ReelWindow
            key={`${gameId}:${cols}:${rows}:${symbolCount}`}
            cols={cols}
            rows={rows}
            cell={cell}
            sprite={sprite}
            icons={icons}
            symbolCount={symbolCount}
            mode={mode}
            lines={pt?.lines ?? []}
            spec={spec}
            skipToken={skipToken}
            ant={ant}
            winCells={celebrating ? celebration.winCells : null}
            paylineIdx={celebrating && mode === "lines" ? celebration.paylineIdx : []}
            error={error}
            winBanner={winBanner}
            onAnticipation={handleAnticipation}
            onAllSettled={handleAllSettled}
          />
        </div>

        {/* Deck */}
        <div
          style={{
            background: "#130a24",
            border: "2px solid #35205c",
            padding: 14,
            display: "flex",
            flexDirection: "column",
            gap: 12,
          }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "flex-end",
              justifyContent: "space-between",
              gap: 16,
            }}
          >
            <div>
              <div
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 11,
                  letterSpacing: 2,
                  color: "#8878b8",
                  marginBottom: 6,
                }}
              >
                CREDITS
              </div>
              {credits === null ? (
                <div style={{ fontFamily: "var(--font-body)", fontSize: 36, color: "#5c4f80" }}>
                  ····
                </div>
              ) : (
                <div style={{ display: "flex", gap: 4 }}>
                  {digitStr.map((d, i) => (
                    <div
                      key={i}
                      style={{
                        height: 44,
                        width: 30,
                        overflow: "hidden",
                        background: "#06040d",
                        boxShadow:
                          "inset 0 0 0 1px #35205c, inset 0 0 14px rgba(255,138,31,.18)",
                      }}
                    >
                      <div
                        style={{
                          transform: `translateY(-${parseInt(d, 10) * 44}px)`,
                          transition:
                            "transform 380ms cubic-bezier(.2,.85,.2,1)",
                        }}
                      >
                        {Array.from({ length: 10 }, (_, digit) => (
                          <div
                            key={digit}
                            style={{
                              height: 44,
                              lineHeight: "44px",
                              textAlign: "center",
                              fontFamily: "var(--font-body)",
                              fontSize: 36,
                              color: "#ff8a1f",
                              textShadow: "0 0 10px rgba(255,138,31,.7)",
                            }}
                          >
                            {digit}
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            <div style={{ textAlign: "right" }}>
              <div
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 11,
                  letterSpacing: 2,
                  color: "#8878b8",
                  marginBottom: 6,
                }}
              >
                BET PER SPIN
              </div>
              <div style={{ display: "flex", gap: 6 }}>
                {betSteps.map((b) => {
                  const selected = b === effBet;
                  return (
                    <button
                      key={b}
                      type="button"
                      onClick={() => {
                        if (busy) return;
                        sound.unlock();
                        sound.click();
                        setBet(b);
                      }}
                      style={{
                        width: 62,
                        padding: "11px 0",
                        cursor: busy ? "not-allowed" : "pointer",
                        fontFamily: "var(--font-display)",
                        fontSize: 14,
                        border: `2px solid ${selected ? "#22e8ff" : "#35205c"}`,
                        background: selected ? "#0b2a33" : "#0d0619",
                        color: selected ? "#22e8ff" : "#8878b8",
                        boxShadow: selected
                          ? "0 0 18px rgba(34,232,255,.5)"
                          : "none",
                      }}
                    >
                      {b}
                    </button>
                  );
                })}
              </div>
            </div>
          </div>

          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: 12,
              borderTop: "1px solid #241640",
              paddingTop: 10,
            }}
          >
            <span style={{ fontFamily: "var(--font-body)", fontSize: 18, color: "#8878b8" }}>
              {statusLine}
            </span>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 11,
                letterSpacing: 2,
                color: "#22e8ff",
                animation: "hintBlink 1.4s steps(1) infinite",
              }}
            >
              {phase === "idle" ? "PULL THE LEVER" : phase === "spinning" ? "PULL AGAIN TO SKIP" : "…"}
            </span>
          </div>
        </div>

        {/* Coin door strip */}
        <div
          style={{
            marginTop: 12,
            textAlign: "center",
            fontFamily: "var(--font-body)",
            fontSize: 17,
            letterSpacing: 1,
            color: "#8878b8",
          }}
        >
          <span style={{ color: "#ff2d95" }}>PLAY FOR FUN</span> — NO CASH VALUE ·{" "}
          {mode === "scatter" ? "SCATTER PAYS" : `${pt?.paylines ?? 0} LINES`}
        </div>
      </div>

      {/* Lever column */}
      <div
        onClick={() => pullRef.current()}
        role="button"
        aria-label="Pull the lever"
        style={{
          width: 96,
          padding: "14px 0",
          background: "linear-gradient(#170c2b,#0d0619)",
          border: "2px solid #35205c",
          boxShadow: "0 0 40px rgba(157,77,255,.22)",
          cursor: inert ? "default" : "pointer",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 14,
        }}
      >
        <div style={{ position: "relative", width: "100%", height: 320 }}>
          <div
            style={{
              position: "absolute",
              left: "50%",
              marginLeft: -11,
              top: 0,
              bottom: 0,
              width: 22,
              background:
                "linear-gradient(90deg,#0d0619,#2c1c4d 45%,#0d0619)",
              boxShadow: "inset 0 0 0 1px #35205c",
            }}
          />
          <div
            style={{
              position: "absolute",
              left: "50%",
              marginLeft: -6,
              top: 0,
              width: 12,
              height: pulled ? 297 : 23,
              background:
                "linear-gradient(90deg,#4a3a72,#ece6ff 42%,#3a2c5c)",
              boxShadow: "0 0 10px rgba(236,230,255,.3)",
              transition: leverTrans,
            }}
          />
          <div
            style={{
              position: "absolute",
              left: "50%",
              marginLeft: -23,
              top: pulled ? 274 : 0,
              width: 46,
              height: 46,
              borderRadius: "50%",
              background:
                "radial-gradient(circle at 34% 30%, #fff, #ff2d95 42%, #8a0d46)",
              boxShadow: "0 0 26px rgba(255,45,149,.8)",
              transition: leverTrans,
            }}
          />
        </div>
        <div
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 10,
            letterSpacing: 2,
            color: "#8878b8",
          }}
        >
          {phase === "spinning" ? "SKIP" : "PULL"}
        </div>
      </div>
    </div>
  );
}
