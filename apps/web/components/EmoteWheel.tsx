"use client";

// Radial emote picker: a fan of chips sweeping from "up" to "left" around
// the dock, so it never leaves the screen. Same round-chip visual language
// as the lobby satellites. Portals to the body (the lobby stage scales).

import type { CSSProperties } from "react";
import { createPortal } from "react-dom";
import { EMOTES, emoteVisual } from "@/lib/emotes";
import { sound } from "@/lib/sound";

const CHIP = 54;
const RADIUS = 150;

export default function EmoteWheel({
  open,
  onPick,
}: {
  open: boolean;
  onPick: (emoteId: string) => void;
}) {
  if (!open) return null;

  const n = EMOTES.length;
  return createPortal(
    <div
      style={{
        position: "fixed",
        right: 40,
        bottom: 120,
        width: 0,
        height: 0,
        zIndex: 82,
        animation: "emoteWheelSpin .22s cubic-bezier(.2,1.4,.4,1) both",
      }}
    >
      {EMOTES.map((emote, i) => {
        // Fan from 100° (near-vertical) to 175° (near-horizontal left).
        const deg = 100 + (75 * i) / (n - 1);
        const rad = (deg * Math.PI) / 180;
        const x = Math.cos(rad) * RADIUS - CHIP / 2;
        const y = -Math.sin(rad) * RADIUS - CHIP / 2;
        const visual = emoteVisual(emote.id);
        const chipStyle: CSSProperties = {
          position: "absolute",
          left: x,
          top: y,
          width: CHIP,
          height: CHIP,
          borderRadius: "50%",
          border: "3px solid #ff2d95",
          background: "radial-gradient(circle at 34% 30%, #341d55, #160b28 68%)",
          boxShadow: "0 0 12px rgba(255,45,149,.4), inset 0 0 0 2px #06040d",
          display: "grid",
          placeItems: "center",
          cursor: "pointer",
          fontSize: 22,
          lineHeight: 1,
          padding: 0,
          transition: "transform .08s ease, box-shadow .08s ease",
        };
        return (
          <button
            key={emote.id}
            type="button"
            title={`${emote.label} — :${emote.id}:`}
            onClick={() => {
              sound.unlock();
              sound.emotePop();
              onPick(emote.id);
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.transform = "scale(1.22)";
              e.currentTarget.style.boxShadow = "0 0 22px rgba(255,45,149,.85), inset 0 0 0 2px #06040d";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.transform = "scale(1)";
              e.currentTarget.style.boxShadow = "0 0 12px rgba(255,45,149,.4), inset 0 0 0 2px #06040d";
            }}
            style={chipStyle}
          >
            {visual?.type === "img" ? (
              <img src={visual.src} width={CHIP - 12} height={CHIP - 12} className="pixelated" alt={emote.label} />
            ) : (
              <span>{visual?.emoji ?? "?"}</span>
            )}
          </button>
        );
      })}
    </div>,
    document.body
  );
}
