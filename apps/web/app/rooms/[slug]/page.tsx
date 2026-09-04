import RoomGate from "@/components/RoomGate";
import CrashRoom from "@/components/CrashRoom";

export default async function RoomPage({
  params,
  searchParams,
}: {
  params: Promise<{ slug: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { slug } = await params;
  const sp = await searchParams;
  // Dev-only visual demo (?demo=…) renders the crash room without a backend.
  if (process.env.NODE_ENV !== "production" && typeof sp?.demo === "string") {
    return <CrashRoom slug={slug} />;
  }
  return <RoomGate slug={slug} />;
}
