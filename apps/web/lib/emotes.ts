// Emote registry — the single source the emote wheel, chat inline rendering
// and the floating-reaction overlay all read from. The server only ever
// sees the `id`, so new entries here need no backend change.
//
// GIF sprites: drop a file into `public/emotes/<id>.gif`, set `src` on the
// entry, and every surface (wheel chip, chat inline, float overlay) picks it
// up automatically. Entries without `src` render their emoji placeholder.

export interface Emote {
  id: string;
  label: string;
  emoji: string;
  /** /emotes/<id>.gif once the sprite exists; emoji renders until then. */
  src?: string;
}

export const EMOTES: Emote[] = [
  { id: "gg", label: "GG", emoji: "👏" },
  { id: "fire", label: "ON FIRE", emoji: "🔥" },
  { id: "jackpot", label: "JACKPOT", emoji: "🎰" },
  { id: "laugh", label: "LOL", emoji: "😂" },
  { id: "cool", label: "COOL", emoji: "😎" },
  { id: "skull", label: "DEAD", emoji: "💀" },
  { id: "clown", label: "CLOWN", emoji: "🤡" },
  { id: "cry", label: "Crying", emoji: "😭" },
  { id: "shock", label: "SHOOK", emoji: "😱" },
  { id: "heart", label: "LOVE", emoji: "❤️" },
  { id: "money", label: "CASH", emoji: "💸" },
  { id: "snake", label: "SNAKE", emoji: "🐍" },
];

const byId = new Map(EMOTES.map((e) => [e.id, e]));

export function getEmote(id: string): Emote | undefined {
  return byId.get(id);
}

/** Renders one emote's visual: GIF sprite when registered, else the emoji. */
export function emoteVisual(id: string): { type: "img"; src: string } | { type: "text"; emoji: string } | null {
  const emote = getEmote(id);
  if (!emote) return null;
  if (emote.src) return { type: "img", src: emote.src };
  return { type: "text", emoji: emote.emoji };
}

/** Splits chat text on `:emote_id:` tokens for inline rendering. */
export function parseChatBody(body: string): ({ kind: "text"; text: string } | { kind: "emote"; id: string })[] {
  const out: ({ kind: "text"; text: string } | { kind: "emote"; id: string })[] = [];
  const re = /:([a-z0-9_]{1,32}):/g;
  let last = 0;
  for (let m = re.exec(body); m; m = re.exec(body)) {
    if (m.index > last) out.push({ kind: "text", text: body.slice(last, m.index) });
    out.push({ kind: "emote", id: m[1] });
    last = m.index + m[0].length;
  }
  if (last < body.length) out.push({ kind: "text", text: body.slice(last) });
  return out;
}
