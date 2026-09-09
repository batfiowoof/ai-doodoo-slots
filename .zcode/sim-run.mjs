// Headless plinko physics audit v2 — corrected stall detection (y grows DOWN,
// so falling = ball.y increasing; a stall is near-zero DESCENT while falling).
import { Ball, buildGeometry } from "./plinkoPhysics.sim.mjs";

const W = 560, TOP = 46, BOTTOM = 560 - 64;
const DT = 1 / 120;
const BUDGET = 8;

function simOne(rows, path) {
  const geo = buildGeometry(rows, W, TOP, BOTTOM);
  let pegsHit = 0;
  const ball = new Ball(geo, path, { onPegHit: () => pegsHit++ });
  let t = 0;
  let stallT = 0;
  let lastY = ball.y;
  let maxJump = 0;
  let lastX = ball.x;
  let worstStall = 0;
  let stallAt = 0;
  let jumpTrace = null;
  while (ball.state !== "done" && t < BUDGET) {
    const xb = ball.x;
    const yb = ball.y;
    ball.step(DT);
    t += DT;
    const dxNow = Math.abs(ball.x - xb);
    if (dxNow > 15 && !jumpTrace) {
      // nearest peg before the jump + row context
      let near = null;
      for (let r = 2; r <= rows; r++) {
        for (const peg of geo.pegRows[r]) {
          const d = Math.hypot(xb - peg.x, yb - peg.y);
          if (!near || d < near.d) near = { d: +d.toFixed(1), px: peg.x, py: peg.y, row: r };
        }
      }
      jumpTrace = {
        t: +t.toFixed(2), dx: +dxNow.toFixed(1), xb: +xb.toFixed(0), xa: +ball.x.toFixed(0),
        yb: +yb.toFixed(0), ya: +ball.y.toFixed(0), vx: +ball.vx.toFixed(0), vy: +ball.vy.toFixed(0),
        near, lastRowY: +(TOP + (rows - 2) * geo.gapY).toFixed(0),
      };
    }
    const descend = ball.y - lastY; // + means falling
    maxJump = Math.max(maxJump, Math.abs(ball.x - lastX));
    if (ball.state === "falling" && descend < 0.05) {
      stallT += DT;
      if (stallT > worstStall) { worstStall = stallT; stallAt = t; }
    } else {
      stallT = 0;
    }
    lastY = ball.y;
    lastX = ball.x;
  }
  const landed = ball.state === "done";
  return {
    rows, landed, t, pegsHit, expected: rows - 1,
    missed: rows - 1 - pegsHit, worstStall, stallAt,
    stallMs: Math.round(worstStall * 1000), maxJump, jumpTrace,
    endX: ball.x, path: path.join(""),
  };
}

let total = 0;
const landedT = { 8: [], 12: [], 16: [] };
let notLanded = 0, missedAny = 0, stallLong = 0, jumpBig = 0, wrongLane = 0;
const badSamples = [];
for (const rows of [8, 12, 16]) {
  for (let i = 0; i < 400; i++) {
    const path = Array.from({ length: rows }, () => Math.random() < 0.5);
    const r = simOne(rows, path);
    total++;
    if (!r.landed) { notLanded++; if (badSamples.length < 5) badSamples.push({ kind: "not-landed", ...r }); continue; }
    landedT[rows].push(r.t);
    if (r.missed > 0) missedAny++;
    if (r.worstStall > 0.35) { stallLong++; if (badSamples.length < 8) badSamples.push({ kind: `stall-${Math.round(r.worstStall * 1000)}ms`, ...r }); }
    if (r.maxJump > 15) { jumpBig++; if (badSamples.length < 8) badSamples.push({ kind: `jump-${r.maxJump.toFixed(1)}px`, ...r }); }
    // lane correctness: final x should be within the bucket span of cum
    const geo = buildGeometry(rows, W, TOP, BOTTOM);
    const cum = path.reduce((s, p) => s + (p ? 1 : -1), 0);
    const laneX = W / 2 + (cum * geo.gapX) / 2;
    if (Math.abs(r.endX - laneX) > geo.gapX * 0.5) { wrongLane++; if (badSamples.length < 12) badSamples.push({ kind: "wrong-lane", ...r, laneX: Math.round(laneX) }); }
  }
}
const avg = (a) => (a.length ? (a.reduce((s, v) => s + v, 0) / a.length).toFixed(2) : "n/a");
const p99 = (a) => (a.length ? a.slice().sort((x, y) => x - y)[Math.floor(a.length * 0.99)].toFixed(2) : "n/a");
console.log(`total=${total} notLanded=${notLanded} missed>=1row=${missedAny} stall>350ms=${stallLong} jump>15px=${jumpBig} wrongLane=${wrongLane}`);
for (const rows of [8, 12, 16]) console.log(`rows=${rows} landed=${landedT[rows].length} landTime avg=${avg(landedT[rows])}s p99=${p99(landedT[rows])}s`);
if (badSamples.length) { console.log("\nSAMPLES:"); badSamples.forEach((s) => console.log(JSON.stringify({ ...s, path: s.path.slice(0, 20) }))); }
