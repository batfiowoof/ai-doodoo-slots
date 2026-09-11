"use client";

import { TITLES, nameEffectClass } from "@/lib/cosmetics";

/**
 * Renders a player name with their equipped shop cosmetics: the animated
 * name effect on the text and the title badge beside it. One component for
 * every name surface (chat, roster, leaderboard, player card) so effects
 * read identically everywhere. The glitch effect mirrors its text through
 * data-name for the ::before/::after layers.
 */
export default function NameTag({
  displayName,
  title,
  nameEffect,
  withTitle = true,
  className = "",
  titleClassName = "",
}: {
  displayName: string;
  title?: string | null;
  nameEffect?: string | null;
  /** Compact surfaces (big-win banner, roster) can drop the badge. */
  withTitle?: boolean;
  className?: string;
  titleClassName?: string;
}) {
  const effect = nameEffectClass(nameEffect);
  const t = title ? TITLES[title] : undefined;
  return (
    <span className={`inline-flex min-w-0 items-center gap-1 ${className}`}>
      <span className={effect} data-name={displayName}>
        {displayName}
      </span>
      {withTitle && t && (
        <span
          className={`shrink-0 border px-1 font-display text-[8px] leading-[1.5] tracking-wider ${t.className} ${titleClassName}`}
        >
          {t.label}
        </span>
      )}
    </span>
  );
}
