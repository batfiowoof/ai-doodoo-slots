"use client";

import PixelSymbol from "./PixelSymbol";
import { useGames } from "@/lib/api";

function paysChips(pays: Record<string, number>): string {
  const entries = Object.entries(pays ?? {}).sort(([a], [b]) => Number(a) - Number(b));
  return entries.map(([count, pay]) => `${count}→×${pay}`).join("  ");
}

export default function Paytable({ gameId }: { gameId: string }) {
  const games = useGames();
  const slots = games.data?.find((g) => g.id === gameId);

  return (
    <section className="border-4 border-stone bg-shadow p-4 shadow-hard">
      <h2 className="m-0 mb-4 border-b-4 border-slate pb-2 font-display text-base text-cyan">
        PAYTABLE
      </h2>

      {games.isLoading && <p className="p-2 text-haze">loading…</p>}
      {games.isError && (
        <p className="p-2 text-ember">paytable unavailable</p>
      )}

      {slots?.paytable && (
        <>
          {slots.paytable.mode === "scatter" && (
            <p className="mb-2 text-base text-haze">
              scatter pays — N anywhere on the grid → × bet
            </p>
          )}
          {slots.paytable.symbols.map((symbol, i) => {
            const chips = paysChips(symbol.pays);
            return (
              <div
                key={symbol.name}
                className="flex items-center gap-4 border-b-4 border-slate py-2 last:border-b-0"
              >
                <div className="pixelated flex h-12 w-12 shrink-0 items-center justify-center border-4 border-slate bg-ink">
                  <PixelSymbol index={i} icon={slots.paytable?.icons[i]} scale={1} />
                </div>
                <span className="flex-1 capitalize">{symbol.name}</span>
                <span className="text-base text-haze">w{symbol.weight}</span>
                {chips ? (
                  <span className="font-body text-xl text-amber">{chips}</span>
                ) : (
                  <span className="text-base text-haze">—</span>
                )}
              </div>
            );
          })}
          <p className="mt-4 text-base text-mint">
            {slots.paytable.mode === "scatter"
              ? "scatter game"
              : `${slots.paytable.paylines} paylines · wins count from the left`}{" "}
            · pays × total bet · RTP {(slots.theoreticalRtp * 100).toFixed(2)}%
          </p>
        </>
      )}
    </section>
  );
}
