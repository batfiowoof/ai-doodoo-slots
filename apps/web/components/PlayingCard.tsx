"use client";

import { useEffect, useRef } from "react";
import PixelCard from "@/components/PixelCard";
import { sound } from "@/lib/sound";

// Animated playing card: the pixel-art face wrapped in the table's motion
// vocabulary — thrown in from the shoe, flipped for a hole-card reveal,
// haloed when it wins. Sound fires alongside the animation so every visible
// card movement is heard; the AudioContext simply stays silent until the
// first user gesture unlocks it.

type DealFrom = "shoe" | "felt";

export interface PlayingCardProps {
  /** Two-character code ("As", "Td") or "back" for a face-down card. */
  code: string;
  /** Integer pixel-art scale factor (card is 20×28 units). */
  scale?: number;
  /** Deal-in animation origin. */
  dealFrom?: DealFrom;
  /** Stagger for the deal animation, ms. */
  dealDelay?: number;
  /** Mount face-down and flip to the face (hole-card reveal). */
  flip?: boolean;
  /** Winner halo. */
  glow?: boolean;
  /** Dim (folded / losing / mucked). */
  dim?: boolean;
  /** Static fan tilt in degrees, applied outside the animation. */
  tilt?: number;
  /** Skip the movement sound (crowded scenes). */
  silent?: boolean;
  style?: React.CSSProperties;
}

export default function PlayingCard({
  code,
  scale = 3,
  dealFrom,
  dealDelay = 0,
  flip = false,
  glow = false,
  dim = false,
  tilt = 0,
  silent = false,
  style,
}: PlayingCardProps) {
  const played = useRef(false);

  // One deal swish per mounted card, synced to its stagger.
  useEffect(() => {
    if (played.current || silent) return;
    played.current = true;
    if (dealFrom) sound.dealCard(dealDelay / 1000);
    if (flip) sound.flipCard(dealDelay / 1000);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const w = 20 * scale;
  const h = 28 * scale;

  const dealAnim = dealFrom
    ? `${dealFrom === "shoe" ? "cardDealShoe" : "cardDealFelt"} 0.42s cubic-bezier(.2,.9,.3,1.15) ${dealDelay}ms both`
    : undefined;

  const outer: React.CSSProperties = {
    width: w,
    height: h,
    flexShrink: 0,
    opacity: dim ? 0.45 : 1,
    transform: tilt ? `rotate(${tilt}deg)` : undefined,
    ...style,
  };

  if (flip) {
    // 3D flipper: the back starts toward the viewer and turns over once.
    return (
      <div style={{ ...outer, perspective: 900 }}>
        <div
          style={{
            position: "relative",
            width: "100%",
            height: "100%",
            transformStyle: "preserve-3d",
            animation: `cardFlipReveal .5s cubic-bezier(.3,.1,.3,1) ${dealDelay}ms both`,
          }}
        >
          <div style={{ position: "absolute", inset: 0, backfaceVisibility: "hidden" }}>
            <PixelCard code={code} scale={scale} />
          </div>
          <div
            style={{
              position: "absolute",
              inset: 0,
              backfaceVisibility: "hidden",
              transform: "rotateY(180deg)",
            }}
          >
            <PixelCard code="back" scale={scale} />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div style={{ ...outer, animation: dealAnim }}>
      <div style={glow ? { animation: "winGlow 1.1s ease-in-out infinite" } : undefined}>
        <PixelCard code={code} scale={scale} />
      </div>
    </div>
  );
}
