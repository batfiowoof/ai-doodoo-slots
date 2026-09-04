"use client";

import type { CSSProperties } from "react";
import { AVATAR_PRESETS, avatarUploadURL } from "../lib/types";

/**
 * Player avatar, rendered in the house style: a framed pixel sprite from the
 * shared slot-symbol sheet, an uploaded image (already 64x64 server-side), or
 * a deterministic letter tile for players without one. Integer sizes only —
 * the frame swallows 2px of border on each side.
 */
export function Avatar({
  userId,
  displayName,
  avatarPreset,
  avatarVersion,
  size = 24,
  ring = "#35205c",
  glow = false,
}: {
  userId: number;
  displayName: string;
  avatarPreset?: string;
  avatarVersion?: number;
  size?: number;
  ring?: string;
  glow?: boolean;
}) {
  const frame: CSSProperties = {
    width: size,
    height: size,
    border: `2px solid ${ring}`,
    background: "#0c0718",
    display: "grid",
    placeItems: "center",
    overflow: "hidden",
    flex: "none",
    boxShadow: glow
      ? `0 0 14px rgba(34,232,255,.45), inset 0 0 0 1px #06040d`
      : "inset 0 0 0 1px #06040d",
  };
  const inner = size - 4;

  if (avatarPreset) {
    const preset = AVATAR_PRESETS.find((p) => p.key === avatarPreset);
    if (preset) {
      return (
        <div style={frame}>
          <img src={preset.src} width={inner} height={inner} className="pixelated" alt="" />
        </div>
      );
    }
  }
  if (avatarVersion && avatarVersion > 0) {
    return (
      <div style={frame}>
        <img
          src={avatarUploadURL(userId, avatarVersion)}
          width={inner}
          height={inner}
          className="pixelated"
          alt=""
        />
      </div>
    );
  }

  // Letter tile: hue picked from the user id so the floor stays colorful
  // without every fallback looking identical.
  const accents = [
    { bg: "#1a0d2e", fg: "#22e8ff" },
    { bg: "#0b2a33", fg: "#5fe08a" },
    { bg: "#2a1406", fg: "#ffb15c" },
    { bg: "#2d0a1e", fg: "#ff2d95" },
  ];
  const pick = accents[userId % accents.length];
  const initial = (displayName.trim()[0] ?? "?").toUpperCase();
  return (
    <div style={frame}>
      <span
        style={{
          fontFamily: "var(--font-display)",
          fontSize: Math.max(8, Math.round(size * 0.5)),
          color: pick.fg,
          lineHeight: 1,
        }}
      >
        {initial}
      </span>
    </div>
  );
}
