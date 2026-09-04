"use client";

import { useEffect, useRef, useState } from "react";
import Backdrop from "@/components/Backdrop";
import Chip, { ChipStack } from "@/components/Chip";
import HistoryTable from "@/components/HistoryTable";
import { NavButton, NavLink } from "@/components/NavButton";
import PlayingCard from "@/components/PlayingCard";
import { Avatar } from "@/components/Avatar";
import { sound } from "@/lib/sound";
import {
  useActiveHand,
  useBets,
  useDeal,
  useFairCurrent,
  useGames,
  useHandAction,
  useSession,
} from "@/lib/api";
import type { BlackjackHandView } from "@/lib/types";

/** Split a compact card list ("AsKd7c") into codes; "" → []. */
function cardsOf(list: string | undefined | null): string[] {
  if (!list) return [];
  const out: string[] = [];
  for (let i = 0; i + 1 < list.length; i += 2) out.push(list.slice(i, i + 2));
  return out;
}

const OUTCOME_LABELS: Record<string, string> = {
  blackjack: "BLACKJACK!",
  win: "YOU WIN",
  push: "PUSH — STAKE RETURNED",
  lose: "DEALER WINS",
  bust: "BUST",
};

const OUTCOME_COLORS: Record<string, string> = {
  blackjack: "#ff2d95",
  win: "#5fe08a",
  push: "#ffb15c",
  lose: "#f2643d",
  bust: "#f2643d",
};

const CHIP_COLOR_BY_STEP: Record<number, "cyan" | "green" | "orange" | "pink"> = {
  5: "cyan",
  10: "green",
  25: "orange",
  50: "pink",
};

// Base felt geometry; the whole stage scales to fit the viewport.
const FELT_W = 1400;
const FELT_H = 580;
const STAGE_W = FELT_W + 20;
const STAGE_H = FELT_H + 136;

type Panel = "history" | "info" | null;

/** The blackjack table: an arc of felt seen from the player's seat. */
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
  const [coins, setCoins] = useState(0);
  const [shake, setShake] = useState(false);
  // Marks that we watched the hand go active, so a completed hand restored
  // on page load doesn't replay its celebration.
  const sawActive = useRef(false);

  // Before the first deal, adopt the server's view of any in-progress hand
  // (picked up on page load); after that, local state owns the view.
  const currentHand = hand ?? activeQuery.data ?? null;

  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth;
      const h = window.innerHeight;
      setScale(Math.min(1, (h - 240) / STAGE_H, (w - 50) / STAGE_W));
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

  // Central celebration: fires once, only when we watched the hand live.
  useEffect(() => {
    if (!currentHand) {
      sawActive.current = false;
      return;
    }
    if (currentHand.status !== "complete") {
      sawActive.current = true;
      return;
    }
    if (!sawActive.current) return;
    sawActive.current = false;
    const oc = currentHand.outcome;
    if (oc === "blackjack") {
      window.setTimeout(() => {
        sound.jackpot(2);
        setCoins(Date.now());
      }, 900);
    } else if (oc === "win") {
      window.setTimeout(() => {
        sound.bell();
        sound.bell(0.16);
        setCoins(Date.now());
      }, 550);
    } else if (oc === "push") {
      window.setTimeout(() => sound.push(), 550);
    } else {
      window.setTimeout(() => {
        sound.error();
        setShake(true);
        window.setTimeout(() => setShake(false), 420);
      }, 450);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentHand?.handId, currentHand?.status]);

  useEffect(() => {
    if (!coins) return;
    const t = window.setTimeout(() => setCoins(0), 2400);
    return () => window.clearTimeout(t);
  }, [coins]);

  const onDeal = () => {
    sound.unlock();
    sound.chipToss(2);
    sound.shuffle();
    deal.mutate(
      { betCredits: betStep, clientSeed: "" },
      {
        onSuccess: (res) => {
          if (res.replay) return;
          sawActive.current = true;
          setHand(res.hand);
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
    if (action === "double") sound.chipToss(3);
    act.mutate(
      { handId: currentHand.handId, action },
      {
        onSuccess: (res) => {
          if (res.replay) return;
          sawActive.current = true;
          setHand(res.hand);
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
  const playerCards = cardsOf(currentHand?.playerCards);
  const playerLost =
    currentHand?.status === "complete" &&
    (currentHand.outcome === "lose" || currentHand.outcome === "bust");
  const complete = currentHand?.status === "complete";

  // Dealer card renders per phase: up card always; live hole shimmering
  // face-down; on completion the hole flips over and any extra draws slide in.
  const dealerRender = (() => {
    if (dealerCards.length === 0 && !active) return null;
    if (active) {
      return (
        <>
          {dealerCards.map((code, i) => (
            <PlayingCard key={`d${i}`} code={code} scale={4} dealFrom="shoe" dealDelay={120 + i * 160} />
          ))}
          <span style={{ position: "relative" }}>
            <PlayingCard code="back" scale={4} dealFrom="shoe" dealDelay={280} />
            <span
              style={{
                position: "absolute",
                inset: 0,
                pointerEvents: "none",
                background:
                  "linear-gradient(115deg, transparent 32%, rgba(255,244,214,.22) 46%, transparent 60%)",
                animation: "backShimmer 1.8s ease-in-out infinite",
              }}
            />
          </span>
        </>
      );
    }
    return dealerCards.map((code, i) => {
      if (i === 1) return <PlayingCard key={`d1`} code={code} scale={4} flip dealDelay={260} />;
      if (i > 1)
        return <PlayingCard key={`d${i}`} code={code} scale={4} dealFrom="shoe" dealDelay={480 + (i - 2) * 170} />;
      return <PlayingCard key={`d${i}`} code={code} scale={4} />;
    });
  })();

  // Player cards: dealt after the dealer's, fanned slightly toward the hand.
  const playerFan = (i: number) => (i - (playerCards.length - 1) / 2) * 4;

  const outcome = complete ? currentHand?.outcome : undefined;
  const outcomeColor = outcome ? OUTCOME_COLORS[outcome] ?? "#8878b8" : "#8878b8";

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
          <div
            style={{
              transform: `perspective(1700px) rotateX(5deg) scale(${scale})`,
              transformOrigin: "center center",
            }}
          >
            {/* Wood rim + arc felt */}
            <div
              style={{
                width: FELT_W,
                height: FELT_H,
                position: "relative",
                padding: 14,
                borderRadius: "300px 300px 30px 30px",
                background: "linear-gradient(160deg, #7a4c26, #5c3a1e 45%, #38200e)",
                boxShadow:
                  "0 34px 90px rgba(0,0,0,.65), 0 0 70px rgba(255,45,149,.14), inset 0 2px 0 rgba(255,200,140,.35)",
                animation: shake ? "cabShake .4s linear" : undefined,
              }}
            >
              <div
                style={{
                  position: "absolute",
                  inset: 14,
                  borderRadius: "290px 290px 22px 22px",
                  background:
                    "radial-gradient(ellipse at 50% -12%, rgba(255,244,214,.16), transparent 52%), linear-gradient(#15503a, #0b352a)",
                  boxShadow:
                    "inset 0 0 0 3px rgba(34,232,255,.28), inset 0 0 110px rgba(0,0,0,.55)",
                  overflow: "hidden",
                }}
              >
                {/* Inner dashed inlay */}
                <div
                  style={{
                    position: "absolute",
                    inset: 26,
                    borderRadius: "270px 270px 16px 16px",
                    border: "2px dashed rgba(159,216,192,.22)",
                    pointerEvents: "none",
                  }}
                />

                {/* Felt printing: arced paytable */}
                <svg
                  width={980}
                  height={240}
                  viewBox="0 0 980 240"
                  style={{
                    position: "absolute",
                    left: "50%",
                    top: 128,
                    transform: "translateX(-50%)",
                    pointerEvents: "none",
                  }}
                >
                  <defs>
                    <path id="bjArc" d="M 70 205 Q 490 30 910 205" fill="none" />
                  </defs>
                  <text
                    fill="#22e8ff"
                    opacity={0.55}
                    style={{ filter: "blur(6px)", fontFamily: "var(--font-display)" }}
                    fontSize={40}
                    letterSpacing={10}
                  >
                    <textPath href="#bjArc" startOffset="50%" textAnchor="middle">
                      BLACKJACK PAYS 3 TO 2
                    </textPath>
                  </text>
                  <text
                    fill="#b8f4e4"
                    style={{ fontFamily: "var(--font-display)" }}
                    fontSize={40}
                    letterSpacing={10}
                  >
                    <textPath href="#bjArc" startOffset="50%" textAnchor="middle">
                      BLACKJACK PAYS 3 TO 2
                    </textPath>
                  </text>
                </svg>

                {/* Dealer shoe, top right */}
                <div
                  style={{
                    position: "absolute",
                    right: 84,
                    top: 44,
                    transform: "rotate(8deg)",
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    gap: 6,
                  }}
                >
                  <div style={{ position: "relative", width: 78, height: 96 }}>
                    <span style={{ position: "absolute", left: 8, top: 6, opacity: 0.5 }}>
                      <PlayingCard code="back" scale={3} />
                    </span>
                    <span style={{ position: "absolute", left: 4, top: 3, opacity: 0.75 }}>
                      <PlayingCard code="back" scale={3} />
                    </span>
                    <span style={{ position: "absolute", left: 0, top: 0 }}>
                      <PlayingCard code="back" scale={3} />
                    </span>
                  </div>
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 10,
                      letterSpacing: 3,
                      color: "#9fe8d0",
                      opacity: 0.8,
                    }}
                  >
                    SHOE
                  </span>
                </div>

                {/* Dealer row */}
                <div
                  style={{
                    position: "absolute",
                    left: "50%",
                    top: 42,
                    transform: "translateX(-50%)",
                    display: "flex",
                    alignItems: "center",
                    gap: 22,
                  }}
                >
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 14,
                      letterSpacing: 4,
                      color: "#9fe8d0",
                      textShadow: "0 0 12px rgba(34,232,255,.4)",
                    }}
                  >
                    DEALER
                  </span>
                  <div style={{ display: "flex", gap: 10, minHeight: 112 }}>
                    {dealerRender ?? <EmptySlot scale={4} />}
                  </div>
                  {complete && currentHand?.dealerTotal != null && (
                    <TotalBadge value={currentHand.dealerTotal} />
                  )}
                </div>

                {/* Betting circle */}
                <div
                  style={{
                    position: "absolute",
                    left: "50%",
                    top: 312,
                    transform: "translate(-50%, -50%)",
                    width: 120,
                    height: 120,
                    borderRadius: "50%",
                    border: currentHand
                      ? "3px solid rgba(255,45,149,.75)"
                      : "3px dashed rgba(255,45,149,.45)",
                    boxShadow: currentHand ? "0 0 26px rgba(255,45,149,.4)" : "none",
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                    justifyContent: "center",
                    gap: 4,
                  }}
                >
                  {currentHand ? (
                    <span key={currentHand.handId} style={{ animation: "potPop .3s ease-out both", display: "flex", flexDirection: "column", alignItems: "center", gap: 2 }}>
                      <ChipStack amount={currentHand.betCredits} color={CHIP_COLOR_BY_STEP[currentHand.betCredits] ?? "pink"} chipSize={40} />
                      <span
                        style={{
                          fontFamily: "var(--font-display)",
                          fontSize: 14,
                          color: "#ffb15c",
                          textShadow: "0 0 10px rgba(255,138,31,.7)",
                        }}
                      >
                        {currentHand.betCredits.toLocaleString()}
                      </span>
                    </span>
                  ) : (
                    <span
                      style={{
                        fontFamily: "var(--font-display)",
                        fontSize: 10,
                        letterSpacing: 2,
                        color: "rgba(255,45,149,.6)",
                      }}
                    >
                      PLACE
                      <br />
                      BET
                    </span>
                  )}
                </div>

                {/* Player row */}
                <div
                  style={{
                    position: "absolute",
                    left: "50%",
                    bottom: 30,
                    transform: "translateX(-50%)",
                    display: "flex",
                    alignItems: "flex-end",
                    gap: 26,
                  }}
                >
                  <div style={{ display: "flex", minHeight: 140, paddingLeft: 10 }}>
                    {playerCards.length === 0 && <EmptySlot scale={5} />}
                    {playerCards.map((code, i) => (
                      <span key={`p${i}`} style={{ marginLeft: i === 0 ? 0 : -16, zIndex: i }}>
                        <PlayingCard
                          code={code}
                          scale={5}
                          tilt={playerFan(i)}
                          dim={playerLost}
                          dealFrom="shoe"
                          dealDelay={playerCards.length <= 2 ? 430 + i * 170 : 0}
                        />
                      </span>
                    ))}
                  </div>
                  {playerCards.length > 0 && (
                    <TotalBadge value={currentHand?.playerTotal ?? 0} highlight={!playerLost} />
                  )}
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 14,
                      letterSpacing: 4,
                      color: "#ffd9a8",
                      textShadow: "0 0 12px rgba(255,138,31,.5)",
                    }}
                  >
                    YOU
                  </span>
                </div>

                {/* Outcome banner slams over the felt center */}
                {outcome && (
                  <div
                    style={{
                      position: "absolute",
                      left: "50%",
                      top: 308,
                      transform: "translate(-50%, -50%)",
                      zIndex: 6,
                      padding: "14px 34px",
                      background: "rgba(6,4,13,.88)",
                      border: `3px solid ${outcomeColor}`,
                      boxShadow: `0 0 34px ${outcomeColor}88`,
                      animation: `bigPop .45s cubic-bezier(.2,1.4,.4,1) ${outcome === "blackjack" ? 1.1 : 0.35}s both`,
                      display: "flex",
                      flexDirection: "column",
                      alignItems: "center",
                      gap: 2,
                    }}
                  >
                    <span
                      style={{
                        fontFamily: "var(--font-display)",
                        fontSize: outcome === "push" ? 20 : 30,
                        letterSpacing: 6,
                        color: outcomeColor,
                        textShadow: `0 0 18px ${outcomeColor}`,
                        whiteSpace: "nowrap",
                      }}
                    >
                      {OUTCOME_LABELS[outcome] ?? outcome.toUpperCase()}
                    </span>
                    {currentHand && currentHand.payoutCredits > 0 && (
                      <span
                        style={{
                          fontFamily: "var(--font-body)",
                          fontSize: 26,
                          color: "#ffb15c",
                          textShadow: "0 0 12px rgba(255,138,31,.8)",
                        }}
                      >
                        +{currentHand.payoutCredits.toLocaleString()} CREDITS
                      </span>
                    )}
                  </div>
                )}
              </div>
            </div>

            {/* Rail: chip tray + actions */}
            <div
              style={{
                marginTop: 22,
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 20,
                padding: "16px 26px",
                background: "linear-gradient(#1a0d2e, #120822)",
                border: "2px solid #35205c",
                boxShadow: "0 14px 40px rgba(0,0,0,.5), inset 0 1px 0 rgba(255,255,255,.05)",
              }}
            >
              {active ? (
                <div style={{ display: "flex", gap: 18, flex: 1, justifyContent: "center" }}>
                  <ActionPill label="HIT" hint="H" color="#22e8ff" disabled={busy} onClick={() => onAction("hit")} />
                  <ActionPill label="STAND" hint="S" color="#5fe08a" disabled={busy} onClick={() => onAction("stand")} />
                  <ActionPill
                    label="DOUBLE"
                    hint="D"
                    color="#ff8a1f"
                    disabled={busy || !currentHand?.canDouble}
                    onClick={() => onAction("double")}
                  />
                </div>
              ) : (
                <>
                  <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
                    <span
                      style={{
                        fontFamily: "var(--font-display)",
                        fontSize: 11,
                        letterSpacing: 3,
                        color: "#8878b8",
                      }}
                    >
                      CHIPS
                    </span>
                    <div style={{ display: "flex", gap: 12 }}>
                      {steps.map((s) => (
                        <Chip
                          key={s}
                          label={String(s)}
                          color={CHIP_COLOR_BY_STEP[s] ?? "pink"}
                          size={62}
                          selected={s === betStep}
                          disabled={busy}
                          title={`${s} credits`}
                          onClick={() => {
                            sound.unlock();
                            sound.click();
                            sound.chipClink();
                            setBetStep(s);
                          }}
                        />
                      ))}
                    </div>
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
                    <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#9fd8c0" }}>
                      bet <span style={{ color: "#ffb15c" }}>{betStep}</span>
                    </span>
                    <ActionPill label="DEAL" color="#5fe08a" big disabled={busy || !me} onClick={onDeal} />
                  </div>
                </>
              )}
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
          <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
            {me && (
              <Avatar
                userId={me.user.id}
                displayName={me.user.displayName}
                avatarPreset={me.user.avatarPreset}
                avatarVersion={me.user.avatarVersion}
                size={20}
                ring="#35205c"
              />
            )}
            {me ? (me.user.isGuest ? "guest" : me.user.displayName) : "····"}
            {me ? ` · balance ${me.balanceCredits.toLocaleString()}` : ""}
            {hand?.doubled ? " · DOUBLED" : ""}
            {fair.data ? ` · seed ${fair.data.serverSeedHash.slice(0, 8)}… · nonce ${fair.data.nonce}` : ""}
          </span>
          <span>H = HIT · S = STAND · D = DOUBLE · {bets.data?.bets.length ?? 0} BETS THIS SESSION</span>
        </div>
      </div>

      {/* Coin fountain on a win */}
      {coins > 0 && (
        <div
          style={{
            position: "absolute",
            inset: 0,
            zIndex: 8,
            pointerEvents: "none",
            overflow: "hidden",
          }}
        >
          {Array.from({ length: 30 }, (_, i) => (
            <span
              key={`${coins}-${i}`}
              style={{
                position: "absolute",
                bottom: "8%",
                left: `${((i * 137 + 23) % 96) + 2}%`,
                width: 16,
                height: 16,
                borderRadius: "50%",
                background:
                  "radial-gradient(circle at 34% 30%, #fff8d8, #ff8a1f 55%, #a1450a)",
                boxShadow: "0 0 12px rgba(255,138,31,.8)",
                animation: `coinFly ${1500 + ((i * 61) % 700)}ms cubic-bezier(.3,.05,.6,1) ${(i * 83) % 900}ms forwards`,
              }}
            />
          ))}
        </div>
      )}

      {note && (
        <div
          role="status"
          style={{
            position: "absolute",
            top: 84,
            left: "50%",
            transform: "translateX(-50%)",
            zIndex: 9,
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

function EmptySlot({ scale }: { scale: number }) {
  return (
    <div
      style={{
        width: 20 * scale,
        height: 28 * scale,
        borderRadius: 4,
        border: "2px dashed rgba(159,216,192,.28)",
        background: "rgba(0,0,0,.18)",
      }}
    />
  );
}

function TotalBadge({ value, highlight }: { value: number; highlight?: boolean }) {
  return (
    <span
      style={{
        padding: "6px 14px",
        background: "rgba(6,4,13,.8)",
        border: `2px solid ${highlight ? "#5fe08a" : "#35205c"}`,
        boxShadow: highlight ? "0 0 16px rgba(95,224,138,.4)" : "none",
        fontFamily: "var(--font-body)",
        fontSize: 30,
        lineHeight: 1,
        color: highlight ? "#c8ffd9" : "#cfe8dc",
        textShadow: highlight ? "0 0 12px rgba(95,224,138,.7)" : "0 0 10px rgba(34,232,255,.5)",
        animation: "potPop .25s ease-out both",
      }}
    >
      {value}
    </span>
  );
}

function ActionPill({
  label,
  hint,
  color,
  big,
  disabled,
  onClick,
}: {
  label: string;
  hint?: string;
  color: string;
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
        background: "rgba(6,4,13,.72)",
        color,
        fontFamily: "var(--font-display)",
        fontSize: big ? 20 : 17,
        letterSpacing: 3,
        padding: big ? "16px 44px" : "15px 32px",
        cursor: disabled ? "wait" : "pointer",
        opacity: disabled ? 0.4 : 1,
        boxShadow: big ? `0 0 22px ${color}55` : "none",
        transition: "transform .1s ease, box-shadow .12s ease, background .12s ease",
      }}
      onMouseEnter={(e) => {
        if (disabled) return;
        e.currentTarget.style.background = color;
        e.currentTarget.style.color = "#06040d";
        e.currentTarget.style.boxShadow = `0 0 26px ${color}aa`;
        e.currentTarget.style.transform = "translateY(-2px)";
      }}
      onMouseLeave={(e) => {
        if (disabled) return;
        e.currentTarget.style.background = "rgba(6,4,13,.72)";
        e.currentTarget.style.color = color;
        e.currentTarget.style.boxShadow = big ? `0 0 22px ${color}55` : "none";
        e.currentTarget.style.transform = "translateY(0)";
      }}
      onMouseDown={(e) => {
        if (!disabled) e.currentTarget.style.transform = "translateY(1px) scale(.97)";
      }}
      onMouseUp={(e) => {
        e.currentTarget.style.transform = "translateY(-2px)";
      }}
    >
      {label}
      {hint ? (
        <span
          style={{
            marginLeft: 10,
            padding: "2px 7px",
            border: `1px solid ${color}88`,
            fontSize: 11,
            opacity: 0.8,
          }}
        >
          {hint}
        </span>
      ) : null}
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
