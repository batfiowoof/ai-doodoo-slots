"use client";

import PixelSymbol from "./PixelSymbol";
import { useGames } from "@/lib/api";

function paysChips(pays: Record<string, number>): string {
  const entries = Object.entries(pays ?? {}).sort(
    ([a], [b]) => Number(a) - Number(b),
  );
  return entries.map(([count, pay]) => `${count}→×${pay}`).join("  ");
}

/** Paytable body — rendered inside the overlay panel host. */
export default function Paytable({ gameId }: { gameId: string }) {
  const games = useGames();
  const slots = games.data?.find((g) => g.id === gameId);
  const pt = slots?.paytable;

  if (games.isLoading) {
    return <p style={{ margin: 0, fontSize: 20, color: "#8878b8" }}>loading…</p>;
  }
  if (games.isError) {
    return <p style={{ margin: 0, fontSize: 20, color: "#ff8a1f" }}>paytable unavailable</p>;
  }
  if (!slots || !pt) return null;

  return (
    <div>
      {pt.mode === "scatter" && (
        <p style={{ margin: "0 0 12px", fontSize: 20, color: "#ff8a1f" }}>
          scatter pays — N anywhere on the grid → × bet
        </p>
      )}

      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 14,
          paddingBottom: 10,
          borderBottom: "1px solid #241640",
          fontFamily: "var(--font-display)",
          fontSize: 10,
          letterSpacing: 2,
          color: "#8878b8",
        }}
      >
        <span style={{ width: 56 }}>SYMBOL</span>
        <span style={{ flex: 1 }}>NAME</span>
        <span style={{ width: 70, textAlign: "right" }}>WEIGHT</span>
        <span style={{ width: 250, textAlign: "right" }}>PAYS × BET</span>
      </div>

      {pt.symbols.map((symbol, i) => {
        const chips = paysChips(symbol.pays);
        return (
          <div
            key={symbol.name}
            style={{
              display: "flex",
              alignItems: "center",
              gap: 14,
              padding: "10px 0",
              borderBottom: "1px solid #1b1030",
            }}
          >
            <div
              style={{
                width: 56,
                height: 56,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                background: "#06040d",
                boxShadow: "inset 0 0 0 1px #241640",
                flexShrink: 0,
              }}
            >
              <PixelSymbol index={i} icon={pt.icons[i]} scale={1} />
            </div>
            <span
              style={{
                flex: 1,
                fontFamily: "var(--font-body)",
                fontSize: 24,
                textTransform: "uppercase",
                letterSpacing: 2,
                color: "#ece6ff",
              }}
            >
              {symbol.name}
            </span>
            <span style={{ width: 70, textAlign: "right", fontSize: 20, color: "#8878b8" }}>
              w{symbol.weight}
            </span>
            <span
              style={{
                width: 250,
                textAlign: "right",
                fontFamily: "var(--font-body)",
                fontSize: 24,
                color: chips ? "#22e8ff" : "#5c4f80",
              }}
            >
              {chips || "—"}
            </span>
          </div>
        );
      })}

      <p style={{ margin: "16px 0 0", fontSize: 20, color: "#ff2d95" }}>
        {pt.mode === "scatter"
          ? "scatter game · pays × total bet"
          : `${pt.paylines} paylines · wins count from the left · pays × total bet · lines stack`}{" "}
        · RTP {(slots.theoreticalRtp * 100).toFixed(2)}%
      </p>
    </div>
  );
}
