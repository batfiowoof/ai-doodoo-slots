"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import ReelWindow, {
  type Anticipation,
  type SpinSpec,
} from "./ReelWindow";
import BetInput from "./BetInput";
import { PlayError, useFairCurrent, useGames, usePlay, useSession } from "@/lib/api";
import { sound } from "@/lib/sound";
import type { BonusOutcome, SlotsPaytable } from "@/lib/types";

const GAP = 6;

export interface OverlayState {
  spinKey: number;
  payout: number;
  winShown: number;
  summary: string;
  bigWin: boolean;
  coins: boolean;
}

type SpinPhase = "idle" | "lever" | "spinning" | "celebrating" | "bonus";

/** Where the free spins sequence stands while it plays out. */
interface BonusRuntime {
  round: BonusOutcome;
  /** Next spin index to play. */
  index: number;
  /** Bonus credits won so far (meter counts up per spin). */
  won: number;
  bet: number;
}

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
  // The bonus trigger symbol teases one below its trigger count — two landed
  // bonus scatters means the whole cabinet holds its breath.
  const bonusPt = pt.bonus;
  const bonusSym =
    bonusPt != null
      ? pt.symbols.findIndex((s) => s.name === bonusPt.symbol)
      : -1;
  const bonusTrigger = bonusPt
    ? Math.min(...Object.keys(bonusPt.triggerSpins).map(Number))
    : 0;

  if (pt.mode === "scatter") {
    // A paying symbol one short of its lowest tier, outside the last reel.
    let bestCount = -1;
    let bestCells: Record<string, boolean> | null = null;
    for (let si = 0; si < pt.symbols.length; si++) {
      const sym = pt.symbols[si];
      const tiers = Object.keys(sym.pays ?? {}).map(Number);
      const min =
        si === bonusSym && bonusTrigger > 0
          ? bonusTrigger
          : tiers.length === 0
            ? 0
            : Math.min(...tiers);
      if (min === 0) continue;
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
  // Symbols with no pays at all — dimmed while the boosted bonus reels run.
  const deadSymbols = pt
    ? pt.symbols
        .map((s, i) => (Object.keys(s.pays ?? {}).length === 0 ? i : -1))
        .filter((i) => i >= 0)
    : [];

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
  // Free spins: intro takeover -> sequential spins -> summary. State drives
  // rendering; the ref drives the imperative sequence.
  const [bonus, setBonus] = useState<BonusRuntime | null>(null);
  const bonusRef = useRef<BonusRuntime | null>(null);
  const [bonusIntro, setBonusIntro] = useState<BonusOutcome | null>(null);
  const [spinPop, setSpinPop] = useState<{
    payout: number;
    retrigger: boolean;
    total: number;
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

  /** Win meter count-up shared by the base celebration and bonus summary. */
  const runCountUp = (
    payout: number,
    base: number,
    overlay: OverlayState | null,
  ) => {
    if (reduced) {
      setWinShown(payout);
      setSpinCredits(base + payout);
      overlayRef.current = overlay ? { ...overlay, winShown: payout } : null;
      onOverlay(overlay ? overlayRef.current! : null);
      return;
    }
    const step = Math.max(1, Math.ceil(payout / 18));
    let shown = 0;
    if (tickRef.current !== null) clearInterval(tickRef.current);
    tickRef.current = window.setInterval(() => {
      shown = Math.min(payout, shown + step);
      sound.winTick(shown);
      setWinShown(shown);
      setSpinCredits(base + shown);
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

  const celebrate = () => {
    const r = resultRef.current;
    if (!r || !pt) return;
    const { res, bet: betAtSpin } = r;
    const payout = res.payoutCredits;
    const bonusRound = res.outcome.bonus ?? null;
    // Meter base = balance after the stake debit; the payout counts up on top.
    const base = res.balanceCredits - payout;
    if (payout <= 0 && !bonusRound) {
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
    if (payout > 0) {
      sound.jackpot(lines.length + scatter.length);
      if (big) sound.bigWin();
    }

    if (bonusRound) {
      // Short base-beat (usually zero), then the takeover owns the screen.
      later(
        () => startBonus(bonusRound, betAtSpin),
        reduced ? 400 : payout > 0 ? 1400 : 550,
      );
      if (payout > 0 && !reduced) {
        const step = Math.max(1, Math.ceil(payout / 8));
        let shown = 0;
        if (tickRef.current !== null) clearInterval(tickRef.current);
        tickRef.current = window.setInterval(() => {
          shown = Math.min(payout, shown + step);
          sound.winTick(shown);
          setWinShown(shown);
          setSpinCredits(base + shown);
          if (shown >= payout && tickRef.current !== null) {
            clearInterval(tickRef.current);
            tickRef.current = null;
          }
        }, 70);
      } else {
        setWinShown(payout);
        setSpinCredits(base + payout);
      }
      return;
    }

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

    runCountUp(payout, base, overlay);
  };

  // ---- free spins sequence ----

  const startBonus = (round: BonusOutcome, betAtSpin: number) => {
    sound.bonusTrigger();
    setBonusIntro(round);
    setPhase("bonus");
    later(
      () => {
        setBonusIntro(null);
        const runtime: BonusRuntime = { round, index: 0, won: 0, bet: betAtSpin };
        bonusRef.current = runtime;
        setBonus(runtime);
        nextBonusSpin();
      },
      reduced ? 1000 : 2600,
    );
  };

  const nextBonusSpin = () => {
    const b = bonusRef.current;
    const r = resultRef.current;
    if (!b || !r || !pt) return;
    if (b.index >= b.round.spins.length) {
      finishBonus();
      return;
    }
    const spin = b.round.spins[b.index];
    spinIdRef.current += 1;
    const targets = spin.grid[0].map((_, c) => spin.grid.map((row) => row[c]));
    // holds must be a full-length numeric array: the launch effect indexes it
    // per reel, and an undefined entry poisons the filler count, transition
    // duration, and safety-net timeout with NaN.
    setSpec({
      id: spinIdRef.current,
      targets,
      holds: Array.from({ length: cols }, () => 0),
      hotFor: {},
    });
    setPhase("spinning");
    sound.bonusSpin();
    sound.startWhir();
  };

  const bonusSpinSettled = () => {
    const b = bonusRef.current;
    const r = resultRef.current;
    if (!b || !r || !pt) return;
    const spin = b.round.spins[b.index];
    const won = b.won + spin.payout;
    const runtime: BonusRuntime = { ...b, won, index: b.index + 1 };
    bonusRef.current = runtime;
    setBonus(runtime);
    setSpinCredits(r.res.balanceCredits - r.res.payoutCredits + won);
    setSpinPop({ payout: spin.payout, retrigger: spin.retrigger, total: won });
    if (spin.retrigger) sound.retrigger();
    else if (spin.payout > 0) sound.jackpot(1);

    const popMs = reduced
      ? 200
      : spin.retrigger
        ? 1900
        : spin.payout > 0
          ? 1250
          : 420;
    later(() => {
      setSpinPop(null);
      nextBonusSpin();
    }, popMs + 240);
  };

  const finishBonus = () => {
    const b = bonusRef.current;
    const r = resultRef.current;
    if (!b || !r) return;
    bonusRef.current = null;
    setBonus(null);
    setAnt(null);
    const payout = r.res.payoutCredits;
    const base = r.res.balanceCredits - payout;
    sound.bonusEnd();
    const big = payout >= b.bet * 20;
    setPhase("celebrating");
    setCelebration({
      payout,
      summary: `FREE SPINS ×${b.round.multiplier} · ${b.round.spins.length} SPUN`,
      bigWin: big,
      winCells: null,
      paylineIdx: [],
    });
    setWinShown(0);
    const overlay: OverlayState = {
      spinKey: r.res.betId + 1_000_000,
      payout,
      winShown: 0,
      summary: `FREE SPINS WON ×${b.round.multiplier}`,
      bigWin: big,
      coins: big || payout >= b.bet * 6,
    };
    overlayRef.current = overlay;
    onOverlay(overlay);
    later(
      () => {
        setPhase("idle");
        setCelebration(null);
        setWinShown(0);
        setAnt(null);
        setSpinCredits(r.res.balanceCredits);
        overlayRef.current = null;
        onOverlay(null);
      },
      big ? 5200 : 3400,
    );
    runCountUp(payout, base, overlay);
  };

  const handleAllSettled = () => {
    sound.stopWhir();
    if (bonusRef.current) {
      bonusSpinSettled();
      return;
    }
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
    bonusRef.current = null;
    setBonus(null);
    setBonusIntro(null);
    setSpinPop(null);
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
            // Stake left the meter at pull; hold the payout back until the reels land.
            setSpinCredits(res.balanceCredits - res.payoutCredits);
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

  // Per-free-spin pop: the win, the retrigger shout, and the running total.
  const bonusBanner = spinPop ? (
    <div
      style={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom: 0,
        padding: 10,
        textAlign: "center",
        background: "rgba(6,4,13,.82)",
        borderTop: "2px solid #ffd75e",
      }}
    >
      {spinPop.retrigger && (
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 28,
            color: "#ff8a1f",
            textShadow: "0 0 16px rgba(255,138,31,.95)",
            marginRight: 14,
            animation: "hintBlink .8s steps(1) infinite",
          }}
        >
          RETRIGGER · +{pt?.bonus?.retriggerSpins ?? 5} SPINS
        </span>
      )}
      {spinPop.payout > 0 && (
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 26,
            color: "#22e8ff",
            textShadow: "0 0 14px rgba(34,232,255,.9)",
            marginRight: 14,
          }}
        >
          WIN {spinPop.payout.toLocaleString()}
        </span>
      )}
      <span style={{ fontSize: 19, color: "#ffd75e" }}>
        TOTAL {spinPop.total.toLocaleString()}
      </span>
    </div>
  ) : null;
  const reelBanner = winBanner ?? bonusBanner;

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
      ? bonus
        ? "FREE SPIN IN MOTION"
        : "REELS IN MOTION"
      : phase === "lever"
        ? "LEVER RELEASED"
        : phase === "bonus"
          ? "BONUS STARTING"
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

        {/* Free spins meter */}
        {bonus && (
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              marginTop: 14,
              padding: "8px 14px",
              border: "2px solid #ffd75e",
              background: "#1a0d2e",
              boxShadow: "0 0 22px rgba(255,215,94,.4), inset 0 0 24px rgba(255,138,31,.12)",
            }}
          >
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 19,
                letterSpacing: 2,
                color: "#ffd75e",
              }}
            >
              FREE SPINS{" "}
              {Math.min(
                phase === "spinning" ? bonus.index + 1 : bonus.index,
                bonus.round.spins.length,
              )}
              /{bonus.round.spins.length}
            </span>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 19,
                color: "#22e8ff",
                border: "2px solid #22e8ff",
                borderRadius: 999,
                padding: "2px 12px",
              }}
            >
              ×{bonus.round.multiplier}
            </span>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#ff8a1f" }}>
              WON {bonus.won.toLocaleString()}
            </span>
          </div>
        )}

        {/* Reel window */}
        <div
          style={{
            position: "relative",
            margin: "14px 0",
            padding: 10,
            background: "#06040d",
            border: "2px solid #22e8ff",
            boxShadow:
              bonus
                ? "0 0 24px rgba(255,215,94,.55), inset 0 0 40px rgba(255,138,31,.14)"
                : "0 0 24px rgba(34,232,255,.3), inset 0 0 40px rgba(34,232,255,.08)",
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
            winBanner={reelBanner}
            boosted={bonus !== null}
            dimSymbols={bonus ? deadSymbols : null}
            onAnticipation={handleAnticipation}
            onAllSettled={handleAllSettled}
          />

          {bonusIntro && (
            <div
              style={{
                position: "absolute",
                inset: 0,
                zIndex: 5,
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                justifyContent: "center",
                gap: 14,
                background: "rgba(6,4,13,.92)",
                animation: "cellWin .4s steps(1) infinite",
              }}
            >
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 68,
                  letterSpacing: 8,
                  color: "#ffd75e",
                  textShadow: "0 0 30px rgba(255,138,31,.95)",
                  animation: "titleGlow 1.1s ease-in-out infinite",
                }}
              >
                BONUS!
              </span>
              <span style={{ fontSize: 24, color: "#ff2d95", letterSpacing: 3 }}>
                {bonusIntro.triggerCount} BONUS SCATTERS
              </span>
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 40,
                  color: "#fff",
                  textShadow: "0 0 18px rgba(34,232,255,.9)",
                }}
              >
                {bonusIntro.spinsAwarded} FREE SPINS
              </span>
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 24,
                  color: "#22e8ff",
                  border: "2px solid #22e8ff",
                  borderRadius: 999,
                  padding: "6px 22px",
                }}
              >
                ALL WINS ×{bonusIntro.multiplier}
              </span>
            </div>
          )}
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
              <div style={{ width: 320 }}>
                <BetInput
                  value={effBet}
                  onChange={(v) => setBet(v)}
                  steps={betSteps}
                  min={info?.minBet ?? 1}
                  max={info?.maxBet ?? 10000}
                  balance={credits ?? undefined}
                  accent="#22e8ff"
                  disabled={busy}
                  testIdPrefix="slots-bet"
                />
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
