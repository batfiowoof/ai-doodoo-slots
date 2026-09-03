import BlackjackTable from "@/components/BlackjackTable";
import MachineScreen from "@/components/MachineScreen";

export default async function PlayPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  if (id === "blackjack") {
    return <BlackjackTable />;
  }
  return <MachineScreen gameId={id} />;
}
