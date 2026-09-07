"use client";

import { useState } from "react";
import Link from "next/link";
import { PlayError, useMinesActive, useMinesCashOut, useMinesReveal, useMinesStart, useGames, useSession } from "@/lib/api";
import type { MinesRoundView } from "@/lib/api";
import { sound } from "@/lib/sound";
import BetInput from "@/components/BetInput";
import Backdrop from "@/components/Backdrop";

// MINES — 5×5 field, hidden mines. Start a round, flip safe tiles to climb
// the multiplier, cash out before you hit one. Red neon; gems pop, bombs
// end the night.

const ACCENT = "#f2643d";
const GOOD = "#5fe08a";
const CREDITS = "#ff8a1f";
const BET_STEPS = [5, 10, 25, 50, 100];
const MINE_PRESETS = [1, 3, 5, 10, 24];
const TILES = 25;

export default function MinesScreen({ gameId }: { gameId: string }) {
  const session = useSession();
  const games = useGames();
  const info = games.data?.find((g) => g.id === gameId);
  const activeQ = useMinesActive(!!session.data);
  const start = useMinesStart();
  const reveal = useMinesReveal();
  const cashOut = useMinesCashOut();

  const [bet, setBet] = useState(10);
  const [mineCount, setMineCount] = useState(3);
  const [justHit, setJustHit] = useState<number | null>(null);
  const [note, setNote] = useState<string | null>(null);

  const round: MinesRoundView | null = activeQ.data ?? null;
  const active = round?.status === "active";
  const finished = round && round.status !== "active" ? round : null;
  const balance = session.data?.balanceCredits;

  const revealedSet = new Set(round?.revealed ?? []);
  const finishedMineSet = new Set(finished?.mines ?? []);

  const doStart = () => {
    if (!session.data || start.isPending) return;
    if (!balance || balance < bet) {
      sound.error();
      setNote("Not enough credits.");
      return;
    }
    sound.unlock();
    sound.chipClink();
    setNote(null);
    setJustHit(null);
    start.mutate({ betCredits: bet, mineCount });
  };

  const doReveal = (tile: number) => {
    if (!round || !active || reveal.isPending || revealedSet.has(tile)) return;
    sound.unlock();
    reveal.mutate(
      { roundId: round.roundId, tile },
      {
        onSuccess: (res) => {
          if (res.round.status === "busted") {
            setJustHit(tile);
            sound.explosion();
            setNote("BOOM — mine hit. Stake lost.");
          } else {
            sound.flipCard();
            sound.winTick(res.round.revealed.length);
          }
        },
        onError: () => sound.error(),
      },
    );
  };

  const doCashOut = () => {
    if (!round || !active || !round.cashable || cashOut.isPending) return;
    sound.unlock();
    cashOut.mutate(
      { roundId: round.roundId },
      {
        onSuccess: (res) => {
          const won = res.round.payoutCredits - res.round.betCredits;
          if (won >= res.round.betCredits * 4) sound.bigWin();
          else sound.jackpot(2);
          setNote(`Cashed out +${won.toLocaleString()} CR`);
        },
        onError: () => sound.error(),
      },
    );
  };

  const multiplierNow = active || finished ? round!.multiplier : 1;
  const profitNow = active ? Math.floor(round!.betCredits * round!.multiplier) - round!.betCredits : 0;

  const tileStyle = (tile: number): React.CSSProperties => {
    const isRevealed = revealedSet.has(tile);
    const isMine = finishedMineSet.has(tile);
    const isHit = justHit === tile;
    const revealedMine = finished && isMine;
    return {
      height: 74,
      borderRadius: 10,
      cursor: active && !isRevealed ? "pointer" : "default",
      border: `2px solid ${isHit ? ACCENT : isRevealed ? GOOD : revealedMine ? "#5c4f80" : "#35205c"}`,
      background: isHit ? ACCENT : isRevealed ? "rgba(95,224,138,.12)" : revealedMine ? "rgba(242,100,61,.1)" : "#0d0619",
      boxShadow: isHit ? `0 0 26px ${ACCENT}` : isRevealed ? "0 0 12px rgba(95,224,138,.25)" : "none",
      fontFamily: "var(--font-display)",
      fontSize: 26,
      color: isRevealed ? GOOD : revealedMine ? ACCENT : "#4a3a72",
      animation: isHit || isRevealed ? "potPop .4s ease" : undefined,
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
    };
  };

  return (
    <main className="crt" style={{ minHeight: "100vh", background: "#06040d", padding: "18px 24px", position: "relative", overflow: "hidden" }}>
      <Backdrop />
      <div style={{ maxWidth: 1000, margin: "0 auto", position: "relative" }}>
        <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 14 }}>
          <Link href="/" style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 2, color: "#8878b8", textDecoration: "none" }}>
            ◂ LOBBY
          </Link>
          <h1 style={{ fontFamily: "var(--font-display)", fontSize: 30, letterSpacing: 6, color: ACCENT, textShadow: `0 0 22px ${ACCENT}aa`, margin: 0 }}>
            💣 MINES
          </h1>
          <div style={{ textAlign: "right" }}>
            <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 3, color: "#5c4f80" }}>BALANCE</div>
            <div style={{ fontFamily: "var(--font-body)", fontSize: 28, color: CREDITS, textShadow: "0 0 14px rgba(255,138,31,.5)" }} data-testid="mines-balance">
              {(balance ?? 0).toLocaleString()}
            </div>
          </div>
        </header>

        <div style={{ display: "grid", gridTemplateColumns: "auto 1fr", gap: 18, alignItems: "start", justifyContent: "center" }}>
          {/* field */}
          <div
            data-testid="mines-grid"
            style={{
              border: `2px solid ${ACCENT}44`,
              background: "linear-gradient(180deg, #0d0619, #170c2b)",
              padding: 16,
              display: "grid",
              gridTemplateColumns: "repeat(5, 74px)",
              gap: 8,
            }}
          >
            {Array.from({ length: TILES }, (_, tile) => {
              const isRevealed = revealedSet.has(tile);
              const label = isRevealed ? "◆" : finishedMineSet.has(tile) ? "✸" : "?";
              return (
                <button
                  key={tile}
                  type="button"
                  data-testid={`mines-tile-${tile}`}
                  style={tileStyle(tile)}
                  disabled={!active || isRevealed}
                  onClick={() => doReveal(tile)}
                >
                  {label}
                </button>
              );
            })}
          </div>

          {/* console */}
          <div style={{ display: "flex", flexDirection: "column", gap: 14, width: 330 }}>
            {!active && (
              <div style={{ border: "2px solid #35205c", background: "#0d0619", padding: "14px 16px", display: "flex", flexDirection: "column", gap: 12 }}>
                <div>
                  <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80", marginBottom: 6 }}>MINES</div>
                  <div style={{ display: "flex", gap: 8 }}>
                    {MINE_PRESETS.map((m) => (
                      <button
                        key={m}
                        type="button"
                        data-testid={`mines-count-${m}`}
                        onClick={() => {
                          sound.click();
                          setMineCount(m);
                        }}
                        style={{
                          flex: 1,
                          fontFamily: "var(--font-display)",
                          fontSize: 14,
                          padding: "9px 0",
                          cursor: "pointer",
                          border: `2px solid ${mineCount === m ? ACCENT : "#35205c"}`,
                          background: mineCount === m ? "#2d0a1e" : "#06040d",
                          color: mineCount === m ? ACCENT : "#8878b8",
                          boxShadow: mineCount === m ? `0 0 14px ${ACCENT}44` : "none",
                        }}
                      >
                        {m}
                      </button>
                    ))}
                  </div>
                </div>
                <BetInput value={bet} onChange={setBet} steps={BET_STEPS} min={info?.minBet ?? 1} max={info?.maxBet ?? 10000} balance={balance} accent={ACCENT} testIdPrefix="mines-bet" />
                <button
                  type="button"
                  data-testid="mines-start"
                  disabled={start.isPending || !session.data}
                  onClick={doStart}
                  style={{
                    minHeight: 64,
                    fontFamily: "var(--font-display)",
                    fontSize: 21,
                    letterSpacing: 4,
                    cursor: start.isPending ? "wait" : "pointer",
                    border: `3px solid ${ACCENT}`,
                    background: "linear-gradient(180deg, #2d0a1e, #0d0619)",
                    color: ACCENT,
                    textShadow: `0 0 18px ${ACCENT}`,
                    boxShadow: `0 0 26px ${ACCENT}44, inset 0 0 22px ${ACCENT}22`,
                    animation: "radHubGlow 2.2s ease-in-out infinite alternate",
                  }}
                >
                  {start.isPending ? "PLACING…" : `BET ${bet} CR`}
                </button>
              </div>
            )}

            {active && round && (
              <div style={{ border: "2px solid #35205c", background: "#0d0619", padding: "14px 16px", display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={{ display: "flex", gap: 10 }}>
                  <div style={{ flex: 1, border: "1px solid #35205c", background: "#06040d", padding: "8px 10px" }}>
                    <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80" }}>CURRENT</div>
                    <div data-testid="mines-multiplier" style={{ fontFamily: "var(--font-body)", fontSize: 26, color: "#ece6ff" }}>
                      {round.multiplier.toFixed(2)}×
                    </div>
                  </div>
                  <div style={{ flex: 1, border: "1px solid #35205c", background: "#06040d", padding: "8px 10px" }}>
                    <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80" }}>NEXT</div>
                    <div style={{ fontFamily: "var(--font-body)", fontSize: 26, color: GOOD }}>
                      {round.nextMultiplier?.toFixed(2)}×
                    </div>
                  </div>
                </div>
                <div style={{ border: "1px solid #35205c", background: "#06040d", padding: "8px 10px" }}>
                  <div style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#5c4f80" }}>CASHOUT VALUE</div>
                  <div style={{ fontFamily: "var(--font-body)", fontSize: 26, color: CREDITS }}>
                    {Math.floor(round.betCredits * round.multiplier).toLocaleString()} CR
                    <span style={{ fontSize: 17, color: GOOD }}> (+{profitNow.toLocaleString()})</span>
                  </div>
                </div>
                <button
                  type="button"
                  data-testid="mines-cashout"
                  disabled={!round.cashable || cashOut.isPending}
                  onClick={doCashOut}
                  style={{
                    minHeight: 66,
                    fontFamily: "var(--font-display)",
                    fontSize: 22,
                    letterSpacing: 4,
                    cursor: round.cashable ? "pointer" : "default",
                    border: `3px solid ${round.cashable ? GOOD : "#35205c"}`,
                    background: round.cashable ? "linear-gradient(180deg, #0a2d18, #06040d)" : "#0d0619",
                    color: round.cashable ? GOOD : "#5c4f80",
                    textShadow: round.cashable ? `0 0 18px ${GOOD}` : "none",
                    boxShadow: round.cashable ? `0 0 26px ${GOOD}44` : "none",
                    animation: round.cashable ? "radHubGlow 2s ease-in-out infinite alternate" : undefined,
                  }}
                >
                  {round.revealed.length === 0 ? "REVEAL A TILE FIRST" : `CASH OUT ${Math.floor(round.betCredits * multiplierNow).toLocaleString()}`}
                </button>
              </div>
            )}

            {finished && (
              <div
                data-testid="mines-result"
                style={{
                  border: `2px solid ${finished.status === "cashed" ? GOOD : ACCENT}`,
                  background: "#0d0619",
                  padding: "14px 16px",
                  animation: "bannerIn .4s ease",
                }}
              >
                <div style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 3, color: finished.status === "cashed" ? GOOD : ACCENT }}>
                  {finished.status === "cashed" ? `CASHED ${finished.multiplier.toFixed(2)}×` : "BOOM"}
                </div>
                <div style={{ fontFamily: "var(--font-body)", fontSize: 26, color: finished.status === "cashed" ? GOOD : ACCENT }}>
                  {finished.status === "cashed"
                    ? `+${(finished.payoutCredits - finished.betCredits).toLocaleString()} CR`
                    : `−${finished.betCredits.toLocaleString()} CR`}
                </div>
              </div>
            )}

            {note && (
              <div role="status" style={{ fontFamily: "var(--font-body)", fontSize: 18, color: "#8878b8" }}>
                {note}
              </div>
            )}
            {reveal.isError && (
              <div role="alert" style={{ fontFamily: "var(--font-body)", fontSize: 18, color: ACCENT }}>
                {(reveal.error as PlayError)?.message ?? "reveal failed"}
              </div>
            )}
            <div style={{ fontFamily: "var(--font-body)", fontSize: 15, color: "#5c4f80" }}>
              {mineCount} mines · each safe tile raises the multiplier · cash out anytime · provably fair
              {info ? ` · RTP ${(info.theoreticalRtp * 100).toFixed(2)}%` : ""}
            </div>
          </div>
        </div>
      </div>
    </main>
  );
}
