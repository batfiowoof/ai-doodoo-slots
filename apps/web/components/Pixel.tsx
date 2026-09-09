"use client";

// Pixel-border language: stepped 8-bit corners instead of smooth curves.
// A clip-path would clip box-shadow, so the neon glow is a drop-shadow
// filter on the wrapper — it hugs the stepped silhouette.

import type { CSSProperties, ReactNode } from "react";

/** Clip path with two-step pixel corners (8-bit rounded rectangle). */
export function pixelClip(step = 4): string {
  const s = step;
  const s2 = step * 2;
  return `polygon(
    0 ${s2}px, ${s}px ${s2}px, ${s}px ${s}px, ${s2}px ${s}px, ${s2}px 0,
    calc(100% - ${s2}px) 0, calc(100% - ${s2}px) ${s}px, calc(100% - ${s}px) ${s}px, calc(100% - ${s}px) ${s2}px, 100% ${s2}px,
    100% calc(100% - ${s2}px), calc(100% - ${s}px) calc(100% - ${s2}px), calc(100% - ${s}px) calc(100% - ${s}px), calc(100% - ${s2}px) calc(100% - ${s}px), calc(100% - ${s2}px) 100%,
    ${s2}px 100%, ${s2}px calc(100% - ${s}px), ${s}px calc(100% - ${s}px), ${s}px calc(100% - ${s2}px), 0 calc(100% - ${s2}px)
  )`;
}

/**
 * Pixel-bordered panel: accent frame layer + dark core, both stepped, with
 * the glow (when lit) following the silhouette. Padding is the border
 * weight; size comes from the content or innerStyle.
 */
export function PixelPanel({
  accent,
  background,
  glow,
  padding = 3,
  style,
  innerStyle,
  children,
}: {
  accent: string;
  background: string;
  /** CSS color for the drop-shadow glow; omit for no glow. */
  glow?: string;
  padding?: number;
  style?: CSSProperties;
  innerStyle?: CSSProperties;
  children?: ReactNode;
}) {
  return (
    <span
      style={{
        display: "inline-block",
        filter: glow ? `drop-shadow(0 0 10px ${glow})` : undefined,
        transition: "filter .12s ease",
        ...style,
      }}
    >
      <span style={{ display: "block", background: accent, clipPath: pixelClip(padding), padding }}>
        <span style={{ display: "block", background, clipPath: pixelClip(padding), ...innerStyle }}>
          {children}
        </span>
      </span>
    </span>
  );
}
