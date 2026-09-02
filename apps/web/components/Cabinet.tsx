"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import ReelWindow, { type SpinRequest } from "./ReelWindow";
import { PlayError, useFairCurrent, useGames, usePlay, useSession } from "@/lib/api";
import { sound } from "@/lib/sound";
import { SYMBOL_NAMES } from "@/lib/symbols";

const CELEBRATE_MS = 2600;

type SpinPhase = "idle" | "awaiting" | "spinning" | "celebrating";

interface Celebration {
  payout: number;
  lines: number[];
  scatterSymbols: number[];
}

/** Reel cell size: integer pixel sizes only, chosen by game + viewport. */
function useCellSize(cols: number): number {
  const base = cols <= 3 ? 112 : cols === 4 ? 96 : 96;
  const [small, setSmall] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia("(max-width: 860px)");
    const apply = () => setSmall(mq.matches);
    apply();
    mq.addEventListener("change", apply);
    return () => mq.removeEventListener("change", apply);
  }, []);
  // Shrink one integer sprite step on small screens (96 -> 64, 112 -> 80 is
  // not a 32-multiple, so 3-col games drop to 64+16=80).
  if (!small) return base;
  return base >= 112 ? 80 : 64;
}

/** Payout counter ticking digit by digit; steps, never easing. */
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
    const timer = setInterval(() => {
      setTicks((t) => {
        sound.winTick(t);
        return t + 1;
      });
    }, 80);
    return () => clearInterval(timer);
  }, [reduced, done]);

  const shown = reduced ? payout : Math.min(payout, ticks * step);

  return (
    <div className="m-0 mt-4 border-4 border-mint bg-ink p-2 text-center font-display text-xl text-mint">
      WIN {shown.toLocaleString()}
    </div>
  );
}

export default function Cabinet({ gameId }: { gameId: string }) {
  const session = useSession();
  const fair = useFairCurrent(session.isSuccess);
  const games = useGames();
  const play = usePlay();

  const info = games.data?.find((g) => g.id === gameId);
  const pt = info?.paytable;
  const cols = pt?.reels ?? 5;
  const lines = pt?.lines ?? [];
  const icons = pt?.icons ?? [];
  const symbolCount = pt?.symbols.length ?? 8;
  const betSteps = pt?.betSteps ?? [5, 10, 25, 50, 100];
  const mode = pt?.mode ?? "lines";

  const [betStep, setBetStep] = useState(10);
  const [phase, setPhase] = useState<SpinPhase>("idle");
  const [error, setError] = useState<string | null>(null);
  const [spin, setSpin] = useState<SpinRequest | null>(null);
  const [celebrate, setCelebrate] = useState<Celebration | null>(null);
  const [muted, setMuted] = useState(false);
  const spinIdRef = useRef(0);
  const celebrateTimer = useRef<number | null>(null);

  const cellPx = useCellSize(cols);
  const cabinetWidth = `${cols * cellPx + (cols - 1) * 8 + 88}px`;

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
        setCelebrate({
          payout: current.payout,
          lines: current.winningLines,
          scatterSymbols: current.scatterSymbols,
        });
        sound.winJingle(
          current.winningLines.length + current.scatterSymbols.length,
        );
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
    sound.unlock();
    sound.click();
    sound.spinStart();
    if (celebrateTimer.current !== null) clearTimeout(celebrateTimer.current);
    setError(null);
    setCelebrate(null);
    setPhase("awaiting"); // disable on request, not on animation start
    play.mutate(
      { gameId, betCredits: betStep, clientSeed: fair.data.clientSeed },
      {
        onSuccess: (res) => {
          spinIdRef.current += 1;
          setSpin({
            id: spinIdRef.current,
            grid: res.outcome.grid,
            winningLines: res.outcome.winningLines ?? [],
            scatterSymbols: (res.outcome.scatterWins ?? []).map((s) => s.symbol),
            payout: res.payoutCredits,
          });
          setPhase("spinning");
        },
        onError: (err) => {
          setPhase("idle");
          sound.error();
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

  const toggleMute = () => {
    const next = !muted;
    setMuted(next);
    sound.setMuted(next);
    if (!next) {
      sound.unlock();
      sound.click();
    }
  };

  const busy = phase === "awaiting" || phase === "spinning" || play.isPending;

  const statusText =
    phase === "awaiting"
      ? "WAITINGâ€¦"
      : phase === "spinning"
        ? "SPINNINGâ€¦"
        : null;

  const iconFor = (symbolIndex: number) => icons[symbolIndex];

  return (
    <section
      aria-label={`Slot machine: ${info?.name ?? gameId}`}
      style={{ width: cabinetWidth }}
      className={`max-w-full shrink-0 border-4 border-stone bg-rust p-4 shadow-hard ${
        celebrate ? "cabinet-shake" : ""
      }`}
    >
      {/* Marquee with chasing bulbs */}
      <div className="border-4 border-plum bg-magenta p-4 text-center">
        <h2 className="m-0 font-display text-3xl text-white shadow-marquee">
          {(info?.name ?? "SLOTS").toUpperCase()}
        </h2>
        <div className="mt-4 flex justify-center gap-2">
          {Array.from({ length: 18 }, (_, i) => (
            <span
              key={i}
              className="bulb inline-block h-2 w-2"
              style={{ animationDelay: `${(i * 1200) / 18}ms` }}
            />
          ))}
        </div>
      </div>

      {/* Reel window â€” server outcome, animated as theater */}
      <ReelWindow
	key={gameId + String(cols) + String(symbolCount)}
        spin={spin}
        winningLines={celebrate?.lines ?? null}
        scatterSymbols={celebrate?.scatterSymbols ?? null}
        cellPx={cellPx}
        cols={cols}
        lines={lines}
        iconFor={iconFor}
        symbolCount={symbolCount}
        onAllSettled={handleAllSettled}
      />

      {/* Deck */}
      <div className="border-4 border-stone bg-ink p-4">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-4">
            <span className="font-display text-base text-haze">CREDITS</span>
            <span
              data-testid="credits"
              className="font-body text-4xl leading-none text-amber"
            >
              {balance === undefined ? "Â·Â·Â·Â·" : balance.toLocaleString()}
            </span>
          </div>
          <button
            type="button"
            onClick={toggleMute}
            className="border-4 border-slate bg-shadow px-2 py-1 font-display text-base text-haze"
          >
            {muted ? "SND OFF" : "SND ON"}
          </button>
        </div>

        <div className="mt-4 flex gap-2">
          {betSteps.map((step) => (
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
              : "btn-spin-idle border-plum bg-magenta shadow-spin hover:translate-y-[2px] hover:shadow-[0_6px_0_var(--color-rust)] active:translate-y-[8px] active:shadow-none"
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
        <span className="text-mint">PLAY FOR FUN</span> â€” NO CASH VALUE Â·{" "}
        {mode === "scatter" ? "SCATTER PAYS" : `${lines.length} LINES`}
      </div>

      {/* Session / fairness status line */}
      <div className="mt-2 text-center text-base text-haze">
        {session.isError && (
          <span className="text-ember">casino unreachable</span>
        )}
        {session.isSuccess && me && (
          <span>
            <span className="text-bone">{me.user.displayName}</span>
            {me.user.isGuest ? " Â· guest" : ""} Â·{" "}
            {fair.data ? (
              <span title={fair.data.serverSeedHash}>
                seed {fair.data.serverSeedHash.slice(0, 8)}â€¦ nonce{" "}
                {fair.data.nonce}
              </span>
            ) : (
              "seed Â·Â·Â·Â·"
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
