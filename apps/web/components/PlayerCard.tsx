"use client";

// Player card: click any name in chat / roster / leaderboard. Shows the
// public profile plus its biggest-win stat, and (for anyone but yourself)
// the tip pad. Tips ride the dock's socket; the ledger does the rest.

import { useState } from "react";
import NeonDialog from "./NeonDialog";
import { Avatar } from "./Avatar";
import NameTag from "./NameTag";
import { FRAMES, themeClass } from "@/lib/cosmetics";
import { fmtCredits, usePlayerProfile } from "@/lib/social";
import { sound } from "@/lib/sound";

const ACCENT = "#22e8ff";

export default function PlayerCard({
  userId,
  me,
  onClose,
  onTip,
}: {
  userId: number | null;
  me: { id: number; displayName: string; balanceCredits: number } | null;
  onClose: () => void;
  onTip: (toUserId: number, credits: number) => void;
}) {
  const query = usePlayerProfile(userId);
  const p = query.data;
  const [tipCredits, setTipCredits] = useState("50");
  const [tipNote, setTipNote] = useState("");

  const isSelf = p != null && me != null && p.id === me.id;

  const sendTip = () => {
    if (!p || !me) return;
    const v = Number(tipCredits);
    if (!Number.isFinite(v) || v < 1) {
      setTipNote("Enter a real amount.");
      sound.error();
      return;
    }
    if (v > me.balanceCredits) {
      setTipNote("Not enough credits — hit the deposit pill.");
      sound.error();
      return;
    }
    setTipNote("");
    sound.unlock();
    onTip(p.id, Math.trunc(v));
  };

  return (
    <NeonDialog open={userId != null} onClose={onClose} title="PLAYER FILE" accent={ACCENT} width={430}>
      {query.isLoading || !p ? (
        <span style={{ display: "block", padding: "48px 0", fontFamily: "var(--font-body)", fontSize: 20, color: "#8878b8", textAlign: "center" }}>
          Pulling the file…
        </span>
      ) : (
        <div style={{ padding: "20px 24px 24px", display: "flex", flexDirection: "column", gap: 16 }}>
          <div
            style={{ display: "flex", alignItems: "center", gap: 14, padding: "12px 12px", border: "1px solid #241640" }}
            className={themeClass(p.profileTheme)}
          >
            <span className={`inline-block rounded-full ${p.avatarFrame ? FRAMES[p.avatarFrame]?.className ?? "" : ""}`}>
              <Avatar
                userId={p.id}
                displayName={p.displayName}
                avatarPreset={p.avatarPreset}
                avatarVersion={p.avatarVersion}
                size={72}
                ring={ACCENT}
                glow
              />
            </span>
            <div style={{ display: "flex", flexDirection: "column", gap: 4, minWidth: 0 }}>
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 18,
                  letterSpacing: 2,
                  color: "#dcd4f5",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                <NameTag displayName={p.displayName} title={p.title} nameEffect={p.nameEffect} titleClassName="text-[8px] px-1" />
                {p.role !== "player" ? <span style={{ color: "#ffb15c" }}> ★</span> : ""}
              </span>
              <span style={{ fontFamily: "var(--font-body)", fontSize: 17, color: "#8878b8" }}>
                {p.role === "admin" ? "HOUSE MANAGEMENT" : p.role === "moderator" ? "FLOOR SECURITY" : "PLAYER"}
              </span>
              <span style={{ fontFamily: "var(--font-body)", fontSize: 16, color: "#5a4a88" }}>
                ON THE FLOOR SINCE {new Date(p.createdAt).toLocaleDateString()}
              </span>
            </div>
          </div>

          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              padding: "12px 16px",
              border: "1px solid #241640",
              background: "#120a24",
            }}
          >
            <span style={{ fontFamily: "var(--font-display)", fontSize: 12, letterSpacing: 2, color: "#8878b8" }}>
              BIGGEST SINGLE WIN
            </span>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 15, color: "#5fe08a" }}>
              {fmtCredits(p.stats?.biggestWin ?? 0)} cr
            </span>
          </div>

          {isSelf ? (
            <span style={{ fontFamily: "var(--font-body)", fontSize: 18, color: "#5a4a88", textAlign: "center" }}>
              That&apos;s you, hotshot.
            </span>
          ) : (
            <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              <span style={{ fontFamily: "var(--font-display)", fontSize: 12, letterSpacing: 3, color: "#ffb15c" }}>
                SEND A TIP
              </span>
              <div style={{ display: "flex", gap: 8 }}>
                {[50, 100, 500].map((v) => (
                  <button
                    key={v}
                    type="button"
                    onClick={() => {
                      sound.click();
                      setTipCredits(String(v));
                    }}
                    style={{
                      flex: 1,
                      border: "2px solid #ff8a1f",
                      background: Number(tipCredits) === v ? "#ff8a1f" : "#2a1406",
                      color: Number(tipCredits) === v ? "#06040d" : "#ffb15c",
                      fontFamily: "var(--font-display)",
                      fontSize: 13,
                      padding: "8px 0",
                      cursor: "pointer",
                    }}
                  >
                    {fmtCredits(v)}
                  </button>
                ))}
              </div>
              <div style={{ display: "flex", gap: 8 }}>
                <input
                  value={tipCredits}
                  onChange={(e) => setTipCredits(e.target.value.replace(/[^0-9]/g, "").slice(0, 6))}
                  style={{
                    flex: 1,
                    background: "#0c0718",
                    border: "1px solid #35205c",
                    color: "#ffb15c",
                    fontFamily: "var(--font-display)",
                    fontSize: 16,
                    padding: "8px 12px",
                    outline: "none",
                  }}
                />
                <button
                  type="button"
                  onClick={sendTip}
                  style={{
                    border: "2px solid #5fe08a",
                    background: "#0b2a1a",
                    color: "#5fe08a",
                    fontFamily: "var(--font-display)",
                    fontSize: 12,
                    letterSpacing: 1,
                    padding: "0 16px",
                    cursor: "pointer",
                  }}
                  onMouseEnter={(e) => {
                    e.currentTarget.style.background = "#5fe08a";
                    e.currentTarget.style.color = "#06040d";
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.background = "#0b2a1a";
                    e.currentTarget.style.color = "#5fe08a";
                  }}
                >
                  TIP 💸
                </button>
              </div>
              {tipNote && (
                <span role="status" style={{ fontFamily: "var(--font-body)", fontSize: 17, color: "#ff8a1f", textAlign: "center", animation: "notePop .2s ease-out both" }}>
                  {tipNote}
                </span>
              )}
            </div>
          )}
        </div>
      )}
    </NeonDialog>
  );
}
