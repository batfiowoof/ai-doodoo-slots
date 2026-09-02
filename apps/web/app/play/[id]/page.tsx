import MachineScreen from "@/components/MachineScreen";

export default async function PlayPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <MachineScreen gameId={id} />;
}
