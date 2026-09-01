// Client-side provable-fairness verification. This file recomputes slot
// outcomes entirely in the browser from (server seed, client seed, nonce) —
// it makes NO network calls and shares no code with the backend. The
// byte-stream rule mirrors the documented contract in the Go fair package:
//
//   block 0 = HMAC-SHA256(key, base)          base = clientSeed + ":" + nonce
//   block i = HMAC-SHA256(key, base + ":" + i)
//
// bytes consumed big-endian, 4 at a time, as uint32; float = u32 / 2^32;
// weighted pick walks the cumulative table with trunc(float * 100).

export const VERIFY_WEIGHTS = [22, 20, 16, 14, 11, 8, 6, 3];
export const VERIFY_PAYS = [4, 5, 8, 11, 16, 26, 52, 200];

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

  while (u32s.length < 9) {
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
    [picks[0], picks[1], picks[2]],
    [picks[3], picks[4], picks[5]],
    [picks[6], picks[7], picks[8]],
  ];

  const lines = [
    [
      [0, 0],
      [0, 1],
      [0, 2],
    ], // top row
    [
      [1, 0],
      [1, 1],
      [1, 2],
    ], // middle row
    [
      [2, 0],
      [2, 1],
      [2, 2],
    ], // bottom row
    [
      [0, 0],
      [1, 1],
      [2, 2],
    ], // diagonal down
    [
      [2, 0],
      [1, 1],
      [0, 2],
    ], // diagonal up
  ];

  const winningLines: number[] = [];
  let payoutMultiplier = 0;
  lines.forEach((line, i) => {
    const [a, b, c] = line;
    const first = grid[a[0]][a[1]];
    if (grid[b[0]][b[1]] === first && grid[c[0]][c[1]] === first) {
      winningLines.push(i);
      payoutMultiplier += VERIFY_PAYS[first];
    }
  });

  return { grid, winningLines, payoutMultiplier, u32s };
}
