"use client";

import { useQuery } from "@tanstack/react-query";
import CrashRoom from "@/components/CrashRoom";
import PokerRoom from "@/components/PokerRoom";
import RouletteRoom from "@/components/RouletteRoom";

interface RoomDetail {
  room: {
    slug: string;
    name: string;
    gameId: string;
    minBet: number;
    maxBet: number;
    capacity: number;
    playerCount: number;
  };
  round: unknown;
}

/** Picks the room renderer by game: holdem tables, roulette wheels, crash rooms. */
export default function RoomGate({ slug }: { slug: string }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["room", slug],
    queryFn: async (): Promise<RoomDetail> => {
      const res = await fetch(`/api/v1/rooms/${slug}`);
      if (!res.ok) throw new Error(`room failed: ${res.status}`);
      return res.json() as Promise<RoomDetail>;
    },
    staleTime: 30_000,
    retry: false,
  });

  if (isLoading) {
    return (
      <div style={{ position: "fixed", inset: 0, background: "#06040d", display: "flex", alignItems: "center", justifyContent: "center" }}>
        <span style={{ fontFamily: "var(--font-display)", fontSize: 14, letterSpacing: 3, color: "#5c4f80" }}>
          OPENING THE ROOM…
        </span>
      </div>
    );
  }
  if (isError || !data) {
    return (
      <div style={{ position: "fixed", inset: 0, background: "#06040d", display: "flex", alignItems: "center", justifyContent: "center" }}>
        <span style={{ fontFamily: "var(--font-display)", fontSize: 14, letterSpacing: 3, color: "#ff8a1f" }}>
          ROOM UNREACHABLE
        </span>
      </div>
    );
  }
  if (data.room.gameId === "holdem") {
    return <PokerRoom slug={slug} room={data.room} />;
  }
  if (data.room.gameId === "roulette") {
    return <RouletteRoom slug={slug} />;
  }
  return <CrashRoom slug={slug} />;
}
