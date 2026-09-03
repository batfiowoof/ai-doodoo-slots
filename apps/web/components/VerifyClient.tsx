"use client";

import { Suspense, useState } from "react";
import { useSearchParams } from "next/navigation";
import PixelCard from "./PixelCard";
import PixelSymbol from "./PixelSymbol";
import { deriveBlackjackDeck, deriveGrid, type BJVerifyResult, type VerifyResult } from "@/lib/verify";
import { VERIFY_PAYS3, VERIFY_PAYS4, VERIFY_PAYS5, VERIFY_WEIGHTS } from "@/lib/verify";
import { SYMBOL_NAMES } from "@/lib/symbols";

const FIELD: React.CSSProperties = {
  width: "100%",
  boxSizing: "border-box",
  background: "#06040d",
  border: "2px solid #35205c",
  color: "#ece6ff",
  fontFamily: "var(--font-body)",
  fontSize: 22,
  padding: "10px 12px",
};

function Verifier() {
  const params = useSearchParams();
  const [game, setGame] = useState<"slots" | "blackjack">("slots");
  const [serverSeed, setServerSeed] = useState(params.get("server") ?? "");
  const [clientSeed, setClientSeed] = useState(params.get("client") ?? "");
  const [nonce, setNonce] = useState(params.get("nonce") ?? "0");
  const [bet, setBet] = useState(params.get("bet") ?? "10");
  const [result, setResult] = useState<VerifyResult | null>(null);
  const [bjResult, setBjResult] = useState<BJVerifyResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const compute = async () => {
    setBusy(true);
    setError(null);
    setResult(null);
    setBjResult(null);
    try {
      if (game === "blackjack") {
        const r = await deriveBlackjackDeck({
          serverSeedHex: serverSeed,
          clientSeed,
          nonce: Number.parseInt(nonce, 10),
        });
        setBjResult(r);
      } else {
        const r = await deriveGrid({
          serverSeedHex: serverSeed,
          clientSeed,
          nonce: Number.parseInt(nonce, 10),
        });
        if (Number.isNaN(r.payoutMultiplier)) throw new Error("bad input");
        setResult(r);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "verification failed");
    } finally {
      setBusy(false);
    }
  };

  const payout = result ? result.payoutMultiplier * Number.parseInt(bet, 10) : 0;

  return (
    <div>
      <p style={{ margin: "0 0 16px", fontSize: 20, color: "#8878b8" }}>
        Every outcome derives from HMAC-SHA256(serverSeed, clientSeed + &quot;:&quot; +
        nonce). This recomputes in your browser with WebCrypto and never calls
        the API — that independence is what makes the check meaningful.
      </p>

      <div style={{ display: "flex", gap: 8, marginBottom: 14 }}>
        {(["slots", "blackjack"] as const).map((g) => (
          <button
            key={g}
            type="button"
            onClick={() => {
              setGame(g);
              setResult(null);
              setBjResult(null);
              setError(null);
            }}
            style={{
              border: `2px solid ${game === g ? "#ff2d95" : "#35205c"}`,
              background: game === g ? "#ff2d95" : "transparent",
              color: game === g ? "#06040d" : "#8878b8",
              fontFamily: "var(--font-display)",
              fontSize: 11,
              letterSpacing: 2,
              padding: "9px 14px",
              cursor: "pointer",
            }}
          >
            {g === "slots" ? "SLOT SPIN" : "BLACKJACK DEAL"}
          </button>
        ))}
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 14, maxWidth: 560 }}>
        <label style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 10,
              letterSpacing: 2,
              color: "#8878b8",
            }}
          >
            SERVER SEED (HEX, REVEALED ON ROTATION)
          </span>
          <input
            style={FIELD}
            value={serverSeed}
            onChange={(e) => setServerSeed(e.target.value)}
            spellCheck={false}
            placeholder="64 hex characters"
          />
        </label>
        <label style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 10,
              letterSpacing: 2,
              color: "#8878b8",
            }}
          >
            CLIENT SEED
          </span>
          <input
            style={FIELD}
            value={clientSeed}
            onChange={(e) => setClientSeed(e.target.value)}
            spellCheck={false}
          />
        </label>
        <div style={{ display: "flex", gap: 14 }}>
          <label style={{ flex: 1, display: "flex", flexDirection: "column", gap: 6 }}>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 10,
                letterSpacing: 2,
                color: "#8878b8",
              }}
            >
              NONCE
            </span>
            <input
              style={FIELD}
              value={nonce}
              onChange={(e) => setNonce(e.target.value)}
              inputMode="numeric"
            />
          </label>
          <label style={{ flex: 1, display: "flex", flexDirection: "column", gap: 6 }}>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 10,
                letterSpacing: 2,
                color: "#8878b8",
              }}
            >
              BET
            </span>
            <input
              style={FIELD}
              value={bet}
              onChange={(e) => setBet(e.target.value)}
              inputMode="numeric"
            />
          </label>
        </div>
        <button
          type="button"
          onClick={compute}
          disabled={busy || serverSeed.length === 0}
          style={{
            border: "2px solid #22e8ff",
            background: "#0b2a33",
            color: "#22e8ff",
            fontFamily: "var(--font-display)",
            fontSize: 13,
            letterSpacing: 2,
            padding: 12,
            cursor: busy ? "wait" : "pointer",
            opacity: busy ? 0.6 : 1,
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.background = "#22e8ff";
            e.currentTarget.style.color = "#06040d";
            e.currentTarget.style.boxShadow = "0 0 22px rgba(34,232,255,.6)";
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.background = "#0b2a33";
            e.currentTarget.style.color = "#22e8ff";
            e.currentTarget.style.boxShadow = "none";
          }}
        >
          {busy ? "COMPUTING…" : "RECOMPUTE OUTCOME"}
        </button>
        {error && (
          <p role="alert" style={{ margin: 0, border: "2px solid #ff8a1f", padding: 10, fontSize: 20, color: "#ff8a1f" }}>
            {error}
          </p>
        )}
      </div>

      {bjResult && (
        <div
          style={{
            marginTop: 18,
            borderTop: "1px solid #1b1030",
            paddingTop: 18,
          }}
        >
          <h2
            style={{
              margin: "0 0 12px",
              fontFamily: "var(--font-display)",
              fontSize: 13,
              letterSpacing: 2,
              color: "#22e8ff",
            }}
          >
            RECOMPUTED DECK · DRAW ORDER
          </h2>
          <p style={{ margin: "0 0 12px", fontSize: 20, color: "#8878b8" }}>
            Index 0,1 = your cards · 2,3 = dealer (3 is the hole) · 4,5,6… =
            hits and dealer draws, in order.
          </p>
          <div style={{ display: "flex", gap: 6, flexWrap: "wrap", maxWidth: 640 }}>
            {bjResult.deck.slice(0, 8).map((code, i) => (
              <div
                key={i}
                style={{
                  display: "flex",
                  flexDirection: "column",
                  alignItems: "center",
                  gap: 4,
                }}
              >
                <PixelCard code={code} scale={2} />
                <span style={{ fontSize: 15, color: "#5c4f80" }}>#{i}</span>
              </div>
            ))}
          </div>
          <p style={{ marginTop: 12, fontSize: 20, color: "#8878b8", wordBreak: "break-all" }}>
            full deck {bjResult.deck.join(" ")}
          </p>
          <p
            style={{
              marginTop: 12,
              borderTop: "1px solid #1b1030",
              paddingTop: 12,
              fontSize: 20,
              color: "#5c4f80",
            }}
          >
            Fisher-Yates with rejection sampling (v &lt; 2^32 − 2^32 mod n) ·
            first draws u32 [{bjResult.u32s.slice(0, 4).join(", ")}…]
          </p>
        </div>
      )}

      {result && (
        <div
          style={{
            marginTop: 18,
            borderTop: "1px solid #1b1030",
            paddingTop: 18,
          }}
        >
          <h2
            style={{
              margin: "0 0 12px",
              fontFamily: "var(--font-display)",
              fontSize: 13,
              letterSpacing: 2,
              color: "#22e8ff",
            }}
          >
            RECOMPUTED OUTCOME
          </h2>
          <div style={{ display: "flex", gap: 4, flexWrap: "wrap", maxWidth: 5 * 48 + 16 }}>
            {result.grid.flat().map((symbolIndex, i) => (
              <div
                key={i}
                style={{
                  width: 48,
                  height: 48,
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  background: "#06040d",
                  boxShadow: "inset 0 0 0 1px #241640",
                }}
              >
                <PixelSymbol index={symbolIndex} scale={1} />
              </div>
            ))}
          </div>
          <p style={{ marginTop: 12, fontSize: 24 }}>
            {result.winningLines.length > 0 ? (
              <span style={{ color: "#22e8ff" }}>
                WIN · {result.winningLines.length}{" "}
                {result.winningLines.length === 1 ? "line" : "lines"} ·{" "}
                {result.payoutMultiplier}× bet = {payout} credits
              </span>
            ) : (
              <span style={{ color: "#5c4f80" }}>NO WINNING LINES</span>
            )}
          </p>
          <p style={{ marginTop: 8, fontSize: 20, color: "#8878b8" }}>
            grid {JSON.stringify(result.grid)} · lines [{result.winningLines.join(", ")}]
          </p>
          <p
            style={{
              marginTop: 12,
              borderTop: "1px solid #1b1030",
              paddingTop: 12,
              fontSize: 20,
              color: "#5c4f80",
            }}
          >
            Weights [{VERIFY_WEIGHTS.join(", ")}] · pays [
            {VERIFY_PAYS3.join(" / ")} · {VERIFY_PAYS4.join(" / ")} ·{" "}
            {VERIFY_PAYS5.join(" / ")}] · symbols {SYMBOL_NAMES.join(", ")} ·
            first draws u32 [{result.u32s.slice(0, 4).join(", ")}…]
          </p>
        </div>
      )}
    </div>
  );
}

export default function VerifyClient() {
  return (
    <Suspense fallback={null}>
      <Verifier />
    </Suspense>
  );
}
