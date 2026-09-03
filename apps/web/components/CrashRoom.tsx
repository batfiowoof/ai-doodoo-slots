"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useQueryClient } from "@tanstack/react-query";
import { useSession } from "@/lib/api";
import type { Me } from "@/lib/types";
import { sound } from "@/lib/sound";

// The crash room: curve, bet panel, players, history. The server decides
// everything — the client renders whatever arrives and never predicts a
// balance or a multiplier.
type RoundState = "betting_open" | "locked" | "running" | "settled";

interface Stake {
  userId: number;
  displayName?: string;
  credits: number;
  cashedAt?: number;
}

interface RoomSnapshot {
  room: { slug: string; name: string; minBet: number; maxBet: number; playerCount: number };
  round: {
    roundId: number;
    state: RoundState;
    multiplier: number;
    recentCrashes: number[];
    stakes: Stake[];
  } | null;
}

// SSR-safe: module scope also evaluates on the server, where `location`
// does not exist. The browser default points the dev server at the API.
const WS_URL =
  typeof location === "undefined"
    ? "ws://localhost:8080/api/v1/ws"
    : process.env.NEXT_PUBLIC_WS_URL ??
      (location.port === "3000"
        ? "ws://localhost:8080/api/v1/ws"
        : `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/ws`);

const BET_STEPS = [5, 10, 25, 50, 100];

export default function CrashRoom({ slug }: { slug: string }) {
  const session = useSession();
  const qc = useQueryClient();

  const [connected, setConnected] = useState(false);
  const [snapshot, setSnapshot] = useState<RoomSnapshot | null>(null);
  const [multiplier, setMultiplier] = useState(1);
  const [state, setState] = useState<RoundState>("betting_open");
  const [, setRoundId] = useState(0);
  const [stakes, setStakes] = useState<Stake[]>([]);
  const [history, setHistory] = useState<number[]>([]);
  const [crashed, setCrashed] = useState(false);
  const [lastPayout, setLastPayout] = useState<number | null>(null);
  const [betAmount, setBetAmount] = useState(10);
  const [autoTarget, setAutoTarget] = useState("2.00");
  const [myBet, setMyBet] = useState<{ credits: number; cashed: boolean } | null>(null);
  const [note, setNote] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const userId = session.data?.user.id;

  // Server → client envelope: one loose shape covering every message type.
  interface ServerPayload {
    state?: string;
    multiplier?: number;
    seq?: number;
    crashMultiplier?: number;
    recentCrashes?: number[];
    stakes?: Stake[];
    payouts?: { userId: number; payoutCredits: number }[];
    userId?: number;
    displayName?: string;
    credits?: number;
    balanceCredits?: number;
    betCredits?: number;
    payoutCredits?: number;
    code?: string;
    slug?: string;
  }

  const showNote = useCallback((text: string) => {
    setNote(text);
    window.setTimeout(() => setNote(null), 2600);
  }, []);

  // Socket lifecycle: connect once, rejoin the room on (re)connect.
  useEffect(() => {
    let closed = false;
    let retry: number | undefined;

    const connect = () => {
      if (closed) return;
      const ws = new WebSocket(WS_URL);
      wsRef.current = ws;

      ws.onopen = () => {
        if (wsRef.current !== ws) return; // stale socket from a previous effect run
        setConnected(true);
        // Full state snapshot on join AND reconnect.
        ws.send(JSON.stringify({ type: "join_room", payload: { slug } }));
      };
      ws.onclose = () => {
        if (wsRef.current !== ws) return;
        setConnected(false);
        if (!closed) retry = window.setTimeout(connect, 1000);
      };
      ws.onmessage = (ev) => {
        const msg = JSON.parse(ev.data) as { type: string; payload?: ServerPayload };
        if (!msg.payload) return;
        const p = msg.payload;
        switch (msg.type) {
          case "room_snapshot": {
            const snap = msg.payload as RoomSnapshot;
            setSnapshot(snap);
            if (snap.round) {
              setRoundId(snap.round.roundId);
              setState(snap.round.state);
              setMultiplier(snap.round.multiplier || 1);
              setStakes(snap.round.stakes ?? []);
              setHistory(snap.round.recentCrashes ?? []);
              setCrashed(snap.round.state === "settled");
            }
            break;
          }
          case "round_state": {
            const next = p.state as RoundState;
            setState(next);
            if (next === "betting_open") {
              setCrashed(false);
              setMyBet(null);
              setLastPayout(null);
              void qc.invalidateQueries({ queryKey: ["me"] });
            }
            break;
          }
          case "round_tick":
            setMultiplier(p.multiplier ?? 1);
            break;
          case "round_result":
            setMultiplier(p.crashMultiplier ?? 1);
            setCrashed(true);
            sound.error();
            void qc.invalidateQueries({ queryKey: ["me"] });
            break;
          case "round_settlements": {
            const mine = (p.payouts ?? []).find(
              (p: { userId: number }) => p.userId === userId,
            );
            if (mine) setLastPayout(mine.payoutCredits);
            setStakes([]);
            break;
          }
          case "bet_placed":
            if (p.userId === undefined) break;
            {
              const uid = p.userId;
              setStakes((prev) => [
                ...prev.filter((s) => s.userId !== uid),
                {
                  userId: uid,
                  displayName: p.displayName,
                  credits: p.credits ?? 0,
                },
              ]);
            }
            break;
          case "bet_cashout":
            setStakes((prev) =>
              prev.map((s) =>
                s.userId === p.userId
                  ? { ...s, cashedAt: p.multiplier }
                  : s,
              ),
            );
            break;
          case "bet_ack": {
            const p = msg.payload;
            const bal = p.balanceCredits;
            if (bal !== undefined) {
              qc.setQueryData<Me>(["me"], (old) =>
                old ? { ...old, balanceCredits: bal } : old,
              );
            }
            if (p.betCredits !== undefined) {
              setMyBet({ credits: p.betCredits, cashed: false });
              sound.bell();
            }
            if (p.payoutCredits !== undefined) {
              setMyBet((prev) => (prev ? { ...prev, cashed: true } : prev));
              sound.bell();
            }
            break;
          }
          case "error":
            showNote((p?.code ?? "error").toUpperCase().replaceAll("_", " "));
            sound.error();
            break;
        }
      };
    };

    connect();
    return () => {
      closed = true;
      if (retry) window.clearTimeout(retry);
      wsRef.current?.close();
    };
  }, [slug, userId, qc, showNote]);

  // The curve: server ticks land on frames, no client-side interpolation.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const W = canvas.width;
    const H = canvas.height;
    ctx.clearRect(0, 0, W, H);

    // Dithered baseline (checkerboard), pixel rules: hard edges only.
    ctx.fillStyle = "#241640";
    for (let x = 0; x < W; x += 4) {
      ctx.fillRect(x, H - 2, 2, 2);
      ctx.fillRect(x + 2, H - 4, 2, 2);
    }

    // The curve: sample the exponential growth up to the shown multiplier.
    const maxShown = Math.max(2, multiplier);
    ctx.strokeStyle = crashed ? "#f2643d" : "#22e8ff";
    ctx.lineWidth = 2;
    ctx.beginPath();
    const steps = 60;
    const elapsedFor = (m: number) => Math.log(m) / 0.12;
    const tMax = elapsedFor(maxShown);
    for (let i = 0; i <= steps; i++) {
      const t = (i / steps) * tMax;
      const m = Math.exp(0.12 * t);
      const x = (t / tMax) * (W - 8);
      const y = H - 6 - ((m - 1) / (maxShown - 1)) * (H - 24);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(Math.round(x), Math.round(y));
    }
    ctx.stroke();
  }, [multiplier, crashed]);

  const send = useCallback((type: string, payload: unknown) => {
    wsRef.current?.send(JSON.stringify({ type, payload }));
  }, []);

  const placeBet = () => {
    const target = Math.max(1.01, parseFloat(autoTarget) || 2);
    sound.unlock();
    sound.click();
    send("place_bet", {
      credits: betAmount,
      autoCashout: target,
      idempotencyKey: crypto.randomUUID(),
    });
  };

  const cashOut = () => {
    sound.unlock();
    sound.click();
    send("cash_out", {});
  };

  const balance = session.data?.balanceCredits;
  const canBet = state === "betting_open" && !myBet && connected;
  const canCash = state === "running" && myBet !== null && !myBet.cashed;

  return (
    <div style={{ position: "fixed", inset: 0, overflow: "hidden", background: "#06040d" }}>
      <div
        style={{
          position: "relative",
          zIndex: 5,
          height: "100%",
          display: "flex",
          flexDirection: "column",
          padding: "18px 26px",
          gap: 12,
        }}
      >
        <header style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <Link
              href="/"
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 11,
                color: "#8878b8",
                textDecoration: "none",
              }}
            >
              ◀ FLOOR
            </Link>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 16,
                letterSpacing: 2,
                color: "#ff2d95",
              }}
            >
              {snapshot?.room.name?.toUpperCase() ?? slug.toUpperCase()}
            </span>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 11,
                color: connected ? "#5fe08a" : "#f2643d",
              }}
            >
              {connected ? "● LIVE" : "○ OFFLINE"}
            </span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 26, color: "#ff8a1f" }}>
              {balance === undefined ? "····" : balance.toLocaleString()}
            </span>
          </div>
        </header>

        {/* History strip: last crash multipliers as pixel chips */}
        <div style={{ display: "flex", gap: 6, minHeight: 22 }}>
          {history.slice(0, 10).map((m, i) => (
            <span
              key={i}
              style={{
                fontFamily: "var(--font-body)",
                fontSize: 16,
                padding: "1px 8px",
                border: `1px solid ${m >= 2 ? "#5fe08a" : m >= 1.3 ? "#6b5f9e" : "#8c3b2e"}`,
                color: m >= 2 ? "#5fe08a" : m >= 1.3 ? "#8878b8" : "#f2643d",
              }}
            >
              {m.toFixed(2)}×
            </span>
          ))}
        </div>

        {/* Curve window */}
        <div
          style={{
            position: "relative",
            flex: 1,
            border: "2px solid #35205c",
            background: "#0d0619",
            overflow: "hidden",
          }}
        >
          <canvas
            ref={canvasRef}
            width={1200}
            height={520}
            style={{ width: "100%", height: "100%", imageRendering: "pixelated" }}
          />
          <div
            style={{
              position: "absolute",
              inset: 0,
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              pointerEvents: "none",
            }}
          >
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 54,
                color: crashed ? "#f2643d" : state === "running" ? "#fff" : "#8878b8",
                textShadow: crashed
                  ? "0 0 18px rgba(242,100,61,.8)"
                  : state === "running"
                    ? "0 0 14px rgba(34,232,255,.6)"
                    : "none",
              }}
            >
              {state === "betting_open" ? "PLACE YOUR BETS" : `${multiplier.toFixed(2)}×`}
            </span>
            {crashed && (
              <span style={{ fontFamily: "var(--font-display)", fontSize: 18, color: "#f2643d" }}>
                CRASHED
              </span>
            )}
            {lastPayout !== null && (
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 14,
                  color: "#5fe08a",
                  marginTop: 8,
                }}
              >
                YOU CASHED +{lastPayout.toLocaleString()}
              </span>
            )}
            {myBet && !crashed && (
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 12,
                  color: "#ff8a1f",
                  marginTop: 8,
                }}
              >
                YOUR BET {myBet.credits.toLocaleString()} {myBet.cashed ? "· CASHED" : "· RIDING"}
              </span>
            )}
          </div>
        </div>

        {/* Bet panel + players */}
        <div style={{ display: "flex", gap: 16, minHeight: 130 }}>
          <div
            style={{
              width: 420,
              padding: 14,
              border: "2px solid #35205c",
              background: "#170c2b",
              display: "flex",
              flexDirection: "column",
              gap: 8,
            }}
          >
            <div style={{ display: "flex", gap: 6 }}>
              {BET_STEPS.map((v) => (
                <button
                  key={v}
                  type="button"
                  onClick={() => {
                    setBetAmount(v);
                    sound.unlock();
                    sound.click();
                  }}
                  style={{
                    flex: 1,
                    fontFamily: "var(--font-body)",
                    fontSize: 18,
                    padding: "4px 0",
                    border: `1px solid ${betAmount === v ? "#ff8a1f" : "#6b4a1c"}`,
                    background: betAmount === v ? "#2a1406" : "#06040d",
                    color: betAmount === v ? "#ff8a1f" : "#8878b8",
                    cursor: "pointer",
                  }}
                >
                  {v}
                </button>
              ))}
            </div>
            <label style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span style={{ fontFamily: "var(--font-display)", fontSize: 10, color: "#8878b8" }}>
                AUTO CASHOUT ×
              </span>
              <input
                value={autoTarget}
                onChange={(e) => setAutoTarget(e.target.value.replace(/[^0-9.]/g, ""))}
                style={{
                  width: 80,
                  fontFamily: "var(--font-body)",
                  fontSize: 18,
                  background: "#06040d",
                  border: "1px solid #35205c",
                  color: "#ff8a1f",
                  padding: "2px 8px",
                }}
              />
            </label>
            {canCash ? (
              <button
                type="button"
                onClick={cashOut}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 14,
                  padding: "10px 0",
                  border: "2px solid #5fe08a",
                  background: "#0b2a33",
                  color: "#5fe08a",
                  cursor: "pointer",
                }}
              >
                CASH OUT {(myBet!.credits * multiplier).toFixed(0)}
              </button>
            ) : (
              <button
                type="button"
                onClick={placeBet}
                disabled={!canBet || (balance ?? 0) < betAmount}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 14,
                  padding: "10px 0",
                  border: "2px solid #ff8a1f",
                  background: "#2a1406",
                  color: "#ff8a1f",
                  cursor: canBet ? "pointer" : "not-allowed",
                  opacity: canBet ? 1 : 0.5,
                }}
              >
                {myBet
                  ? "BET PLACED"
                  : state === "betting_open"
                    ? `BET ${betAmount}`
                    : "WAIT FOR NEXT ROUND"}
              </button>
            )}
            {note && (
              <span style={{ fontFamily: "var(--font-display)", fontSize: 10, color: "#f2643d" }}>
                {note}
              </span>
            )}
          </div>

          <div
            style={{
              flex: 1,
              padding: 14,
              border: "2px solid #35205c",
              background: "#170c2b",
              overflowY: "auto",
            }}
          >
            <div
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 10,
                color: "#8878b8",
                marginBottom: 8,
              }}
            >
              PLAYERS IN ROUND — {stakes.length}
            </div>
            {stakes.map((s) => (
              <div
                key={s.userId}
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  fontFamily: "var(--font-body)",
                  fontSize: 18,
                  color: s.userId === userId ? "#ff8a1f" : "#cfc4f2",
                  padding: "1px 0",
                }}
              >
                <span>{s.displayName || `PLAYER ${s.userId}`}</span>
                <span>
                  {s.credits.toLocaleString()}
                  {s.cashedAt ? (
                    <span style={{ color: "#5fe08a" }}> · {s.cashedAt.toFixed(2)}×</span>
                  ) : (
                    <span style={{ color: "#6b5f9e" }}> · riding</span>
                  )}
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
