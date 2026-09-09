// Steered physics for the plinko board. The server records the exact L/R
// decision per row (provably fair); this sim runs real gravity + peg
// collisions, but every peg bounce is biased so the ball honors the recorded
// path — what you watch is what paid. Randomness here is cosmetic only.

export interface Peg {
  row: number;
  col: number;
  x: number;
  y: number;
}

export interface PlinkoGeometry {
  rows: number;
  w: number;
  top: number;
  bottom: number;
  gapX: number;
  gapY: number;
  pegR: number;
  ballR: number;
  /** Pegs by row number (rows run 2..rows). */
  pegRows: Peg[][];
  bucketX(bucket: number): number;
}

export function buildGeometry(rows: number, w: number, top: number, bottom: number): PlinkoGeometry {
  const gapX = w / (rows + 2);
  const gapY = (bottom - top) / (rows + 1);
  const pegRows: Peg[][] = [];
  for (let r = 2; r <= rows; r++) {
    const row: Peg[] = [];
    for (let c = 0; c <= r; c++) {
      row.push({ row: r, col: c, x: w / 2 + (c - r / 2) * gapX, y: top + (r - 1) * gapY });
    }
    pegRows[r] = row;
  }
  return {
    rows,
    w,
    top,
    bottom,
    gapX,
    gapY,
    pegR: Math.min(5.5, gapX * 0.17),
    ballR: Math.min(7.5, gapX * 0.24),
    pegRows,
    bucketX: (b: number) => w / 2 + (b - rows / 2) * gapX,
  };
}

// Tuned for a ~1.5s board crossing at every row count: gravity pulls, each
// peg hit resets the fall to a per-row rhythm, steering sets the lateral pace.
const G = 2400; // px/s²
const V_MAX = 780; // terminal fall speed
const RESTITUTION = 0.28; // normal bounce kept on peg contact
const TANGENTIAL_KEEP = 0.9;
const AIM_JITTER = 0.1; // ±fraction of a gap around the aim column
const AIM_VX_MAX = 3.5; // lateral speed cap, × steer speed — recovery headroom
// for tight lattices (16 rows: pegs ≈ ball-width apart)
const TAU_MIN = 0.02; // aim divisor floor when crossing a peg row
const TRANSIT_FLOOR = 0.8; // min lateral pace while a transit is owed, × steer speed
const TRANSIT_BAND = 0.22; // |error| beyond this fraction of a gap keeps the floor
const PIN_LIMIT = 12; // repeat contacts on one peg before a forced hop
const PIN_HOP_T = 0.09; // controller override while the hop carries
const ROW_TIME_CAP = 0.17;
const ROW_TIME_FLOOR = 0.095;

export interface BallEvents {
  onPegHit?(peg: Peg): void;
  onLand?(x: number): void;
}

export type BallState = "falling" | "landing" | "fading" | "done";

export class Ball {
  readonly geo: PlinkoGeometry;
  state: BallState = "falling";
  alpha = 1;
  x: number;
  y: number;
  vx = 0;
  vy = 0;
  trail: { x: number; y: number }[] = [];

  private path: boolean[];
  private events: BallEvents;
  private cum = 0; // decided offset so far, in half-gaps (+ = right)
  private nextRow = 2; // next peg row owed a steering decision
  private nextIdx = 0; // decisions consumed from the path
  private finalLane = false; // past the last peg, falling into the bucket
  private aimOff = 0; // cosmetic offset from the column center for this row
  private maxVx = 0; // lateral speed cap
  private stateT = 0;
  private pinPeg: Peg | null = null; // peg the ball may be resting on
  private pinCount = 0; // consecutive contacts with it
  private overrideT = 0; // >0: the aim controller yields to a forced hop
  private rowTime: number;
  private steerV: number;
  private minVy: number;

  /** Aim jitter around the column center — small enough that the aim point
   *  stays in free space, never inside a peg (targets on the peg surface
   *  pin the ball against it). */
  private flankOff(): number {
    return (Math.random() - 0.5) * this.geo.gapX * AIM_JITTER;
  }

  constructor(geo: PlinkoGeometry, path: boolean[], events: BallEvents = {}) {
    this.geo = geo;
    this.path = path;
    this.events = events;
    this.rowTime = Math.min(ROW_TIME_CAP, Math.max(ROW_TIME_FLOOR, 1.5 / geo.rows));
    this.steerV = (geo.gapX * 0.5) / this.rowTime;
    this.maxVx = this.steerV * AIM_VX_MAX;
    this.minVy = Math.max(60, (geo.gapY - 0.5 * G * this.rowTime * this.rowTime) / this.rowTime);
    this.aimOff = this.flankOff();
    this.x = geo.w / 2 + (Math.random() - 0.5) * 6;
    this.y = geo.top - 24;
    this.vy = 40;
    this.vx = (Math.random() - 0.5) * 30;
  }

  /** Cosmetic-only random uses; the path itself comes from the server. */
  step(dt: number): void {
    if (this.state === "done") return;
    if (this.state === "landing") {
      this.stateT += dt;
      this.y += 150 * dt;
      this.alpha = Math.max(0, 1 - this.stateT / 0.16);
      if (this.alpha <= 0) this.state = "done";
      return;
    }
    if (this.state === "fading") {
      this.stateT += dt;
      this.y += 60 * dt;
      this.alpha = Math.max(0, 1 - this.stateT / 0.3);
      if (this.alpha <= 0) this.state = "done";
      return;
    }

    // Aim the lateral velocity at the column the server's path says is next,
    // but never let it decay below the natural transit pace while the transit
    // is still owed — otherwise the ball crawls off a peg it just hit and
    // visibly stalls on top of it.
    const targetX = this.geo.w / 2 + this.cum * (this.geo.gapX / 2) + this.aimOff;
    const rowY = this.finalLane
      ? this.geo.bottom
      : this.geo.top + (Math.min(Math.max(this.nextRow, 2), this.geo.rows) - 1) * this.geo.gapY;
    // Exact time-to-row under gravity — dividing distance by the current fall
    // speed would ignore acceleration and undershoot every lateral intercept.
    const sRow = rowY - this.y;
    const vFall = Math.max(this.minVy, this.vy);
    const tau =
      sRow <= 0 ? TAU_MIN : (-vFall + Math.sqrt(vFall * vFall + 2 * G * sRow)) / G;
    const err = targetX - this.x;
    let desired = Math.max(-this.maxVx, Math.min(this.maxVx, err / tau));
    if (!this.finalLane && Math.abs(err) > this.geo.gapX * TRANSIT_BAND) {
      desired = Math.sign(err) * Math.max(Math.abs(desired), TRANSIT_FLOOR * this.steerV);
    }
    if (this.overrideT > 0) {
      this.overrideT = Math.max(0, this.overrideT - dt);
    } else {
      this.vx = desired;
    }
    this.vy = Math.min(this.vy + G * dt, V_MAX);
    this.x += this.vx * dt;
    this.y += this.vy * dt;

    this.collidePegs();

    if (this.finalLane) {
      // Runaway guard: redirect the velocity, never move the ball — a
      // position snap here reads as a teleport. The aim controller does the
      // actual steering into the bucket; the ball may legitimately enter the
      // lane up to a full gap wide (last peg contact + lane veer pointing
      // the same way, or a grazed neighboring peg on tight lattices).
      const lane = this.geo.w / 2 + this.cum * (this.geo.gapX / 2);
      const maxOff = this.geo.gapX * 1.05;
      if (this.x < lane - maxOff && this.vx < 0) {
        this.vx = -this.vx * 0.4;
      } else if (this.x > lane + maxOff && this.vx > 0) {
        this.vx = -this.vx * 0.4;
      }
    }

    if (this.y >= this.geo.bottom + 4) {
      this.state = "landing";
      this.stateT = 0;
      this.events.onLand?.(this.x);
    }
  }

  fadeOut(): void {
    if (this.state === "falling") {
      this.state = "fading";
      this.stateT = 0;
    }
  }

  pushTrail(): void {
    this.trail.push({ x: this.x, y: this.y });
    if (this.trail.length > 9) this.trail.shift();
  }

  get speed(): number {
    return Math.hypot(this.vx, this.vy);
  }

  private collidePegs(): void {
    const rSum = this.geo.ballR + this.geo.pegR;
    const rf = (this.y - this.geo.top) / this.geo.gapY + 1;
    for (const r of [Math.floor(rf), Math.ceil(rf)]) {
      if (r < 2 || r > this.geo.rows) continue;
      for (const peg of this.geo.pegRows[r]) {
        const dx = this.x - peg.x;
        if (dx > rSum || dx < -rSum) continue;
        const dy = this.y - peg.y;
        const d2 = dx * dx + dy * dy;
        if (d2 >= rSum * rSum || d2 < 1e-6) continue;
        const d = Math.sqrt(d2);
        const nx = dx / d;
        const ny = dy / d;
        this.x = peg.x + nx * (rSum + 0.01);
        this.y = peg.y + ny * (rSum + 0.01);
        const vn = this.vx * nx + this.vy * ny;
        if (vn < 0) {
          this.vx -= (1 + RESTITUTION) * vn * nx;
          this.vy -= (1 + RESTITUTION) * vn * ny;
        }
        this.vx *= TANGENTIAL_KEEP;
        if (this.steerOff(peg)) {
          this.pinPeg = null;
          this.pinCount = 0;
        } else {
          // Repeat contact with an already-consumed peg: keep descending, and
          // if the ball has rested on one peg too long, hop it off decisively
          // with the controller briefly overridden so it isn't steered back.
          this.vy = Math.max(this.vy, this.minVy * 0.7);
          if (this.pinPeg === peg) {
            this.pinCount++;
          } else {
            this.pinPeg = peg;
            this.pinCount = 1;
          }
          if (this.pinCount > PIN_LIMIT) {
            // Forced hop: re-velocity AND physically nudge off the sphere so
            // the next contact can't immediately re-pin it.
            this.x = peg.x + nx * (rSum + 3);
            this.y = peg.y + ny * (rSum + 3);
            this.vx = nx * this.steerV;
            this.vy = this.minVy;
            this.overrideT = PIN_HOP_T;
            this.pinPeg = null;
            this.pinCount = 0;
          }
        }
      }
    }
  }

  /** Applies the path decision for this peg; true if this contact consumed one. */
  private steerOff(peg: Peg): boolean {
    if (peg.row < this.nextRow || this.nextIdx >= this.path.length) return false;
    // If a peg row got skipped (fast fall), fast-forward the missed decisions.
    const i = peg.row - 2;
    for (let k = this.nextIdx; k < i; k++) this.cum += this.path[k] ? 1 : -1;
    this.nextIdx = i;

    const s = this.path[i] ? 1 : -1;
    this.cum += s;
    this.nextRow = peg.row + 1;
    this.nextIdx = i + 1;
    this.aimOff = this.flankOff();
    const kick = Math.min(this.maxVx, Math.max(Math.abs(this.vx) * 0.6, this.steerV * (0.95 + Math.random() * 0.25)));
    this.vx = s * kick;
    this.vy = Math.max(this.vy * 0.45, this.minVy);
    this.events.onPegHit?.(peg);

    // The path has one more decision than peg rows: the final L/R is the
    // lane entry, applied as the veer off the last peg.
    if (peg.row === this.geo.rows && this.nextIdx < this.path.length) {
      const s2 = this.path[this.nextIdx] ? 1 : -1;
      this.cum += s2;
      this.nextIdx = this.path.length;
      this.finalLane = true;
      this.aimOff = 0;
      this.vx = s2 * this.steerV * 1.05;
    }
    return true;
  }
}

export class PlinkoWorld {
  readonly geo: PlinkoGeometry;

  constructor(rows: number, w: number, top: number, bottom: number) {
    this.geo = buildGeometry(rows, w, top, bottom);
  }

  drop(path: boolean[], events?: BallEvents): Ball {
    return new Ball(this.geo, path, events);
  }
}
