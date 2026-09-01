"use client";

import { Suspense, useState } from "react";
import { useSearchParams } from "next/navigation";
import PixelSymbol from "./PixelSymbol";
import { deriveGrid, type VerifyResult } from "@/lib/verify";
import { VERIFY_PAYS, VERIFY_WEIGHTS } from "@/lib/verify";
import { SYMBOL_NAMES } from "@/lib/symbols";

function Verifier() {
  const params = useSearchParams();
  const [serverSeed, setServerSeed] = useState(params.get("server") ?? "");
  const [clientSeed, setClientSeed] = useState(params.get("client") ?? "");
  const [nonce, setNonce] = useState(params.get("nonce") ?? "0");
  const [bet, setBet] = useState(params.get("bet") ?? "10");
  const [result, setResult] = useState<VerifyResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const compute = async () => {
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      const r = await deriveGrid({
        serverSeedHex: serverSeed,
        clientSeed,
        nonce: Number.parseInt(nonce, 10),
      });
      if (Number.isNaN(r)) throw new Error("bad input");
      setResult(r);
    } catch (e) {
      setError(e instanceof Error ? e.message : "verification failed");
    } finally {
      setBusy(false);
    }
  };

  const payout = result ? result.payoutMultiplier * Number.parseInt(bet, 10) : 0;

  const field =
    "w-full border-4 border-slate bg-ink p-2 font-body text-xl text-bone focus:border-cyan focus:outline-none";

  return (
    <div className="flex flex-col gap-6 lg:flex-row">
      <div className="flex flex-col gap-4 lg:w-[480px]">
        <label className="flex flex-col gap-2">
          <span className="font-display text-base text-haze">
            SERVER SEED (HEX, REVEALED ON ROTATION)
          </span>
          <input
            className={`${field} break-all`}
            value={serverSeed}
            onChange={(e) => setServerSeed(e.target.value)}
            spellCheck={false}
            placeholder="64 hex characters"
          />
        </label>
        <label className="flex flex-col gap-2">
          <span className="font-display text-base text-haze">CLIENT SEED</span>
          <input
            className={field}
            value={clientSeed}
            onChange={(e) => setClientSeed(e.target.value)}
            spellCheck={false}
          />
        </label>
        <div className="flex gap-4">
          <label className="flex flex-1 flex-col gap-2">
            <span className="font-display text-base text-haze">NONCE</span>
            <input
              className={field}
              value={nonce}
              onChange={(e) => setNonce(e.target.value)}
              inputMode="numeric"
            />
          </label>
          <label className="flex flex-1 flex-col gap-2">
            <span className="font-display text-base text-haze">BET</span>
            <select
              className={field}
              value={bet}
              onChange={(e) => setBet(e.target.value)}
            >
              {[5, 10, 25, 50, 100].map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
        </div>
        <button
          type="button"
          onClick={compute}
          disabled={busy || serverSeed.length === 0}
          className="border-4 border-plum bg-cyan p-2 font-display text-base text-ink hover:bg-bone disabled:cursor-not-allowed disabled:border-slate disabled:bg-stone disabled:text-haze"
        >
          {busy ? "COMPUTING…" : "RECOMPUTE OUTCOME"}
        </button>
        {error && (
          <p role="alert" className="m-0 border-4 border-ember p-2 text-ember">
            {error}
          </p>
        )}
      </div>

      {result && (
        <div className="flex-1 border-4 border-slate bg-ink p-4">
          <h2 className="m-0 mb-4 font-display text-base text-cyan">
            RECOMPUTED OUTCOME
          </h2>
          <div className="inline-grid grid-cols-3 gap-2 bg-slate p-2">
            {result.grid.flat().map((symbolIndex, i) => (
              <div
                key={i}
                className="pixelated flex h-16 w-16 items-center justify-center border-4 border-slate bg-ink"
              >
                <PixelSymbol index={symbolIndex} scale={8} />
              </div>
            ))}
          </div>
          <p className="mt-4 text-xl">
            {result.winningLines.length > 0 ? (
              <span className="text-mint">
                WIN · {result.winningLines.length} line
                {result.winningLines.length > 1 ? "s" : ""} ·{" "}
                {result.payoutMultiplier}× bet = {payout} credits
              </span>
            ) : (
              <span className="text-haze">NO WINNING LINES</span>
            )}
          </p>
          <p className="mt-2 text-base text-haze">
            grid {JSON.stringify(result.grid)} · lines{" "}
            [{result.winningLines.join(", ")}]
          </p>
          <p className="mt-4 border-t-4 border-slate pt-4 text-base text-haze">
            Weights [{VERIFY_WEIGHTS.join(", ")}] · pays [
            {VERIFY_PAYS.join(", ")}] · symbols {SYMBOL_NAMES.join(", ")} ·
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
