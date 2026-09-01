"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import ReelWindow, { type SpinRequest } from "./ReelWindow";
import { PlayError, useFairCurrent, usePlay, useSession } from "@/lib/api";
import { SYMBOL_NAMES } from "@/lib/symbols";

const BULB_LIT = [true, true, false, true, true, false, true, true];
const BET_STEPS = [5, 10, 25, 50, 100];
const CELEBRATE_MS = 2600;

type SpinPhase = "idle" | "awaiting" | "spinning" | "celebrating";

interface Celebration {
  payout: number;
  lines: number[];
}

/** Payout counter ticking digit by digit; steps, never easing. Skips the
 * tick and shows the full amount when the user prefers reduced motion. */
function WinBanner({ payout }: { payout: number }) {
  const [reduced] = useState(
    () =>
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  const [ticks, setTicks] = useState(0);
  const step = Math.max(1, Math.ceil(payout / 16));
  const done = ticks * step >= payout;

  useEffect(() => {
    if (reduced || done) return;
    const timer = setInterval(() => setTicks((t) => t + 1), 80);
    return () => clearInterval(timer);
  }, [reduced, done]);

  const shown = reduced ? payout : Math.min(payout, ticks * step);

  return (
    <div className="m-0 mt-4 border-4 border-mint bg-ink p-2 text-center font-display text-xl text-mint">
      WIN {shown.toLocaleString()}
    </div>
  );
}

export default function Cabinet() {
  const session = useSession();
  const fair = useFairCurrent(session.isSuccess);
  const play = usePlay();

  const [betStep, setBetStep] = useState(10);
  const [phase, setPhase] = useState<SpinPhase>("idle");
  const [error, setError] = useState<string | null>(null);
  const [spin, setSpin] = useState<SpinRequest | null>(null);
  const [celebrate, setCelebrate] = useState<Celebration | null>(null);
  const spinIdRef = useRef(0);
  const celebrateTimer = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (celebrateTimer.current !== null) clearTimeout(celebrateTimer.current);
    };
  }, []);

  const me = session.data;
  const balance = me?.balanceCredits;
  const canAfford = balance !== undefined && balance >= betStep;

  const handleAllSettled = useCallback(() => {
    setPhase((prev) => (prev === "spinning" ? "celebrating" : prev));
    setSpin((current) => {
      if (current && current.payout > 0) {
        setCelebrate({ payout: current.payout, lines: current.winningLines });
        if (celebrateTimer.current !== null) clearTimeout(celebrateTimer.current);
        celebrateTimer.current = window.setTimeout(() => {
          setCelebrate(null);
          setPhase("idle");
        }, CELEBRATE_MS);
      } else {
        setPhase("idle");
      }
      return current;
    });
  }, []);

  const doSpin = () => {
    if (phase !== "idle" || play.isPending || !fair.data) return;
    if (celebrateTimer.current !== null) clearTimeout(celebrateTimer.current);
    setError(null);
    setCelebrate(null);
    setPhase("awaiting"); // disable on request, not on animation start
    play.mutate(
      { betCredits: betStep, clientSeed: fair.data.clientSeed },
      {
        onSuccess: (res) => {
          // Outcome known — now play the theater over it.
          spinIdRef.current += 1;
          setSpin({
            id: spinIdRef.current,
            grid: res.outcome.grid,
            winningLines: res.outcome.winningLines ?? [],
            payout: res.payoutCredits,
          });
          setPhase("spinning");
        },
        onError: (err) => {
          setPhase("idle");
          if (err instanceof PlayError) {
            switch (err.status) {
              case 402:
                setError("INSUFFICIENT CREDITS");
                break;
              case 429:
                setError("SLOW DOWN");
                break;
              case 403:
                setError("BETTING BLOCKED");
                break;
              default:
                setError("SPIN REJECTED");
            }
          } else {
            setError("CASINO UNREACHABLE");
          }
        },
      },
    );
  };

  const busy = phase === "awaiting" || phase === "spinning" || play.isPending;

  const statusText =
    phase === "awaiting"
      ? "WAITING…"
      : phase === "spinning"
        ? "SPINNING…"
        : null;

  return (
    <section
      aria-label="Slot machine"
      className={`w-[488px] max-w-full shrink-0 border-4 border-stone bg-rust p-4 shadow-hard ${
        celebrate ? "cabinet-shake" : ""
      }`}
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

      {/* Reel window — server outcome, animated as theater */}
      <ReelWindow
        spin={spin}
        winningLines={celebrate?.lines ?? null}
        onAllSettled={handleAllSettled}
      />

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
          {BET_STEPS.map((step) => (
            <button
              key={step}
              type="button"
              onClick={() => setBetStep(step)}
              disabled={busy}
              className={`flex-1 border-4 p-2 font-display text-base ${
                betStep === step
                  ? "border-bone bg-stone text-white"
                  : "border-slate bg-shadow text-haze"
              } ${busy ? "cursor-not-allowed opacity-60" : ""}`}
            >
              {step}
            </button>
          ))}
        </div>

        <button
          type="button"
          onClick={doSpin}
          disabled={busy || !canAfford || session.isPending || fair.isLoading}
          title={!canAfford && balance !== undefined ? "Insufficient credits" : undefined}
          className={`mt-4 block w-full border-4 p-4 font-display text-2xl text-white ${
            busy || !canAfford
              ? "cursor-not-allowed border-slate bg-stone text-haze shadow-spin-disabled"
              : "border-plum bg-magenta shadow-spin hover:translate-y-[2px] hover:shadow-[0_6px_0_var(--color-rust)] active:translate-y-[8px] active:shadow-none"
          }`}
        >
          {statusText ?? "SPIN"}
        </button>

        {celebrate && <WinBanner payout={celebrate.payout} />}

        {error && (
          <p
            role="alert"
            className={`m-0 mt-4 border-4 p-2 text-center font-display text-base ${
              error === "INSUFFICIENT CREDITS"
                ? "border-ember bg-ink text-ember"
                : "border-brass bg-ink text-amber"
            }`}
          >
            {error}
          </p>
        )}
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
