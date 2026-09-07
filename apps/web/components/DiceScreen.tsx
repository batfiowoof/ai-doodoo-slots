"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { PlayError, useGames, usePlay, useSession } from "@/lib/api";
import { sound } from "@/lib/sound";
import BetInput from "@/components/BetInput";
import Backdrop from "@/components/Backdrop";

// DICE — over/under roll on a 0–100 track. Pick a direction and target,
// watch the marker fly. Gold neon; the track is the whole show.

const ACCENT = "#ffd21f";
const DANGER = "#f2643d";
const GOOD = "#5fe08a";
const BET_STEPS = [5, 10, 25, 50, 100];

interface DiceOutcome {
  roll: number;
  direction: "under" | "over";
  target: number;
  chance: number;
  multiplier: number;
  win: boolean;
  payoutMultiplier: number;
}

interface RollRow {
  roll: number;
  win: boolean;
  key: number;
}

export default function DiceScreen({ gameId }: { gameId: string }) {
  const session = useSession();
  const games = useGames();
  const play = usePlay();
  const info = games.data?.find((g) => g.id === gameId);

  const [bet, setBet] = useState(10);
  const [direction, setDirection] = useState<"under" | "over">("under");
  const [target, setTarget] = useState(50);
  const [last, setLast] = useState<DiceOutcome | null>(null);
  const [rolls, setRolls] = useState<RollRow[]>([]);
  const [rolling, setRolling] = useState(false);
  const [markerRoll, setMarkerRoll] = useState<number | null>(null);
  const [flash, setFlash] = useState<"win" | "lose" | null>(null);

  const chance = direction === "under" ? target : 100 - target;
  const multiplier = 99 / chance;
  const profit = Math.floor(bet * multiplier) - bet;
  const balance = session.data?.balanceCredits;

  const rollTicks = useMemo(() => Array.from({ length: 11 }, (_, i) => i * 10), []);

  const doRoll = () => {
    if (rolling || !session.data) return;
    if (!balance || balance < bet) {
      sound.error();
      return;
    }
    sound.unlock();
    setRolling(true);
    setFlash(null);
    // Ticker sweep: the marker darts around before the real result lands.
    let ticks = 0;
    const sweep = setInterval(() => {
      setMarkerRoll(Math.random() * 100);
      sound.winTick(ticks);
      if (++ticks >= 8) clearInterval(sweep);
    }, 70);
    play.mutate(
      { gameId, betCredits: bet, clientSeed: "", params: { direction, target } },
      {
        onSuccess: (res) => {
          setTimeout(() => {
            clearInterval(sweep);
            const o = res.outcome as unknown as DiceOutcome;
            setLast(o);
            setMarkerRoll(o.roll);
            setRolling(false);
            setRolls((r) => [{ roll: o.roll, win: o.win, key: Date.now() }, ...r].slice(0, 14));
            if (o.win) {
              setFlash("win");
              if (o.payoutMultiplier >= 10) {
                sound.bigWin();
              } else if (o.payoutMultiplier >= 4) {
                sound.jackpot(2);
              } else {
                sound.winTick(4);
              }
            } else {
              setFlash("lose");
              sound.error();
            }
            setTimeout(() => setFlash(null), 900);
          }, 560);
        },
        onError: () => {
          clearInterval(sweep);
          setRolling(false);
          setMarkerRoll(null);
          sound.error();
        },
      },
    );
  };

  return (
    <main
      className="crt"
      style={{ minHeight: "100vh", background: "#06040d", padding: "18px 24px", position: "relative", overflow: "hidden" }}
    >
      <Backdrop />
      <div
        style={{
          position: "absolute",
          inset: 0,
          pointerEvents: "none",
          background: flash === "win" ? "radial-gradient(circle, rgba(95,224,138,.22), transparent 65%)" : flash === "lose" ? "radial-gradient(circle, rgba(242,100,61,.22), transparent 65%)" : "none",
          animation: flash ? "bannerIn .9s ease forwards" : undefined,
        }}
      />
      <div style={{ maxWidth: 1100, margin: "0 auto", position: "relative" }}>
        <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 14 }}>
          <Link href="/" style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 2, color: "#8878b8", textDecoration: "none" }}>
            ◂ LOBBY
          </Link>
          <h1 style={{ fontFamily: "var(--font-display)", fontSize: 30, letterSpacing: 6, color: ACCENT, textShadow: `0 0 22px ${ACCENT}aa`, margin: 0 }}>
            🎲 DICE
          </h1>
          <div style={{ textAlign: "right" }}>
            <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 3, color: "#5c4f80" }}>BALANCE</div>
            <div style={{ fontFamily: "var(--font-body)", fontSize: 28, color: "#ff8a1f", textShadow: "0 0 14px rgba(255,138,31,.5)" }} data-testid="dice-balance">
              {(balance ?? 0).toLocaleString()}
            </div>
          </div>
        </header>

        {/* ── the track ─────────────────────────────────────────── */}
        <div
          style={{
            border: `2px solid ${ACCENT}44`,
            background: "linear-gradient(180deg, #0d0619, #170c2b)",
            padding: "34px 40px 26px",
            position: "relative",
          }}
        >
          <div style={{ textAlign: "center", marginBottom: 26 }}>
            <span
              key={rolls[0]?.key ?? 0}
              data-testid="dice-roll"
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 96,
                lineHeight: 1,
                color: last ? (last.win ? GOOD : DANGER) : "#ece6ff",
                textShadow: last
                  ? `0 0 30px ${last.win ? GOOD : DANGER}`
                  : "0 0 24px rgba(236,230,255,.35)",
                display: "inline-block",
                animation: last ? "potPop .5s ease" : undefined,
              }}
            >
              {last ? last.roll.toFixed(2) : "00.00"}
            </span>
            {last && (
              <div
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 13,
                  letterSpacing: 3,
                  marginTop: 6,
                  color: last.win ? GOOD : DANGER,
                  animation: "bannerIn .5s ease",
                }}
              >
                {last.win ? `WIN +${Math.floor(bet * last.payoutMultiplier).toLocaleString()}` : "BUST"}
              </div>
            )}
          </div>

          <div style={{ position: "relative", height: 56, marginBottom: 8 }}>
            {/* the 0–100 bar */}
            <div
              style={{
                position: "absolute",
                inset: "14px 0",
                borderRadius: 10,
                background: direction === "under"
                  ? `linear-gradient(90deg, ${GOOD} 0%, ${GOOD} ${target}%, #2a1b4d ${target}%, #2a1b4d 100%)`
                  : `linear-gradient(90deg, #2a1b4d 0%, #2a1b4d ${target}%, ${GOOD} ${target}%, ${GOOD} 100%)`,
                boxShadow: "inset 0 0 18px rgba(0,0,0,.6)",
                transition: "background .15s ease",
              }}
            />
            {/* target marker */}
            <div
              style={{
                position: "absolute",
                left: `${target}%`,
                top: 4,
                transform: "translateX(-50%)",
                width: 4,
                height: 48,
                background: "#ece6ff",
                boxShadow: "0 0 10px #ece6ff",
                transition: "left .15s ease",
              }}
            />
            {/* roll marker */}
            {markerRoll !== null && (
              <div
                data-testid="dice-marker"
                style={{
                  position: "absolute",
                  left: `${Math.max(0, Math.min(100, markerRoll))}%`,
                  top: -6,
                  transform: "translateX(-50%)",
                  transition: "left .12s cubic-bezier(.2,.8,.3,1)",
                  filter: `drop-shadow(0 0 8px ${ACCENT})`,
                }}
              >
                <div style={{ width: 0, height: 0, margin: "0 auto", borderLeft: "9px solid transparent", borderRight: "9px solid transparent", borderTop: `14px solid ${ACCENT}` }} />
              </div>
            )}
          </div>
          <div style={{ display: "flex", justifyContent: "space-between", fontFamily: "var(--font-body)", fontSize: 17, color: "#5c4f80" }}>
            {rollTicks.map((t) => (
              <span key={t}>{t}</span>
            ))}
          </div>

          {/* recent rolls */}
          <div style={{ display: "flex", gap: 6, marginTop: 18, minHeight: 30, flexWrap: "wrap" }}>
            {rolls.map((r) => (
              <span
                key={r.key}
                className="pixelated"
                style={{
                  fontFamily: "var(--font-body)",
                  fontSize: 18,
                  padding: "1px 8px",
                  border: `1px solid ${r.win ? GOOD : DANGER}66`,
                  color: r.win ? GOOD : DANGER,
                  background: r.win ? "rgba(95,224,138,.08)" : "rgba(242,100,61,.08)",
                  animation: "radNodeIn .3s ease",
                }}
              >
                {r.roll.toFixed(2)}
              </span>
            ))}
          </div>
        </div>

        {/* ── controls ──────────────────────────────────────────── */}
        <div
          style={{
            marginTop: 16,
            border: "2px solid #35205c",
            background: "#0d0619",
            padding: "16px 20px",
            display: "grid",
            gridTemplateColumns: "1.4fr 1fr",
            gap: 20,
          }}
        >
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            <div style={{ display: "flex", gap: 10 }}>
              {(["under", "over"] as const).map((d) => (
                <button
                  key={d}
                  type="button"
                  data-testid={`dice-${d}`}
                  disabled={rolling}
                  onClick={() => {
                    sound.unlock();
                    sound.click();
                    setDirection(d);
                  }}
                  style={{
                    flex: 1,
                    fontFamily: "var(--font-display)",
                    fontSize: 16,
                    letterSpacing: 3,
                    padding: "12px 0",
                    cursor: "pointer",
                    border: `2px solid ${direction === d ? ACCENT : "#35205c"}`,
                    background: direction === d ? "#2a2006" : "#06040d",
                    color: direction === d ? ACCENT : "#8878b8",
                    boxShadow: direction === d ? `0 0 16px ${ACCENT}55` : "none",
                  }}
                >
                  ROLL {d === "under" ? "UNDER" : "OVER"}
                </button>
              ))}
            </div>
            <label style={{ display: "flex", alignItems: "center", gap: 12 }}>
              <span style={{ fontFamily: "var(--font-display)", fontSize: 10, letterSpacing: 2, color: "#8878b8" }}>TARGET</span>
              <input
                type="range"
                min={2}
                max={98}
                value={target}
                disabled={rolling}
                data-testid="dice-slider"
                onChange={(e) => setTarget(Number(e.target.value))}
                style={{ flex: 1, accentColor: ACCENT, height: 22 }}
              />
              <span style={{ fontFamily: "var(--font-body)", fontSize: 24, color: ACCENT, minWidth: 44, textAlign: "right" }}>{target}</span>
            </label>
            <BetInput value={bet} onChange={setBet} steps={BET_STEPS} min={info?.minBet ?? 1} max={info?.maxBet ?? 10000} balance={balance} accent={ACCENT} disabled={rolling} testIdPrefix="dice-bet" />
          </div>

          <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <div style={{ display: "flex", gap: 10 }}>
              {[
                { label: "CHANCE", value: `${chance}%`, testid: "dice-chance" },
                { label: "MULTIPLIER", value: `${multiplier.toFixed(4)}×`, testid: "dice-multiplier" },
              ].map((box) => (
                <div key={box.label} style={{ flex: 1, border: "1px solid #35205c", background: "#06040d", padding: "10px 12px" }}>
                  <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80" }}>{box.label}</div>
                  <div data-testid={box.testid} style={{ fontFamily: "var(--font-body)", fontSize: 26, color: "#ece6ff" }}>{box.value}</div>
                </div>
              ))}
            </div>
            <div style={{ border: "1px solid #35205c", background: "#06040d", padding: "10px 12px" }}>
              <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80" }}>PROFIT ON WIN</div>
              <div style={{ fontFamily: "var(--font-body)", fontSize: 26, color: GOOD }}>+{profit.toLocaleString()}</div>
            </div>
            <button
              type="button"
              data-testid="dice-roll-button"
              disabled={rolling || !session.data || (balance ?? 0) < bet}
              onClick={doRoll}
              style={{
                flex: 1,
                minHeight: 74,
                fontFamily: "var(--font-display)",
                fontSize: 24,
                letterSpacing: 4,
                cursor: rolling ? "wait" : "pointer",
                border: `3px solid ${ACCENT}`,
                background: rolling ? "#2a2006" : "linear-gradient(180deg, #2a2006, #0d0619)",
                color: ACCENT,
                textShadow: `0 0 18px ${ACCENT}`,
                boxShadow: `0 0 26px ${ACCENT}44, inset 0 0 22px ${ACCENT}22`,
                animation: rolling ? "countdownBlink .4s linear infinite" : "radHubGlow 2.2s ease-in-out infinite alternate",
              }}
            >
              {rolling ? "ROLLING…" : `ROLL ${bet} CR`}
            </button>
          </div>
        </div>

        {play.isError && (
          <div role="alert" style={{ marginTop: 10, fontFamily: "var(--font-body)", fontSize: 19, color: DANGER }}>
            {(play.error as PlayError)?.message ?? "roll failed"}
          </div>
        )}
        <div style={{ marginTop: 10, fontFamily: "var(--font-body)", fontSize: 16, color: "#5c4f80" }}>
          Roll 0.00–99.99 · win pays {multiplier.toFixed(3)}× · 1% house edge · provably fair
          {info ? ` · RTP ${(info.theoreticalRtp * 100).toFixed(2)}%` : ""}
        </div>
      </div>
    </main>
  );
}
