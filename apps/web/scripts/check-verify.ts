// Fixture check for lib/verify.ts — run with: npm run verify:fixture
// Expected grids derived with an independent node implementation
// (crypto module, hex-decoded 32-byte key — the same key semantics as the
// Go engine's play path). Blackjack deck fixtures were printed directly by
// the Go engine (cards.NewDeck + cards.Shuffle over the personal stream).
import { deriveBlackjackDeck, deriveGrid } from "../lib/verify.ts";

const SEED = "4f0c2ba1e6d54b8fa3c9d0e7f1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4";

const FIXTURES = [
  {
    name: "nonce 0",
    input: { serverSeedHex: SEED, clientSeed: "player-one", nonce: 0 },
    wantGrid: [
      [2, 1, 3, 3, 3],
      [1, 5, 2, 7, 4],
      [5, 4, 0, 3, 1],
    ],
    wantLines: [] as number[],
  },
  {
    name: "nonce 1",
    input: { serverSeedHex: SEED, clientSeed: "player-one", nonce: 1 },
    wantGrid: [
      [7, 5, 1, 0, 3],
      [5, 1, 5, 2, 5],
      [2, 5, 4, 0, 4],
    ],
    wantLines: [5, 6] as number[],
  },
];

const BJ_FIXTURES = [
  {
    name: "blackjack nonce 0",
    input: { serverSeedHex: SEED, clientSeed: "player-one", nonce: 0 },
    wantDeck:
      "4h3d6s7dThKs3h4s4cTc2s5dJhAd5s2d9cQc5cQhQsTdQd3c7h7s6h4dTs8hKc5h2cKh8sAh7cKdAs8c9d6d9sJdJc6cJs2h3sAc8d9h",
  },
  {
    name: "blackjack nonce 7",
    input: { serverSeedHex: SEED, clientSeed: "player-one", nonce: 7 },
    wantDeck:
      "Ad9sQdJsAcTc3cAh7h3d5s3h4sTd5c8d7cJd4cKs6d8s9h7s7d3sKdAs9d6h2s5hQhQsJc2c8h2h4hKh9cJhQcTs6cTh2d5d6sKc8c4d",
  },
];

let failed = false;
for (const f of FIXTURES) {
  const got = await deriveGrid(f.input);
  const gridOk = JSON.stringify(got.grid) === JSON.stringify(f.wantGrid);
  const linesOk = JSON.stringify(got.winningLines) === JSON.stringify(f.wantLines);
  console.log(
    `${f.name}: grid ${gridOk ? "OK" : "MISMATCH got " + JSON.stringify(got.grid)}` +
      ` lines ${linesOk ? "OK" : "MISMATCH got " + JSON.stringify(got.winningLines)}`,
  );
  if (!gridOk || !linesOk) failed = true;
}

for (const f of BJ_FIXTURES) {
  const got = await deriveBlackjackDeck(f.input);
  const deckStr = got.deck.join("");
  const ok = deckStr === f.wantDeck;
  console.log(
    `${f.name}: deck ${ok ? "OK" : "MISMATCH got " + deckStr}`,
  );
  if (!ok) failed = true;
}

if (failed) {
  console.error("VERIFY FIXTURES FAILED");
  process.exit(1);
}
console.log("all verify fixtures passed");
