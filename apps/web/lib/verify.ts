// Client-side provable-fairness verification. This file recomputes slot
// outcomes entirely in the browser from (server seed, client seed, nonce) â€”
// it makes NO network calls and shares no code with the backend. The
// byte-stream rule mirrors the documented contract in the Go fair package:
//
//   block 0 = HMAC-SHA256(key, base)          base = clientSeed + ":" + nonce
//   block i = HMAC-SHA256(key, base + ":" + i)
//
// bytes consumed big-endian, 4 at a time, as uint32; float = u32 / 2^32;
// weighted pick walks the cumulative table with trunc(float * 100).

export const VERIFY_WEIGHTS = [22, 20, 16, 14, 11, 8, 6, 3];
export const VERIFY_PAYS3 = [1, 2, 4, 5, 6, 12, 24, 90];
export const VERIFY_PAYS4 = [3, 6, 10, 15, 18, 36, 72, 270];
export const VERIFY_PAYS5 = [7, 14, 28, 35, 42, 84, 168, 630];

// Payline rows per reel, mirroring the Go engine.
export const VERIFY_LINES = [
  [1, 1, 1, 1, 1],
  [0, 0, 0, 0, 0],
  [2, 2, 2, 2, 2],
  [0, 1, 2, 1, 0],
  [2, 1, 0, 1, 2],
  [1, 0, 1, 0, 1],
  [1, 2, 1, 2, 1],
  [0, 0, 1, 0, 0],
  [2, 2, 1, 2, 2],
];

export interface VerifyInput {
  serverSeedHex: string;
  clientSeed: string;
  nonce: number;
}

export interface VerifyResult {
  grid: number[][];
  winningLines: number[];
  payoutMultiplier: number;
  u32s: number[];
}

function hexToBytes(hex: string): Uint8Array {
  const clean = hex.trim().toLowerCase();
  if (clean.length === 0 || clean.length % 2 !== 0) {
    throw new Error("seed must be an even-length hex string");
  }
  if (!/^[0-9a-f]+$/.test(clean)) {
    throw new Error("seed must contain only hex digits");
  }
  const out = new Uint8Array(clean.length / 2);
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(clean.slice(i * 2, i * 2 + 2), 16);
  }
  return out;
}

async function hmacSha256(
  key: Uint8Array,
  message: string,
): Promise<Uint8Array> {
  const cryptoKey = await crypto.subtle.importKey(
    "raw",
    key as BufferSource,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const sig = await crypto.subtle.sign(
    "HMAC",
    cryptoKey,
    new TextEncoder().encode(message) as BufferSource,
  );
  return new Uint8Array(sig);
}

export async function deriveGrid(input: VerifyInput): Promise<VerifyResult> {
  const key = hexToBytes(input.serverSeedHex);
  if (key.length !== 32) {
    throw new Error(
      `server seed must be 32 bytes (64 hex chars), got ${key.length}`,
    );
  }

  const base = `${input.clientSeed}:${input.nonce}`;
  const u32s: number[] = [];
  let buf: number[] = [];
  let blockIdx = 0;

  while (u32s.length < 15) {
    if (buf.length < 4) {
      const msg = blockIdx === 0 ? base : `${base}:${blockIdx}`;
      const block = await hmacSha256(key, msg);
      buf.push(...block);
      blockIdx += 1;
      continue;
    }
    const u =
      ((buf[0] << 24) | (buf[1] << 16) | (buf[2] << 8) | buf[3]) >>> 0;
    u32s.push(u);
    buf = buf.slice(4);
  }

  const picks = u32s.map((u) => {
    const f = u / 2 ** 32;
    const target = Math.trunc(f * 100);
    let cum = 0;
    for (let i = 0; i < VERIFY_WEIGHTS.length; i++) {
      cum += VERIFY_WEIGHTS[i];
      if (target < cum) return i;
    }
    return VERIFY_WEIGHTS.length - 1;
  });

  const grid = [
    picks.slice(0, 5),
    picks.slice(5, 10),
    picks.slice(10, 15),
  ];

  const winningLines: number[] = [];
  let payoutMultiplier = 0;
	VERIFY_LINES.forEach((line, i) => {
		const first = grid[line[0]][0];
		let count = 1;
		for (let c = 1; c < line.length; c++) {
			if (grid[line[c]][c] !== first) break;
			count++;
		}
		if (count >= 3) {
			winningLines.push(i);
			const pays =
				count === 3 ? VERIFY_PAYS3 : count === 4 ? VERIFY_PAYS4 : VERIFY_PAYS5;
			payoutMultiplier += pays[first];
		}
	});

  return { grid, winningLines, payoutMultiplier, u32s };
}

// ---- Blackjack deck derivation ----
//
// The deck is a fresh 52-card deck in canonical order (suit s,h,d,c; rank
// 2..A within each suit) shuffled once at deal time by Fisher-Yates driven
// by the same HMAC stream. To pick a uniform j in [0, n): draw u32 v,
// accept when v < 2^32 - (2^32 mod n), else draw again; j = v mod n. The
// shuffled array IS the draw order: indexes 0,1 = player cards, 2,3 =
// dealer cards (3 is the hole), then hits/dealer draws take 4,5,6…

const BJ_SUITS = ["s", "h", "d", "c"] as const;
const BJ_RANKS = ["2", "3", "4", "5", "6", "7", "8", "9", "T", "J", "Q", "K", "A"] as const;

export interface BJVerifyResult {
  deck: string[];
  u32s: number[];
}

export async function deriveBlackjackDeck(
  input: VerifyInput,
): Promise<BJVerifyResult> {
  const key = hexToBytes(input.serverSeedHex);
  if (key.length !== 32) {
    throw new Error(
      `server seed must be 32 bytes (64 hex chars), got ${key.length}`,
    );
  }

  const base = `${input.clientSeed}:${input.nonce}`;
  const u32s: number[] = [];
  let buf: number[] = [];
  let blockIdx = 0;

  const nextU32 = async (): Promise<number> => {
    while (buf.length < 4) {
      const msg = blockIdx === 0 ? base : `${base}:${blockIdx}`;
      const block = await hmacSha256(key, msg);
      buf.push(...block);
      blockIdx += 1;
    }
    const u =
      ((buf[0] << 24) | (buf[1] << 16) | (buf[2] << 8) | buf[3]) >>> 0;
    buf = buf.slice(4);
    u32s.push(u);
    return u;
  };

  const deck: string[] = [];
  for (const suit of BJ_SUITS) {
    for (const rank of BJ_RANKS) deck.push(rank + suit);
  }

  for (let i = deck.length - 1; i > 0; i--) {
    const n = i + 1;
    const threshold = 2 ** 32 - (2 ** 32 % n);
    // Rejection sampling: values at or above the largest multiple of n
    // that fits in 32 bits are redrawn — the exact Go rule.
    let v = await nextU32();
    while (v >= threshold) v = await nextU32();
    const j = v % n;
    [deck[i], deck[j]] = [deck[j], deck[i]];
  }

  return { deck, u32s };
}
