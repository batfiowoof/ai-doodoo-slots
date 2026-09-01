// Gate check: verify a server-made bet using the browser verifier code path.
// Usage: node e2e-verify.ts <serverSeedHex> <clientSeed> <nonce>
import { deriveGrid } from "../lib/verify.ts";

const [serverSeedHex, clientSeed, nonce] = process.argv.slice(2);
const result = await deriveGrid({
  serverSeedHex,
  clientSeed,
  nonce: Number.parseInt(nonce, 10),
});
console.log(JSON.stringify({ grid: result.grid, winningLines: result.winningLines, payoutMultiplier: result.payoutMultiplier }));
