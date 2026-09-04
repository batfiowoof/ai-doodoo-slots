"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useQueryClient } from "@tanstack/react-query";
import { useSession } from "@/lib/api";
import type { Me, ProfileUpdatedEvent } from "@/lib/types";
import { sound } from "@/lib/sound";
import { Avatar } from "@/components/Avatar";
import { CrashScene, type ScenePhase } from "@/lib/crashScene";

// The crash room: flight scene, bet panel, crew manifest, flight log. The
// server decides everything — the client renders whatever arrives and never
// predicts a balance or a multiplier. (`?demo=` dev modes are the one
// exception: they fake the message flow locally for visual testing.)
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
    msLeft?: number; // ms remaining in the current phase (server clock)
    recentCrashes: number[];
    stakes: Stake[];
  } | null;
}

// Resolved lazily: module scope runs during SSR where `location` is absent.
const wsUrl = () =>
  process.env.NEXT_PUBLIC_WS_URL ??
  (typeof location !== "undefined" && location.port === "3000"
    ? "ws://localhost:8082/api/v1/ws"
    : `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/ws`);

const BET_STEPS = [5, 10, 25, 50, 100];
const BETTING_MS = 7000; // mirrors the server window; visual countdown only

// Glow colour tiers as the multiplier climbs.
const MILESTONES = [2, 5, 10, 25, 50, 100];
const TIER_BOOSTS = [4, 8]; // sprite throttle-stage changes → whoosh

const tierColor = (m: number): string =>
  m >= 25 ? "#f2643d" : m >= 10 ? "#ff2d95" : m >= 5 ? "#ffb15c" : m >= 2 ? "#5fe08a" : "#22e8ff";

const scenePhaseOf = (s: RoundState): ScenePhase =>
  s === "betting_open" ? "betting" : s === "locked" ? "locked" : s === "running" ? "running" : "settled";

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
  const [lastCashoutAt, setLastCashoutAt] = useState<number | null>(null);
  const [betAmount, setBetAmount] = useState(10);
  const [autoTarget, setAutoTarget] = useState("2.00");
  const [myBet, setMyBet] = useState<{ credits: number; cashed: boolean } | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [tMinus, setTMinus] = useState(7);
  const [pulseKey, setPulseKey] = useState(0);
  const [demoBalance, setDemoBalance] = useState<number | null>(null);
  // Live profile edits from other players overlay manifest names.
  const [profiles, setProfiles] = useState<Record<number, ProfileUpdatedEvent>>({});

  const wsRef = useRef<WebSocket | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const sceneRef = useRef<CrashScene | null>(null);
  const lastMRef = useRef(1);
  const betOpenAtRef = useRef(0);
  const beepedRef = useRef(99);
  const myBetRef = useRef<{ credits: number; cashed: boolean } | null>(null);
  // Bumped on every (re)connect and cleanup so async events from a superseded
  // socket never clobber the live one's state.
  const sockGenRef = useRef(0);
  const userId = session.data?.user.id;

  useEffect(() => {
    myBetRef.current = myBet;
  }, [myBet]);

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
    msLeft?: number;
    code?: string;
    slug?: string;
  }

  const showNote = useCallback((text: string) => {
    setNote(text);
    window.setTimeout(() => setNote(null), 2600);
  }, []);

  // Scene engine lifecycle.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const scene = new CrashScene(canvas);
    sceneRef.current = scene;
    scene.start();
    return () => {
      scene.destroy();
      sceneRef.current = null;
    };
  }, []);

  const applyTick = useCallback((m: number) => {
    setMultiplier(m);
    sceneRef.current?.setMultiplier(m);
    sound.enginePitch(m);
    const prev = lastMRef.current;
    for (const ms of MILESTONES) {
      if (prev < ms && m >= ms) {
        sceneRef.current?.milestone(ms);
        sound.milestone(ms);
        setPulseKey((k) => k + 1);
      }
    }
    for (const t of TIER_BOOSTS) {
      if (prev < t && m >= t) sound.boost();
    }
    lastMRef.current = m;
  }, []);

  const applyPhase = useCallback(
    (next: RoundState, msLeft?: number) => {
      setState(next);
      sceneRef.current?.setPhase(scenePhaseOf(next));
      if (next === "betting_open") {
        setCrashed(false);
        setMyBet(null);
        setLastPayout(null);
        setLastCashoutAt(null);
        lastMRef.current = 1;
        beepedRef.current = 99;
        // Anchor the countdown to the server's remaining time when it
        // provides one (refresh mid-window, transition events), otherwise
        // assume a full window.
        const left = msLeft !== undefined ? Math.max(0, msLeft) : BETTING_MS;
        betOpenAtRef.current = performance.now() - (BETTING_MS - left);
        setTMinus(Math.ceil(left / 1000));
        sound.engineStop();
        void qc.invalidateQueries({ queryKey: ["me"] });
      } else if (next === "locked") {
        sound.launch();
      } else if (next === "running") {
        sound.engineStart();
        lastMRef.current = 1;
      } else if (next === "settled") {
        sound.engineStop();
      }
    },
    [qc],
  );

  const applyCrash = useCallback(
    (m: number) => {
      setMultiplier(m);
      setCrashed(true);
      lastMRef.current = m;
      sceneRef.current?.setCrash(m);
      sound.engineStop();
      sound.explosion();
      void qc.invalidateQueries({ queryKey: ["me"] });
    },
    [qc],
  );

  // T-minus countdown while bets are open. Purely visual — the server
  // decides when betting actually locks. Frozen while disconnected so a
  // dead link never shows a fake free-running countdown.
  useEffect(() => {
    if (state !== "betting_open" || !connected) return;
    const id = window.setInterval(() => {
      const left = Math.max(0, BETTING_MS - (performance.now() - betOpenAtRef.current));
      const secs = Math.ceil(left / 1000);
      setTMinus(secs);
      if (secs <= 3 && secs >= 1 && beepedRef.current > secs) {
        beepedRef.current = secs;
        sound.countdownBeep(secs === 1);
      }
    }, 100);
    return () => window.clearInterval(id);
  }, [state, connected]);

  // Socket lifecycle: connect once, rejoin the room on (re)connect — or run
  // a local demo loop in dev when ?demo= is on the URL.
  useEffect(() => {
    const demoParam =
      process.env.NODE_ENV !== "production" && typeof location !== "undefined"
        ? new URLSearchParams(location.search).get("demo")
        : null;

    if (demoParam) {
      let timer: number | undefined;
      let t = 0;
      const mode =
        demoParam === "loop" || demoParam === "betting" || demoParam === "locked" || demoParam === "running" || demoParam === "crashed"
          ? demoParam
          : "loop";

      const startDemo = () => {
        setConnected(true);
        setDemoBalance(1000);
        setSnapshot({
          room: { slug, name: "NOVA OUTPOST-1", minBet: 1, maxBet: 5000, playerCount: 4 },
          round: null,
        });
        setHistory([2.31, 1.08, 4.5, 1.92, 8.13, 1.01, 3.07]);
        setStakes([
          { userId: 1, displayName: "MAVERICK", credits: 50 },
          { userId: 2, displayName: "NOVA_9", credits: 25, cashedAt: 1.87 },
          { userId: 3, displayName: "PIP", credits: 100 },
          { userId: 4, displayName: "GHOST", credits: 10, cashedAt: 3.4 },
        ]);
        let demoCrashed = false;
        applyPhase("betting_open");
        if (mode === "crashed") {
          applyPhase("running");
          applyCrash(4.87);
          demoCrashed = true;
        }
        if (mode === "locked") applyPhase("locked");

        const tick = () => {
          t += 0.1;
          applyTick(Math.exp(0.12 * t));
          if (mode === "loop" && !demoCrashed && t > 2 && Math.random() < 0.012) {
            applyCrash(Math.exp(0.12 * t));
            demoCrashed = true;
            timer = window.setTimeout(() => {
              t = 0;
              demoCrashed = false;
              applyPhase("betting_open");
              timer = window.setTimeout(() => {
                applyPhase("locked");
                timer = window.setTimeout(() => {
                  applyPhase("running");
                  timer = window.setTimeout(tick, 100);
                }, 1000);
              }, BETTING_MS + 1000);
            }, 4000);
            return;
          }
          timer = window.setTimeout(tick, 100);
        };
        if (mode === "running") {
          applyPhase("locked");
          timer = window.setTimeout(() => {
            applyPhase("running");
            tick();
          }, 100);
        } else if (mode === "loop") {
          timer = window.setTimeout(() => {
            applyPhase("locked");
            timer = window.setTimeout(() => {
              applyPhase("running");
              tick();
            }, 1000);
          }, BETTING_MS + 1000);
        } else if (mode === "crashed") {
          applyPhase("settled");
        }
      };

      const kick = window.setTimeout(startDemo, 0);
      return () => {
        window.clearTimeout(kick);
        if (timer) window.clearTimeout(timer);
        sound.engineStop();
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
        // Full state snapshot on join AND reconnect.
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
            const snap = msg.payload as RoomSnapshot;
            setSnapshot(snap);
            if (snap.round) {
              setRoundId(snap.round.roundId);
              const s = snap.round.state;
              setStakes(snap.round.stakes ?? []);
              setHistory(snap.round.recentCrashes ?? []);
              setMultiplier(snap.round.multiplier || 1);
              lastMRef.current = snap.round.multiplier || 1;
              sceneRef.current?.setMultiplier(snap.round.multiplier || 1);
              setCrashed(s === "settled");
              applyPhase(s, snap.round.msLeft);
              if (s === "settled") {
                // Place the wreck at the restored crash point.
                sceneRef.current?.setCrash(snap.round.multiplier || 1);
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
          case "round_tick":
            applyTick(p.multiplier ?? 1);
            break;
          case "round_result":
            applyCrash(p.crashMultiplier ?? 1);
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
              const prev = myBetRef.current;
              if (prev && prev.credits > 0) {
                setLastCashoutAt(Math.round((p.payoutCredits / prev.credits) * 100) / 100);
              }
              setLastPayout(p.payoutCredits);
              setMyBet((old) => (old ? { ...old, cashed: true } : old));
              sound.cashout();
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
      sockGenRef.current++; // invalidate any in-flight socket events
      if (retry) window.clearTimeout(retry);
      wsRef.current?.close();
      sound.engineStop();
    };
  }, [slug, userId, qc, showNote, applyPhase, applyTick, applyCrash]);

  // Keep the auto-eject marker on the altitude gauge in sync.
  useEffect(() => {
    const t = parseFloat(autoTarget);
    sceneRef.current?.setAutoTarget(Number.isFinite(t) ? t : null);
  }, [autoTarget]);

  const send = useCallback((type: string, payload: unknown) => {
    wsRef.current?.send(JSON.stringify({ type, payload }));
  }, []);

  const placeBet = () => {
    const target = Math.max(1.01, parseFloat(autoTarget) || 2);
    sound.unlock();
    sound.click();
    if (demoBalance !== null) {
      setDemoBalance((b) => (b ?? 0) - betAmount);
      setMyBet({ credits: betAmount, cashed: false });
      sound.bell();
      return;
    }
    send("place_bet", {
      credits: betAmount,
      autoCashout: target,
      idempotencyKey: crypto.randomUUID(),
    });
  };

  const cashOut = () => {
    sound.unlock();
    sound.click();
    if (demoBalance !== null && myBet) {
      const at = Math.round(multiplier * 100) / 100;
      setMyBet({ ...myBet, cashed: true });
      setLastCashoutAt(at);
      setLastPayout(Math.floor(myBet.credits * at));
      setDemoBalance((b) => (b ?? 0) + Math.floor(myBet.credits * at));
      sound.cashout();
      return;
    }
    send("cash_out", {});
  };

  const balance = demoBalance ?? session.data?.balanceCredits;
  const canBet = state === "betting_open" && !myBet && connected;
  const canCash = state === "running" && myBet !== null && !myBet.cashed && !crashed;
  const running = state === "running";
  const myStake = stakes.find((s) => s.userId === userId);

  const roomName = snapshot?.room.name?.toUpperCase() ?? slug.toUpperCase();

  return (
    <div style={{ position: "fixed", inset: 0, overflow: "hidden", background: "#06040d" }}>
      <div
        style={{
          position: "relative",
          zIndex: 5,
          height: "100%",
          display: "flex",
          flexDirection: "column",
          padding: "16px 26px 18px",
          gap: 10,
        }}
      >
        {/* ── console bar ───────────────────────────────── */}
        <header style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
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

        {/* ── flight log ────────────────────────────────── */}
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
            FLIGHT LOG
          </span>
          {history.slice(0, 12).map((m, i) => (
            <span
              key={`${i}-${m}`}
              style={{
                fontFamily: "var(--font-body)",
                fontSize: 16,
                lineHeight: 1,
                padding: "3px 8px",
                border: `1px solid ${m >= 2 ? "#5fe08a" : m >= 1.3 ? "#6b5f9e" : "#8c3b2e"}`,
                color: m >= 2 ? "#5fe08a" : m >= 1.3 ? "#8878b8" : "#f2643d",
                background: "#0d0619",
                animation: i === 0 ? "logPop .25s ease-out both" : undefined,
              }}
            >
              {m.toFixed(2)}×
            </span>
          ))}
        </div>

        {/* ── flight scene ──────────────────────────────── */}
        <section
          style={{
            position: "relative",
            flex: 1,
            border: "2px solid #35205c",
            background: "#0d0619",
            overflow: "hidden",
            animation: running && !crashed ? "hullWarn 1.1s ease-in-out infinite" : undefined,
          }}
        >
          <canvas
            ref={canvasRef}
            width={1280}
            height={640}
            style={{ width: "100%", height: "100%", display: "block", imageRendering: "pixelated" }}
          />
          <div className="crt" style={{ position: "absolute", inset: 0, pointerEvents: "none" }} />

          {/* readouts */}
          <div
            style={{
              position: "absolute",
              left: 14,
              bottom: 66,
              fontFamily: "var(--font-body)",
              fontSize: 15,
              color: "#5c4f80",
              pointerEvents: "none",
              lineHeight: 1.3,
            }}
          >
            VEL {Math.round((multiplier - 1) * 124)}c
            <br />
            M-{Math.max(1, Math.round(multiplier * 10) / 10).toFixed(1)} · THRUST{" "}
            {state === "running"
              ? multiplier >= 8
                ? "OVERDRIVE"
                : multiplier >= 4
                  ? "STAGE III"
                  : multiplier >= 2
                    ? "STAGE II"
                    : "STAGE I"
              : "IDLE"}
          </div>

          {/* centre overlay */}
          <div
            style={{
              position: "absolute",
              inset: 0,
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              pointerEvents: "none",
              paddingBottom: 58,
            }}
          >
            {state === "betting_open" && (
              <div style={{ textAlign: "center", animation: "bannerIn .25s ease-out both" }}>
                <div
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 13,
                    letterSpacing: 6,
                    color: "#8878b8",
                  }}
                >
                  T-MINUS
                </div>
                <div
                  key={tMinus}
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 58,
                    lineHeight: 1.1,
                    color: tMinus <= 3 ? "#ff8a1f" : "#ece6ff",
                    textShadow:
                      tMinus <= 3 ? "0 0 22px rgba(255,138,31,.9)" : "0 0 14px rgba(34,232,255,.5)",
                    animation: tMinus <= 3 ? "countdownBlink .5s step-end infinite" : undefined,
                  }}
                >
                  {tMinus}
                </div>
                <div
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 12,
                    letterSpacing: 4,
                    color: "#5fe08a",
                  }}
                >
                  PLACE YOUR BETS
                </div>
              </div>
            )}

            {state === "locked" && (
              <div
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 34,
                  letterSpacing: 6,
                  color: "#ff8a1f",
                  textShadow: "0 0 24px rgba(255,138,31,.95)",
                  animation: "lockStrobe .16s step-end infinite",
                }}
              >
                IGNITION
              </div>
            )}

            {state === "running" && !crashed && (
              <span
                key={pulseKey}
                data-testid="crash-multiplier"
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 62,
                  lineHeight: 1,
                  color: "#fff",
                  textShadow: `0 0 10px ${tierColor(multiplier)}, 0 0 34px ${tierColor(multiplier)}aa`,
                  animation: "milestonePop .35s ease-out both",
                }}
              >
                {multiplier.toFixed(2)}×
              </span>
            )}

            {crashed && (
              <div style={{ textAlign: "center", animation: "bannerIn .18s ease-out both" }}>
                <div
                  data-testid="crash-multiplier"
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 62,
                    lineHeight: 1,
                    color: "#f2643d",
                    textShadow: "0 0 12px rgba(242,100,61,.95), 0 0 44px rgba(242,100,61,.6)",
                    animation: "crashGlitch 1.4s steps(1) infinite",
                  }}
                >
                  {multiplier.toFixed(2)}×
                </div>
                <div
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 15,
                    letterSpacing: 5,
                    color: "#f2643d",
                    marginTop: 6,
                  }}
                >
                  CRASHED
                </div>
              </div>
            )}

            {myBet && !crashed && (
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 12,
                  letterSpacing: 2,
                  color: myBet.cashed ? "#5fe08a" : "#ff8a1f",
                  marginTop: 14,
                }}
              >
                {myBet.cashed
                  ? `✓ EJECTED${lastCashoutAt ? ` @ ${lastCashoutAt.toFixed(2)}×` : ""}`
                  : `▲ ABOARD — ${myBet.credits.toLocaleString()} CR`}
              </span>
            )}
          </div>

          {/* eject banner after a successful cashout */}
          {lastPayout !== null && (crashed || myBet?.cashed) && (
            <div
              style={{
                position: "absolute",
                right: 16,
                top: 14,
                padding: "8px 14px",
                border: "2px solid #5fe08a",
                background: "rgba(6,4,13,.88)",
                boxShadow: "0 0 18px rgba(95,224,138,.4)",
                animation: "bannerIn .22s ease-out both",
                pointerEvents: "none",
                textAlign: "right",
              }}
            >
              <div
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 11,
                  letterSpacing: 2,
                  color: "#5fe08a",
                }}
              >
                EJECTED{lastCashoutAt ? ` @ ${lastCashoutAt.toFixed(2)}×` : ""}
              </div>
              <div
                style={{
                  fontFamily: "var(--font-body)",
                  fontSize: 24,
                  lineHeight: 1.1,
                  color: "#5fe08a",
                  textShadow: "0 0 10px rgba(95,224,138,.7)",
                }}
              >
                +{lastPayout.toLocaleString()}
              </div>
            </div>
          )}
        </section>

        {/* ── mission control + crew manifest ───────────── */}
        <div style={{ display: "flex", gap: 16, minHeight: 148 }}>
          <div
            style={{
              width: 430,
              padding: "12px 14px",
              border: "2px solid #35205c",
              background: "#170c2b",
              display: "flex",
              flexDirection: "column",
              gap: 9,
            }}
          >
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span style={{ width: 8, height: 8, background: "#ff8a1f", boxShadow: "0 0 8px #ff8a1f" }} />
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 10,
                  letterSpacing: 3,
                  color: "#8878b8",
                }}
              >
                MISSION CONTROL
              </span>
              <span style={{ fontFamily: "var(--font-body)", fontSize: 14, color: "#5c4f80" }}>
                BETS {snapshot?.room.minBet ?? 1}–{(snapshot?.room.maxBet ?? 5000).toLocaleString()}
              </span>
            </div>

            <div style={{ display: "flex", gap: 6 }}>
              {BET_STEPS.map((v) => {
                const on = betAmount === v;
                return (
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
                      fontSize: 19,
                      lineHeight: 1,
                      padding: "6px 0",
                      border: `1px solid ${on ? "#ff8a1f" : "#6b4a1c"}`,
                      background: on ? "#2a1406" : "#06040d",
                      color: on ? "#ff8a1f" : "#8878b8",
                      cursor: "pointer",
                      boxShadow: on ? "0 0 12px rgba(255,138,31,.35)" : "none",
                    }}
                  >
                    {v}
                  </button>
                );
              })}
            </div>

            <label style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 9,
                  letterSpacing: 2,
                  color: "#8878b8",
                }}
              >
                AUTO EJECT
              </span>
              <span
                style={{
                  fontFamily: "var(--font-body)",
                  fontSize: 18,
                  color: "#5fe08a",
                  background: "#06040d",
                  border: "1px solid #35205c",
                  padding: "1px 8px",
                }}
              >
                ×
              </span>
              <input
                value={autoTarget}
                onChange={(e) => setAutoTarget(e.target.value.replace(/[^0-9.]/g, ""))}
                style={{
                  width: 84,
                  fontFamily: "var(--font-body)",
                  fontSize: 19,
                  lineHeight: 1,
                  background: "#06040d",
                  border: "1px solid #35205c",
                  color: "#5fe08a",
                  padding: "3px 8px",
                }}
              />
            </label>

            {canCash ? (
              <button
                type="button"
                data-testid="eject-button"
                onClick={cashOut}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 15,
                  letterSpacing: 2,
                  padding: "12px 0",
                  border: "2px solid #5fe08a",
                  background: "#0b2a33",
                  color: "#5fe08a",
                  cursor: "pointer",
                  animation: "ejectPulse .8s ease-in-out infinite",
                }}
              >
                ⟵ EJECT {((myBet?.credits ?? 0) * multiplier).toFixed(0)} CR
              </button>
            ) : (
              <button
                type="button"
                data-testid="bet-button"
                onClick={placeBet}
                disabled={!canBet || (balance ?? 0) < betAmount}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 15,
                  letterSpacing: 2,
                  padding: "12px 0",
                  border: `2px solid ${canBet ? "#ff8a1f" : "#35205c"}`,
                  background: canBet ? "#2a1406" : "#0d0619",
                  color: canBet ? "#ff8a1f" : "#5c4f80",
                  cursor: canBet ? "pointer" : "not-allowed",
                  opacity: canBet ? 1 : 0.6,
                  animation: canBet ? "launchReady 1.2s ease-in-out infinite" : undefined,
                }}
              >
                {myBet
                  ? "✓ BET PLACED — STANDING BY"
                  : state === "betting_open"
                    ? `▲ LAUNCH ${betAmount} CR`
                    : "STANDBY — NEXT WINDOW SOON"}
              </button>
            )}
            {note && (
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 10,
                  letterSpacing: 1,
                  color: "#f2643d",
                  animation: "notePop .2s ease-out both",
                }}
              >
                ! {note}
              </span>
            )}
          </div>

          <div
            style={{
              flex: 1,
              padding: "12px 14px",
              border: "2px solid #35205c",
              background: "#170c2b",
              overflowY: "auto",
            }}
          >
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
              <span style={{ width: 8, height: 8, background: "#22e8ff", boxShadow: "0 0 8px #22e8ff" }} />
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 10,
                  letterSpacing: 3,
                  color: "#8878b8",
                }}
              >
                CREW MANIFEST — {stakes.length}
              </span>
            </div>
            {stakes.map((s) => (
              <div
                key={s.userId}
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  fontFamily: "var(--font-body)",
                  fontSize: 19,
                  lineHeight: 1.35,
                  color: s.userId === userId ? "#ff8a1f" : "#cfc4f2",
                  padding: "0 6px",
                  background: s.cashedAt ? "rgba(95,224,138,.08)" : "transparent",
                  animation: s.cashedAt ? "rowCash .8s ease-out both" : undefined,
                }}
              >
                <span style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <Avatar
                    userId={s.userId}
                    displayName={profiles[s.userId]?.displayName ?? s.displayName ?? `PLAYER ${s.userId}`}
                    avatarPreset={profiles[s.userId]?.avatarPreset}
                    avatarVersion={profiles[s.userId]?.avatarVersion}
                    size={22}
                    ring={s.userId === userId ? "#ff8a1f" : "#35205c"}
                  />
                  {s.cashedAt ? (
                    <span style={{ color: "#5fe08a" }}>✓ </span>
                  ) : (
                    <span
                      style={{
                        color: "#22e8ff",
                        animation: "ridingDot 1s step-end infinite",
                      }}
                    >
                      ▸{" "}
                    </span>
                  )}
                  {profiles[s.userId]?.displayName ?? s.displayName ?? `PLAYER ${s.userId}`}
                  {s.userId === userId ? " (YOU)" : ""}
                </span>
                <span>
                  {s.credits.toLocaleString()}
                  {s.cashedAt ? (
                    <span style={{ color: "#5fe08a" }}> CR · {s.cashedAt.toFixed(2)}×</span>
                  ) : (
                    <span style={{ color: "#6b5f9e" }}> CR · riding</span>
                  )}
                </span>
              </div>
            ))}
            {stakes.length === 0 && (
              <span style={{ fontFamily: "var(--font-body)", fontSize: 17, color: "#5c4f80" }}>
                {state === "betting_open" ? "NO CREW ABOARD YET" : "CREW LOGS SEALED UNTIL NEXT ROUND"}
              </span>
            )}
            {myStake && !myStake.cashedAt && state === "running" && (
              <div style={{ fontFamily: "var(--font-body)", fontSize: 15, color: "#ff8a1f", marginTop: 6 }}>
                YOUR SHARE: {((myBet?.credits ?? 0) * multiplier).toFixed(0)} CR AND CLIMBING
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
