"use client";

// Emote picker: a staggered three-row fan of pixel chips sweeping up-left
// from the dock, so same-row neighbours sit ~3 chip-widths apart instead of
// piling into one blob. Hovering a chip pops a large preview beside the fan
// — you always see exactly what you're about to send. Portals to the body
// (the lobby stage scales).

import { useState } from "react";
import { createPortal } from "react-dom";
import { EMOTES, emoteVisual } from "@/lib/emotes";
import { useShopInventory } from "@/lib/api";
import { PixelPanel, pixelClip } from "./Pixel";
import { sound } from "@/lib/sound";

const CHIP = 54;
// Three interleaved radii: same-ring neighbours are 3 fan-steps apart.
const RADII = [150, 208, 266];

export default function EmoteWheel({
  open,
  onPick,
}: {
  open: boolean;
  onPick: (emoteId: string) => void;
}) {
  const [hovered, setHovered] = useState<string | null>(null);
  const inventory = useShopInventory(open);
  const ownedPacks = new Set((inventory.data?.items ?? []).map((i) => i.itemId));
  if (!open) return null;

  const n = EMOTES.length;
  const hoveredEmote = EMOTES.find((e) => e.id === hovered);
  // Pack emotes are visible but locked until owned; the server rejects
  // sending them regardless, this is just the honest picker.
  const locked = (pack: string | undefined) => pack != null && !ownedPacks.has(pack);

  return createPortal(
    <>
      <div
        style={{
          position: "fixed",
          right: 40,
          bottom: 110,
          width: 0,
          height: 0,
          zIndex: 82,
          animation: "emoteWheelSpin .22s cubic-bezier(.2,1.4,.4,1) both",
        }}
      >
        {EMOTES.map((emote, i) => {
          // Fan sweeps 95° (near-vertical) to 190° (just below horizontal).
          const deg = 95 + (95 * i) / (n - 1);
          const rad = (deg * Math.PI) / 180;
          const radius = RADII[i % 3];
          const x = Math.cos(rad) * radius - CHIP / 2;
          const y = -Math.sin(rad) * radius - CHIP / 2;
          const visual = emoteVisual(emote.id);
          const hot = hovered === emote.id;
          const isLocked = locked(emote.pack);
          return (
            <button
              key={emote.id}
              type="button"
              title={isLocked ? `${emote.label} — unlock in THE VAULT` : `${emote.label} — :${emote.id}:`}
              onClick={() => {
                sound.unlock();
                if (isLocked) {
                  sound.error();
                  return;
                }
                sound.emotePop();
                onPick(emote.id);
              }}
              onMouseEnter={() => {
                sound.chipClink();
                setHovered(emote.id);
              }}
              onMouseLeave={() => setHovered((h) => (h === emote.id ? null : h))}
              style={{
                position: "absolute",
                left: x,
                top: y,
                border: "none",
                padding: 0,
                background: "none",
                cursor: isLocked ? "not-allowed" : "pointer",
                zIndex: hot ? 9 : 1,
                transform: hot ? "scale(1.45)" : "scale(1)",
                transition: "transform .1s cubic-bezier(.2,1.4,.4,1), filter .12s ease",
                filter: hot
                  ? "drop-shadow(0 0 22px rgba(255,210,31,.95))"
                  : "drop-shadow(0 0 8px rgba(255,45,149,.45))",
                opacity: isLocked ? 0.45 : 1,
              }}
            >
              {/* pixel chip: accent frame + dark stepped core */}
              <span
                style={{
                  display: "block",
                  background: hot ? "#ffd21f" : "#ff2d95",
                  clipPath: pixelClip(4),
                  padding: 3,
                  transition: "background .1s ease",
                }}
              >
                <span
                  style={{
                    position: "relative",
                    display: "grid",
                    placeItems: "center",
                    width: CHIP - 6,
                    height: CHIP - 6,
                    clipPath: pixelClip(4),
                    background: "radial-gradient(circle at 34% 30%, #341d55, #160b28 68%)",
                    fontSize: 24,
                    lineHeight: 1,
                  }}
                >
                  {visual?.type === "img" ? (
                    <img src={visual.src} width={CHIP - 18} height={CHIP - 18} className="pixelated" alt={emote.label} />
                  ) : (
                    <span>{visual?.emoji ?? "?"}</span>
                  )}
                  {isLocked && (
                    <span style={{ position: "absolute", fontSize: 12, right: 2, bottom: 2 }}>🔒</span>
                  )}
                </span>
              </span>
            </button>
          );
        })}
      </div>

      {/* the pop-up: a large, unmistakable preview of what you'll send */}
      {hoveredEmote && (
        <div
          style={{
            position: "fixed",
            right: 380,
            bottom: 150,
            zIndex: 83,
            animation: "notePop .16s cubic-bezier(.2,1.4,.4,1) both",
          }}
        >
          <PixelPanel accent="#ffd21f" background="linear-gradient(160deg, #1d1036, #0d0619 75%)" glow="rgba(255,210,31,.7)" padding={4}>
            <span
              style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                gap: 6,
                padding: "14px 22px 12px",
                minWidth: 130,
              }}
            >
              <EmotePreviewBig id={hoveredEmote.id} />
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 13,
                  letterSpacing: 2,
                  color: "#ffd21f",
                  textShadow: "0 0 10px rgba(255,210,31,.8)",
                  whiteSpace: "nowrap",
                }}
              >
                {hoveredEmote.label}
              </span>
              <span style={{ fontFamily: "var(--font-body)", fontSize: 15, color: "#8878b8" }}>
                :{hoveredEmote.id}:
              </span>
              {locked(hoveredEmote.pack) && (
                <span style={{ fontFamily: "var(--font-display)", fontSize: 11, letterSpacing: 2, color: "#ff2d95" }}>
                  🔒 VAULT-LOCKED
                </span>
              )}
            </span>
          </PixelPanel>
        </div>
      )}
    </>,
    document.body
  );
}

function EmotePreviewBig({ id }: { id: string }) {
  const visual = emoteVisual(id);
  if (!visual) return <span style={{ fontSize: 56 }}>🎲</span>;
  if (visual.type === "img") {
    return <img src={visual.src} width={72} height={72} className="pixelated" alt="" />;
  }
  return <span style={{ fontSize: 64, lineHeight: 1, filter: "drop-shadow(0 0 14px rgba(255,210,31,.6))" }}>{visual.emoji}</span>;
}
