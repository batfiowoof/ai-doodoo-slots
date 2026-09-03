"use client";

import { useEffect, useState } from "react";
import Backdrop from "@/components/Backdrop";
import HistoryTable from "@/components/HistoryTable";
import { NavButton, NavLink } from "@/components/NavButton";
import PixelCard from "@/components/PixelCard";
import { sound } from "@/lib/sound";
import { useActiveHand, useBets, useDeal, useFairCurrent, useGames, useHandAction, useSession } from "@/lib/api";
import type { BlackjackHandView } from "@/lib/types";

/** Split a compact card list ("AsKd7c") into codes; "" → []. */
function cardsOf(list: string | undefined | null): string[] {
  if (!list) return [];
  const out: string[] = [];
  for (let i = 0; i + 1 < list.length; i += 2) out.push(list.slice(i, i + 2));
  return out;
}

const OUTCOME_LABELS: Record<string, string> = {
  blackjack: "BLACKJACK! PAYS 3:2",
  win: "WIN",
  push: "PUSH — STAKE RETURNED",
  lose: "DEALER WINS",
  bust: "BUST",
};

const OUTCOME_COLORS: Record<string, string> = {
  blackjack: "#ff2d95",
  win: "#22e8ff",
  push: "#ffb15c",
  lose: "#8c3b2e",
  bust: "#f2643d",
};

type Panel = "history" | "info" | null;

/** The blackjack table: felt, dealer/player rows, chip buttons. */
export default function BlackjackTable() {
  const session = useSession();
  const fair = useFairCurrent(session.isSuccess);
  const games = useGames();
  const bets = useBets();
  const activeQuery = useActiveHand(session.isSuccess);
  const deal = useDeal();
  const act = useHandAction();

  const info = games.data?.find((g) => g.id === "blackjack");
  const steps = info?.betSteps ?? [5, 10, 25, 50];

  const [hand, setHand] = useState<BlackjackHandView | null>(null);
  const [betStep, setBetStep] = useState<number>(10);
  const [note, setNote] = useState<string | null>(null);
  const [panel, setPanel] = useState<Panel>(null);
  const [muted, setMuted] = useState(false);
  const [scale, setScale] = useState(1);

  // Before the first deal, adopt the server's view of any in-progress hand
  // (picked up on page load); after that, local state owns the view.
  const currentHand = hand ?? activeQuery.data ?? null;

  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth;
      const h = window.innerHeight;
      setScale(Math.min(1, (h - 240) / 520, (w - 80) / 860));
    };
    onResize();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const flash = (msg: string) => {
    setNote(msg);
    window.setTimeout(() => setNote((cur) => (cur === msg ? null : cur)), 2600);
  };

  const active = currentHand?.status === "active";
  const busy = deal.isPending || act.isPending;

  const onDeal = () => {
    sound.unlock();
    sound.click();
    deal.mutate(
      { betCredits: betStep, clientSeed: "" },
      {
        onSuccess: (res) => {
          if (res.replay) return;
          setHand(res.hand);
          if (res.hand.status === "complete" && res.hand.outcome === "blackjack") {
            sound.bell();
          }
        },
        onError: (err) => {
          sound.error();
          flash(err instanceof Error ? err.message.toUpperCase() : "TABLE UNAVAILABLE");
        },
      },
    );
  };

  const onAction = (action: "hit" | "stand" | "double") => {
    if (!currentHand || !active || busy) return;
    sound.click();
    act.mutate(
      { handId: currentHand.handId, action },
      {
        onSuccess: (res) => {
          if (res.replay) return;
          setHand(res.hand);
          if (res.hand.status === "complete") {
            if ((res.hand.payoutCredits ?? 0) > res.hand.betCredits) sound.bell();
          }
        },
        onError: (err) => {
          sound.error();
          flash(err instanceof Error ? err.message.toUpperCase() : "ACTION REJECTED");
        },
      },
    );
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (panel !== null) {
        if (e.code === "Escape") setPanel(null);
        return;
      }
      if (!currentHand || currentHand.status !== "active" || busy) return;
      if (e.code === "KeyH") onAction("hit");
      if (e.code === "KeyS") onAction("stand");
      if (e.code === "KeyD" && currentHand.canDouble) onAction("double");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hand, busy, panel]);

  const toggleMute = () => {
    const next = !muted;
    setMuted(next);
    sound.setMuted(next);
    if (!next) {
      sound.unlock();
      sound.click();
    }
  };

  const me = session.data;
  const subtitle = info
    ? `${info.name} · dealer stands on 17 · blackjack pays 3:2 · RTP ${(info.theoreticalRtp * 100).toFixed(2)}%`
    : "BLACKJACK";

  const dealerCards = cardsOf(currentHand?.dealerCards);
  // While active the server sends only the up card; render the hole as a back.
  const dealerShow = active
    ? [...dealerCards, "back"]
    : dealerCards;
  const playerCards = cardsOf(currentHand?.playerCards);
  const playerLost = currentHand?.status === "complete" && (currentHand.outcome === "lose" || currentHand.outcome === "bust");

  return (
    <div style={{ position: "fixed", inset: 0, overflow: "hidden", background: "#06040d" }}>
      <Backdrop />

      <div
        style={{
          position: "relative",
          zIndex: 5,
          display: "flex",
          flexDirection: "column",
          height: "100%",
        }}
      >
        <header
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 16,
            padding: "14px 22px",
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
            <NavLink href="/" variant="back">
              ◂ GAME MENU
            </NavLink>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 19, color: "#8878b8" }}>
              {subtitle}
            </span>
          </div>
          <nav style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <NavButton onClick={() => { sound.click(); setPanel("history"); }}>HISTORY</NavButton>
            <NavButton onClick={() => { sound.click(); setPanel("info"); }}>HOW IT WORKS</NavButton>
            <NavButton variant="sound" onClick={toggleMute}>
              {muted ? "SND OFF" : "SND ON"}
            </NavButton>
          </nav>
        </header>

        <div
          style={{
            flex: 1,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            minHeight: 0,
          }}
        >
          <div style={{ transform: `scale(${scale})`, transformOrigin: "center center" }}>
            {/* The felt */}
            <div
              style={{
                width: 820,
                padding: "34px 40px 28px",
                background: "linear-gradient(#124232,#0c3026)",
                border: "4px solid #5c3a1e",
                boxShadow: "0 0 60px rgba(34,232,255,.12), inset 0 0 80px rgba(0,0,0,.45)",
                display: "flex",
                flexDirection: "column",
                gap: 18,
              }}
            >
              {/* Dealer row */}
              <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
                <span
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 12,
                    letterSpacing: 2,
                    color: "#9fd8c0",
                    width: 76,
                  }}
                >
                  DEALER
                </span>
                <div style={{ display: "flex", gap: 8, minHeight: 84 }}>
                  {dealerShow.length === 0 && <EmptySlot />}
                  {dealerShow.map((code, i) => (
                    <PixelCard key={i} code={code} scale={3} dim={playerLost === true && code !== "back"} />
                  ))}
                </div>
                {currentHand?.status === "complete" && currentHand.dealerTotal != null && (
                  <span
                    style={{
                      fontFamily: "var(--font-body)",
                      fontSize: 26,
                      color: "#cfe8dc",
                      textShadow: "0 0 10px rgba(34,232,255,.5)",
                    }}
                  >
                    {currentHand.dealerTotal}
                  </span>
                )}
              </div>

              <div
                style={{
                  height: 2,
                  background: "repeating-linear-gradient(90deg,#9fd8c0 0 14px,transparent 14px 26px)",
                  opacity: 0.35,
                }}
              />

              {/* Player row */}
              <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
                <span
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 12,
                    letterSpacing: 2,
                    color: "#9fd8c0",
                    width: 76,
                  }}
                >
                  YOU
                </span>
                <div style={{ display: "flex", gap: 8, minHeight: 84 }}>
                  {playerCards.length === 0 && <EmptySlot />}
                  {playerCards.map((code, i) => (
                    <PixelCard key={i} code={code} scale={3} dim={playerLost} />
                  ))}
                </div>
                {playerCards.length > 0 && (
                  <span
                    style={{
                      fontFamily: "var(--font-body)",
                      fontSize: 26,
                      color: "#cfe8dc",
                      textShadow: "0 0 10px rgba(34,232,255,.5)",
                    }}
                  >
                    {hand?.playerTotal}
                  </span>
                )}
              </div>

              {/* Outcome banner */}
              {currentHand?.status === "complete" && currentHand.outcome && (
                <div
                  style={{
                    alignSelf: "center",
                    padding: "10px 22px",
                    background: "rgba(6,4,13,.75)",
                    border: `2px solid ${OUTCOME_COLORS[currentHand.outcome] ?? "#8878b8"}`,
                    fontFamily: "var(--font-display)",
                    fontSize: 16,
                    letterSpacing: 3,
                    color: OUTCOME_COLORS[currentHand.outcome] ?? "#8878b8",
                    animation: "notePop .2s ease-out both",
                  }}
                >
                  {OUTCOME_LABELS[currentHand.outcome] ?? currentHand.outcome.toUpperCase()}
                  {currentHand.payoutCredits > 0 ? ` · +${currentHand.payoutCredits.toLocaleString()}` : ""}
                </div>
              )}

              {/* Chip row: bet steps when idle, actions when a hand is live */}
              {active ? (
                <div style={{ display: "flex", justifyContent: "center", gap: 16 }}>
                  <ChipButton label="HIT" hint="H" color="#22e8ff" disabled={busy} onClick={() => onAction("hit")} />
                  <ChipButton label="STAND" hint="S" color="#5fe08a" disabled={busy} onClick={() => onAction("stand")} />
                  <ChipButton
                    label="DOUBLE"
                    hint="D"
                    color="#ff8a1f"
                    disabled={busy || !currentHand?.canDouble}
                    onClick={() => onAction("double")}
                  />
                </div>
              ) : (
                <div style={{ display: "flex", justifyContent: "center", gap: 12, alignItems: "center" }}>
                  {steps.map((s) => (
                    <ChipButton
                      key={s}
                      label={String(s)}
                      color="#ff2d95"
                      selected={s === betStep}
                      disabled={busy}
                      onClick={() => { sound.click(); setBetStep(s); }}
                    />
                  ))}
                  <ChipButton
                    label="DEAL"
                    color="#5fe08a"
                    big
                    disabled={busy || !me}
                    onClick={onDeal}
                  />
                </div>
              )}

              <div
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  fontFamily: "var(--font-body)",
                  fontSize: 17,
                  color: "#9fd8c0",
                }}
              >
                <span>
                  {me ? `balance ${me.balanceCredits.toLocaleString()}` : "····"}
                  {currentHand ? ` · bet ${currentHand.betCredits}` : ""}
                  {hand?.doubled ? " · DOUBLED" : ""}
                </span>
                <span>
                  {fair.data ? `seed ${fair.data.serverSeedHash.slice(0, 8)}… · nonce ${fair.data.nonce}` : ""}
                </span>
              </div>
            </div>
          </div>
        </div>

        <div
          style={{
            padding: "10px 22px 14px",
            display: "flex",
            justifyContent: "space-between",
            fontFamily: "var(--font-body)",
            fontSize: 17,
            color: "#5c4f80",
          }}
        >
          <span>{me ? (me.user.isGuest ? "guest" : me.user.displayName) : "····"}</span>
          <span>H = HIT · S = STAND · D = DOUBLE · {bets.data?.bets.length ?? 0} BETS THIS SESSION</span>
        </div>
      </div>

      {note && (
        <div
          role="status"
          style={{
            position: "absolute",
            top: 84,
            left: "50%",
            transform: "translateX(-50%)",
            zIndex: 8,
            padding: "10px 18px",
            background: "rgba(6,4,13,.9)",
            border: "2px solid #ff8a1f",
            fontFamily: "var(--font-display)",
            fontSize: 14,
            letterSpacing: 2,
            color: "#ff8a1f",
            animation: "notePop .2s ease-out both",
          }}
        >
          {note}
        </div>
      )}

      {panel && (
        <div
          style={{
            position: "absolute",
            inset: 0,
            zIndex: 12,
            background: "rgba(6,4,13,.86)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            padding: 32,
          }}
        >
          <div
            style={{
              width: 900,
              maxWidth: "100%",
              maxHeight: "84vh",
              overflow: "auto",
              background: "#0f0720",
              border: "2px solid #22e8ff",
              boxShadow: "0 0 60px rgba(34,232,255,.35)",
            }}
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                padding: "14px 18px",
                borderBottom: "2px solid #241640",
                background: "#150a2a",
                position: "sticky",
                top: 0,
              }}
            >
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 16,
                  letterSpacing: 3,
                  color: "#ff2d95",
                  textShadow: "0 0 12px rgba(255,45,149,.8)",
                }}
              >
                {panel === "history" ? "BET HISTORY" : "HOW IT WORKS"}
              </span>
              <button
                type="button"
                onClick={() => { sound.click(); setPanel(null); }}
                style={{
                  border: "1px solid #35205c",
                  background: "transparent",
                  color: "#8878b8",
                  fontFamily: "var(--font-display)",
                  fontSize: 11,
                  letterSpacing: 2,
                  padding: "8px 12px",
                  cursor: "pointer",
                }}
              >
                CLOSE ✕
              </button>
            </div>
            <div style={{ padding: 18 }} key={panel}>
              {panel === "history" && <HistoryTable />}
              {panel === "info" && <HowItWorks />}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function EmptySlot() {
  return (
    <div
      style={{
        width: 60,
        height: 84,
        boxShadow: "inset 0 0 0 2px rgba(159,216,192,.25)",
        background: "rgba(0,0,0,.2)",
      }}
    />
  );
}

function ChipButton({
  label,
  hint,
  color,
  selected,
  big,
  disabled,
  onClick,
}: {
  label: string;
  hint?: string;
  color: string;
  selected?: boolean;
  big?: boolean;
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
        background: selected ? color : "rgba(6,4,13,.6)",
        color: selected ? "#06040d" : color,
        fontFamily: "var(--font-display)",
        fontSize: big ? 15 : 13,
        letterSpacing: 2,
        padding: big ? "14px 30px" : "12px 20px",
        cursor: disabled ? "wait" : "pointer",
        opacity: disabled ? 0.45 : 1,
      }}
      onMouseEnter={(e) => {
        if (disabled || selected) return;
        e.currentTarget.style.background = color;
        e.currentTarget.style.color = "#06040d";
        e.currentTarget.style.boxShadow = `0 0 18px ${color}66`;
      }}
      onMouseLeave={(e) => {
        if (disabled || selected) return;
        e.currentTarget.style.background = "rgba(6,4,13,.6)";
        e.currentTarget.style.color = color;
        e.currentTarget.style.boxShadow = "none";
      }}
    >
      {label}
      {hint ? ` · ${hint}` : ""}
    </button>
  );
}

function HowItWorks() {
  const rows: Array<[string, string]> = [
    ["DEAL", "pick a chip, get two cards; the dealer shows one"],
    ["HIT / STAND", "draw or stop; the dealer draws to 17 and stands"],
    ["DOUBLE", "first action only: double the stake, take exactly one card"],
    ["BLACKJACK", "natural 21 pays 3:2; equal naturals push"],
    ["BET STEPS", "5 · 10 · 25 · 50 credits — one active hand at a time"],
    ["FAIRNESS", "the whole 52-card deck order derives from your seed pair; the same shuffle is recomputable on the VERIFY page after seed rotation"],
    ["TOP UP", "+1000 credits from the kiosk, once per hour"],
  ];
  return (
    <div>
      <p
        style={{
          margin: 0,
          fontFamily: "var(--font-body)",
          fontSize: 22,
          lineHeight: 1.35,
          color: "#ece6ff",
          textWrap: "pretty",
        }}
      >
        Every card is decided by the server and derived from your seed pair —
        the deck order is fixed at deal time and every draw is the next card
        of that deck. Verify any hand from the VERIFY page with the server
        seed revealed on rotation.
      </p>
      <div style={{ marginTop: 18, display: "flex", flexDirection: "column", gap: 10 }}>
        {rows.map(([label, value]) => (
          <div
            key={label}
            style={{ display: "flex", gap: 14, padding: "10px 0", borderTop: "1px solid #1b1030" }}
          >
            <span
              style={{
                width: 220,
                fontFamily: "var(--font-display)",
                fontSize: 11,
                letterSpacing: 2,
                color: "#8878b8",
              }}
            >
              {label}
            </span>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#ece6ff" }}>
              {value}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
