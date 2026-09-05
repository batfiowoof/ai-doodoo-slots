import RoomGate from "@/components/RoomGate";
import CrashRoom from "@/components/CrashRoom";
import RouletteRoom from "@/components/RouletteRoom";

export default async function RoomPage({
  params,
  searchParams,
}: {
  params: Promise<{ slug: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { slug } = await params;
  const sp = await searchParams;
  // Dev-only visual demo (?demo=…) renders a room without a backend.
  if (process.env.NODE_ENV !== "production" && typeof sp?.demo === "string") {
    if (slug.startsWith("roulette")) {
      return <RouletteRoom slug={slug} />;
    }
    return <CrashRoom slug={slug} />;
  }
  return <RoomGate slug={slug} />;
}
