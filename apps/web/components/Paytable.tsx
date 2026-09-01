"use client";

import PixelSymbol from "./PixelSymbol";
import { useGames } from "@/lib/api";

export default function Paytable() {
  const games = useGames();
  const slots = games.data?.find((g) => g.id === "slots");

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
          <div className="mb-2 flex gap-4 text-base text-haze">
            <span className="w-16">3 OF A KIND →</span>
            <span>4 OF A KIND →</span>
            <span>5 OF A KIND</span>
          </div>
          {slots.paytable.symbols.map((symbol, i) => (
            <div
              key={symbol.name}
              className="flex items-center gap-4 border-b-4 border-slate py-2 last:border-b-0"
            >
              <div className="pixelated flex h-12 w-12 shrink-0 items-center justify-center border-4 border-slate bg-ink">
                <PixelSymbol index={i} scale={2} />
              </div>
              <span className="flex-1 capitalize">{symbol.name}</span>
              <span className="text-base text-haze">w{symbol.weight}</span>
              <span className="w-24 text-right font-body text-xl text-bone">
                {symbol.pay3} / {symbol.pay4} /{" "}
                <span className="text-amber">{symbol.pay5}</span>
              </span>
            </div>
          ))}
          <p className="mt-4 text-base text-mint">
            {slots.paytable.paylines} paylines · wins count from the left ·
            pays × total bet · lines stack · RTP{" "}
            {(slots.theoreticalRtp * 100).toFixed(2)}%
          </p>
        </>
      )}
    </section>
  );
}
