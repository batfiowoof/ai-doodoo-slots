"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import {
  PlayError,
  useChickenActive,
  useChickenCashOut,
  useChickenHop,
  useChickenStart,
  useGames,
  useSession,
} from "@/lib/api";
import type { ChickenRoundView } from "@/lib/api";
import { sound } from "@/lib/sound";
import BetInput from "@/components/BetInput";
import Backdrop from "@/components/Backdrop";

// CHICKEN RUN — the chicken crosses road lanes one hop at a time; every
// lane passed raises the multiplier, cash out before the car on the fatal
// lane arrives. The fatal lane is decided server-side from the fairness
// stream; every car on the road is decoration except the one that is
// steered to arrive exactly on the recorded outcome — what you watch is
// what paid.
//
// PLACEHOLDER ART: the chicken sprite and car shapes below are deliberate
// stand-ins (icons get reworked app-wide later). All drawing lives in the
// SPRITES section so a swap is one edit.

const ACCENT = "#ffd21f";
const GOOD = "#5fe08a";
const BAD = "#ff4d6d";
const CREDITS = "#ff8a1f";
const ASPHALT = "#120a24";
const BET_STEPS = [5, 10, 25, 50, 100];

// Cosmetic only — the server's View is authoritative once the run starts.
const ROADS = [
  { key: "easy", label: "EASY", lanes: 20 },
  { key: "medium", label: "MEDIUM", lanes: 18 },
  { key: "hard", label: "HARD", lanes: 15 },
  { key: "hardcore", label: "HARDCORE", lanes: 12 },
];

const STAGE_W = 560;
const STAGE_H = 620;
const PAD_T = 52;
const PAD_B = 56;

// ---- SPRITES (placeholder art; swap here later) --------------------------
const CAR_COLORS = ["#ff4d6d", "#22e8ff", "#b18cff", "#ff8a1f", "#5fe08a"];
const CAR_LEN = 58;
const CAR_H = 20;
const CHICKEN = [
  "...r...",
  "..rr...",
  ".wwwww.",
  "wwwwwwk",
  "wwwwwwo",
  ".wwwww.",
  ".o...o.",
];
const PX = 6;
const SPRITE_COLORS: Record<string, string> = {
  r: "#ff4d6d",
  w: "#f4efff",
  o: "#ff8a1f",
  k: "#0d0619",
};

type Particle = {
  x: number;
  y: number;
  vx: number;
  vy: number;
  rot: number;
  vr: number;
  life: number;
  kind: "feather" | "coin";
};

type Traffic = { dir: 1 | -1; speed: number; phase: number };

type Hop = {
  from: number; // y the hop starts at
  to: number; // y it lands on
  toLane: number; // lane index it lands on (1-based)
  t0: number;
  dur: number;
  fatal: boolean;
};

type Anim = {
  lanes: number;
  crossed: number; // visual lane the chicken stands on (0 = curb)
  hop: Hop | null;
  impact: boolean; // the steered car has landed
  impactY: number;
  shake: number;
  flash: number;
  feathers: Particle[];
  coins: Particle[];
  traffic: Traffic[];
};

const hash01 = (i: number) => {
  const s = Math.sin(i * 127.1 + 311.7) * 43758.5453;
  return s - Math.floor(s);
};

function freshAnim(lanes: number): Anim {
  return {
    lanes,
    crossed: 0,
    hop: null,
    impact: false,
    impactY: 0,
    shake: 0,
    flash: 0,
    feathers: [],
    coins: [],
    traffic: Array.from({ length: lanes }, (_, i) => ({
      dir: i % 2 === 0 ? 1 : -1,
      speed: 46 + hash01(i) * 70,
      phase: hash01(i + 40) * (STAGE_W + CAR_LEN * 3),
    })),
  };
}

export default function ChickenScreen({ gameId }: { gameId: string }) {
  const session = useSession();
  const games = useGames();
  const info = games.data?.find((g) => g.id === gameId);
  const activeQ = useChickenActive(!!session.data);
  const start = useChickenStart();
  const hop = useChickenHop();
  const cashOut = useChickenCashOut();

  const [bet, setBet] = useState(10);
  const [road, setRoad] = useState(ROADS[0].key);
  const [localRound, setLocalRound] = useState<ChickenRoundView | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const animRef = useRef<Anim>(freshAnim(ROADS[0].lanes));

  const round = localRound ?? activeQ.data ?? null;
  const active = round?.status === "active";
  const finished = round && round.status !== "active" ? round : null;
  const balance = session.data?.balanceCredits;
  const lanes = round?.lanes ?? ROADS.find((r) => r.key === road)?.lanes ?? 20;

  const layout = (ln: number) => {
    const laneH = (STAGE_H - PAD_T - PAD_B) / ln;
    const curbY = STAGE_H - PAD_B;
    const laneY = (k: number) => curbY - (k - 1) * laneH - laneH / 2;
    return { laneH, curbY, laneY };
  };

  // Keep the canvas anim in sync with the authoritative round state, but
  // never fight an in-flight hop/squash animation.
  useEffect(() => {
    const a = animRef.current;
    if (!round) {
      if (a.lanes !== lanes) animRef.current = freshAnim(lanes);
      return;
    }
    if (a.lanes !== round.lanes) {
      animRef.current = freshAnim(round.lanes);
      return;
    }
    if (!a.hop && !a.impact) a.crossed = round.crossed;
  }, [round, lanes]);

  const burst = (kind: "feather" | "coin", x: number, y: number) => {
    const a = animRef.current;
    const list = kind === "feather" ? a.feathers : a.coins;
    for (let i = 0; i < (kind === "feather" ? 26 : 34); i++) {
      const ang = Math.random() * Math.PI * 2;
      const sp = kind === "feather" ? 60 + Math.random() * 160 : 120 + Math.random() * 240;
      list.push({
        x,
        y,
        vx: Math.cos(ang) * sp,
        vy: Math.sin(ang) * sp - 140,
        rot: Math.random() * Math.PI * 2,
        vr: (Math.random() - 0.5) * 12,
        life: 1,
        kind,
      });
    }
  };

  const doStart = () => {
    if (!session.data || start.isPending) return;
    if (!balance || balance < bet) {
      sound.error();
      setNote("Not enough credits.");
      return;
    }
    sound.unlock();
    sound.chipClink();
    setNote(null);
    start.mutate(
      { betCredits: bet, difficulty: road },
      {
        onSuccess: (res) => {
          animRef.current = freshAnim(res.round.lanes);
          setLocalRound(res.round);
        },
        onError: (e) => {
          sound.error();
          setNote((e as PlayError)?.message ?? "start failed");
        },
      },
    );
  };

  const doHop = () => {
    if (!round || !active || hop.isPending) return;
    const a = animRef.current;
    if (a.hop || a.impact) return; // mid-air or already flat
    sound.unlock();
    const { laneY, curbY } = layout(a.lanes);
    const toLane = a.crossed + 1;
    a.hop = {
      from: a.crossed === 0 ? curbY - 14 : laneY(a.crossed),
      to: laneY(toLane),
      toLane,
      t0: performance.now(),
      dur: 380,
      fatal: false,
    };
    hop.mutate(
      { roundId: round.roundId },
      {
        onSuccess: (res) => {
          const r = res.round;
          setLocalRound(r);
          if (r.status === "squashed") {
            // The recorded fatal lane is crossed+1 of the settled round;
            // steer the car to arrive exactly as this hop lands.
            a.hop!.fatal = true;
            setTimeout(() => {
              const { laneY: ly } = layout(a.lanes);
              a.crossed = a.hop ? a.hop.toLane : toLane;
              a.impact = true;
              a.impactY = ly(a.crossed);
              a.shake = 1;
              a.flash = 1;
              burst("feather", STAGE_W / 2, a.impactY);
              sound.explosion();
            }, 220);
          } else if (r.status === "cashed") {
            // Cleared the whole road — auto-settled at the top multiplier.
            sound.bigWin();
            a.crossed = r.crossed;
            burst("coin", STAGE_W / 2, laneY(r.crossed));
            setTimeout(() => {
              a.hop = null;
            }, 900);
          } else {
            sound.winTick(r.crossed);
            a.crossed = r.crossed;
            a.hop = null;
          }
        },
        onError: () => {
          sound.error();
          a.hop = null;
        },
      },
    );
  };

  const doCashOut = () => {
    if (!round || !active || !round.cashable || cashOut.isPending) return;
    sound.unlock();
    cashOut.mutate(
      { roundId: round.roundId },
      {
        onSuccess: (res) => {
          const r = res.round;
          setLocalRound(r);
          const won = r.payoutCredits - r.betCredits;
          if (won >= r.betCredits * 4) sound.bigWin();
          else sound.cashout();
          const a = animRef.current;
          const { laneY } = layout(a.lanes);
          burst("coin", STAGE_W / 2, laneY(Math.max(a.crossed, 1)));
          setNote(`Cashed out +${won.toLocaleString()} CR`);
        },
        onError: () => sound.error(),
      },
    );
  };

  const newRun = () => {
    sound.click();
    setLocalRound(null);
    setNote(null);
    animRef.current = freshAnim(lanes);
  };

  // ---- canvas loop -------------------------------------------------------
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = STAGE_W * dpr;
    canvas.height = STAGE_H * dpr;
    ctx.scale(dpr, dpr);

    let raf = 0;
    let last = performance.now();

    const drawCar = (x: number, y: number, dir: number, color: string, alpha: number, glow: number) => {
      ctx.save();
      ctx.globalAlpha = alpha;
      ctx.translate(x + CAR_LEN / 2, y);
      if (dir < 0) ctx.scale(-1, 1);
      ctx.shadowColor = color;
      ctx.shadowBlur = glow;
      ctx.fillStyle = color;
      ctx.fillRect(-CAR_LEN / 2, -CAR_H / 2, CAR_LEN, CAR_H);
      ctx.shadowBlur = 0;
      ctx.fillStyle = "rgba(6,4,13,.75)";
      ctx.fillRect(-CAR_LEN / 2 + 10, -CAR_H / 2 + 3, 16, CAR_H - 6);
      ctx.fillRect(CAR_LEN / 2 - 22, -CAR_H / 2 + 3, 10, CAR_H - 6);
      ctx.fillStyle = "#fff9d9";
      ctx.fillRect(CAR_LEN / 2 - 3, -CAR_H / 2 + 4, 3, 4);
      ctx.fillRect(CAR_LEN / 2 - 3, CAR_H / 2 - 8, 3, 4);
      ctx.restore();
    };

    const drawChicken = (x: number, y: number, squash: number) => {
      ctx.save();
      ctx.translate(x, y);
      ctx.scale(1 + squash * 0.5, Math.max(0.22, 1 - squash));
      ctx.shadowColor = ACCENT;
      ctx.shadowBlur = 16;
      for (let r = 0; r < CHICKEN.length; r++) {
        for (let c = 0; c < CHICKEN[r].length; c++) {
          const col = SPRITE_COLORS[CHICKEN[r][c]];
          if (!col) continue;
          ctx.fillStyle = col;
          ctx.fillRect((c - CHICKEN[r].length / 2) * PX, (r - CHICKEN.length / 2) * PX, PX, PX);
        }
      }
      ctx.restore();
    };

    const frame = (now: number) => {
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      const a = animRef.current;
      const { laneH, curbY, laneY } = layout(a.lanes);
      const cx = STAGE_W / 2;

      ctx.clearRect(0, 0, STAGE_W, STAGE_H);
      ctx.save();
      if (a.shake > 0) {
        ctx.translate((Math.random() - 0.5) * 18 * a.shake, (Math.random() - 0.5) * 18 * a.shake);
        a.shake = Math.max(0, a.shake - dt * 2.4);
      }

      // road base
      ctx.fillStyle = ASPHALT;
      ctx.fillRect(0, curbY - a.lanes * laneH - 8, STAGE_W, a.lanes * laneH + 8 + 6);

      // lanes
      for (let i = 1; i <= a.lanes; i++) {
        const top = curbY - i * laneH;
        ctx.fillStyle = i % 2 === 0 ? "rgba(255,255,255,.025)" : "transparent";
        ctx.fillRect(0, top, STAGE_W, laneH);
        if (i <= a.crossed) {
          ctx.fillStyle = "rgba(95,224,138,.07)";
          ctx.fillRect(0, top, STAGE_W, laneH);
        }
        // dashed center line, scrolling with the lane's traffic
        const t = a.traffic[i - 1];
        ctx.save();
        ctx.strokeStyle = i <= a.crossed ? "rgba(95,224,138,.35)" : "rgba(255,210,31,.16)";
        ctx.lineWidth = 3;
        ctx.setLineDash([24, 20]);
        ctx.lineDashOffset = t.dir * (now / 1000) * t.speed * 0.9;
        ctx.beginPath();
        ctx.moveTo(0, top + laneH / 2);
        ctx.lineTo(STAGE_W, top + laneH / 2);
        ctx.stroke();
        ctx.restore();
      }

      // curb + finish
      ctx.fillStyle = "#241543";
      ctx.fillRect(0, curbY, STAGE_W, 6);
      ctx.fillStyle = "#06040d";
      ctx.fillRect(0, curbY + 6, STAGE_W, STAGE_H - curbY);
      const finY = curbY - a.lanes * laneH;
      ctx.save();
      ctx.shadowColor = ACCENT;
      ctx.shadowBlur = 22;
      ctx.fillStyle = "rgba(255,210,31,.14)";
      ctx.fillRect(0, finY - 8, STAGE_W, 8);
      ctx.restore();
      ctx.fillStyle = ACCENT;
      ctx.font = "bold 15px monospace";
      ctx.textAlign = "center";
      ctx.fillText("▲ COOP ▲", cx, finY - 16);
      ctx.fillStyle = "#8878b8";
      ctx.font = "bold 12px monospace";
      ctx.fillText("START", cx, curbY + 26);

      // traffic — decoration only; it ghosts out before reaching the
      // chicken because looping cars must never end a run. The steered
      // fatal car below is the exception, and it lands on the server's
      // recorded lane.
      for (let i = 1; i <= a.lanes; i++) {
        const t = a.traffic[i - 1];
        const y = laneY(i);
        const span = STAGE_W + CAR_LEN * 3;
        for (let c = 0; c < 3; c++) {
          let x = (t.phase + c * (span / 3) + t.dir * (now / 1000) * t.speed) % span;
          if (x < 0) x += span;
          x -= CAR_LEN * 1.5;
          const color = CAR_COLORS[(i + c) % CAR_COLORS.length];
          const near = i === Math.round(a.crossed) && Math.abs(x + CAR_LEN / 2 - cx) < 120;
          const alpha = near ? 0.15 : i === Math.round(a.crossed) ? 0.55 : 0.8;
          drawCar(x, y, t.dir, color, alpha, 10);
        }
      }

      // steered fatal car: arrives exactly as the recorded hop lands
      if (a.hop && a.hop.fatal && !a.impact) {
        const p = Math.min(1, (now - a.hop.t0) / a.hop.dur);
        const k = Math.min(1, p / 0.58);
        const dir = a.traffic[a.hop.toLane - 1]?.dir ?? 1;
        const startX = dir === 1 ? -CAR_LEN : STAGE_W + CAR_LEN;
        drawCar(startX + (cx - CAR_LEN / 2 - startX) * k, a.hop.to, dir, BAD, 1, 26);
      }
      if (a.impact) {
        drawCar(cx - CAR_LEN / 2, a.impactY, 1, BAD, 1, 30);
      }

      // chicken
      let chickY = a.crossed === 0 ? curbY - 14 : laneY(a.crossed);
      let squash = 0;
      if (a.hop) {
        const p = Math.min(1, (now - a.hop.t0) / a.hop.dur);
        const ease = 1 - Math.pow(1 - p, 2.2);
        chickY = a.hop.from + (a.hop.to - a.hop.from) * ease;
        chickY -= Math.sin(p * Math.PI) * 26; // hop arc
        squash = p < 0.15 || p > 0.85 ? 0.25 : 0;
        if (a.impact) squash = 1;
      } else if (a.impact) {
        squash = 1;
      } else {
        chickY += Math.sin(now / 300) * 3; // idle bob
      }
      drawChicken(cx, chickY, squash);

      // particles
      const step = (list: Particle[], grav: number) => {
        for (const p of list) {
          p.x += p.vx * dt;
          p.y += p.vy * dt;
          p.vy += grav * dt;
          p.rot += p.vr * dt;
          p.life -= dt * (p.kind === "feather" ? 0.7 : 0.9);
        }
        for (let i = list.length - 1; i >= 0; i--) {
          if (list[i].life <= 0) list.splice(i, 1);
        }
      };
      step(a.feathers, 60);
      step(a.coins, 420);
      for (const p of a.feathers) {
        ctx.save();
        ctx.globalAlpha = Math.max(0, p.life);
        ctx.translate(p.x, p.y);
        ctx.rotate(p.rot);
        ctx.fillStyle = "#f4efff";
        ctx.fillRect(-4, -2, 8, 4);
        ctx.restore();
      }
      for (const p of a.coins) {
        ctx.save();
        ctx.globalAlpha = Math.max(0, p.life);
        ctx.translate(p.x, p.y);
        ctx.rotate(p.rot);
        ctx.shadowColor = CREDITS;
        ctx.shadowBlur = 12;
        ctx.fillStyle = CREDITS;
        ctx.beginPath();
        ctx.arc(0, 0, 6, 0, Math.PI * 2);
        ctx.fill();
        ctx.restore();
      }

      if (a.flash > 0) {
        ctx.fillStyle = `rgba(255,77,109,${a.flash * 0.35})`;
        ctx.fillRect(0, 0, STAGE_W, STAGE_H);
        a.flash = Math.max(0, a.flash - dt * 3);
      }
      ctx.restore();
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, []);

  const { laneY } = layout(lanes);
  const multiplierNow = active || finished ? round!.multiplier : 1;
  const profitNow = active ? Math.floor(round!.betCredits * round!.multiplier) - round!.betCredits : 0;

  const chip = (r: (typeof ROADS)[number]) => (
    <button
      key={r.key}
      type="button"
      data-testid={`chicken-road-${r.key}`}
      disabled={active || !!finished}
      onClick={() => {
        sound.click();
        setRoad(r.key);
      }}
      style={{
        flex: 1,
        fontFamily: "var(--font-display)",
        fontSize: 12,
        letterSpacing: 1,
        padding: "9px 0",
        cursor: active || finished ? "default" : "pointer",
        border: `2px solid ${road === r.key ? ACCENT : "#35205c"}`,
        background: road === r.key ? "#2d2305" : "#06040d",
        color: road === r.key ? ACCENT : "#8878b8",
        boxShadow: road === r.key ? "0 0 14px rgba(255,210,31,.3)" : "none",
      }}
    >
      {r.label}
    </button>
  );

  return (
    <main className="crt" style={{ minHeight: "100vh", background: "#06040d", padding: "18px 24px", position: "relative", overflow: "hidden" }}>
      <Backdrop />
      <div style={{ maxWidth: 1060, margin: "0 auto", position: "relative" }}>
        <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 14 }}>
          <Link href="/" style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 2, color: "#8878b8", textDecoration: "none" }}>
            ◂ LOBBY
          </Link>
          <h1 style={{ fontFamily: "var(--font-display)", fontSize: 30, letterSpacing: 6, color: ACCENT, textShadow: `0 0 22px ${ACCENT}aa`, margin: 0 }}>
            🐔 CHICKEN RUN
          </h1>
          <div style={{ textAlign: "right" }}>
            <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 3, color: "#5c4f80" }}>BALANCE</div>
            <div style={{ fontFamily: "var(--font-body)", fontSize: 28, color: CREDITS, textShadow: "0 0 14px rgba(255,138,31,.5)" }} data-testid="chicken-balance">
              {(balance ?? 0).toLocaleString()}
            </div>
          </div>
        </header>

        <div style={{ display: "grid", gridTemplateColumns: "auto 1fr", gap: 18, alignItems: "start", justifyContent: "center" }}>
          {/* road stage */}
          <div
            data-testid="chicken-stage"
            style={{
              position: "relative",
              width: STAGE_W,
              height: STAGE_H,
              border: `2px solid ${ACCENT}44`,
              background: "linear-gradient(180deg, #0d0619, #170c2b)",
              overflow: "hidden",
            }}
          >
            <canvas ref={canvasRef} style={{ width: STAGE_W, height: STAGE_H, display: "block" }} />
            {active && round && (
              <div
                key={round.crossed}
                style={{
                  position: "absolute",
                  right: 14,
                  top: laneY(Math.max(round.crossed, 1)) - 14,
                  fontFamily: "var(--font-display)",
                  fontSize: 24,
                  color: GOOD,
                  textShadow: "0 0 16px rgba(95,224,138,.8)",
                  animation: "potPop .4s ease",
                  pointerEvents: "none",
                }}
              >
                ×{round.multiplier.toFixed(2)}
              </div>
            )}
            {!round && (
              <div
                style={{
                  position: "absolute",
                  inset: 0,
                  display: "grid",
                  placeItems: "center",
                  pointerEvents: "none",
                }}
              >
                <div
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 15,
                    letterSpacing: 3,
                    color: "#8878b8",
                    background: "rgba(6,4,13,.82)",
                    border: "2px solid #35205c",
                    padding: "10px 16px",
                  }}
                >
                  PICK A ROAD · BET · CROSS
                </div>
              </div>
            )}
          </div>

          {/* console */}
          <div style={{ display: "flex", flexDirection: "column", gap: 14, width: 330 }}>
            {!active && (
              <div style={{ border: "2px solid #35205c", background: "#0d0619", padding: "14px 16px", display: "flex", flexDirection: "column", gap: 12 }}>
                <div>
                  <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80", marginBottom: 6 }}>ROAD</div>
                  <div style={{ display: "flex", gap: 8 }}>{ROADS.map(chip)}</div>
                  <div style={{ fontFamily: "var(--font-body)", fontSize: 13, color: "#5c4f80", marginTop: 6 }}>
                    {lanes} lanes · every lane passed raises the multiplier · one lane hides a car
                  </div>
                </div>
                <BetInput value={bet} onChange={setBet} steps={BET_STEPS} min={info?.minBet ?? 1} max={info?.maxBet ?? 10000} balance={balance} accent={ACCENT} testIdPrefix="chicken-bet" />
                <button
                  type="button"
                  data-testid="chicken-start"
                  disabled={start.isPending || !session.data}
                  onClick={doStart}
                  style={{
                    minHeight: 64,
                    fontFamily: "var(--font-display)",
                    fontSize: 21,
                    letterSpacing: 4,
                    cursor: start.isPending ? "wait" : "pointer",
                    border: `3px solid ${ACCENT}`,
                    background: "linear-gradient(180deg, #2d2305, #0d0619)",
                    color: ACCENT,
                    textShadow: `0 0 18px ${ACCENT}`,
                    boxShadow: `0 0 26px ${ACCENT}44, inset 0 0 22px ${ACCENT}22`,
                    animation: "radHubGlow 2.2s ease-in-out infinite alternate",
                  }}
                >
                  {start.isPending ? "PLACING…" : `BET ${bet} CR`}
                </button>
              </div>
            )}

            {active && round && (
              <div style={{ border: "2px solid #35205c", background: "#0d0619", padding: "14px 16px", display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={{ display: "flex", gap: 10 }}>
                  <div style={{ flex: 1, border: "1px solid #35205c", background: "#06040d", padding: "8px 10px" }}>
                    <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80" }}>CURRENT</div>
                    <div data-testid="chicken-multiplier" style={{ fontFamily: "var(--font-body)", fontSize: 26, color: "#ece6ff" }}>
                      {round.multiplier.toFixed(2)}×
                    </div>
                  </div>
                  <div style={{ flex: 1, border: "1px solid #35205c", background: "#06040d", padding: "8px 10px" }}>
                    <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80" }}>NEXT</div>
                    <div style={{ fontFamily: "var(--font-body)", fontSize: 26, color: GOOD }}>
                      {round.nextMultiplier?.toFixed(2)}×
                    </div>
                  </div>
                </div>
                <div style={{ border: "1px solid #35205c", background: "#06040d", padding: "8px 10px" }}>
                  <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80" }}>
                    LANE {round.crossed} / {round.lanes} · CASHOUT VALUE
                  </div>
                  <div style={{ fontFamily: "var(--font-body)", fontSize: 26, color: CREDITS }}>
                    {Math.floor(round.betCredits * round.multiplier).toLocaleString()} CR
                    <span style={{ fontSize: 17, color: GOOD }}> (+{profitNow.toLocaleString()})</span>
                  </div>
                </div>
                <button
                  type="button"
                  data-testid="chicken-hop"
                  disabled={hop.isPending}
                  onClick={doHop}
                  style={{
                    minHeight: 66,
                    fontFamily: "var(--font-display)",
                    fontSize: 24,
                    letterSpacing: 6,
                    cursor: "pointer",
                    border: `3px solid ${ACCENT}`,
                    background: "linear-gradient(180deg, #2d2305, #0d0619)",
                    color: ACCENT,
                    textShadow: `0 0 18px ${ACCENT}`,
                    boxShadow: `0 0 26px ${ACCENT}44`,
                    animation: "radHubGlow 1.6s ease-in-out infinite alternate",
                  }}
                >
                  {hop.isPending ? "HOPPING…" : "HOP"}
                </button>
                <button
                  type="button"
                  data-testid="chicken-cashout"
                  disabled={!round.cashable || cashOut.isPending}
                  onClick={doCashOut}
                  style={{
                    minHeight: 60,
                    fontFamily: "var(--font-display)",
                    fontSize: 20,
                    letterSpacing: 4,
                    cursor: round.cashable ? "pointer" : "default",
                    border: `3px solid ${round.cashable ? GOOD : "#35205c"}`,
                    background: round.cashable ? "linear-gradient(180deg, #0a2d18, #06040d)" : "#0d0619",
                    color: round.cashable ? GOOD : "#5c4f80",
                    textShadow: round.cashable ? `0 0 18px ${GOOD}` : "none",
                    boxShadow: round.cashable ? "0 0 26px rgba(95,224,138,.3)" : "none",
                  }}
                >
                  {round.crossed === 0 ? "HOP A LANE FIRST" : `CASH OUT ${Math.floor(round.betCredits * multiplierNow).toLocaleString()}`}
                </button>
              </div>
            )}

            {finished && (
              <div
                data-testid="chicken-result"
                style={{
                  border: `2px solid ${finished.status === "cashed" ? GOOD : BAD}`,
                  background: "#0d0619",
                  padding: "14px 16px",
                  animation: "bannerIn .4s ease",
                }}
              >
                <div style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 3, color: finished.status === "cashed" ? GOOD : BAD }}>
                  {finished.status === "cashed" ? `CASHED ${finished.multiplier.toFixed(2)}×` : "SPLAT"}
                </div>
                <div style={{ fontFamily: "var(--font-body)", fontSize: 26, color: finished.status === "cashed" ? GOOD : BAD }}>
                  {finished.status === "cashed"
                    ? `+${(finished.payoutCredits - finished.betCredits).toLocaleString()} CR`
                    : `−${finished.betCredits.toLocaleString()} CR`}
                </div>
                {finished.fatalLane ? (
                  <div style={{ fontFamily: "var(--font-body)", fontSize: 14, color: "#8878b8", marginTop: 4 }}>
                    the car was waiting on lane {finished.fatalLane}
                  </div>
                ) : null}
                <button
                  type="button"
                  onClick={newRun}
                  style={{
                    marginTop: 10,
                    width: "100%",
                    fontFamily: "var(--font-display)",
                    fontSize: 15,
                    letterSpacing: 3,
                    padding: "10px 0",
                    cursor: "pointer",
                    border: `2px solid ${ACCENT}`,
                    background: "#06040d",
                    color: ACCENT,
                  }}
                >
                  NEW RUN
                </button>
              </div>
            )}

            {note && (
              <div role="status" style={{ fontFamily: "var(--font-body)", fontSize: 18, color: "#8878b8" }}>
                {note}
              </div>
            )}
            {hop.isError && (
              <div role="alert" style={{ fontFamily: "var(--font-body)", fontSize: 18, color: BAD }}>
                {(hop.error as PlayError)?.message ?? "hop failed"}
              </div>
            )}
            <div style={{ fontFamily: "var(--font-body)", fontSize: 15, color: "#5c4f80" }}>
              {round?.difficulty ?? road} road · the fatal lane is drawn from the fairness stream before the first hop · cash out anytime · provably fair
              {info ? ` · RTP ${(info.theoreticalRtp * 100).toFixed(2)}%` : ""}
            </div>
          </div>
        </div>
      </div>
    </main>
  );
}
