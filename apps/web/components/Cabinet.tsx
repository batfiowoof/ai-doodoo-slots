"use client";

import { useState } from "react";
import PixelSymbol from "./PixelSymbol";
import { useFairCurrent, useSession } from "@/lib/api";
import { SYMBOL_NAMES } from "@/lib/symbols";

// Static decorative grid until the first spin arrives in phase 7.
const IDLE_GRID = [0, 1, 2, 3, 4, 5, 6, 7, 0];

const BULB_LIT = [true, true, false, true, true, false, true, true];

export default function Cabinet() {
  const session = useSession();
  const fair = useFairCurrent(session.isSuccess);
  const [betStep, setBetStep] = useState(10);

  const me = session.data;
  const balance = me?.balanceCredits;
  const betSteps = [5, 10, 25, 50, 100];

  return (
    <section
      aria-label="Slot machine"
      className="w-[488px] max-w-full shrink-0 border-4 border-stone bg-rust p-4 shadow-hard"
    >
      {/* Marquee */}
      <div className="border-4 border-plum bg-magenta p-4 text-center">
        <h2 className="m-0 font-display text-2xl text-white shadow-marquee">
          RETRO SLOTS
        </h2>
        <div className="mt-4 flex justify-center gap-2">
          {BULB_LIT.map((lit, i) => (
            <span
              key={i}
              className={`inline-block h-2 w-2 ${lit ? "bg-amber" : "bg-brass"}`}
            />
          ))}
        </div>
      </div>

      {/* Reel window */}
      <div className="my-4 border-8 border-stone bg-plum p-2">
        <div className="grid grid-cols-3 gap-2 bg-slate p-2">
          {IDLE_GRID.map((symbolIndex, i) => (
            <div
              key={i}
              className="pixelated flex aspect-square items-center justify-center border-4 border-slate bg-ink"
            >
              <PixelSymbol index={symbolIndex} />
            </div>
          ))}
        </div>
      </div>

      {/* Deck */}
      <div className="border-4 border-stone bg-ink p-4">
        <div className="flex items-center justify-between gap-4">
          <span className="font-display text-base text-haze">CREDITS</span>
          <span
            data-testid="credits"
            className="font-body text-4xl leading-none text-amber"
          >
            {balance === undefined ? "····" : balance.toLocaleString()}
          </span>
        </div>

        <div className="mt-4 flex gap-2">
          {betSteps.map((step) => (
            <button
              key={step}
              type="button"
              onClick={() => setBetStep(step)}
              className={`flex-1 border-4 p-2 font-display text-base ${
                betStep === step
                  ? "border-bone bg-stone text-white"
                  : "border-slate bg-shadow text-haze"
              }`}
            >
              {step}
            </button>
          ))}
        </div>

        <button
          type="button"
          disabled
          title="Spins arrive in phase 7"
          className="mt-4 block w-full cursor-not-allowed border-4 border-slate bg-stone p-4 font-display text-2xl text-haze shadow-spin-disabled"
        >
          SPIN
        </button>
      </div>

      {/* Coin door */}
      <div className="mt-4 border-4 border-stone bg-slate p-2 text-center text-base text-haze">
        <span className="text-mint">PLAY FOR FUN</span> — NO CASH VALUE · NO
        DEPOSITS · NO CASH-OUT
      </div>

      {/* Session / fairness status line */}
      <div className="mt-2 text-center text-base text-haze">
        {session.isError && (
          <span className="text-ember">casino unreachable</span>
        )}
        {session.isSuccess && me && (
          <span>
            <span className="text-bone">{me.user.displayName}</span>
            {me.user.isGuest ? " · guest" : ""} ·{" "}
            {fair.data ? (
              <span title={fair.data.serverSeedHash}>
                seed {fair.data.serverSeedHash.slice(0, 8)}… nonce{" "}
                {fair.data.nonce}
              </span>
            ) : (
              "seed ····"
            )}
          </span>
        )}
      </div>

      <p className="sr-only">
        Symbols: {SYMBOL_NAMES.join(", ")}. All outcomes are decided by the
        server and provably fair.
      </p>
    </section>
  );
}
