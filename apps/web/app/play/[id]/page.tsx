import BlackjackTable from "@/components/BlackjackTable";
import DiceScreen from "@/components/DiceScreen";
import PlinkoScreen from "@/components/PlinkoScreen";
import MachineScreen from "@/components/MachineScreen";
import MinesScreen from "@/components/MinesScreen";

export default async function PlayPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  if (id === "blackjack") {
    return <BlackjackTable />;
  }
  if (id === "dice") {
    return <DiceScreen gameId={id} />;
  }
  if (id === "plinko") {
    return <PlinkoScreen gameId={id} />;
  }
  if (id === "mines") {
    return <MinesScreen gameId={id} />;
  }
  return <MachineScreen gameId={id} />;
}
