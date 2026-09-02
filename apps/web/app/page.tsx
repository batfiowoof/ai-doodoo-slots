"use client";

import { useState } from "react";
import Link from "next/link";
import PixelSymbol from "@/components/PixelSymbol";
import { sound } from "@/lib/sound";
import { useDeposit, useGames, useSession } from "@/lib/api";

/** The arcade floor: one card per machine, plus the deposit kiosk. */
export default function GameMenu() {
  const session = useSession();
  const games = useGames();
  const deposit = useDeposit();
  const [depositNote, setDepositNote] = useState<string | null>(null);

  const balance = session.data?.balanceCredits;

  const doDeposit = () => {
    sound.click();
    deposit.mutate(undefined, {
      onSuccess: (data) => {
        setDepositNote(
          data.claimed
            ? `+${data.amountCredits.toLocaleString()} CREDITS`
            : "ALREADY CLAIMED — BACK IN UNDER AN HOUR",
        );
        window.setTimeout(() => setDepositNote(null), 2600);
      },
      onError: () => setDepositNote("KIOSK UNAVAILABLE"),
    });
  };

  return (
    <div className="flex min-h-screen flex-col px-4 pt-4 pb-10">
      <header className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-baseline gap-4">
          <h1 className="font-display text-2xl text-magenta">RETRO CASINO</h1>
          <Link href="/verify" className="font-display text-base text-cyan">
            VERIFY ▸
          </Link>
        </div>
        <div className="flex items-center gap-4">
          <span
            data-testid="credits"
            className="border-4 border-stone bg-shadow px-3 py-1 font-body text-2xl leading-none text-amber"
          >
            {balance === undefined ? "····" : balance.toLocaleString()}
          </span>
          <button
            type="button"
            onClick={doDeposit}
            disabled={deposit.isPending || !session.isSuccess}
            className={`border-4 p-2 font-display text-base ${
              deposit.isPending
                ? "cursor-wait border-slate bg-stone text-haze"
                : "border-brass bg-amber text-ink hover:brightness-110"
            }`}
          >
            {deposit.isPending ? "…" : "DEPOSIT +1000"}
          </button>
        </div>
      </header>

      {depositNote && (
        <p
          role="status"
          className="m-0 mb-6 self-center border-4 border-mint bg-ink px-4 py-2 text-center font-display text-lg text-mint"
        >
          {depositNote}
        </p>
      )}

      <main className="flex flex-1 flex-col items-center gap-6">
        {games.isLoading && <p className="p-6 text-haze">OPENING THE FLOOR…</p>}
        {games.isError && (
          <p className="p-6 text-ember">casino unreachable</p>
        )}

        <div className="grid w-full max-w-[1280px] gap-6 md:grid-cols-2 lg:grid-cols-3">
          {games.data?.map((g) => (
            <GameCard key={g.id} game={g} />
          ))}
        </div>

        <p className="mt-4 text-center text-base text-haze">
          play-money arcade · no cash value · no deposits · no cash-out
        </p>
      </main>
    </div>
  );
}

function GameCard({ game }: { game: import("@/lib/types").GameInfo }) {
  const pt = game.paytable;
  const preview = pt?.icons.slice(0, pt.icons.length) ?? [];
  return (
    <Link
      href={`/play/${game.id}`}
      className="block border-4 border-stone bg-shadow p-4 shadow-hard transition-transform hover:-translate-y-1 hover:border-bone"
    >
      <h2 className="m-0 font-display text-lg text-cyan">
        {game.name.toUpperCase()}
      </h2>
      <p className="mt-1 text-base text-haze">
        {pt ? `${pt.reels} × ${pt.rows} · ` : ""}
        {pt
          ? pt.mode === "scatter"
            ? "SCATTER PAYS"
            : `${pt.paylines} LINES`
          : "···"}
      </p>
      <div className="mt-3 flex flex-wrap gap-2 border-4 border-slate bg-ink p-2">
        {preview.map((icon, i) => (
          <PixelSymbol key={i} index={i} icon={icon} scale={1} />
        ))}
      </div>
      <p className="mt-3 text-base text-mint">
        RTP {(game.theoreticalRtp * 100).toFixed(2)}%
      </p>
      <p className="mt-2 font-display text-base text-magenta">PLAY ▸</p>
    </Link>
  );
}
