"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { PlayError, useGames, usePlay, useSession } from "@/lib/api";
import { sound } from "@/lib/sound";
import BetInput from "@/components/BetInput";
import Backdrop from "@/components/Backdrop";

// PLINKO — canvas peg board, server-decided path. Each drop animates the
// exact L/R sequence the engine recorded, so what you watch is what paid.

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

interface PlinkoOutcome {
  rows: number;
  risk: string;
  bucket: number;
  path: boolean[];
  multiplier: number;
  profit: boolean;
}

interface Drop {
  path: boolean[];
  rows: number;
  bucket: number;
  payout: number;
  bet: number;
  profit: boolean;
  key: number;
  /** rAF bookkeeping: last peg row the ball crossed. */
  lastSeg?: number;
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
  const [dropBusy, setDropBusy] = useState(false);
  const [lastResult, setLastResult] = useState<Drop | null>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const boardRef = useRef({ rows, risk });
  boardRef.current = { rows, risk };
  const balance = session.data?.balanceCredits;

  // Board geometry (CSS pixels; canvas is DPR-scaled).
  const W = 560;
  const H = 560;
  const TOP = 46;
  const BOTTOM = H - 64;

  // The rAF board: pegs are static, balls interpolate their recorded path,
  // buckets flash on landing.
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
    const start = performance.now();

    const draw = (now: number) => {
      const b = boardRef.current;
      const table = TABLES[b.risk][b.rows];
      const gapX = W / (b.rows + 2);
      const gapY = (BOTTOM - TOP) / (b.rows + 1);

      ctx.clearRect(0, 0, W, H);

      // pegs
      for (let r = 2; r <= b.rows; r++) {
        for (let c = 0; c <= r; c++) {
          const x = W / 2 + (c - r / 2) * gapX;
          const y = TOP + (r - 1) * gapY;
          ctx.beginPath();
          ctx.arc(x, y, 3.2, 0, Math.PI * 2);
          ctx.fillStyle = "#8f7fd8";
          ctx.fill();
        }
      }

      // falling balls — each animates its recorded path over ~1.4s
      const active: Drop[] = [];
      for (const d of dropsRef.current) {
        const t = (now - start - d.key) / 1400;
        if (t < 0) {
          active.push(d);
          continue;
        }
        if (t > 1.35) continue; // landed; bucket flash handled below
        active.push(d);
        const seg = Math.min(b.rows, Math.floor(t * b.rows));
        const frac = Math.min(b.rows, t * b.rows) - seg;
        let x = W / 2;
        for (let i = 0; i < seg; i++) x += d.path[i] ? gapX / 2 : -gapX / 2;
        const midX = x + (seg < d.rows ? (d.path[seg] ? gapX / 4 : -gapX / 4) : 0);
        const yTop = TOP + Math.max(0, seg - 0.5) * gapY;
        const y = seg >= d.rows ? BOTTOM : yTop + frac * gapY;
        const bx = seg >= d.rows ? x : x + (midX - x) * frac;
        ctx.beginPath();
        ctx.arc(bx, y, 6.5, 0, Math.PI * 2);
        ctx.fillStyle = "#ffd21f";
        ctx.shadowColor = ACCENT;
        ctx.shadowBlur = 14;
        ctx.fill();
        ctx.shadowBlur = 0;
        // peg tick per row crossing
        if (seg !== (d.lastSeg ?? -1)) {
          d.lastSeg = seg;
          if (seg > 0) sound.winTick(seg);
        }
      }
      dropsRef.current = dropsRef.current.filter((d) => now - start - d.key < 1600);

      // buckets
      const bw = gapX * 0.92;
      for (let k = 0; k < table.length; k++) {
        const x = W / 2 + (k - (table.length - 1) / 2) * gapX;
        const mult = table[k];
        const hot = lastFlashRef.current?.bucket === k && performance.now() - lastFlashRef.current.at < 900;
        const color = mult >= 10 ? "#ff2d95" : mult >= 2 ? "#ff8a1f" : mult >= 1 ? GOOD : "#4a3a72";
        ctx.fillStyle = hot ? color : "#170c2b";
        ctx.strokeStyle = hot ? "#ece6ff" : color;
        ctx.lineWidth = hot ? 2.5 : 1.4;
        const bx = x - bw / 2;
        const by = BOTTOM + 8;
        const r = 6;
        ctx.beginPath();
        ctx.roundRect(bx, by, bw, 26, r);
        ctx.fill();
        ctx.stroke();
        ctx.fillStyle = hot ? "#0d0619" : color;
        ctx.font = "12px Silkscreen, monospace";
        ctx.textAlign = "center";
        ctx.fillText(`${mult}×`, x, by + 17);
      }

      raf = requestAnimationFrame(draw);
    };
    raf = requestAnimationFrame(draw);
    return () => cancelAnimationFrame(raf);
  }, []);

  // Mutable refs read by the rAF loop without re-subscribing.
  const dropsRef = useRef<Drop[]>([]);
  const lastFlashRef = useRef<{ bucket: number; at: number } | null>(null);
  useEffect(() => {
    dropsRef.current = drops;
  }, [drops]);

  const doDrop = () => {
    if (dropBusy || !session.data) return;
    if (!balance || balance < bet) {
      sound.error();
      return;
    }
    sound.unlock();
    setDropBusy(true);
    play.mutate(
      { gameId, betCredits: bet, clientSeed: "", params: { rows, risk } },
      {
        onSuccess: (res) => {
          const o = res.outcome as unknown as PlinkoOutcome;
          const d: Drop = {
            path: o.path,
            rows: o.rows,
            bucket: o.bucket,
            payout: res.payoutCredits,
            bet,
            profit: o.profit,
            key: performance.now(),
          };
          dropsRef.current = [...dropsRef.current, d];
          setDrops((prev) => [...prev.slice(-8), d]);
          setTimeout(() => {
            lastFlashRef.current = { bucket: o.bucket, at: performance.now() };
            sound.chipClink();
            if (o.multiplier >= 10) sound.bigWin();
            else if (o.multiplier > 1) sound.jackpot(1);
            setLastResult(d);
            setDropBusy(false);
          }, 1400);
        },
        onError: () => {
          setDropBusy(false);
          sound.error();
        },
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
              {(balance ?? 0).toLocaleString()}
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
                      disabled={dropBusy}
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
                      disabled={dropBusy}
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
              <BetInput value={bet} onChange={setBet} steps={BET_STEPS} min={info?.minBet ?? 1} max={info?.maxBet ?? 10000} balance={balance} accent={ACCENT} disabled={dropBusy} testIdPrefix="plinko-bet" />
              <button
                type="button"
                data-testid="plinko-drop"
                disabled={dropBusy || !session.data || (balance ?? 0) < bet}
                onClick={doDrop}
                style={{
                  minHeight: 66,
                  fontFamily: "var(--font-display)",
                  fontSize: 22,
                  letterSpacing: 4,
                  cursor: dropBusy ? "wait" : "pointer",
                  border: `3px solid ${ACCENT}`,
                  background: dropBusy ? "#1d1036" : "linear-gradient(180deg, #241640, #0d0619)",
                  color: ACCENT,
                  textShadow: `0 0 18px ${ACCENT}`,
                  boxShadow: `0 0 26px ${ACCENT}44, inset 0 0 22px ${ACCENT}22`,
                  animation: dropBusy ? "countdownBlink .4s linear infinite" : "radHubGlow 2.2s ease-in-out infinite alternate",
                }}
              >
                {dropBusy ? "DROPPING…" : `DROP ${bet} CR`}
              </button>
            </div>

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
                  BUCKET {lastResult.bucket + 1}/{table.length} · {lastResult.path.filter(Boolean).length} RIGHTS
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
                    {TABLES[risk][d.rows]?.[d.bucket] ?? "?"}×
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
