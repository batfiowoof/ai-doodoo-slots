"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useQueryClient } from "@tanstack/react-query";
import PixelCard from "@/components/PixelCard";
import { useSession } from "@/lib/api";
import type { Me } from "@/lib/types";
import { sound } from "@/lib/sound";

// The poker room: seat ring, board, action bar. The server decides
// everything — every card, pot, and payout arrives from the table runner;
// the client renders and never predicts an outcome.

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
    ? "ws://localhost:8080/api/v1/ws"
    : `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/ws`);

const PHASE_LABELS: Record<string, string> = {
  waiting: "WAITING FOR PLAYERS",
  preflop: "PREFLOP",
  flop: "FLOP",
  turn: "TURN",
  river: "RIVER",
  showdown: "SHOWDOWN",
};

export default function PokerRoom({ slug, room }: { slug: string; room: RoomInfo }) {
  const session = useSession();
  const qc = useQueryClient();

  const [connected, setConnected] = useState(false);
  const [view, setView] = useState<TableView | null>(null);
  const [myCards, setMyCards] = useState<string[]>([]);
  const [showdown, setShowdown] = useState<HandRecord | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [buyOpen, setBuyOpen] = useState(false);
  const [buyAmount, setBuyAmount] = useState(String(room.minBet * 20));
  const [busy, setBusy] = useState(false);

  const wsRef = useRef<WebSocket | null>(null);
  const userId = session.data?.user.id;

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
        setConnected(true);
        send("join_room", { slug });
        setTimeout(() => send("game_action", { action: "state" }), 200);
      };
      ws.onclose = () => {
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
            sound.click();
            // Fresh hole cards come via a personalized pull.
            send("game_action", { action: "state" });
            break;
          }
          case "hand_result": {
            const rec = (p as { record?: HandRecord })?.record;
            if (rec) setShowdown(rec);
            const mine = (rec?.results ?? []).find((r) => r.userId === userId);
            if (mine && mine.net > 0) sound.bell();
            else if (mine && mine.net < 0) sound.error();
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
                sound.bell();
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
    sound.click();
    setBusy(true);
    send("game_action", { action, ...(amount !== undefined ? { amount } : {}) });
  };

  const buyIn = () => {
    const amount = parseInt(buyAmount, 10);
    if (!Number.isFinite(amount) || amount <= 0) return;
    sound.unlock();
    sound.click();
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

  const balance = session.data?.balanceCredits;
  const phase = view?.phase ?? "waiting";
  const boardCards = showdown?.board ?? view?.board ?? [];

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
              style={{ fontFamily: "var(--font-display)", fontSize: 11, color: "#8878b8", textDecoration: "none" }}
            >
              ◀ FLOOR
            </Link>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 16, letterSpacing: 2, color: "#ff2d95" }}>
              {room.name.toUpperCase()}
            </span>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 11, color: connected ? "#5fe08a" : "#f2643d" }}>
              {connected ? "● LIVE" : "○ OFFLINE"}
            </span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 10, color: "#8878b8" }}>
              BLINDS {view ? `${view.sb}/${view.bb}` : `${room.minBet / 2}/${room.minBet}`}
            </span>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 26, color: "#ff8a1f" }}>
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

        {/* The oval */}
        <div
          style={{
            position: "relative",
            flex: 1,
            border: "4px solid #5c3a1e",
            background: "linear-gradient(#124232,#0c3026)",
            boxShadow: "inset 0 0 80px rgba(0,0,0,.45)",
            overflow: "hidden",
          }}
        >
          {/* Board + pot */}
          <div
            style={{
              position: "absolute",
              left: "50%",
              top: "44%",
              transform: "translate(-50%, -50%)",
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: 8,
            }}
          >
            <span style={{ fontFamily: "var(--font-display)", fontSize: 12, letterSpacing: 3, color: "#9fd8c0" }}>
              {view && view.pot > 0 ? `POT ${view.pot.toLocaleString()}` : PHASE_LABELS[phase] ?? phase.toUpperCase()}
            </span>
            <div style={{ display: "flex", gap: 6, minHeight: 56 }}>
              {boardCards.length === 0
                ? [0, 1, 2, 3, 4].map((i) => (
                    <div
                      key={i}
                      style={{ width: 40, height: 56, boxShadow: "inset 0 0 0 2px rgba(159,216,192,.25)" }}
                    />
                  ))
                : boardCards.map((c, i) => <PixelCard key={i} code={c} scale={2} />)}
            </div>
            {view && view.pot > 0 && (
              <span style={{ fontFamily: "var(--font-display)", fontSize: 10, letterSpacing: 2, color: "#5c8f7a" }}>
                {PHASE_LABELS[phase] ?? phase.toUpperCase()}
              </span>
            )}
          </div>

          {/* Seats around the oval */}
          {(view?.seats ?? []).map((s, i, arr) => {
            const angle = (i / Math.max(arr.length, 2)) * Math.PI * 2 - Math.PI / 2;
            const rx = 40;
            const ry = 36;
            const x = 50 + rx * Math.cos(angle);
            const y = 46 + ry * Math.sin(angle);
            const isMe = s.userId === userId;
            const isTurn = view?.toAct === s.seatNo;
            const cards =
              s.userId === userId && myCards.length === 2
                ? myCards
                : s.cards
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
                  gap: 3,
                  minWidth: 150,
                }}
              >
                {cards.length > 0 && (
                  <div style={{ display: "flex", gap: 3, opacity: s.folded ? 0.35 : 1 }}>
                    {cards.map((c, j) => (
                      <PixelCard key={j} code={c} scale={1} dim={s.folded} />
                    ))}
                  </div>
                )}
                <div
                  style={{
                    padding: "4px 10px",
                    background: "rgba(6,4,13,.75)",
                    border: `2px solid ${isTurn ? "#22e8ff" : isMe ? "#ff8a1f" : "#241640"}`,
                    boxShadow: isTurn ? "0 0 14px rgba(34,232,255,.5)" : "none",
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    minWidth: 130,
                    opacity: s.folded ? 0.5 : 1,
                  }}
                >
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 11,
                      letterSpacing: 1,
                      color: isMe ? "#ff8a1f" : "#cfc4f2",
                      maxWidth: 130,
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {s.displayName}
                    {view?.button === s.seatNo ? " ·D" : ""}
                  </span>
                  <span style={{ fontFamily: "var(--font-body)", fontSize: 17, color: "#9fd8c0" }}>
                    {s.stack.toLocaleString()}
                  </span>
                  <span style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 1, color: "#6b5f9e", minHeight: 11 }}>
                    {s.bet > 0
                      ? `BET ${s.bet.toLocaleString()}`
                      : s.allIn
                        ? "ALL IN"
                        : s.folded
                          ? "FOLDED"
                          : s.lastAction === "blind"
                            ? "BLIND"
                            : ""}
                  </span>
                </div>
              </div>
            );
          })}

          {/* Showdown overlay */}
          {showdown && (
            <div
              style={{
                position: "absolute",
                inset: 0,
                background: "rgba(6,4,13,.82)",
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                justifyContent: "center",
                gap: 10,
                zIndex: 8,
              }}
              onClick={() => setShowdown(null)}
            >
              <span style={{ fontFamily: "var(--font-display)", fontSize: 18, letterSpacing: 4, color: "#22e8ff" }}>
                HAND #{showdown.handNo} — SHOWDOWN
              </span>
              {showdown.results.map((r) => (
                <div
                  key={r.userId}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 12,
                    padding: "6px 14px",
                    border: `1px solid ${r.net > 0 ? "#5fe08a" : "#8c3b2e"}`,
                    background: "#0f0720",
                  }}
                >
                  <PixelCard code={r.cards.slice(0, 2)} scale={1} />
                  <PixelCard code={r.cards.slice(2, 4)} scale={1} />
                  <span style={{ fontFamily: "var(--font-display)", fontSize: 12, color: "#cfc4f2", minWidth: 110 }}>
                    {r.displayName}
                  </span>
                  <span style={{ fontFamily: "var(--font-body)", fontSize: 16, color: "#8878b8", minWidth: 120 }}>
                    {r.handName || "—"}
                  </span>
                  <span
                    style={{
                      fontFamily: "var(--font-body)",
                      fontSize: 20,
                      color: r.net > 0 ? "#5fe08a" : "#f2643d",
                      minWidth: 90,
                      textAlign: "right",
                    }}
                  >
                    {r.net >= 0 ? "+" : ""}
                    {r.net.toLocaleString()}
                  </span>
                </div>
              ))}
              <span style={{ fontFamily: "var(--font-display)", fontSize: 10, letterSpacing: 2, color: "#5c4f80" }}>
                TAP TO CONTINUE
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
            gap: 12,
            minHeight: 64,
            border: "2px solid #35205c",
            background: "#170c2b",
            padding: "8px 16px",
          }}
        >
          {note && (
            <span style={{ fontFamily: "var(--font-display)", fontSize: 11, color: "#f2643d", position: "absolute", top: 74 }}>
              {note}
            </span>
          )}
          {!seated ? (
            <span style={{ fontFamily: "var(--font-display)", fontSize: 12, letterSpacing: 2, color: "#5c4f80" }}>
              TAKE A SEAT TO PLAY · BLINDS {room.minBet / 2}/{room.minBet}
            </span>
          ) : myTurn && legal.length > 0 ? (
            <>
              {legal.includes("fold") && <ActionBtn label="FOLD" color="#f2643d" disabled={busy} onClick={() => act("fold")} />}
              {legal.includes("check") && <ActionBtn label="CHECK" color="#5fe08a" disabled={busy} onClick={() => act("check")} />}
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
                  <input
                    value={raiseTo}
                    onChange={(e) => setRaiseTo(e.target.value.replace(/[^0-9]/g, ""))}
                    style={{
                      width: 90,
                      fontFamily: "var(--font-body)",
                      fontSize: 18,
                      background: "#06040d",
                      border: "1px solid #35205c",
                      color: "#ff8a1f",
                      padding: "6px 8px",
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
            <span style={{ fontFamily: "var(--font-display)", fontSize: 12, letterSpacing: 2, color: "#8878b8" }}>
              {phase === "waiting" ? "WAITING FOR PLAYERS…" : "NOT YOUR TURN"}
            </span>
          )}
        </div>
      </div>

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
          <div style={{ width: 420, background: "#0f0720", border: "2px solid #5fe08a", padding: 22, display: "flex", flexDirection: "column", gap: 14 }}>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 14, letterSpacing: 3, color: "#5fe08a" }}>
              BUY IN
            </span>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 18, color: "#8878b8" }}>
              {room.minBet * 20} – {room.maxBet.toLocaleString()} credits · blinds {room.minBet / 2}/{room.minBet}
            </span>
            <input
              value={buyAmount}
              onChange={(e) => setBuyAmount(e.target.value.replace(/[^0-9]/g, ""))}
              style={{
                fontFamily: "var(--font-body)",
                fontSize: 26,
                background: "#06040d",
                border: "2px solid #35205c",
                color: "#ff8a1f",
                padding: "8px 12px",
              }}
            />
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
                  padding: "10px 0",
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
                  padding: "10px 0",
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
        fontSize: 13,
        letterSpacing: 1,
        padding: "12px 22px",
        cursor: disabled ? "wait" : "pointer",
        opacity: disabled ? 0.45 : 1,
      }}
      onMouseEnter={(e) => {
        if (disabled) return;
        e.currentTarget.style.background = color;
        e.currentTarget.style.color = "#06040d";
      }}
      onMouseLeave={(e) => {
        if (disabled) return;
        e.currentTarget.style.background = "rgba(6,4,13,.6)";
        e.currentTarget.style.color = color;
      }}
    >
      {label}
    </button>
  );
}
