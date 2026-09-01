"use client";

import PixelSymbol from "./PixelSymbol";
import { useBets, useSession } from "@/lib/api";

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export default function HistoryTable() {
  const session = useSession();
  const bets = useBets();

  return (
    <section className="border-4 border-stone bg-shadow p-4 shadow-hard">
      <h2 className="m-0 mb-4 border-b-4 border-slate pb-2 font-display text-base text-cyan">
        HISTORY
      </h2>

      {!session.isSuccess && (
        <p className="p-6 text-center text-haze">····</p>
      )}

      {session.isSuccess && bets.isLoading && (
        <p className="p-2 text-haze">loading…</p>
      )}
      {session.isSuccess && bets.isError && (
        <p className="p-2 text-ember">history unavailable</p>
      )}

      {session.isSuccess && bets.data && bets.data.bets.length === 0 && (
        <p className="p-6 text-center text-haze">NO SPINS YET</p>
      )}

      {session.isSuccess && bets.data && bets.data.bets.length > 0 && (
        <table className="w-full border-collapse text-base">
          <thead>
            <tr>
              <th className="border-b-4 border-slate p-2 text-left font-display font-normal text-haze">
                TIME
              </th>
              <th className="border-b-4 border-slate p-2 text-left font-display font-normal text-haze">
                RESULT
              </th>
              <th className="border-b-4 border-slate p-2 text-right font-display font-normal text-haze">
                BET
              </th>
              <th className="border-b-4 border-slate p-2 text-right font-display font-normal text-haze">
                PAYOUT
              </th>
            </tr>
          </thead>
          <tbody>
            {bets.data.bets.map((bet) => (
              <tr key={bet.id}>
                <td className="border-b-4 border-slate p-2 text-haze">
                  {formatTime(bet.createdAt)}
                </td>
                <td className="border-b-4 border-slate p-2">
                  {bet.outcome?.grid ? (
                    <span
                      className="pixelated inline-grid grid-cols-3 gap-[2px] align-middle"
                      aria-hidden
                    >
                      {bet.outcome.grid.flat().map((symbolIndex, i) => (
                        <span
                          key={i}
                          className="pixelated h-2 w-2"
                          title={`symbol ${symbolIndex}`}
                        >
                          <PixelSymbol index={symbolIndex} scale={2} />
                        </span>
                      ))}
                    </span>
                  ) : (
                    "—"
                  )}
                </td>
                <td className="border-b-4 border-slate p-2 text-right text-bone">
                  {bet.betCredits}
                </td>
                <td
                  className={`border-b-4 border-slate p-2 text-right ${
                    bet.payoutCredits > 0 ? "text-mint" : "text-ember"
                  }`}
                >
                  {bet.payoutCredits > 0 ? `+${bet.payoutCredits}` : "0"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
