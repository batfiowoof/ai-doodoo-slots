import Cabinet from "@/components/Cabinet";
import HistoryTable from "@/components/HistoryTable";
import Paytable from "@/components/Paytable";

export default function Home() {
  return (
    <div className="mx-auto max-w-[1128px] px-4 pt-6 pb-12">
      <header className="mb-6 flex items-center justify-between gap-4">
        <h1 className="font-display text-2xl text-magenta">RETRO CASINO</h1>
        <p className="text-base text-haze">
          play-money arcade · no cash value
        </p>
      </header>

      <div className="flex flex-col items-start gap-6 lg:flex-row lg:justify-center">
        <Cabinet />
        <div className="flex min-w-0 flex-1 flex-col gap-6">
          <Paytable />
          <HistoryTable />
        </div>
      </div>
    </div>
  );
}
