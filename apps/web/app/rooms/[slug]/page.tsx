import CrashRoom from "@/components/CrashRoom";

export default async function RoomPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await params;
  return <CrashRoom slug={slug} />;
}
