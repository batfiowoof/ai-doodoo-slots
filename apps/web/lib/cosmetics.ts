// Cosmetics registry — the client-side mirror of the shop catalog. The
// server owns ownership and prices; this file owns looks: every equipped
// item id resolves to its visual here (same mirror-on-both-sides convention
// as the plinko multiplier tables — ids must match the migration seed).

export type CosmeticsKind =
  | "title"
  | "name_effect"
  | "card_skin"
  | "avatar_frame"
  | "plinko_ball"
  | "profile_theme"
  | "emote_pack";

export interface Cosmetics {
  title: string;
  nameEffect: string;
  cardSkin: string;
  avatarFrame: string;
  plinkoBall: string;
  profileTheme: string;
}

export const EMPTY_COSMETICS: Cosmetics = {
  title: "",
  nameEffect: "",
  cardSkin: "",
  avatarFrame: "",
  plinkoBall: "",
  profileTheme: "",
};

export function cosmeticsOf(
  c: Partial<Cosmetics> | null | undefined,
): Cosmetics {
  return { ...EMPTY_COSMETICS, ...(c ?? {}) };
}

// ---- Titles: badge chip rendered next to the name ----

export interface TitleVisual {
  label: string;
  /** Chip classes: border + text tint per rarity tier. */
  className: string;
}

export const TITLES: Record<string, TitleVisual> = {
  "title.rookie": { label: "ROOKIE", className: "border-zinc-500 text-zinc-300" },
  "title.degen": { label: "DEGEN", className: "border-lime-500 text-lime-300" },
  "title.card-shark": { label: "CARD SHARK", className: "border-cyan-400 text-cyan-300" },
  "title.high-roller": { label: "HIGH ROLLER", className: "border-fuchsia-400 text-fuchsia-300" },
  "title.vip": { label: "VIP", className: "border-amber-300 text-amber-200" },
  "title.whale": { label: "WHALE", className: "border-sky-300 text-sky-200" },
  "title.legend": { label: "LEGEND", className: "border-yellow-300 text-yellow-200 fx-title-legend" },
};

// ---- Name effects: animated text classes (keyframes in globals.css) ----

export const NAME_EFFECTS: Record<string, { className: string }> = {
  "name.rainbow": { className: "fx-name-rainbow" },
  "name.neon": { className: "fx-name-neon" },
  "name.ice": { className: "fx-name-ice" },
  "name.fire": { className: "fx-name-fire" },
  "name.gold": { className: "fx-name-gold" },
  "name.glitch": { className: "fx-name-glitch" },
};

/** Class for a name with an effect, else "". Effects override role colors. */
export function nameEffectClass(id: string | undefined | null): string {
  return (id && NAME_EFFECTS[id]?.className) || "";
}

// ---- Card skins: PixelCard palettes ----

export interface CardSkinVisual {
  label: string;
  /** Card back fill + border + pip motif color. */
  back: string;
  backBorder: string;
  backPip: string;
  /** Face fill + rank/suit ink. */
  face: string;
  faceBorder: string;
  ink: string;
}

export const CARD_SKINS: Record<string, CardSkinVisual> = {
  "card.midnight": {
    label: "MIDNIGHT",
    back: "#101024", backBorder: "#8f8fae", backPip: "#c0c0e0",
    face: "#181830", faceBorder: "#9a9ac0", ink: "#e8e8ff",
  },
  "card.synthwave": {
    label: "SYNTHWAVE",
    back: "#ff2d95", backBorder: "#2de2ff", backPip: "#ffd319",
    face: "#241a3e", faceBorder: "#ff2d95", ink: "#2de2ff",
  },
  "card.8bit": {
    label: "8-BIT",
    back: "#1a7a1a", backBorder: "#7fff00", backPip: "#ffffff",
    face: "#f2ead8", faceBorder: "#101010", ink: "#101010",
  },
  "card.crimson": {
    label: "CRIMSON",
    back: "#4a0410", backBorder: "#ff2244", backPip: "#ff8899",
    face: "#2a050c", faceBorder: "#ff2244", ink: "#ffd9de",
  },
  "card.gold-foil": {
    label: "GOLD FOIL",
    back: "#8a6a00", backBorder: "#ffd700", backPip: "#fff3b0",
    face: "#2b2103", faceBorder: "#ffd700", ink: "#ffd700",
  },
};

/** Skin for an id, with every field defaulted to the classic look. */
export function cardSkinOf(id: string | undefined | null): CardSkinVisual {
  return {
    label: "CLASSIC",
    back: "#2c1250", backBorder: "#1a0b33", backPip: "#ff2d95",
    face: "#f2ead8", faceBorder: "#c9bfa5", ink: "#14101f",
    ...(id ? CARD_SKINS[id] : {}),
  };
}

// ---- Avatar frames: ring style around the avatar ----

export interface FrameVisual {
  label: string;
  className: string;
  /** Animated frames carry a keyframe-driven class. */
  animated?: boolean;
}

export const FRAMES: Record<string, FrameVisual> = {
  "frame.bronze": { label: "BRONZE RING", className: "ring-4 ring-amber-700" },
  "frame.silver": { label: "SILVER RING", className: "ring-4 ring-zinc-300" },
  "frame.neon": { label: "NEON HALO", className: "ring-4 ring-fuchsia-400 shadow-[0_0_12px_2px_rgba(255,45,149,0.7)]" },
  "frame.gold": { label: "GOLD RING", className: "ring-4 ring-yellow-400 shadow-[0_0_12px_2px_rgba(255,215,0,0.55)]" },
  "frame.rainbow": { label: "PRISM RING", className: "fx-frame-rainbow", animated: true },
};

// ---- Plinko balls: puck palettes (shine, body, glow) ----

export interface BallVisual {
  label: string;
  body: string;
  shine: string;
  glow: string;
}

export const BALLS: Record<string, BallVisual> = {
  "ball.chrome": { label: "CHROME", body: "#c0c6d0", shine: "#f4f7fb", glow: "rgba(220,230,240,0.5)" },
  "ball.neon": { label: "NEON PUCK", body: "#39ff14", shine: "#ccffbe", glow: "rgba(57,255,20,0.8)" },
  "ball.gold": { label: "GOLD PUCK", body: "#ffd700", shine: "#fff3b0", glow: "rgba(255,215,0,0.75)" },
  "ball.rainbow": { label: "PRISM PUCK", body: "#ff2d95", shine: "#2de2ff", glow: "rgba(255,45,149,0.8)" },
  "ball.plasma": { label: "PLASMA", body: "#00e5ff", shine: "#e0fbff", glow: "rgba(0,229,255,0.9)" },
};

export function ballOf(id: string | undefined | null): BallVisual {
  return {
    label: "CLASSIC",
    body: "#ff2d95", shine: "#ff9ecb", glow: "rgba(255,45,149,0.6)",
    ...(id && BALLS[id] ? BALLS[id] : {}),
  };
}

// ---- Profile themes: profile card / modal backgrounds ----

export interface ThemeVisual {
  label: string;
  className: string;
}

export const THEMES: Record<string, ThemeVisual> = {
  "theme.midnight": { label: "MIDNIGHT", className: "bg-gradient-to-br from-[#0b0b1e] via-[#101032] to-[#050510]" },
  "theme.felt": { label: "CASINO FELT", className: "bg-gradient-to-br from-[#0d3b24] via-[#0a4a2c] to-[#062b1a]" },
  "theme.synthwave": { label: "SYNTHWAVE", className: "bg-gradient-to-br from-[#2b1055] via-[#7597de33] to-[#ff2d9533]" },
  "theme.gold": { label: "GILDED", className: "bg-gradient-to-br from-[#2b2103] via-[#4a3a06] to-[#1a1402]" },
};

export function themeClass(id: string | undefined | null): string {
  return (id && THEMES[id]?.className) || "";
}

// ---- Emote packs: pack item id → the emote ids it unlocks ----

export const PACKS: Record<string, { label: string; emotes: string[] }> = {
  "pack.party": { label: "PARTY PACK", emotes: ["party", "bolt", "gem", "rocket", "disco"] },
  "pack.cope": { label: "COPE PACK", emotes: ["tilt", "sweat", "ghost", "alien", "poop"] },
  "pack.highroller": { label: "HIGH-ROLLER PACK", emotes: ["whale", "crown", "trophy", "moneybag", "genie"] },
};

/** Pack item id that gates an emote id, if any. */
export function packOfEmote(emoteId: string): string | null {
  for (const [packId, pack] of Object.entries(PACKS)) {
    if (pack.emotes.includes(emoteId)) return packId;
  }
  return null;
}
