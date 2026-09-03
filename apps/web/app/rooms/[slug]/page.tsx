import RoomGate from "@/components/RoomGate";

export default async function RoomPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await params;
  return <RoomGate slug={slug} />;
}
