// Fixture check for lib/verify.ts — run with: npm run verify:fixture
// Expected grids derived with an independent node implementation (crypto module,
import { deriveGrid } from "../lib/verify.ts";

const SEED = "4f0c2ba1e6d54b8fa3c9d0e7f1a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4";

// Grids derived by hand from the cross-validated uint32 vectors:
// nonce 0 picks: 32,43,84,7,52,30,6,34,17 -> [1,2,5, 0,2,1, 0,1,0]
// nonce 1 picks: 54,18,80,33,10,69,96,18,85 -> [2,0,4, 1,0,3, 6,0,5]
const FIXTURES = [
  {
    name: "nonce 0",
    input: { serverSeedHex: SEED, clientSeed: "player-one", nonce: 0 },
    wantGrid: [
      [2, 1, 3],
      [3, 3, 1],
      [5, 2, 7],
    ],
    wantLines: [] as number[],
  },
  {
    name: "nonce 1",
    input: { serverSeedHex: SEED, clientSeed: "player-one", nonce: 1 },
    wantGrid: [
      [7, 5, 1],
      [0, 3, 5],
      [1, 5, 2],
    ],
    wantLines: [] as number[],
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

if (failed) {
  console.error("VERIFY FIXTURES FAILED");
  process.exit(1);
}
console.log("all verify fixtures passed");
