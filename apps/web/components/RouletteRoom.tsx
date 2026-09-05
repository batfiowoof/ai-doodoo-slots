"use client";

// The roulette room: a CSS-rendered European wheel, the classic betting
// tableau, chip console and table stakes. The server decides everything —
// the pocket resolves on the chain before betting even opens, the client
// only renders what arrives and never predicts a balance. (`?demo=` dev
// modes are the one exception: they fake the message flow locally.)

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useQueryClient } from "@tanstack/react-query";
import { useSession } from "@/lib/api";
import type { Me, ProfileUpdatedEvent } from "@/lib/types";
import { sound } from "@/lib/sound";
import { Avatar } from "@/components/Avatar";
import Chip, { ChipStack } from "@/components/Chip";
import {
  BOARD_ROWS,
  EUROPEAN_ORDER,
  POCKET_COUNT,
  POCKET_COLORS,
  payoutOdds,
  pocketColor,
  SPOTS,
  spotWins,
} from "@/lib/roulette";

type RoundState = "betting_open" | "locked" | "running" | "settled";

interface SpotStake {
  spot: string;
  credits: number;
}

interface Stake {
  userId: number;
  displayName?: string;
  credits: number;
  spots?: SpotStake[];
}

interface RoomSnapshot {
  room: { slug: string; name: string; minBet: number; maxBet: number; playerCount: number };
  round: {
    roundId: number;
    state: RoundState;
    multiplier: number;
    msLeft?: number;
    recentCrashes: number[]; // recent winning pockets (shared lobby shape)
    stakes: Stake[];
  } | null;
}

interface ServerPayload {
  state?: string;
  msLeft?: number;
  multiplier?: number;
  seq?: number;
  pocket?: number;
  color?: string;
  wheelIndex?: number;
  recentCrashes?: number[];
  stakes?: Stake[];
  payouts?: { userId: number; payoutCredits: number; spot?: string }[];
  userId?: number;
  displayName?: string;
  credits?: number;
  total?: number;
  balanceCredits?: number;
  betCredits?: number;
  spot?: string;
  totalStaked?: number;
  payoutCredits?: number;
  cleared?: number;
  refundCredits?: number;
  code?: string;
}

// Resolved lazily: module scope runs during SSR where `location` is absent.
const wsUrl = () =>
  process.env.NEXT_PUBLIC_WS_URL ??
  (typeof location !== "undefined" && location.port === "3000"
    ? "ws://localhost:8082/api/v1/ws"
    : `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/ws`);

const BETTING_MS = 18000; // mirrors the server window; visual countdown only
const CHIP_STEPS = [5, 10, 25, 50, 100];
const CHIP_COLOR: Record<number, "pink" | "cyan" | "green" | "orange" | "purple"> = {
  5: "pink",
  10: "cyan",
  25: "green",
  50: "orange",
  100: "purple",
};

// Wheel geometry: pockets fan clockwise from the top marker.
const WHEEL_SIZE = 356;
const SEG = 360 / POCKET_COUNT;
const NUMBER_R = 132;
const BALL_ORBIT_R = 158;
const BALL_POCKET_R = 112;
const IDLE_SPEED = 26; // deg/s drift between spins
const SPIN_SPEED = 430; // deg/s suspense spin
const BALL_SPIN_SPEED = -620;
const EASE_MS = 1900;

const CONIC = (() => {
  const stops = EUROPEAN_ORDER.map((pocket, i) => {
    const c = POCKET_COLORS[pocketColor(pocket)];
    return `${c} ${(i * SEG).toFixed(4)}deg ${((i + 1) * SEG).toFixed(4)}deg`;
  });
  return `conic-gradient(from ${(-SEG / 2).toFixed(4)}deg, ${stops.join(", ")})`;
})();

export default function RouletteRoom({ slug }: { slug: string }) {
  const session = useSession();
  const qc = useQueryClient();

  const [connected, setConnected] = useState(false);
  const [snapshot, setSnapshot] = useState<RoomSnapshot | null>(null);
  const [state, setState] = useState<RoundState>("betting_open");
  const [, setRoundId] = useState(0);
  const [stakes, setStakes] = useState<Stake[]>([]);
  const [history, setHistory] = useState<number[]>([]);
  const [pocket, setPocket] = useState<number | null>(null);
  const [lastPayout, setLastPayout] = useState<number | null>(null);
  const [winCells, setWinCells] = useState<string[]>([]);
  const [myBets, setMyBets] = useState<Record<string, number>>({});
  const [chip, setChip] = useState(10);
  const [note, setNote] = useState<string | null>(null);
  const [tMinus, setTMinus] = useState(18);
  const [demoBalance, setDemoBalance] = useState<number | null>(null);
  const [profiles, setProfiles] = useState<Record<number, ProfileUpdatedEvent>>({});

  const wsRef = useRef<WebSocket | null>(null);
  const betOpenAtRef = useRef(0);
  const beepedRef = useRef(99);
  const sockGenRef = useRef(0);
  const userId = session.data?.user.id;

  // ── wheel engine: one rAF loop drives wheel rotation + the ball ─────────
  const wheelRef = useRef<HTMLDivElement | null>(null);
  const ballRef = useRef<HTMLDivElement | null>(null);
  const anim = useRef({
    mode: "cruise" as "cruise" | "spin" | "ease",
    rot: 0,
    wheelSpeed: IDLE_SPEED,
    ballAngle: 0,
    ballSpeed: IDLE_SPEED,
    ballRadius: BALL_POCKET_R,
    easeFrom: 0,
    easeTo: 0,
    ballFromA: 0,
    ballFromR: 0,
    ballToA: 0,
    easeT0: 0,
  });

  useEffect(() => {
    let raf = 0;
    let last = performance.now();
    const frame = (now: number) => {
      const a = anim.current;
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      if (a.mode === "ease") {
        const t = Math.min(1, (now - a.easeT0) / EASE_MS);
        const k = 1 - Math.pow(1 - t, 3);
        a.rot = a.easeFrom + (a.easeTo - a.easeFrom) * k;
        a.ballAngle = a.ballFromA + (a.ballToA - a.ballFromA) * k;
        a.ballRadius = a.ballFromR + (BALL_POCKET_R - a.ballFromR) * k;
        if (t >= 1) {
          a.mode = "cruise";
          a.wheelSpeed = IDLE_SPEED;
          a.ballSpeed = IDLE_SPEED; // the ball rides the wheel home
          sound.reelStop(0);
        }
      } else {
        a.rot += a.wheelSpeed * dt;
        a.ballAngle += a.ballSpeed * dt;
      }
      if (wheelRef.current) {
        wheelRef.current.style.transform = `rotate(${a.rot.toFixed(3)}deg)`;
      }
      if (ballRef.current) {
        const rad = (a.ballAngle * Math.PI) / 180;
        const x = Math.sin(rad) * a.ballRadius;
        const y = -Math.cos(rad) * a.ballRadius;
        ballRef.current.style.transform = `translate(-50%, -50%) translate(${x.toFixed(2)}px, ${y.toFixed(2)}px)`;
      }
      raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, []);

  const applyPhase = useCallback(
    (next: RoundState, msLeft?: number) => {
      setState(next);
      if (next === "betting_open") {
        setPocket(null);
        setLastPayout(null);
        setWinCells([]);
        setMyBets({});
        beepedRef.current = 99;
        const left = msLeft !== undefined ? Math.max(0, msLeft) : BETTING_MS;
        betOpenAtRef.current = performance.now() - (BETTING_MS - left);
        setTMinus(Math.ceil(left / 1000));
        anim.current.mode = "cruise";
        anim.current.wheelSpeed = IDLE_SPEED;
        anim.current.ballSpeed = IDLE_SPEED;
        void qc.invalidateQueries({ queryKey: ["me"] });
      } else if (next === "locked") {
        sound.turnAlert();
      } else if (next === "running") {
        anim.current.mode = "spin";
        anim.current.wheelSpeed = SPIN_SPEED;
        anim.current.ballSpeed = BALL_SPIN_SPEED;
        anim.current.ballRadius = BALL_ORBIT_R;
        sound.startWhir();
      } else if (next === "settled") {
        sound.stopWhir();
      }
    },
    [qc],
  );

  const applyResult = useCallback((p: ServerPayload) => {
    if (typeof p.pocket !== "number") return;
    setPocket(p.pocket);
    sound.stopWhir();
    // Ease the wheel so the winning pocket glides under the top marker,
    // and send the ball home into it. Two full turns of theatre first.
    const idx = typeof p.wheelIndex === "number" ? p.wheelIndex : EUROPEAN_ORDER.indexOf(p.pocket);
    const a = anim.current;
    const target = a.rot + 720 + (((-(idx * SEG) - a.rot) % 360) + 360) % 360;
    a.easeFrom = a.rot;
    a.easeTo = target;
    const d = ((0 - a.ballAngle) % 360 + 540) % 360 - 180;
    a.ballFromA = a.ballAngle;
    a.ballToA = a.ballAngle + d;
    a.ballFromR = a.ballRadius;
    a.easeT0 = performance.now();
    a.mode = "ease";
    const cells = SPOTS.filter((s) => s.hits(p.pocket!)).map((s) => s.id);
    window.setTimeout(() => setWinCells(cells), EASE_MS - 150);
  }, []);

  const showNote = useCallback((text: string) => {
    setNote(text);
    window.setTimeout(() => setNote(null), 2600);
  }, []);

  // T-minus countdown while bets are open. Purely visual — the server
  // decides when betting actually locks. Frozen while disconnected.
  useEffect(() => {
    if (state !== "betting_open" || !connected) return;
    const id = window.setInterval(() => {
      const left = Math.max(0, BETTING_MS - (performance.now() - betOpenAtRef.current));
      const secs = Math.ceil(left / 1000);
      setTMinus(secs);
      if (secs <= 5 && secs >= 1 && beepedRef.current > secs) {
        beepedRef.current = secs;
        sound.countdownBeep(secs === 1);
      }
    }, 100);
    return () => window.clearInterval(id);
  }, [state, connected]);

  // Socket lifecycle: connect once, rejoin on (re)connect — or run a local
  // demo loop in dev when ?demo= is on the URL.
  useEffect(() => {
    const demoParam =
      process.env.NODE_ENV !== "production" && typeof location !== "undefined"
        ? new URLSearchParams(location.search).get("demo")
        : null;

    if (demoParam) {
      // Compressed local loop for visual testing: betting 6s → locked →
      // spin 2.2s → settled 3s → repeat, with fake table stakes.
      let timer: number | undefined;
      const startDemo = () => {
        setConnected(true);
        setDemoBalance(1000);
        setSnapshot({
          room: { slug, name: "SALON EUROPÉ-1", minBet: 5, maxBet: 1000, playerCount: 3 },
          round: null,
        });
        setHistory([17, 4, 32, 0, 26, 11, 5]);
        setStakes([
          { userId: 1, displayName: "MAVERICK", credits: 80, spots: [{ spot: "red", credits: 50 }, { spot: "n17", credits: 30 }] },
          { userId: 2, displayName: "NOVA_9", credits: 40, spots: [{ spot: "black", credits: 40 }] },
        ]);
        const demoBets = { red: 25, n17: 10 };
        const begin = () => {
          setDemoBalance(1000);
          setMyBets({ ...demoBets });
          applyPhase("betting_open", BETTING_MS);
          timer = window.setTimeout(() => {
            applyPhase("locked");
            timer = window.setTimeout(() => {
              applyPhase("running");
              timer = window.setTimeout(() => {
                const pocket = EUROPEAN_ORDER[Math.floor(Math.random() * EUROPEAN_ORDER.length)];
                applyResult({
                  pocket,
                  color: pocketColor(pocket),
                  wheelIndex: EUROPEAN_ORDER.indexOf(pocket),
                });
                applyPhase("settled");
                const win = Object.entries(demoBets).reduce(
                  (acc, [spot, credits]) =>
                    acc + (spotWins(spot, pocket) ? credits * (spot === "n17" ? 36 : spot.startsWith("d") || spot.startsWith("c") ? 3 : 2) : 0),
                  0,
                );
                setLastPayout(win);
                setDemoBalance((b) => (b ?? 0) + win);
                setWinCells(SPOTS.filter((s) => s.hits(pocket)).map((s) => s.id));
                setHistory((h) => [pocket, ...h].slice(0, 12));
                timer = window.setTimeout(begin, 3200);
              }, 2200);
            }, 800);
          }, 6000);
        };
        begin();
      };
      const kick = window.setTimeout(startDemo, 0);
      return () => {
        window.clearTimeout(kick);
        if (timer) window.clearTimeout(timer);
        sound.stopWhir();
      };
    }

    let closed = false;
    let retry: number | undefined;

    const connect = () => {
      if (closed) return;
      const gen = ++sockGenRef.current;
      const live = () => !closed && gen === sockGenRef.current;
      const ws = new WebSocket(wsUrl());
      wsRef.current = ws;

      ws.onopen = () => {
        if (!live()) return;
        setConnected(true);
        ws.send(JSON.stringify({ type: "join_room", payload: { slug } }));
      };
      ws.onclose = () => {
        if (!live()) return;
        setConnected(false);
        if (!closed) retry = window.setTimeout(connect, 1000);
      };
      ws.onmessage = (ev) => {
        if (!live()) return;
        const msg = JSON.parse(ev.data) as { type: string; payload?: ServerPayload };
        if (!msg.payload) return;
        const p = msg.payload;
        switch (msg.type) {
          case "room_snapshot": {
            const snap = msg.payload as unknown as RoomSnapshot;
            setSnapshot(snap);
            if (snap.round) {
              setRoundId(snap.round.roundId);
              setStakes(snap.round.stakes ?? []);
              setHistory(snap.round.recentCrashes ?? []);
              applyPhase(snap.round.state, snap.round.msLeft);
              if (snap.round.state === "settled" && typeof snap.round.multiplier === "number") {
                // Deep link into a settled round: restore the last pocket.
                const pocket = snap.round.recentCrashes?.[0];
                if (pocket !== undefined) {
                  applyResult({ pocket, wheelIndex: EUROPEAN_ORDER.indexOf(pocket) });
                  setPocket(pocket);
                }
              }
              const mine = (snap.round.stakes ?? []).find((s) => s.userId === userId);
              if (mine?.spots) {
                setMyBets(Object.fromEntries(mine.spots.map((s) => [s.spot, s.credits])));
              }
            }
            break;
          }
          case "round_state": {
            const next = p.state as RoundState;
            if (next === "betting_open" || next === "running" || next === "locked" || next === "settled") {
              applyPhase(next, p.msLeft);
            }
            break;
          }
          case "round_result":
            applyResult(p);
            break;
          case "round_settlements": {
            const mine = (p.payouts ?? []).filter((x) => x.userId === userId);
            const total = mine.reduce((acc, x) => acc + (x.payoutCredits || 0), 0);
            if (mine.length > 0) setLastPayout(total);
            if (total > 0) {
              const straight = mine.some((x) => x.spot?.startsWith("n"));
              if (straight) sound.bigWin();
              else sound.jackpot(1);
              void qc.invalidateQueries({ queryKey: ["me"] });
            }
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
                  credits: p.total ?? p.credits ?? 0,
                },
              ]);
              if (uid !== userId) sound.chipClink();
            }
            break;
          case "bets_cleared":
            setStakes((prev) => prev.filter((s) => s.userId !== p.userId));
            if (p.userId === userId) setMyBets({});
            break;
          case "bet_ack": {
            const bal = p.balanceCredits;
            if (bal !== undefined) {
              qc.setQueryData<Me>(["me"], (old) =>
                old ? { ...old, balanceCredits: bal } : old,
              );
            }
            if (p.spot && p.betCredits !== undefined) {
              setMyBets((prev) => ({ ...prev, [p.spot!]: (prev[p.spot!] ?? 0) + p.betCredits! }));
              sound.chipToss(2);
            }
            break;
          }
          case "game_ack": {
            // clear_bets reply: reconcile the refunded balance immediately.
            const bal = p.balanceCredits;
            if (bal !== undefined) {
              qc.setQueryData<Me>(["me"], (old) =>
                old ? { ...old, balanceCredits: bal } : old,
              );
            }
            break;
          }
          case "error":
            showNote((p?.code ?? "error").toUpperCase().replaceAll("_", " "));
            sound.error();
            break;
          case "profile_updated": {
            const pu = p as unknown as ProfileUpdatedEvent;
            if (pu && typeof pu.userId === "number") {
              setProfiles((prev) => ({ ...prev, [pu.userId]: pu }));
            }
            break;
          }
        }
      };
    };

    connect();
    return () => {
      closed = true;
      sockGenRef.current++;
      if (retry) window.clearTimeout(retry);
      wsRef.current?.close();
      sound.stopWhir();
    };
  }, [slug, userId, qc, showNote, applyPhase, applyResult]);

  const send = useCallback((type: string, payload: unknown) => {
    wsRef.current?.send(JSON.stringify({ type, payload }));
  }, []);

  const canBet = state === "betting_open" && connected;
  const myTotal = Object.values(myBets).reduce((a, b) => a + b, 0);
  const roomName = snapshot?.room.name?.toUpperCase() ?? slug.toUpperCase();

  const placeSpotBet = (spot: string) => {
    if (!canBet) return;
    sound.unlock();
    sound.click();
    if (demoBalance !== null) {
      setDemoBalance((b) => Math.max(0, (b ?? 0) - chip));
      setMyBets((prev) => ({ ...prev, [spot]: (prev[spot] ?? 0) + chip }));
      sound.chipToss(2);
      return;
    }
    send("place_bet", {
      credits: chip,
      spot,
      idempotencyKey: crypto.randomUUID(),
    });
  };

  const clearBets = () => {
    if (!canBet || myTotal === 0) return;
    sound.unlock();
    sound.click();
    if (demoBalance !== null) {
      setDemoBalance((b) => (b ?? 0) + myTotal);
      setMyBets({});
      sound.chipClink();
      return;
    }
    send("game_action", { action: "clear_bets" });
  };

  // Scale-to-fit like the other rooms' surfaces.
  const [stageScale, setStageScale] = useState(1);
  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth;
      const h = window.innerHeight;
      setStageScale(Math.min(1, (w - 36) / 1180, (h - 150) / 850));
    };
    onResize();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const balance = demoBalance ?? session.data?.balanceCredits;

  const stakeRows = stakes.map((s) => ({
    ...s,
    displayName: profiles[s.userId]?.displayName ?? s.displayName,
  }));

  return (
    <div style={{ position: "fixed", inset: 0, overflow: "hidden", background: "#06040d" }}>
      <div
        style={{
          position: "relative",
          zIndex: 5,
          height: "100%",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          padding: "14px 20px 14px",
          gap: 8,
          overflow: "hidden",
        }}
      >
        {/* ── console bar ───────────────────────────────── */}
        <header
          style={{
            width: "100%",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
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
            <span style={{ fontFamily: "var(--font-display)", fontSize: 13, color: "#5c4f80" }}>
              ◢◤
            </span>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 16,
                letterSpacing: 3,
                color: "#ff2d95",
                textShadow: "0 0 12px rgba(255,45,149,.7)",
              }}
            >
              {roomName}
            </span>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 11,
                color: connected ? "#5fe08a" : "#f2643d",
                textShadow: connected ? "0 0 8px rgba(95,224,138,.6)" : "none",
              }}
            >
              {connected ? "● LINK LIVE" : "○ SIGNAL LOST"}
            </span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 10, color: "#5c4f80" }}>
              CREDITS
            </span>
            <span
              data-testid="credits"
              style={{
                fontFamily: "var(--font-body)",
                fontSize: 30,
                lineHeight: 1,
                color: "#ff8a1f",
                textShadow: "0 0 12px rgba(255,138,31,.6)",
                background: "#06040d",
                boxShadow: "inset 0 0 0 1px #35205c",
                padding: "3px 14px",
              }}
            >
              {balance === undefined ? "····" : balance.toLocaleString()}
            </span>
          </div>
        </header>

        <div
          style={{
            transform: `scale(${stageScale})`,
            transformOrigin: "top center",
            display: "flex",
            flexDirection: "column",
            gap: 10,
          }}
        >
          {/* ── last pockets ───────────────────────────────── */}
          <div style={{ display: "flex", alignItems: "center", gap: 6, minHeight: 24 }}>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 9,
                letterSpacing: 2,
                color: "#5c4f80",
                marginRight: 4,
              }}
            >
              LAST POCKETS
            </span>
            {history.slice(0, 12).map((n, i) => (
              <span
                key={`${i}-${n}`}
                style={{
                  fontFamily: "var(--font-body)",
                  fontSize: 17,
                  lineHeight: 1,
                  padding: "3px 9px",
                  border: `1px solid ${POCKET_COLORS[pocketColor(n)]}`,
                  color: pocketColor(n) === "black" ? "#cfc4f2" : POCKET_COLORS[pocketColor(n)],
                  background: "#0d0619",
                  animation: i === 0 ? "logPop .25s ease-out both" : undefined,
                }}
              >
                {n}
              </span>
            ))}
          </div>

          {/* ── wheel + table stakes ──────────────────────── */}
          <div style={{ display: "flex", gap: 10, alignItems: "stretch" }}>
            <section
              style={{
                position: "relative",
                width: 640,
                height: 396,
                border: "2px solid #35205c",
                background: "radial-gradient(circle at 50% 42%, #130a24, #0d0619 72%)",
                overflow: "hidden",
              }}
            >
              <div className="crt" style={{ position: "absolute", inset: 0, pointerEvents: "none", zIndex: 4 }} />

              {/* wheel */}
              <div
                style={{
                  position: "absolute",
                  left: 180,
                  top: 198,
                  width: 0,
                  height: 0,
                }}
              >
                {/* rotating plate */}
                <div
                  ref={wheelRef}
                  style={{
                    position: "absolute",
                    left: -WHEEL_SIZE / 2,
                    top: -WHEEL_SIZE / 2,
                    width: WHEEL_SIZE,
                    height: WHEEL_SIZE,
                    borderRadius: "50%",
                    background: CONIC,
                    boxShadow:
                      "0 0 44px rgba(157,77,255,.35), inset 0 0 0 3px #06040d, inset 0 0 0 5px #35205c",
                    willChange: "transform",
                  }}
                >
                  {EUROPEAN_ORDER.map((n, i) => (
                    <span
                      key={n}
                      style={{
                        position: "absolute",
                        left: "50%",
                        top: "50%",
                        transform: `rotate(${(i * SEG).toFixed(3)}deg) translateY(-${NUMBER_R}px) rotate(${(-(i * SEG)).toFixed(3)}deg) translate(-50%, -50%)`,
                        fontFamily: "var(--font-display)",
                        fontSize: 10,
                        color: "#ece6ff",
                        textShadow: "0 1px 0 #06040d",
                      }}
                    >
                      {n}
                    </span>
                  ))}
                  {/* pocket separators */}
                  <div
                    style={{
                      position: "absolute",
                      inset: 62,
                      borderRadius: "50%",
                      border: "2px dashed rgba(6,4,13,.65)",
                    }}
                  />
                </div>

                {/* ball */}
                <div
                  ref={ballRef}
                  style={{
                    position: "absolute",
                    left: 0,
                    top: 0,
                    width: 10,
                    height: 10,
                    borderRadius: "50%",
                    background: "#f2f9ff",
                    boxShadow: "0 0 8px rgba(242,249,255,.9)",
                    zIndex: 3,
                  }}
                />

                {/* hub */}
                <div
                  style={{
                    position: "absolute",
                    left: -74,
                    top: -74,
                    width: 148,
                    height: 148,
                    borderRadius: "50%",
                    background: "linear-gradient(#1d1036,#0d0619 78%)",
                    border: "2px solid #35205c",
                    boxShadow: "0 0 30px rgba(34,232,255,.25), inset 0 0 0 4px #0d0619",
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    justifyContent: "center",
                    gap: 2,
                    zIndex: 2,
                  }}
                >
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 9,
                      letterSpacing: 3,
                      color: "#8878b8",
                    }}
                  >
                    {state === "betting_open" ? "PLACE BETS" : state === "locked" ? "NO MORE BETS" : state === "running" ? "SPINNING" : "RESULT"}
                  </span>
                  {state === "betting_open" ? (
                    <span
                      style={{
                        fontFamily: "var(--font-display)",
                        fontSize: 34,
                        lineHeight: 1,
                        color: tMinus <= 5 ? "#ff8a1f" : "#ece6ff",
                        textShadow: tMinus <= 5 ? "0 0 18px rgba(255,138,31,.9)" : "0 0 10px rgba(34,232,255,.5)",
                      }}
                    >
                      {tMinus}
                    </span>
                  ) : (
                    <span
                      key={pocket ?? -1}
                      style={{
                        fontFamily: "var(--font-display)",
                        fontSize: 34,
                        lineHeight: 1,
                        color: pocket === null ? "#5c4f80" : pocketColor(pocket) === "black" ? "#ece6ff" : POCKET_COLORS[pocketColor(pocket)],
                        textShadow:
                          pocket === null
                            ? "none"
                            : `0 0 16px ${POCKET_COLORS[pocketColor(pocket)]}`,
                        animation: pocket !== null ? "logPop .3s cubic-bezier(.2,1.4,.4,1) both" : undefined,
                      }}
                    >
                      {pocket ?? "··"}
                    </span>
                  )}
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 8,
                      letterSpacing: 2,
                      color: "#5c4f80",
                    }}
                  >
                    EUROPEAN · 37
                  </span>
                </div>

                {/* top marker */}
                <div
                  style={{
                    position: "absolute",
                    left: -7,
                    top: -WHEEL_SIZE / 2 - 12,
                    width: 0,
                    height: 0,
                    borderLeft: "7px solid transparent",
                    borderRight: "7px solid transparent",
                    borderTop: "12px solid #ff8a1f",
                    filter: "drop-shadow(0 0 6px rgba(255,138,31,.8))",
                    zIndex: 3,
                  }}
                />
              </div>

              {/* payout banner */}
              {state === "settled" && lastPayout !== null && (
                <div
                  style={{
                    position: "absolute",
                    left: 18,
                    bottom: 14,
                    padding: "10px 16px",
                    background: "rgba(6,4,13,.92)",
                    border: `2px solid ${lastPayout > 0 ? "#5fe08a" : "#35205c"}`,
                    animation: "bannerIn .3s ease-out both",
                    zIndex: 5,
                  }}
                >
                  <div
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 10,
                      letterSpacing: 3,
                      color: lastPayout > 0 ? "#5fe08a" : "#8878b8",
                    }}
                  >
                    {lastPayout > 0 ? "YOUR WIN" : "HOUSE TAKES IT"}
                  </div>
                  <div
                    style={{
                      fontFamily: "var(--font-body)",
                      fontSize: 30,
                      lineHeight: 1.1,
                      color: lastPayout > 0 ? "#5fe08a" : "#5c4f80",
                      textShadow: lastPayout > 0 ? "0 0 14px rgba(95,224,138,.7)" : "none",
                    }}
                  >
                    {lastPayout > 0 ? `+${lastPayout.toLocaleString()}` : "—"}
                  </div>
                </div>
              )}

              {/* table stakes panel */}
              <div
                style={{
                  position: "absolute",
                  right: 12,
                  top: 12,
                  bottom: 12,
                  width: 196,
                  border: "1px solid #241640",
                  background: "rgba(6,4,13,.75)",
                  padding: "10px 10px",
                  overflow: "hidden",
                  zIndex: 5,
                  display: "flex",
                  flexDirection: "column",
                  gap: 6,
                }}
              >
                <span
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 9,
                    letterSpacing: 3,
                    color: "#5c4f80",
                  }}
                >
                  AT THE TABLE · {stakeRows.length}
                </span>
                <div style={{ display: "flex", flexDirection: "column", gap: 5, overflowY: "auto" }}>
                  {stakeRows.map((s) => (
                    <div
                      key={s.userId}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 8,
                        border: `1px solid ${s.userId === userId ? "#22e8ff" : "#241640"}`,
                        padding: "5px 8px",
                        background: "#0d0619",
                      }}
                    >
                      <Avatar
                        userId={s.userId}
                        displayName={s.displayName ?? `P${s.userId}`}
                        avatarPreset={profiles[s.userId]?.avatarPreset}
                        avatarVersion={profiles[s.userId]?.avatarVersion}
                        size={20}
                      />
                      <span
                        style={{
                          fontFamily: "var(--font-display)",
                          fontSize: 9,
                          color: "#cfc4f2",
                          flex: 1,
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                          whiteSpace: "nowrap",
                        }}
                      >
                        {(s.displayName ?? `P${s.userId}`).toUpperCase()}
                      </span>
                      <span style={{ fontFamily: "var(--font-body)", fontSize: 17, color: "#ff8a1f" }}>
                        {s.credits.toLocaleString()}
                      </span>
                    </div>
                  ))}
                  {stakeRows.length === 0 && (
                    <span style={{ fontFamily: "var(--font-body)", fontSize: 16, color: "#5c4f80" }}>
                      NO CHIPS DOWN YET
                    </span>
                  )}
                </div>
              </div>
            </section>
          </div>

          {/* ── betting board ─────────────────────────────── */}
          <Board
            canBet={canBet}
            chip={chip}
            myBets={myBets}
            winCells={winCells}
            pocket={pocket}
            onPick={placeSpotBet}
          />

          {/* ── chip console ──────────────────────────────── */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: 16,
              border: "2px solid #35205c",
              background: "#0d0619",
              padding: "10px 16px",
            }}
          >
            <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 9,
                  letterSpacing: 3,
                  color: "#5c4f80",
                }}
              >
                CHIP
              </span>
              {CHIP_STEPS.map((v) => (
                <Chip
                  key={v}
                  label={String(v)}
                  color={CHIP_COLOR[v]}
                  size={44}
                  selected={chip === v}
                  onClick={() => {
                    sound.unlock();
                    sound.chipClink();
                    setChip(v);
                  }}
                />
              ))}
            </div>
            <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
              <div style={{ textAlign: "right" }}>
                <div
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 9,
                    letterSpacing: 3,
                    color: "#5c4f80",
                  }}
                >
                  YOUR STAKE
                </div>
                <div
                  style={{
                    fontFamily: "var(--font-body)",
                    fontSize: 26,
                    lineHeight: 1.1,
                    color: myTotal > 0 ? "#ff8a1f" : "#5c4f80",
                    textShadow: myTotal > 0 ? "0 0 12px rgba(255,138,31,.6)" : "none",
                  }}
                >
                  {myTotal.toLocaleString()}
                </div>
              </div>
              <button
                type="button"
                onClick={clearBets}
                disabled={!canBet || myTotal === 0}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 13,
                  letterSpacing: 2,
                  padding: "12px 20px",
                  border: "2px solid #f2643d",
                  background: canBet && myTotal > 0 ? "#2d0a1e" : "#150a2a",
                  color: canBet && myTotal > 0 ? "#f2643d" : "#5c4f80",
                  cursor: canBet && myTotal > 0 ? "pointer" : "default",
                }}
              >
                ⨯ CLEAR
              </button>
            </div>
          </div>
        </div>

        {note && (
          <div
            role="status"
            style={{
              position: "absolute",
              left: "50%",
              top: 56,
              transform: "translateX(-50%)",
              zIndex: 20,
              padding: "8px 16px",
              background: "rgba(6,4,13,.92)",
              border: "2px solid #f2643d",
              fontFamily: "var(--font-display)",
              fontSize: 12,
              letterSpacing: 2,
              color: "#f2643d",
              animation: "notePop .2s ease-out both",
            }}
          >
            {note}
          </div>
        )}
      </div>
    </div>
  );
}

// ── betting board ─────────────────────────────────────────────

function Board({
  canBet,
  chip,
  myBets,
  winCells,
  pocket,
  onPick,
}: {
  canBet: boolean;
  chip: number;
  myBets: Record<string, number>;
  winCells: string[];
  pocket: number | null;
  onPick: (spot: string) => void;
}) {
  const wins = new Set(winCells);

  const cell = (spot: string, extra: React.CSSProperties, content: React.ReactNode, bg: string, fg: string): React.ReactNode => {
    const isWin = pocket !== null && wins.has(spot) && pocket !== undefined;
    const mine = myBets[spot] ?? 0;
    return (
      <div
        key={spot}
        onClick={() => onPick(spot)}
        title={`${spot} · pays ${payoutOdds(SPOTS.find((s) => s.id === spot)!.payout)}`}
        style={{
          position: "relative",
          display: "grid",
          placeItems: "center",
          background: bg,
          border: `1px solid ${isWin ? "#5fe08a" : "#35205c"}`,
          boxShadow: isWin ? "0 0 16px rgba(95,224,138,.5), inset 0 0 0 1px #5fe08a" : "none",
          animation: isWin ? "cellWin 1s steps(1) infinite" : undefined,
          cursor: canBet ? "pointer" : "default",
          opacity: canBet ? 1 : 0.72,
          transition: "border-color 140ms ease-out, box-shadow 140ms ease-out",
          ...extra,
        }}
        onMouseEnter={(e) => {
          if (!canBet) return;
          e.currentTarget.style.borderColor = "#22e8ff";
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.borderColor = isWin ? "#5fe08a" : "#35205c";
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: extra.fontSize ?? 13,
            color: fg,
            textShadow: bg === "#170c2b" ? "none" : "0 1px 0 rgba(0,0,0,.5)",
          }}
        >
          {content}
        </span>
        {mine > 0 && (
          <span style={{ position: "absolute", right: 2, bottom: 2 }}>
            <ChipStack amount={mine} color="orange" chipSize={16} max={3} />
          </span>
        )}
      </div>
    );
  };

  const numBg = (n: number) => POCKET_COLORS[pocketColor(n)];
  const numFg = (n: number) => (pocketColor(n) === "black" ? "#cfc4f2" : "#fff");
  const outsideBg = "#150a2a";
  const outsideFg = "#cfc4f2";

  return (
    <div
      style={{
        alignSelf: "center",
        display: "grid",
        gridTemplateColumns: "58px repeat(12, 58px) 66px",
        gridTemplateRows: "repeat(3, 46px) 42px 42px",
        gap: 3,
        background: "#0d0619",
        border: "2px solid #35205c",
        padding: 10,
        boxShadow: "0 0 30px rgba(157,77,255,.15)",
      }}
    >
      {/* zero spans the three number rows */}
      {cell(
        "n0",
        { gridColumn: "1", gridRow: "1 / span 3", fontSize: 18 },
        "0",
        numBg(0),
        "#5fe08a",
      )}
      {/* numbers: top row 3..36, middle 2..35, bottom 1..34 */}
      {BOARD_ROWS.map((row, r) =>
        row.map((n, c) =>
          cell(
            `n${n}`,
            { gridColumn: String(c + 2), gridRow: String(r + 1) },
            String(n),
            numBg(n),
            numFg(n),
          ),
        ),
      )}
      {/* column bets down the right edge */}
      {cell("c3", { gridColumn: "14", gridRow: "1" }, "2:1", outsideBg, outsideFg)}
      {cell("c2", { gridColumn: "14", gridRow: "2" }, "2:1", outsideBg, outsideFg)}
      {cell("c1", { gridColumn: "14", gridRow: "3" }, "2:1", outsideBg, outsideFg)}
      {/* dozens */}
      {cell("d1", { gridColumn: "2 / span 4", gridRow: "4" }, "1ST 12", outsideBg, outsideFg)}
      {cell("d2", { gridColumn: "6 / span 4", gridRow: "4" }, "2ND 12", outsideBg, outsideFg)}
      {cell("d3", { gridColumn: "10 / span 4", gridRow: "4" }, "3RD 12", outsideBg, outsideFg)}
      {/* even-money */}
      {cell("low", { gridColumn: "2 / span 2", gridRow: "5" }, "1-18", outsideBg, outsideFg)}
      {cell("even", { gridColumn: "4 / span 2", gridRow: "5" }, "EVEN", outsideBg, outsideFg)}
      {cell("red", { gridColumn: "6 / span 2", gridRow: "5" }, "RED", outsideBg, "#f2643d")}
      {cell("black", { gridColumn: "8 / span 2", gridRow: "5" }, "BLACK", outsideBg, outsideFg)}
      {cell("odd", { gridColumn: "10 / span 2", gridRow: "5" }, "ODD", outsideBg, outsideFg)}
      {cell("high", { gridColumn: "12 / span 2", gridRow: "5" }, "19-36", outsideBg, outsideFg)}
    </div>
  );
}
