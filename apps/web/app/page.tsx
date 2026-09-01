import Cabinet from "@/components/Cabinet";
import HistoryTable from "@/components/HistoryTable";
import Paytable from "@/components/Paytable";
import ThemePanel from "@/components/ThemePanel";
import { ThemeProvider } from "@/lib/theme";
import Link from "next/link";

export default function Home() {
  return (
    <ThemeProvider>
      <div className="flex min-h-screen flex-col px-4 pt-4 pb-10">
        <header className="mb-2 flex items-center justify-between gap-4">
          <div className="flex items-baseline gap-4">
            <h1 className="font-display text-xl text-magenta">RETRO CASINO</h1>
            <Link href="/verify" className="font-display text-base text-cyan">
              VERIFY ▸
            </Link>
          </div>
          <p className="text-base text-haze">
            play-money arcade · no cash value
          </p>
        </header>

        <main className="flex flex-1 flex-col items-center justify-center gap-6">
          <Cabinet />
          <div className="grid w-full max-w-[1280px] gap-6 lg:grid-cols-2">
            <ThemePanel />
            <Paytable />
          </div>
          <div className="w-full max-w-[1280px]">
            <HistoryTable />
          </div>
        </main>
      </div>
    </ThemeProvider>
  );
}
