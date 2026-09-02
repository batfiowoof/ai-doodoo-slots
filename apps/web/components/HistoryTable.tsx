"use client";

import Link from "next/link";
import { useBets, useGames, useSession } from "@/lib/api";

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/** Spin history body — rendered inside the overlay panel host. */
export default function HistoryTable() {
  const session = useSession();
  const games = useGames();
  const bets = useBets();

  const nameOf = new Map<string, string>();
  for (const g of games.data ?? []) nameOf.set(g.id, g.name);

  if (!session.isSuccess) {
    return <p style={{ margin: 0, padding: 40, textAlign: "center", fontSize: 20, color: "#5c4f80" }}>····</p>;
  }
  if (bets.isLoading) {
    return <p style={{ margin: 0, fontSize: 20, color: "#8878b8" }}>loading…</p>;
  }
  if (bets.isError) {
    return <p style={{ margin: 0, fontSize: 20, color: "#ff8a1f" }}>history unavailable</p>;
  }

  const rows = bets.data?.bets ?? [];
  if (rows.length === 0) {
    return (
      <p
        style={{
          margin: 0,
          padding: 40,
          textAlign: "center",
          fontFamily: "var(--font-display)",
          fontSize: 14,
          letterSpacing: 3,
          color: "#5c4f80",
        }}
      >
        NO SPINS YET
      </p>
    );
  }

  return (
    <div>
      {rows.map((bet) => {
        const outcome = bet.outcome;
        const lines = outcome?.winningLines?.length ?? 0;
        const scatter = outcome?.scatterWins?.length ?? 0;
        const result =
          outcome?.grid == null
            ? "—"
            : bet.payoutCredits > 0
              ? scatter > 0
                ? `${scatter} scatter`
                : `${lines} ${lines === 1 ? "line" : "lines"}`
              : "no win";
        return (
          <Link
            key={bet.id}
            href={`/verify?client=${encodeURIComponent(bet.clientSeed)}&nonce=${bet.nonce}&bet=${bet.betCredits}`}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 16,
              padding: "10px 0",
              borderBottom: "1px solid #1b1030",
              color: "inherit",
            }}
          >
            <span style={{ width: 90, fontSize: 20, color: "#8878b8", flexShrink: 0 }}>
              {formatTime(bet.createdAt)}
            </span>
            <span style={{ width: 130, fontSize: 20, color: "#5c4f80", flexShrink: 0 }}>
              {(nameOf.get(bet.gameId) ?? bet.gameId).toUpperCase()}
            </span>
            <span style={{ flex: 1, fontSize: 20, color: "#ece6ff" }}>{result}</span>
            <span style={{ width: 80, textAlign: "right", fontSize: 20, color: "#8878b8", flexShrink: 0 }}>
              bet {bet.betCredits}
            </span>
            <span
              style={{
                width: 110,
                textAlign: "right",
                fontSize: 24,
                color: bet.payoutCredits > 0 ? "#22e8ff" : "#5c4f80",
                flexShrink: 0,
              }}
            >
              {bet.payoutCredits > 0 ? `+${bet.payoutCredits.toLocaleString()}` : "0"}
            </span>
            <span style={{ width: 70, textAlign: "right", fontSize: 18, color: "#5c4f80", flexShrink: 0 }}>
              n{bet.nonce}
            </span>
          </Link>
        );
      })}
    </div>
  );
}
