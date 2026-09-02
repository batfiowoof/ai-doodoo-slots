import Link from "next/link";
import Cabinet from "@/components/Cabinet";
import Paytable from "@/components/Paytable";
import HistoryTable from "@/components/HistoryTable";

export default async function PlayPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return (
    <div className="flex min-h-screen flex-col px-4 pt-4 pb-10">
      <header className="mb-2 flex items-center justify-between gap-4">
        <Link href="/" className="font-display text-base text-cyan">
          ◂ GAME MENU
        </Link>
        <Link href="/verify" className="font-display text-base text-cyan">
          VERIFY ▸
        </Link>
      </header>
      <main className="flex flex-1 flex-col items-center gap-6">
        <Cabinet gameId={id} />
        <div className="grid w-full max-w-[1280px] gap-6 lg:grid-cols-2">
          <Paytable gameId={id} />
          <section className="border-4 border-stone bg-shadow p-4 shadow-hard">
            <h2 className="m-0 mb-4 border-b-4 border-slate pb-2 font-display text-base text-cyan">
              HOW IT WORKS
            </h2>
            <p className="m-0 text-base text-haze">
              Every spin is decided by the server and derived from your seed
              pair — verify any bet from the VERIFY page with the server seed
              revealed on rotation. Outcomes are provably fair; the client
              only renders them.
            </p>
          </section>
        </div>
        <div className="w-full max-w-[1280px]">
          <HistoryTable />
        </div>
      </main>
    </div>
  );
}
