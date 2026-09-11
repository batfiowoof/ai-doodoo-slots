"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { PlayError, useGames, usePlay, useSession } from "@/lib/api";
import { sound } from "@/lib/sound";
import { Ball, PlinkoWorld } from "@/lib/plinkoPhysics";
import { ballOf } from "@/lib/cosmetics";
import BetInput from "@/components/BetInput";
import Backdrop from "@/components/Backdrop";

// PLINKO — canvas peg board with steered real physics. The server records
// the exact L/R sequence per row; balls fall under gravity, bounce off pegs,
// and are biased at each hit so what you watch is what paid. Drop freely —
// balls fly concurrently, each bet settles independently.

const ACCENT = "#b18cff";
const GOOD = "#5fe08a";
const DANGER = "#f2643d";
const BET_STEPS = [5, 10, 25, 50, 100];
const ROWS_OPTIONS = [8, 12, 16];
const RISKS = ["low", "medium", "high"] as const;

const TABLES: Record<string, Record<number, number[]>> = {
  low: {
    8: [5.6, 2.1, 1.1, 1, 0.5, 1, 1.1, 2.1, 5.6],
    12: [10, 3, 1.6, 1.4, 1.1, 1, 0.5, 1, 1.1, 1.4, 1.6, 3, 10],
    16: [16, 9, 2, 1.4, 1.4, 1.2, 1.1, 1, 0.5, 1, 1.1, 1.2, 1.4, 1.4, 2, 9, 16],
  },
  medium: {
    8: [13, 3, 1.3, 0.7, 0.4, 0.7, 1.3, 3, 13],
    12: [33, 11, 4, 2, 1.1, 0.6, 0.3, 0.6, 1.1, 2, 4, 11, 33],
    16: [110, 41, 10, 5, 3, 1.5, 1, 0.5, 0.3, 0.5, 1, 1.5, 3, 5, 10, 41, 110],
  },
  high: {
    8: [29, 4, 1.5, 0.3, 0.2, 0.3, 1.5, 4, 29],
    12: [170, 24, 8.1, 2, 0.7, 0.2, 0.2, 0.2, 0.7, 2, 8.1, 24, 170],
    16: [1000, 130, 26, 9, 4, 2, 0.2, 0.2, 0.2, 0.2, 0.2, 2, 4, 9, 26, 130, 1000],
  },
};

// Board geometry (CSS pixels; canvas is DPR-scaled).
const W = 560;
const H = 560;
const TOP = 46;
const BOTTOM = H - 64;

function multColor(mult: number): string {
  return mult >= 10 ? "#ff2d95" : mult >= 2 ? "#ff8a1f" : mult >= 1 ? GOOD : "#b9a8e8";
}

interface PlinkoOutcome {
  rows: number;
  risk: string;
  bucket: number;
  path: boolean[];
  multiplier: number;
  profit: boolean;
}

interface Drop {
  rows: number;
  bucket: number;
  mult: number;
  rights: number;
  payout: number;
  bet: number;
  profit: boolean;
  key: number;
}

interface PegFlash {
  key: string;
  x: number;
  y: number;
  at: number;
}

interface BucketFlash {
  k: number;
  rows: number;
  at: number;
  color: string;
}

interface Particle {
  x: number;
  y: number;
  vx: number;
  vy: number;
  at: number;
  color: string;
}

interface Popup {
  x: number;
  y: number;
  at: number;
  text: string;
  color: string;
}

export default function PlinkoScreen({ gameId }: { gameId: string }) {
  const session = useSession();
  const games = useGames();
  const play = usePlay();
  const info = games.data?.find((g) => g.id === gameId);

  const [bet, setBet] = useState(10);
  const [rows, setRows] = useState(12);
  const [risk, setRisk] = useState<(typeof RISKS)[number]>("medium");
  const [drops, setDrops] = useState<Drop[]>([]);
  const [lastResult, setLastResult] = useState<Drop | null>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const boardRef = useRef({ rows, risk });
  boardRef.current = { rows, risk };
  // Equipped puck skin (through a ref: the rAF loop never re-runs).
  const ballSkinRef = useRef(ballOf(session.data?.user.plinkoBall));
  ballSkinRef.current = ballOf(session.data?.user.plinkoBall);
  // Server settles instantly at bet time; the displayed balance hides each
  // in-flight payout until its ball lands, so wins count up at the bucket.
  const serverBalance = session.data?.balanceCredits;
  const serverBalanceRef = useRef<number | null>(null);
  serverBalanceRef.current = serverBalance ?? null;
  const [shownBalance, setShownBalance] = useState<number | null>(null);
  const [availBalance, setAvailBalance] = useState<number | null>(null);
  const pendingRef = useRef(new Map<object, number>());
  const shownRef = useRef<number | null>(null);
  const shownIntRef = useRef(0);
  const availIntRef = useRef(0);

  // Physics worlds are pure geometry per row count — shared across drops.
  const worldsRef = useRef(new Map<number, PlinkoWorld>());
  const worldFor = useCallback((r: number) => {
    let wd = worldsRef.current.get(r);
    if (!wd) {
      wd = new PlinkoWorld(r, W, TOP, BOTTOM);
      worldsRef.current.set(r, wd);
    }
    return wd;
  }, []);

  // The rAF board: stepped physics balls + transient effects, no React state.
  const ballsRef = useRef<Ball[]>([]);
  const pegFlashesRef = useRef<PegFlash[]>([]);
  const bucketFlashesRef = useRef<BucketFlash[]>([]);
  const particlesRef = useRef<Particle[]>([]);
  const popupsRef = useRef<Popup[]>([]);
  const lastTickRef = useRef(0);

  // Balls fly on the row geometry they were dropped with; a rows change makes
  // that geometry stale (bets are already settled), so fade them out.
  useEffect(() => {
    for (const b of ballsRef.current) b.fadeOut();
  }, [rows]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = W * dpr;
    canvas.height = H * dpr;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.scale(dpr, dpr);

    let raf = 0;
    let last = performance.now();

    const draw = (now: number) => {
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      const b = boardRef.current;
      const table = TABLES[b.risk][b.rows];
      const geo = worldFor(b.rows).geo;

      // physics: substep so a ball never moves further than its radius
      const steps = Math.max(1, Math.ceil(dt / (1 / 120)));
      const h = dt / steps;
      for (const ball of ballsRef.current) {
        for (let s = 0; s < steps; s++) ball.step(h);
        if (ball.state === "falling") ball.pushTrail();
      }
      // balls that ended without landing (rows-change fades) release their
      // hidden payout — the bet was settled server-side either way
      const alive: Ball[] = [];
      for (const ball of ballsRef.current) {
        if (ball.state === "done") pendingRef.current.delete(ball);
        else alive.push(ball);
      }
      ballsRef.current = alive;

      // displayed balance: stake leaves at drop, payout counts up at the bucket
      if (serverBalanceRef.current != null) {
        let pending = 0;
        for (const v of pendingRef.current.values()) pending += v;
        const target = serverBalanceRef.current - pending;
        if (target !== availIntRef.current) {
          availIntRef.current = target;
          setAvailBalance(target);
        }
        if (shownRef.current === null) shownRef.current = target;
        const diff = target - shownRef.current;
        shownRef.current = Math.abs(diff) < 0.6 ? target : shownRef.current + diff * Math.min(1, dt * 9);
        const shown = Math.round(shownRef.current);
        if (shown !== shownIntRef.current) {
          shownIntRef.current = shown;
          setShownBalance(shown);
        }
      }

      ctx.clearRect(0, 0, W, H);

      // pegs, brightened by fresh hits
      pegFlashesRef.current = pegFlashesRef.current.filter((f) => now - f.at < 320);
      for (let r = 2; r <= b.rows; r++) {
        for (const peg of geo.pegRows[r]) {
          const flash = pegFlashesRef.current.find((f) => f.x === peg.x && f.y === peg.y);
          const hot = flash && now - flash.at < 130;
          ctx.beginPath();
          ctx.arc(peg.x, peg.y, geo.pegR, 0, Math.PI * 2);
          ctx.fillStyle = hot ? "#e4d9ff" : "#8f7fd8";
          ctx.fill();
        }
      }
      for (const f of pegFlashesRef.current) {
        const t = (now - f.at) / 300;
        ctx.beginPath();
        ctx.arc(f.x, f.y, geo.pegR + 2 + t * 14, 0, Math.PI * 2);
        ctx.strokeStyle = `rgba(201,186,255,${(1 - t) * 0.65})`;
        ctx.lineWidth = 1.5;
        ctx.stroke();
      }

      // buckets, squash-bouncing when hit
      bucketFlashesRef.current = bucketFlashesRef.current.filter((f) => now - f.at < 750);
      const bw = geo.gapX * 0.92;
      for (let k = 0; k < table.length; k++) {
        const mult = table[k];
        const x = geo.bucketX(k);
        const flash = bucketFlashesRef.current
          .filter((f) => f.k === k && f.rows === b.rows)
          .sort((p, q) => q.at - p.at)[0];
        const age = flash ? now - flash.at : Infinity;
        const hot = age < 700;
        const color = multColor(mult);
        const squash = age < 160 ? 1 - 0.3 * (1 - age / 160) : 1;
        const bh = 26 * squash;
        ctx.fillStyle = hot ? color : "#170c2b";
        ctx.strokeStyle = hot ? "#ece6ff" : color;
        ctx.lineWidth = hot ? 2.5 : 1.4;
        ctx.beginPath();
        ctx.roundRect(x - bw / 2, BOTTOM + 8, bw, bh, 6);
        ctx.fill();
        ctx.stroke();
        ctx.fillStyle = hot ? "#0d0619" : color;
        ctx.font = "12px Silkscreen, monospace";
        ctx.textAlign = "center";
        ctx.fillText(`${mult}×`, x, BOTTOM + 8 + 17);
      }

      // falling balls — comet trail, velocity squash, glow
      const puck = ballSkinRef.current;
      for (const ball of ballsRef.current) {
        const n = ball.trail.length;
        for (let i = 0; i < n; i++) {
          const p = ball.trail[i];
          const f = (i + 1) / n;
          ctx.globalAlpha = f * f * 0.3;
          ctx.beginPath();
          ctx.arc(p.x, p.y, geo.ballR * (0.25 + 0.6 * f), 0, Math.PI * 2);
          ctx.fillStyle = puck.body;
          ctx.fill();
        }
        ctx.globalAlpha = ball.alpha;
        const stretch = Math.min(0.28, ball.speed / 2600);
        const ang = Math.atan2(ball.vy, ball.vx);
        ctx.save();
        ctx.translate(ball.x, ball.y);
        ctx.rotate(ang);
        ctx.scale(1 + stretch, 1 - stretch);
        ctx.beginPath();
        ctx.arc(0, 0, geo.ballR, 0, Math.PI * 2);
        const grad = ctx.createRadialGradient(-2, -2.5, 1, 0, 0, geo.ballR);
        grad.addColorStop(0, puck.shine);
        grad.addColorStop(0.55, puck.body);
        grad.addColorStop(1, puck.glow);
        ctx.fillStyle = grad;
        ctx.shadowColor = puck.glow;
        ctx.shadowBlur = 16;
        ctx.fill();
        ctx.restore();
        ctx.shadowBlur = 0;
        ctx.globalAlpha = 1;
      }

      // land particles
      particlesRef.current = particlesRef.current.filter((p) => now - p.at < 550);
      for (const p of particlesRef.current) {
        const t = (now - p.at) / 550;
        p.vy += 900 * dt;
        p.x += p.vx * dt;
        p.y += p.vy * dt;
        ctx.globalAlpha = 1 - t;
        ctx.fillStyle = p.color;
        ctx.fillRect(p.x - 1.4, p.y - 1.4, 2.8, 2.8);
      }
      ctx.globalAlpha = 1;

      // floating multiplier popups
      popupsRef.current = popupsRef.current.filter((p) => now - p.at < 750);
      for (const p of popupsRef.current) {
        const t = (now - p.at) / 750;
        const pop = 1 + 0.35 * Math.max(0, 1 - t * 4);
        ctx.globalAlpha = 1 - t;
        ctx.font = `${Math.round(13 * pop)}px Silkscreen, monospace`;
        ctx.textAlign = "center";
        ctx.fillStyle = "#0d0619";
        ctx.fillText(p.text, p.x + 1, p.y - 26 * t + 1);
        ctx.fillStyle = p.color;
        ctx.fillText(p.text, p.x, p.y - 26 * t);
      }
      ctx.globalAlpha = 1;

      raf = requestAnimationFrame(draw);
    };
    raf = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(raf);
  }, [worldFor]);

  const doDrop = () => {
    if (!session.data) return;
    const avail = availBalance ?? serverBalance;
    if (avail == null || avail < bet) {
      sound.error();
      return;
    }
    sound.unlock();
    play.mutate(
      { gameId, betCredits: bet, clientSeed: "", params: { rows, risk } },
      {
        onSuccess: (res) => {
          const o = res.outcome as unknown as PlinkoOutcome;
          const world = worldFor(o.rows);
          const geo = world.geo;
          const betAtDrop = bet;
          const ball = world.drop(o.path, {
            onPegHit: (peg) => {
              pegFlashesRef.current.push({ key: `${peg.x},${peg.y}`, x: peg.x, y: peg.y, at: performance.now() });
              const now = performance.now();
              if (now - lastTickRef.current > 34) {
                lastTickRef.current = now;
                sound.winTick(peg.row - 1);
              }
            },
            onLand: (x) => {
              pendingRef.current.delete(ball); // payout becomes visible now
              const now = performance.now();
              const color = multColor(o.multiplier);
              bucketFlashesRef.current.push({ k: o.bucket, rows: o.rows, at: now, color });
              for (let i = 0; i < 10; i++) {
                particlesRef.current.push({
                  x,
                  y: BOTTOM + 8,
                  vx: (Math.random() - 0.5) * 280,
                  vy: -60 - Math.random() * 220,
                  at: now,
                  color,
                });
              }
              popupsRef.current.push({ x, y: BOTTOM - 4, at: now, text: `${o.multiplier}×`, color });
              sound.chipClink();
              if (o.multiplier >= 10) sound.bigWin();
              else if (o.multiplier > 1) sound.jackpot(1);
              const d: Drop = {
                rows: o.rows,
                bucket: o.bucket,
                mult: o.multiplier,
                rights: o.path.filter(Boolean).length,
                payout: res.payoutCredits,
                bet: betAtDrop,
                profit: o.profit,
                key: now + Math.random(),
              };
              setDrops((prev) => [...prev.slice(-11), d]);
              setLastResult(d);
            },
          });
          pendingRef.current.set(ball, res.payoutCredits); // hidden until the ball lands
          ballsRef.current.push(ball);
          if (ballsRef.current.length > 150) ballsRef.current[0].fadeOut();
        },
        onError: () => sound.error(),
      },
    );
  };

  const table = TABLES[risk][rows];

  return (
    <main className="crt" style={{ minHeight: "100vh", background: "#06040d", padding: "18px 24px", position: "relative", overflow: "hidden" }}>
      <Backdrop />
      <div style={{ maxWidth: 1060, margin: "0 auto", position: "relative" }}>
        <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 14 }}>
          <Link href="/" style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 2, color: "#8878b8", textDecoration: "none" }}>
            ◂ LOBBY
          </Link>
          <h1 style={{ fontFamily: "var(--font-display)", fontSize: 30, letterSpacing: 6, color: ACCENT, textShadow: `0 0 22px ${ACCENT}aa`, margin: 0 }}>
            ⬡ PLINKO
          </h1>
          <div style={{ textAlign: "right" }}>
            <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 3, color: "#5c4f80" }}>BALANCE</div>
            <div style={{ fontFamily: "var(--font-body)", fontSize: 28, color: "#ff8a1f", textShadow: "0 0 14px rgba(255,138,31,.5)" }} data-testid="plinko-balance">
              {(shownBalance ?? serverBalance ?? 0).toLocaleString()}
            </div>
          </div>
        </header>

        <div style={{ display: "grid", gridTemplateColumns: "auto 1fr", gap: 18, alignItems: "start", justifyContent: "center" }}>
          {/* board */}
          <div style={{ border: `2px solid ${ACCENT}44`, background: "linear-gradient(180deg, #0d0619, #170c2b)", padding: 8 }}>
            <canvas ref={canvasRef} style={{ width: W, height: H, display: "block" }} data-testid="plinko-board" />
          </div>

          {/* console */}
          <div style={{ display: "flex", flexDirection: "column", gap: 14, width: 320 }}>
            <div style={{ border: "2px solid #35205c", background: "#0d0619", padding: "14px 16px", display: "flex", flexDirection: "column", gap: 12 }}>
              <div>
                <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80", marginBottom: 6 }}>ROWS</div>
                <div style={{ display: "flex", gap: 8 }}>
                  {ROWS_OPTIONS.map((r) => (
                    <button
                      key={r}
                      type="button"
                      data-testid={`plinko-rows-${r}`}
                      onClick={() => {
                        sound.click();
                        setRows(r);
                      }}
                      style={{
                        flex: 1,
                        fontFamily: "var(--font-display)",
                        fontSize: 14,
                        padding: "9px 0",
                        cursor: "pointer",
                        border: `2px solid ${rows === r ? ACCENT : "#35205c"}`,
                        background: rows === r ? "#1d1036" : "#06040d",
                        color: rows === r ? ACCENT : "#8878b8",
                        boxShadow: rows === r ? `0 0 14px ${ACCENT}44` : "none",
                      }}
                    >
                      {r}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80", marginBottom: 6 }}>RISK</div>
                <div style={{ display: "flex", gap: 8 }}>
                  {RISKS.map((r) => (
                    <button
                      key={r}
                      type="button"
                      data-testid={`plinko-risk-${r}`}
                      onClick={() => {
                        sound.click();
                        setRisk(r);
                      }}
                      style={{
                        flex: 1,
                        fontFamily: "var(--font-display)",
                        fontSize: 12,
                        letterSpacing: 1,
                        padding: "9px 0",
                        cursor: "pointer",
                        textTransform: "uppercase",
                        border: `2px solid ${risk === r ? ACCENT : "#35205c"}`,
                        background: risk === r ? "#1d1036" : "#06040d",
                        color: risk === r ? ACCENT : "#8878b8",
                        boxShadow: risk === r ? `0 0 14px ${ACCENT}44` : "none",
                      }}
                    >
                      {r}
                    </button>
                  ))}
                </div>
              </div>
              <BetInput value={bet} onChange={setBet} steps={BET_STEPS} min={info?.minBet ?? 1} max={info?.maxBet ?? 10000} balance={availBalance ?? serverBalance} accent={ACCENT} testIdPrefix="plinko-bet" />
              <button
                type="button"
                data-testid="plinko-drop"
                disabled={!session.data || (availBalance ?? serverBalance ?? 0) < bet}
                onClick={doDrop}
                style={{
                  minHeight: 66,
                  fontFamily: "var(--font-display)",
                  fontSize: 22,
                  letterSpacing: 4,
                  cursor: "pointer",
                  border: `3px solid ${ACCENT}`,
                  background: "linear-gradient(180deg, #241640, #0d0619)",
                  color: ACCENT,
                  textShadow: `0 0 18px ${ACCENT}`,
                  boxShadow: `0 0 26px ${ACCENT}44, inset 0 0 22px ${ACCENT}22`,
                  animation: "radHubGlow 2.2s ease-in-out infinite alternate",
                }}
              >
                {`DROP ${bet} CR`}
              </button>            </div>

            {lastResult && (
              <div
                data-testid="plinko-result"
                style={{
                  border: `2px solid ${lastResult.profit ? GOOD : DANGER}66`,
                  background: "#06040d",
                  padding: "10px 14px",
                  animation: "bannerIn .4s ease",
                }}
              >
                <div style={{ fontFamily: "var(--font-display)", fontSize: 10, letterSpacing: 2, color: "#5c4f80" }}>
                  BUCKET {lastResult.bucket + 1}/{TABLES[risk][lastResult.rows]?.length ?? "?"} · {lastResult.rights} RIGHTS
                </div>
                <div style={{ fontFamily: "var(--font-body)", fontSize: 24, color: lastResult.profit ? GOOD : DANGER }}>
                  {lastResult.profit ? "+" : ""}
                  {(lastResult.payout - lastResult.bet).toLocaleString()} CR
                </div>
              </div>
            )}

            <div style={{ display: "flex", gap: 5, flexWrap: "wrap" }}>
              {drops
                .slice()
                .reverse()
                .map((d) => (
                  <span
                    key={d.key}
                    style={{
                      fontFamily: "var(--font-body)",
                      fontSize: 17,
                      padding: "0 7px",
                      border: `1px solid ${d.profit ? GOOD : DANGER}55`,
                      color: d.profit ? GOOD : DANGER,
                    }}
                  >
                    {d.mult}×
                  </span>
                ))}
            </div>

            {play.isError && (
              <div role="alert" style={{ fontFamily: "var(--font-body)", fontSize: 19, color: DANGER }}>
                {(play.error as PlayError)?.message ?? "drop failed"}
              </div>
            )}
            <div style={{ fontFamily: "var(--font-body)", fontSize: 15, color: "#5c4f80" }}>
              One coin flip per row · {table[0]}× edge buckets · provably fair
              {info ? ` · RTP ${(info.theoreticalRtp * 100).toFixed(2)}%` : ""}
            </div>
          </div>
        </div>
      </div>
    </main>
  );
}
