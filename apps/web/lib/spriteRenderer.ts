// Renders generated 16x16 sprites. Each sprite is drawn once to an
// offscreen canvas at 1:1, cached as a data URL per theme, then scaled by
// an integer factor with smoothing disabled — fractional scaling kills the
// pixel effect.
const dataUrlCache = new Map<string, string>();

function hexToRgba(hex: string): string {
  const v = hex.replace("#", "");
  if (v.length === 8) {
    const r = parseInt(v.slice(0, 2), 16);
    const g = parseInt(v.slice(2, 4), 16);
    const b = parseInt(v.slice(4, 6), 16);
    const a = parseInt(v.slice(6, 8), 16) / 255;
    return `rgba(${r},${g},${b},${a.toFixed(3)})`;
  }
  const r = parseInt(v.slice(0, 2), 16);
  const g = parseInt(v.slice(2, 4), 16);
  const b = parseInt(v.slice(4, 6), 16);
  return `rgb(${r},${g},${b})`;
}

export function spriteDataUrl(
  themeId: number,
  spriteIndex: number,
  palette: string[],
  rows: string[],
): string {
  const key = `${themeId}:${spriteIndex}`;
  const cached = dataUrlCache.get(key);
  if (cached) return cached;

  const off = document.createElement("canvas");
  off.width = 16;
  off.height = 16;
  const ctx = off.getContext("2d");
  if (!ctx) return "";

  for (let y = 0; y < 16; y++) {
    const row = rows[y] ?? "";
    for (let x = 0; x < 16; x++) {
      const idx = parseInt(row[x] ?? "0", 16);
      if (Number.isNaN(idx) || idx === 0) continue; // 0 = transparent
      ctx.fillStyle = hexToRgba(palette[idx] ?? "#ff00ff");
      ctx.fillRect(x, y, 1, 1);
    }
  }
  const url = off.toDataURL("image/png");
  dataUrlCache.set(key, url);
  return url;
}
