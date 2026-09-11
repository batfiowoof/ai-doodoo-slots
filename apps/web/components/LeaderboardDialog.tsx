"use client";

// THE FLOOR'S FINEST — leaderboard dialog. Metric tabs × window tabs,
// glowing podium for the top three, the caller's own rank pinned below.

import { useState } from "react";
import NeonDialog from "./NeonDialog";
import { Avatar } from "./Avatar";
import NameTag from "./NameTag";
import { fmtCredits, useLeaderboard, type LeaderboardMetric, type LeaderboardWindow } from "@/lib/social";
import { sound } from "@/lib/sound";
import type { LeaderboardEntry } from "@/lib/types";

const GOLD = "#ff8a1f";

const METRICS: { key: LeaderboardMetric; label: string }[] = [
  { key: "biggest_win", label: "BIGGEST WIN" },
  { key: "net_profit", label: "NET PROFIT" },
  { key: "wagered", label: "WAGERED" },
];

const WINDOWS: { key: LeaderboardWindow; label: string }[] = [
  { key: "daily", label: "TODAY" },
  { key: "weekly", label: "WEEK" },
  { key: "all", label: "ALL-TIME" },
];

export default function LeaderboardDialog({
  open,
  onClose,
  meUserId,
  onOpenPlayer,
}: {
  open: boolean;
  onClose: () => void;
  meUserId: number | null;
  onOpenPlayer: (userId: number) => void;
}) {
  const [metric, setMetric] = useState<LeaderboardMetric>("biggest_win");
  const [window, setWindow] = useState<LeaderboardWindow>("weekly");
  const query = useLeaderboard(metric, window, open);
  const entries = query.data?.entries ?? [];
  const top = entries.slice(0, 3);
  const rest = entries.slice(3);
  const me = query.data?.me ?? null;

  return (
    <NeonDialog open={open} onClose={onClose} title="THE FLOOR'S FINEST" accent={GOLD} width={760}>
      <div style={{ padding: "16px 22px 22px", display: "flex", flexDirection: "column", gap: 16 }}>
        {/* tabs */}
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {METRICS.map((m) => (
            <Tab
              key={m.key}
              active={metric === m.key}
              label={m.label}
              onClick={() => {
                sound.click();
                setMetric(m.key);
              }}
            />
          ))}
          <span style={{ flex: 1 }} />
          {WINDOWS.map((w) => (
            <Tab
              key={w.key}
              small
              active={window === w.key}
              label={w.label}
              onClick={() => {
                sound.click();
                setWindow(w.key);
              }}
            />
          ))}
        </div>

        {query.isLoading ? (
          <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#8878b8", textAlign: "center", padding: "40px 0" }}>
            Counting the chips…
          </span>
        ) : entries.length === 0 ? (
          <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#8878b8", textAlign: "center", padding: "40px 0" }}>
            No settled bets in this window yet. The podium waits.
          </span>
        ) : (
          <>
            {/* podium */}
            <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "center", gap: 18, padding: "10px 0 4px" }}>
              {[top[1], top[0], top[2]].map((e, i) =>
                e ? (
                  <PodiumSpot key={e.userId} entry={e} place={i === 1 ? 1 : i === 0 ? 2 : 3} onOpenPlayer={onOpenPlayer} meUserId={meUserId} />
                ) : (
                  <div key={i} style={{ width: 170 }} />
                )
              )}
            </div>

            {/* rows 4..20 */}
            <div style={{ display: "flex", flexDirection: "column" }}>
              {rest.map((e, i) => (
                <Row key={e.userId} entry={e} rank={i + 4} onOpenPlayer={onOpenPlayer} meUserId={meUserId} />
              ))}
            </div>
          </>
        )}

        {/* own rank */}
        {me && (
          <div
            style={{
              position: "sticky",
              bottom: 0,
              display: "flex",
              alignItems: "center",
              gap: 12,
              padding: "10px 14px",
              background: "#1d1036",
              border: `2px solid ${GOLD}`,
              boxShadow: `0 0 20px ${GOLD}66`,
            }}
          >
            <span style={{ fontFamily: "var(--font-display)", fontSize: 15, color: GOLD, minWidth: 48 }}>#{me.rank}</span>
            <span style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 2, color: "#dcd4f5" }}>YOU</span>
            <span style={{ flex: 1 }} />
            <span style={{ fontFamily: "var(--font-display)", fontSize: 15, color: "#5fe08a" }}>
              {fmtCredits(me.value)} cr
            </span>
          </div>
        )}
      </div>
    </NeonDialog>
  );
}

function Tab({
  active,
  label,
  small,
  onClick,
}: {
  active: boolean;
  label: string;
  small?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        border: `2px solid ${active ? GOLD : "#35205c"}`,
        background: active ? "#2a1406" : "transparent",
        color: active ? GOLD : "#8878b8",
        fontFamily: "var(--font-display)",
        fontSize: small ? 11 : 12,
        letterSpacing: 1,
        padding: small ? "6px 10px" : "8px 14px",
        cursor: "pointer",
        boxShadow: active ? `0 0 14px ${GOLD}44` : "none",
      }}
    >
      {label}
    </button>
  );
}

const PLACES: Record<number, { h: number; label: string; color: string }> = {
  1: { h: 96, label: "1ST", color: GOLD },
  2: { h: 64, label: "2ND", color: "#c0c8e8" },
  3: { h: 44, label: "3RD", color: "#c98a4b" },
};

function PodiumSpot({
  entry,
  place,
  onOpenPlayer,
  meUserId,
}: {
  entry: LeaderboardEntry;
  place: number;
  onOpenPlayer: (userId: number) => void;
  meUserId: number | null;
}) {
  const p = PLACES[place];
  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 6, width: 170 }}>
      <Avatar
        userId={entry.userId}
        displayName={entry.displayName}
        avatarPreset={entry.avatarPreset}
        avatarVersion={entry.avatarVersion}
        size={place === 1 ? 64 : 50}
        ring={p.color}
        glow={place === 1}
      />
      <button
        type="button"
        onClick={() => onOpenPlayer(entry.userId)}
        style={{
          background: "none",
          border: "none",
          padding: 0,
          cursor: "pointer",
          fontFamily: "var(--font-display)",
          fontSize: 12,
          letterSpacing: 1,
          color: meUserId === entry.userId ? GOLD : "#dcd4f5",
          maxWidth: 160,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
      >
        <NameTag
          displayName={entry.displayName}
          title={entry.title}
          nameEffect={entry.nameEffect}
          titleClassName="text-[7px] px-0.5"
        />
      </button>
      <span style={{ fontFamily: "var(--font-display)", fontSize: 13, color: "#5fe08a" }}>{fmtCredits(entry.value)} cr</span>
      <div
        style={{
          width: "100%",
          height: p.h,
          background: "linear-gradient(180deg, #1d1036, #120a24)",
          border: `2px solid ${p.color}`,
          borderBottom: "none",
          display: "grid",
          placeItems: "center",
          animation: place === 1 ? "podiumGlow 1.6s ease-in-out infinite" : undefined,
        }}
      >
        <span style={{ fontFamily: "var(--font-display)", fontSize: 18, color: p.color, textShadow: `0 0 10px ${p.color}` }}>
          {p.label}
        </span>
      </div>
    </div>
  );
}

function Row({
  entry,
  rank,
  onOpenPlayer,
  meUserId,
}: {
  entry: LeaderboardEntry;
  rank: number;
  onOpenPlayer: (userId: number) => void;
  meUserId: number | null;
}) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 10,
        padding: "5px 8px",
        borderBottom: "1px solid #241640",
        background: meUserId === entry.userId ? "rgba(255,138,31,.08)" : "transparent",
      }}
    >
      <span style={{ fontFamily: "var(--font-display)", fontSize: 12, color: "#8878b8", minWidth: 30 }}>#{rank}</span>
      <Avatar
        userId={entry.userId}
        displayName={entry.displayName}
        avatarPreset={entry.avatarPreset}
        avatarVersion={entry.avatarVersion}
        size={26}
      />
      <button
        type="button"
        onClick={() => onOpenPlayer(entry.userId)}
        style={{
          background: "none",
          border: "none",
          padding: 0,
          cursor: "pointer",
          fontFamily: "var(--font-display)",
          fontSize: 12,
          letterSpacing: 1,
          color: meUserId === entry.userId ? GOLD : "#dcd4f5",
        }}
      >
        <NameTag
          displayName={entry.displayName}
          title={entry.title}
          nameEffect={entry.nameEffect}
          titleClassName="text-[7px] px-0.5"
        />
      </button>
      <span style={{ flex: 1 }} />
      <span style={{ fontFamily: "var(--font-display)", fontSize: 12, color: "#5fe08a" }}>{fmtCredits(entry.value)} cr</span>
    </div>
  );
}
