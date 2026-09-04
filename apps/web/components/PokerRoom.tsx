"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useQueryClient } from "@tanstack/react-query";
import Backdrop from "@/components/Backdrop";
import Chip, { ChipStack } from "@/components/Chip";
import PlayingCard from "@/components/PlayingCard";
import { Avatar } from "@/components/Avatar";
import { useSession } from "@/lib/api";
import type { Me, ProfileUpdatedEvent } from "@/lib/types";
import { sound } from "@/lib/sound";

// The poker room: a full-width oval felt seen from your seat. The server
// decides everything — every card, pot, and payout arrives from the table
// runner; the client renders and never predicts an outcome. Your own hole
// cards are held large at the near edge, first-person style.

interface SeatView {
  seatNo: number;
  userId: number;
  displayName: string;
  state: string;
  stack: number;
  bet: number;
  totalBet: number;
  folded: boolean;
  allIn: boolean;
  lastAction: string;
  cards: string;
}

interface ResultView {
  userId: number;
  displayName: string;
  cards: string;
  handName?: string;
  winAmount: number;
  contributed: number;
  net: number;
}

interface TableView {
  gameId: string;
  phase: string;
  handNo: number;
  button: number;
  sb: number;
  bb: number;
  board: string[];
  pot: number;
  currentBet: number;
  minRaise: number;
  toAct: number;
  seats: SeatView[];
  results?: ResultView[];
  legal?: { actions?: string[]; callAmount?: number; minRaiseTo?: number; maxRaiseTo?: number };
}

interface HandRecord {
  handNo: number;
  board: string[];
  seats: Array<{
    seatNo: number;
    userId: number;
    displayName: string;
    cards: string;
    folded: boolean;
    contributed: number;
    winAmount: number;
    stackAfter: number;
  }>;
  results: ResultView[];
}

interface RoomInfo {
  slug: string;
  name: string;
  minBet: number;
  maxBet: number;
  capacity: number;
}

// Resolved lazily: module scope runs during SSR where `location` is absent.
const wsUrl = () =>
  process.env.NEXT_PUBLIC_WS_URL ??
  (typeof location !== "undefined" && location.port === "3000"
    ? "ws://localhost:8082/api/v1/ws"
    : `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/ws`);

const PHASE_LABELS: Record<string, string> = {
  waiting: "WAITING FOR PLAYERS",
  preflop: "PREFLOP",
  flop: "FLOP",
  turn: "TURN",
  river: "RIVER",
  showdown: "SHOWDOWN",
};

// Table geometry; the whole stage scales to fit the viewport.
const TABLE_W = 1300;
const TABLE_H = 600;

function chipColorFor(amount: number): "cyan" | "green" | "orange" | "pink" {
  if (amount < 25) return "cyan";
  if (amount < 100) return "green";
  if (amount < 500) return "orange";
  return "pink";
}

export default function PokerRoom({ slug, room }: { slug: string; room: RoomInfo }) {
  const session = useSession();
  const qc = useQueryClient();

  const [connected, setConnected] = useState(false);
  const [view, setView] = useState<TableView | null>(null);
  // Live profile edits (renames, avatars) overlay the names snapshotted into
  // seat state at buy-in; the socket carries them to everyone present.
  const [profiles, setProfiles] = useState<Record<number, ProfileUpdatedEvent>>({});
  const [myCards, setMyCards] = useState<string[]>([]);
  const [showdown, setShowdown] = useState<HandRecord | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [buyOpen, setBuyOpen] = useState(false);
  const [buyAmount, setBuyAmount] = useState(String(room.minBet * 20));
  const [busy, setBusy] = useState(false);
  const [scale, setScale] = useState(1);

  const wsRef = useRef<WebSocket | null>(null);
  const userId = session.data?.user.id;

  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth;
      const h = window.innerHeight;
      setScale(Math.min(1, (h - 220) / (TABLE_H + 60), (w - 50) / (TABLE_W + 40)));
    };
    onResize();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const showNote = useCallback((text: string) => {
    setNote(text);
    window.setTimeout(() => setNote(null), 2600);
  }, []);

  const send = useCallback((type: string, payload: unknown) => {
    wsRef.current?.send(JSON.stringify({ type, payload }));
  }, []);

  // Merge a personalized view: remember own hole cards (masked broadcasts
  // hide them between pulls).
  const mergePersonal = useCallback((v: TableView, uid: number | undefined) => {
    setView(v);
    if (uid === undefined) return;
    const mine = v.seats.find((s) => s.userId === uid);
    if (mine && mine.cards) setMyCards([mine.cards.slice(0, 2), mine.cards.slice(2, 4)]);
    else if (v.phase === "waiting" || v.phase === "showdown") setMyCards([]);
  }, []);

  // Socket lifecycle: connect, join, pull personalized state on every
  // (re)connect and at each new hand.
  useEffect(() => {
    let closed = false;
    let retry: number | undefined;

    const connect = () => {
      if (closed) return;
      const ws = new WebSocket(wsUrl());
      wsRef.current = ws;

      ws.onopen = () => {
        if (wsRef.current !== ws) return; // stale socket from a previous effect run
        setConnected(true);
        send("join_room", { slug });
        setTimeout(() => send("game_action", { action: "state" }), 200);
      };
      ws.onclose = () => {
        if (wsRef.current !== ws) return;
        setConnected(false);
        if (!closed) retry = window.setTimeout(connect, 1000);
      };
      ws.onmessage = (ev) => {
        const msg = JSON.parse(ev.data) as { type: string; payload?: unknown };
        const p = msg.payload as Record<string, unknown> | undefined;
        switch (msg.type) {
          case "room_snapshot": {
            const snap = p as { round?: TableView | null };
            if (snap?.round) setView(snap.round);
            break;
          }
          case "table_state": {
            const v = p as unknown as TableView;
            if (v && typeof v === "object" && "seats" in v) {
              setView(v);
              const mine = v.seats.find((s) => s.userId === userId);
              if (mine && mine.cards) {
                setMyCards([mine.cards.slice(0, 2), mine.cards.slice(2, 4)]);
              } else if (v.phase === "waiting" || v.phase === "showdown") {
                setMyCards([]);
              }
            }
            break;
          }
          case "hand_started": {
            setShowdown(null);
            sound.shuffle();
            // Fresh hole cards come via a personalized pull.
            send("game_action", { action: "state" });
            break;
          }
          case "hand_result": {
            const rec = (p as { record?: HandRecord })?.record;
            if (rec) {
              setShowdown(rec);
              sound.flipCard(0.25);
              const mine = (rec?.results ?? []).find((r) => r.userId === userId);
              if (mine && mine.net > 0) {
                sound.bell(0.6);
                sound.jackpot(1);
              } else if (mine && mine.net < 0) {
                sound.error();
              }
            }
            break;
          }
          case "game_ack": {
            const v = p as unknown as TableView;
            if (v && typeof v === "object" && "seats" in v) {
              mergePersonal(v, userId);
            } else if (v && typeof v === "object" && "balanceCredits" in v) {
              const bal = (p as { balanceCredits?: number }).balanceCredits;
              if (bal !== undefined) {
                qc.setQueryData<Me>(["me"], (old) =>
                  old ? { ...old, balanceCredits: bal } : old,
                );
              }
              if ((p as { seated?: boolean }).seated) {
                setBuyOpen(false);
                sound.chipToss(4);
                sound.bell(0.2);
              }
            }
            setBusy(false);
            break;
          }
          case "cash_out": {
            const bal = (p as { balanceCredits?: number }).balanceCredits;
            if (bal !== undefined) {
              qc.setQueryData<Me>(["me"], (old) =>
                old ? { ...old, balanceCredits: bal } : old,
              );
            }
            sound.chipToss(3);
            void qc.invalidateQueries({ queryKey: ["me"] });
            break;
          }
          case "error": {
            const code = ((p as { code?: string })?.code ?? "error").toUpperCase().replaceAll("_", " ");
            showNote(code);
            sound.error();
            setBusy(false);
            break;
          }
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
      if (retry) window.clearTimeout(retry);
      wsRef.current?.close();
    };
  }, [slug, userId, qc, showNote, send, mergePersonal]);

  const mySeat = view?.seats.find((s) => s.userId === userId);
  const seated = mySeat !== undefined;
  const myTurn = view?.toAct !== undefined && view.toAct >= 0 && mySeat?.seatNo === view.toAct;
  const legal = view?.legal?.actions ?? [];
  const callAmount = view?.legal?.callAmount;
  const minRaiseTo = view?.legal?.minRaiseTo;
  const maxRaiseTo = view?.legal?.maxRaiseTo;

  // Reset the raise input whenever a new betting range opens (state
  // adjusted during render — the React pattern for reacting to prop/state
  // changes without effects).
  const [raiseTo, setRaiseTo] = useState<string>("");
  const [lastMinRaise, setLastMinRaise] = useState<number | undefined>(undefined);
  if (minRaiseTo !== undefined && minRaiseTo !== lastMinRaise) {
    setLastMinRaise(minRaiseTo);
    setRaiseTo(String(minRaiseTo));
  }

  const act = (action: string, amount?: number) => {
    if (busy) return;
    sound.unlock();
    if (action === "fold") sound.fold();
    else sound.chipClink();
    setBusy(true);
    send("game_action", { action, ...(amount !== undefined ? { amount } : {}) });
  };

  const buyIn = () => {
    const amount = parseInt(buyAmount, 10);
    if (!Number.isFinite(amount) || amount <= 0) return;
    sound.unlock();
    sound.click();
    sound.chipClink();
    setBusy(true);
    send("game_action", {
      action: "buy_in",
      amount,
      idempotencyKey: crypto.randomUUID(),
    });
  };

  const leave = () => {
    sound.click();
    send("game_action", { action: "leave" });
  };

  // Your turn: pulse the bar and play the alert once per turn.
  const prevTurn = useRef(false);
  useEffect(() => {
    if (myTurn && !prevTurn.current) sound.turnAlert();
    prevTurn.current = myTurn;
  }, [myTurn]);

  // Chips hitting the pot whenever the total climbs.
  const prevPot = useRef(0);
  useEffect(() => {
    const pot = view?.pot ?? 0;
    if (pot > prevPot.current) sound.chipToss(2);
    prevPot.current = pot;
  }, [view?.pot]);

  // Board deal stagger: only cards beyond the previously seen count animate.
  const boardCards = showdown?.board ?? view?.board ?? [];
  const prevBoardLen = useRef(0);
  const freshFrom = prevBoardLen.current;
  useEffect(() => {
    prevBoardLen.current = boardCards.length;
  }, [boardCards.length]);

  const balance = session.data?.balanceCredits;
  const phase = view?.phase ?? "waiting";

  const opponents = (view?.seats ?? []).filter((s) => s.userId !== userId);

  const raiseQuick = (kind: "min" | "half" | "pot" | "allin") => {
    const pot = view?.pot ?? 0;
    const call = callAmount ?? 0;
    let v: number;
    if (kind === "min") v = minRaiseTo ?? 0;
    else if (kind === "half") v = call + Math.round((pot + call) / 2);
    else if (kind === "pot") v = call + pot + call;
    else v = maxRaiseTo ?? 0;
    if (minRaiseTo !== undefined) v = Math.max(v, minRaiseTo);
    if (maxRaiseTo !== undefined) v = Math.min(v, maxRaiseTo);
    sound.chipClink();
    setRaiseTo(String(v));
  };

  return (
    <div style={{ position: "fixed", inset: 0, overflow: "hidden", background: "#06040d" }}>
      <Backdrop />

      <div
        style={{
          position: "relative",
          zIndex: 5,
          height: "100%",
          display: "flex",
          flexDirection: "column",
          padding: "16px 26px 14px",
          gap: 10,
        }}
      >
        <header style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
            <Link
              href="/"
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 12,
                letterSpacing: 1,
                padding: "9px 12px",
                border: "1px solid #1c5f6b",
                background: "#0b2a33",
                color: "#22e8ff",
                textDecoration: "none",
              }}
            >
              ◀ FLOOR
            </Link>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 22,
                letterSpacing: 4,
                color: "#ff2d95",
                textShadow: "0 0 12px rgba(255,45,149,.8)",
                animation: "titleGlow 3.2s ease-in-out infinite",
              }}
            >
              {room.name.toUpperCase()}
            </span>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 11, color: connected ? "#5fe08a" : "#f2643d" }}>
              {connected ? "● LIVE" : "○ OFFLINE"}
            </span>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 11, color: "#8878b8" }}>
              HAND #{view?.handNo ?? "—"} · {PHASE_LABELS[phase] ?? phase.toUpperCase()}
            </span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 11, color: "#8878b8" }}>
              BLINDS {view ? `${view.sb}/${view.bb}` : `${room.minBet / 2}/${room.minBet}`}
            </span>
            <span
              key={balance}
              style={{
                fontFamily: "var(--font-body)",
                fontSize: 34,
                lineHeight: 1,
                color: "#ff8a1f",
                textShadow: "0 0 16px rgba(255,138,31,.6)",
                animation: "potPop .25s ease-out both",
              }}
            >
              {balance === undefined ? "····" : balance.toLocaleString()}
            </span>
            {seated ? (
              <button
                type="button"
                onClick={leave}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 10,
                  letterSpacing: 1,
                  padding: "8px 10px",
                  border: "1px solid #8c3b2e",
                  background: "transparent",
                  color: "#f2643d",
                  cursor: "pointer",
                }}
              >
                CASH OUT & LEAVE
              </button>
            ) : (
              <button
                type="button"
                onClick={() => {
                  sound.click();
                  setBuyOpen(true);
                }}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 11,
                  letterSpacing: 1,
                  padding: "9px 12px",
                  border: "2px solid #5fe08a",
                  background: "#0b2a33",
                  color: "#5fe08a",
                  cursor: "pointer",
                }}
              >
                TAKE A SEAT
              </button>
            )}
          </div>
        </header>

        {/* The table stage */}
        <div
          style={{
            position: "relative",
            flex: 1,
            minHeight: 0,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <div
            style={{
              position: "relative",
              transform: `perspective(1700px) rotateX(6deg) scale(${scale})`,
              transformOrigin: "center center",
            }}
          >
            {/* Wood rim */}
            <div
              style={{
                width: TABLE_W,
                height: TABLE_H,
                position: "relative",
                padding: 18,
                borderRadius: "50%",
                background: "linear-gradient(160deg, #7a4c26, #5c3a1e 45%, #38200e)",
                boxShadow:
                  "0 40px 110px rgba(0,0,0,.7), 0 0 80px rgba(255,45,149,.12), inset 0 2px 0 rgba(255,200,140,.35)",
              }}
            >
              {/* Felt */}
              <div
                style={{
                  position: "absolute",
                  inset: 18,
                  borderRadius: "50%",
                  background:
                    "radial-gradient(ellipse at 50% -8%, rgba(255,244,214,.15), transparent 55%), linear-gradient(#15503a, #0b352a)",
                  boxShadow:
                    "inset 0 0 0 3px rgba(34,232,255,.3), inset 0 0 130px rgba(0,0,0,.6)",
                }}
              >
                {/* Dashed inlay ring */}
                <div
                  style={{
                    position: "absolute",
                    inset: 34,
                    borderRadius: "50%",
                    border: "2px dashed rgba(159,216,192,.22)",
                    pointerEvents: "none",
                  }}
                />

                {/* Felt printing */}
                <div
                  style={{
                    position: "absolute",
                    left: "50%",
                    top: "21%",
                    transform: "translateX(-50%)",
                    textAlign: "center",
                    pointerEvents: "none",
                  }}
                >
                  <div
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 26,
                      letterSpacing: 6,
                      color: "rgba(184,244,228,.5)",
                      textShadow: "0 0 18px rgba(34,232,255,.35)",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {room.name.toUpperCase()}
                  </div>
                  <div
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 11,
                      letterSpacing: 5,
                      color: "rgba(127,206,172,.55)",
                      marginTop: 4,
                    }}
                  >
                    NO-LIMIT HOLD&apos;EM · PROVABLY FAIR
                  </div>
                </div>

                {/* Board + pot */}
                <div
                  style={{
                    position: "absolute",
                    left: "50%",
                    top: "48%",
                    transform: "translate(-50%, -50%)",
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    gap: 12,
                  }}
                >
                  {view && view.pot > 0 ? (
                    <div
                      key={view.pot}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 10,
                        animation: "potPop .25s ease-out both",
                      }}
                    >
                      <ChipStack amount={view.pot} color={chipColorFor(view.pot)} chipSize={30} />
                      <span
                        style={{
                          fontFamily: "var(--font-display)",
                          fontSize: 18,
                          letterSpacing: 2,
                          color: "#ffb15c",
                          textShadow: "0 0 14px rgba(255,138,31,.8)",
                        }}
                      >
                        POT {view.pot.toLocaleString()}
                      </span>
                    </div>
                  ) : (
                    <span
                      style={{
                        fontFamily: "var(--font-display)",
                        fontSize: 13,
                        letterSpacing: 3,
                        color: "#9fd8c0",
                        opacity: 0.85,
                      }}
                    >
                      {PHASE_LABELS[phase] ?? phase.toUpperCase()}
                    </span>
                  )}
                  <div style={{ display: "flex", gap: 10, minHeight: 84 }}>
                    {boardCards.length === 0
                      ? [0, 1, 2, 3, 4].map((i) => (
                          <div
                            key={i}
                            style={{
                              width: 60,
                              height: 84,
                              borderRadius: 3,
                              border: "2px dashed rgba(159,216,192,.25)",
                              background: "rgba(0,0,0,.16)",
                            }}
                          />
                        ))
                      : boardCards.map((c, i) => (
                          <PlayingCard
                            key={`${i}-${c}`}
                            code={c}
                            scale={3}
                            dealFrom="felt"
                            dealDelay={i >= freshFrom ? (i - freshFrom) * 160 : 0}
                          />
                        ))}
                  </div>
                  {view && view.pot > 0 && (
                    <span
                      style={{
                        fontFamily: "var(--font-display)",
                        fontSize: 11,
                        letterSpacing: 3,
                        color: "#5c8f7a",
                      }}
                    >
                      {PHASE_LABELS[phase] ?? phase.toUpperCase()}
                    </span>
                  )}
                </div>

                {/* Opponent seats on the far arc: plate at the rail, cards
                    on the felt in front, bet nearest the pot. */}
                {opponents.map((s, i) => {
                  const n = Math.max(opponents.length, 1);
                  const theta = Math.PI - ((i + 1) * Math.PI) / (n + 1);
                  const x = 50 + 47 * Math.cos(theta);
                  const y = 49 - 41 * Math.sin(theta);
                  const isTurn = view?.toAct === s.seatNo;
                  const cards =
                    s.cards && s.cards.length >= 4
                      ? [s.cards.slice(0, 2), s.cards.slice(2, 4)]
                      : [];
                  return (
                    <div
                      key={s.seatNo}
                      style={{
                        position: "absolute",
                        left: `${x}%`,
                        top: `${y}%`,
                        transform: "translate(-50%, -50%)",
                        display: "flex",
                        flexDirection: "column",
                        alignItems: "center",
                        gap: 5,
                        opacity: s.folded ? 0.45 : 1,
                      }}
                    >
                      <SeatPlate
                        name={profiles[s.userId]?.displayName ?? s.displayName}
                        userId={s.userId}
                        avatarPreset={profiles[s.userId]?.avatarPreset}
                        avatarVersion={profiles[s.userId]?.avatarVersion}
                        stack={s.stack}
                        bet={s.bet}
                        allIn={s.allIn}
                        folded={s.folded}
                        lastAction={s.lastAction}
                        isTurn={isTurn}
                        isDealer={view?.button === s.seatNo}
                      >
                        {cards.length > 0 && !s.folded ? (
                          <div style={{ display: "flex" }}>
                            <span style={{ transform: "rotate(-9deg)", zIndex: 0 }}>
                              <PlayingCard code={cards[0]} scale={1} silent dealFrom="felt" dealDelay={i * 60} />
                            </span>
                            <span style={{ transform: "rotate(9deg)", marginLeft: -8, zIndex: 1 }}>
                              <PlayingCard code={cards[1]} scale={1} silent dealFrom="felt" dealDelay={i * 60 + 70} />
                            </span>
                          </div>
                        ) : null}
                      </SeatPlate>
                    </div>
                  );
                })}

                {/* Your seat plate just above the held hole cards */}
                <div
                  style={{
                    position: "absolute",
                    left: "50%",
                    bottom: "22%",
                    transform: "translateX(-50%)",
                  }}
                >
                  {seated && mySeat ? (
                    <SeatPlate
                      name={`${profiles[mySeat.userId]?.displayName ?? mySeat.displayName} (YOU)`}
                      userId={mySeat.userId}
                      avatarPreset={profiles[mySeat.userId]?.avatarPreset}
                      avatarVersion={profiles[mySeat.userId]?.avatarVersion}
                      stack={mySeat.stack}
                      bet={mySeat.bet}
                      allIn={mySeat.allIn}
                      folded={mySeat.folded}
                      lastAction={mySeat.lastAction}
                      isTurn={myTurn}
                      isDealer={view?.button === mySeat.seatNo}
                      me
                    />
                  ) : (
                    <button
                      type="button"
                      onClick={() => {
                        sound.click();
                        setBuyOpen(true);
                      }}
                      style={{
                        minWidth: 190,
                        padding: "12px 18px",
                        background: "rgba(6,4,13,.55)",
                        border: "2px dashed rgba(95,224,138,.55)",
                        color: "#5fe08a",
                        fontFamily: "var(--font-display)",
                        fontSize: 12,
                        letterSpacing: 2,
                        cursor: "pointer",
                      }}
                    >
                      ◈ OPEN SEAT — SIT DOWN
                    </button>
                  )}
                </div>
              </div>
            </div>
          </div>

          {/* First-person hole cards, held at the near edge */}
          {myCards.length === 2 && !showdown && (
            <div
              style={{
                position: "absolute",
                bottom: 2,
                left: "50%",
                transform: "translateX(-50%)",
                display: "flex",
                zIndex: 7,
                filter: "drop-shadow(0 22px 26px rgba(0,0,0,.55))",
              }}
            >
              <span className="hole-card" style={{ ["--tilt" as string]: "-8deg", zIndex: 0 }}>
                <PlayingCard code={myCards[0]} scale={5} dealFrom="felt" dealDelay={0} />
              </span>
              <span
                className="hole-card"
                style={{ ["--tilt" as string]: "8deg", marginLeft: -30, zIndex: 1 }}
              >
                <PlayingCard code={myCards[1]} scale={5} dealFrom="felt" dealDelay={150} />
              </span>
            </div>
          )}
        </div>

        {/* Action bar */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            gap: 14,
            minHeight: 84,
            border: "2px solid",
            borderColor: myTurn ? "#22e8ff" : "#35205c",
            background: "#170c2b",
            padding: "10px 16px",
            animation: myTurn ? "turnPulse 1.1s ease-in-out infinite" : undefined,
          }}
        >
          {note && (
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 12,
                letterSpacing: 2,
                color: "#f2643d",
                position: "absolute",
                top: 66,
                left: "50%",
                transform: "translateX(-50%)",
                zIndex: 9,
                padding: "8px 16px",
                background: "rgba(6,4,13,.92)",
                border: "2px solid #f2643d",
                animation: "notePop .2s ease-out both",
              }}
            >
              {note}
            </span>
          )}
          {!seated ? (
            <span style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 2, color: "#5c4f80" }}>
              TAKE A SEAT TO PLAY · BLINDS {room.minBet / 2}/{room.minBet} · BUY-IN {room.minBet * 20}–{room.maxBet.toLocaleString()}
            </span>
          ) : myTurn && legal.length > 0 ? (
            <>
              {legal.includes("fold") && (
                <ActionBtn label="FOLD" color="#f2643d" disabled={busy} onClick={() => act("fold")} />
              )}
              {legal.includes("check") && (
                <ActionBtn label="CHECK" color="#5fe08a" disabled={busy} onClick={() => act("check")} />
              )}
              {legal.includes("call") && (
                <ActionBtn
                  label={`CALL ${callAmount?.toLocaleString() ?? ""}`}
                  color="#22e8ff"
                  disabled={busy}
                  onClick={() => act("call")}
                />
              )}
              {(legal.includes("bet") || legal.includes("raise")) && (
                <>
                  <div style={{ display: "flex", gap: 6 }}>
                    {(["min", "half", "pot", "allin"] as const).map((k) => (
                      <button
                        key={k}
                        type="button"
                        onClick={() => raiseQuick(k)}
                        style={{
                          fontFamily: "var(--font-display)",
                          fontSize: 10,
                          letterSpacing: 1,
                          padding: "7px 9px",
                          border: "1px solid #35205c",
                          background: "transparent",
                          color: "#8878b8",
                          cursor: "pointer",
                        }}
                      >
                        {k === "min" ? "MIN" : k === "half" ? "½ POT" : k === "pot" ? "POT" : "MAX"}
                      </button>
                    ))}
                  </div>
                  <input
                    value={raiseTo}
                    onChange={(e) => setRaiseTo(e.target.value.replace(/[^0-9]/g, ""))}
                    style={{
                      width: 110,
                      fontFamily: "var(--font-body)",
                      fontSize: 24,
                      background: "#06040d",
                      border: "2px solid #35205c",
                      color: "#ff8a1f",
                      padding: "6px 10px",
                      textAlign: "center",
                    }}
                  />
                  <ActionBtn
                    label={(legal.includes("bet") ? "BET " : "RAISE TO ") + (raiseTo || "")}
                    color="#ff8a1f"
                    disabled={busy || !raiseTo}
                    onClick={() => act(legal.includes("bet") ? "bet" : "raise", parseInt(raiseTo, 10))}
                  />
                  {maxRaiseTo !== undefined && (
                    <ActionBtn
                      label="ALL IN"
                      color="#ff2d95"
                      disabled={busy}
                      onClick={() => act(legal.includes("bet") ? "bet" : "raise", maxRaiseTo)}
                    />
                  )}
                </>
              )}
            </>
          ) : (
            <span style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 2, color: "#8878b8" }}>
              {phase === "waiting" ? "WAITING FOR PLAYERS…" : "NOT YOUR TURN"}
            </span>
          )}
        </div>
      </div>

      {/* Showdown overlay */}
      {showdown && (
        <div
          style={{
            position: "absolute",
            inset: 0,
            background: "rgba(6,4,13,.85)",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            gap: 12,
            zIndex: 10,
          }}
          onClick={() => setShowdown(null)}
        >
          {(() => {
            const maxNet = Math.max(...showdown.results.map((r) => r.net));
            const champ = showdown.results.find((r) => r.net === maxNet && maxNet > 0);
            return (
              <>
                <span
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 14,
                    letterSpacing: 4,
                    color: "#5c4f80",
                  }}
                >
                  HAND #{showdown.handNo} — SHOWDOWN
                </span>
                {champ && (
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 34,
                      letterSpacing: 6,
                      color: "#5fe08a",
                      textShadow: "0 0 20px rgba(95,224,138,.9), 0 0 60px rgba(34,232,255,.4)",
                      animation: "bigPop .5s cubic-bezier(.2,1.4,.4,1) .15s both",
                    }}
                  >
                    {champ.displayName} WINS {champ.winAmount.toLocaleString()}
                    {champ.handName ? ` · ${champ.handName.toUpperCase()}` : ""}
                  </span>
                )}
                <div
                  style={{
                    display: "flex",
                    flexDirection: "column",
                    gap: 8,
                    animation: "notePop .3s ease-out both",
                  }}
                >
                  {showdown.results.map((r) => {
                    const isWinner = r.net === maxNet && maxNet > 0;
                    return (
                      <div
                        key={r.userId}
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 14,
                          padding: "8px 16px",
                          border: `2px solid ${isWinner ? "#5fe08a" : "#241640"}`,
                          boxShadow: isWinner ? "0 0 24px rgba(95,224,138,.45)" : "none",
                          background: "#0f0720",
                          opacity: isWinner || maxNet <= 0 ? 1 : 0.55,
                        }}
                      >
                        <span style={isWinner ? { animation: "winGlow 1.1s ease-in-out infinite" } : undefined}>
                          <PlayingCard code={r.cards.slice(0, 2)} scale={2} silent />
                        </span>
                        <span style={isWinner ? { animation: "winGlow 1.1s ease-in-out infinite" } : undefined}>
                          <PlayingCard code={r.cards.slice(2, 4)} scale={2} silent />
                        </span>
                        <span
                          style={{
                            fontFamily: "var(--font-display)",
                            fontSize: 13,
                            color: isWinner ? "#c8ffd9" : "#cfc4f2",
                            minWidth: 130,
                          }}
                        >
                          {r.displayName}
                        </span>
                        <span
                          style={{
                            fontFamily: "var(--font-body)",
                            fontSize: 19,
                            color: isWinner ? "#5fe08a" : "#8878b8",
                            minWidth: 140,
                          }}
                        >
                          {r.handName || "—"}
                        </span>
                        <span
                          style={{
                            fontFamily: "var(--font-body)",
                            fontSize: 24,
                            color: r.net > 0 ? "#5fe08a" : "#f2643d",
                            minWidth: 100,
                            textAlign: "right",
                          }}
                        >
                          {r.net >= 0 ? "+" : ""}
                          {r.net.toLocaleString()}
                        </span>
                      </div>
                    );
                  })}
                </div>
                <span
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 11,
                    letterSpacing: 3,
                    color: "#5c4f80",
                    animation: "hintBlink 1.6s steps(1) infinite",
                  }}
                >
                  TAP TO CONTINUE
                </span>
              </>
            );
          })()}
        </div>
      )}

      {/* Buy-in modal */}
      {buyOpen && (
        <div
          style={{
            position: "absolute",
            inset: 0,
            zIndex: 12,
            background: "rgba(6,4,13,.86)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <div
            style={{
              width: 480,
              background: "#0f0720",
              border: "2px solid #5fe08a",
              boxShadow: "0 0 50px rgba(95,224,138,.3)",
              padding: 24,
              display: "flex",
              flexDirection: "column",
              gap: 16,
              animation: "bigPop .3s cubic-bezier(.2,1.4,.4,1) both",
            }}
          >
            <span style={{ fontFamily: "var(--font-display)", fontSize: 16, letterSpacing: 4, color: "#5fe08a" }}>
              BUY IN — {room.name.toUpperCase()}
            </span>
            <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
              <ChipStack amount={parseInt(buyAmount, 10) || room.minBet * 20} color="green" chipSize={40} />
              <span style={{ fontFamily: "var(--font-body)", fontSize: 19, color: "#8878b8" }}>
                {room.minBet * 20} – {room.maxBet.toLocaleString()} credits · blinds {room.minBet / 2}/{room.minBet}
              </span>
            </div>
            <input
              value={buyAmount}
              onChange={(e) => setBuyAmount(e.target.value.replace(/[^0-9]/g, ""))}
              style={{
                fontFamily: "var(--font-body)",
                fontSize: 30,
                background: "#06040d",
                border: "2px solid #35205c",
                color: "#ff8a1f",
                padding: "8px 12px",
                textAlign: "center",
              }}
            />
            <div style={{ display: "flex", gap: 10 }}>
              {[20, 50, 100].map((bb) => (
                <button
                  key={bb}
                  type="button"
                  onClick={() => {
                    sound.chipClink();
                    setBuyAmount(String(room.minBet * bb));
                  }}
                  style={{
                    flex: 1,
                    fontFamily: "var(--font-display)",
                    fontSize: 11,
                    padding: "9px 0",
                    border: "1px solid #35205c",
                    background: "transparent",
                    color: "#9fd8c0",
                    cursor: "pointer",
                  }}
                >
                  {bb} BB
                </button>
              ))}
            </div>
            <div style={{ display: "flex", gap: 10 }}>
              <button
                type="button"
                onClick={() => {
                  setBuyOpen(false);
                  sound.click();
                }}
                style={{
                  flex: 1,
                  fontFamily: "var(--font-display)",
                  fontSize: 12,
                  padding: "11px 0",
                  border: "1px solid #35205c",
                  background: "transparent",
                  color: "#8878b8",
                  cursor: "pointer",
                }}
              >
                CANCEL
              </button>
              <button
                type="button"
                onClick={buyIn}
                disabled={busy}
                style={{
                  flex: 1,
                  fontFamily: "var(--font-display)",
                  fontSize: 12,
                  letterSpacing: 1,
                  padding: "11px 0",
                  border: "2px solid #5fe08a",
                  background: "#0b2a33",
                  color: "#5fe08a",
                  cursor: busy ? "wait" : "pointer",
                  opacity: busy ? 0.6 : 1,
                }}
              >
                {busy ? "…" : "SIT DOWN"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function SeatPlate({
  name,
  userId,
  avatarPreset,
  avatarVersion,
  stack,
  bet,
  allIn,
  folded,
  lastAction,
  isTurn,
  isDealer,
  me,
  children,
}: {
  name: string;
  userId?: number;
  avatarPreset?: string;
  avatarVersion?: number;
  stack: number;
  bet: number;
  allIn: boolean;
  folded: boolean;
  lastAction: string;
  isTurn: boolean;
  isDealer: boolean;
  me?: boolean;
  children?: React.ReactNode;
}) {
  const ring = isTurn ? "#22e8ff" : me ? "#ff8a1f" : "#241640";
  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 4 }}>
      <div
        style={{
          padding: "7px 14px",
          background: "rgba(6,4,13,.8)",
          border: `2px solid ${ring}`,
          animation: isTurn ? "turnPulse 1.1s ease-in-out infinite" : undefined,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          minWidth: 150,
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 12,
            letterSpacing: 1,
            color: me ? "#ff8a1f" : "#cfc4f2",
            maxWidth: me ? 200 : 150,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            display: "flex",
            alignItems: "center",
            gap: 6,
          }}
        >
          {userId !== undefined && (
            <Avatar
              userId={userId}
              displayName={name}
              avatarPreset={avatarPreset}
              avatarVersion={avatarVersion}
              size={20}
              ring={ring}
            />
          )}
          {isDealer && (
            <span
              style={{
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: 16,
                height: 16,
                borderRadius: "50%",
                background: "#ece6ff",
                color: "#06040d",
                fontSize: 9,
              }}
            >
              D
            </span>
          )}
          {name}
        </span>
        <span style={{ fontFamily: "var(--font-body)", fontSize: 22, lineHeight: 1.1, color: "#9fd8c0" }}>
          {stack.toLocaleString()}
        </span>
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 10,
            letterSpacing: 1,
            color: allIn ? "#ff2d95" : folded ? "#6b5f9e" : "#5c8f7a",
            minHeight: 12,
          }}
        >
          {bet > 0
            ? `BET ${bet.toLocaleString()}`
            : allIn
              ? "ALL IN"
              : folded
                ? "FOLDED"
                : lastAction === "blind"
                  ? "BLIND"
                  : ""}
        </span>
      </div>
      {children}
      {bet > 0 && (
        <div
          key={bet}
          style={{
            display: "flex",
            alignItems: "center",
            gap: 6,
            animation: "potPop .25s ease-out both",
          }}
        >
          <Chip label="" color={chipColorFor(bet)} size={22} />
          <span
            style={{
              fontFamily: "var(--font-body)",
              fontSize: 18,
              color: "#ffb15c",
              textShadow: "0 0 8px rgba(255,138,31,.6)",
            }}
          >
            {bet.toLocaleString()}
          </span>
        </div>
      )}
    </div>
  );
}

function ActionBtn({
  label,
  color,
  disabled,
  onClick,
}: {
  label: string;
  color: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      style={{
        border: `2px solid ${color}`,
        background: "rgba(6,4,13,.6)",
        color,
        fontFamily: "var(--font-display)",
        fontSize: 15,
        letterSpacing: 2,
        padding: "15px 26px",
        cursor: disabled ? "wait" : "pointer",
        opacity: disabled ? 0.45 : 1,
        transition: "transform .1s ease, background .12s ease, box-shadow .12s ease",
      }}
      onMouseEnter={(e) => {
        if (disabled) return;
        e.currentTarget.style.background = color;
        e.currentTarget.style.color = "#06040d";
        e.currentTarget.style.boxShadow = `0 0 24px ${color}99`;
        e.currentTarget.style.transform = "translateY(-2px)";
      }}
      onMouseLeave={(e) => {
        if (disabled) return;
        e.currentTarget.style.background = "rgba(6,4,13,.6)";
        e.currentTarget.style.color = color;
        e.currentTarget.style.boxShadow = "none";
        e.currentTarget.style.transform = "translateY(0)";
      }}
    >
      {label}
    </button>
  );
}
