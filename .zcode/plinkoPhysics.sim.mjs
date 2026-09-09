function buildGeometry(rows, w, top, bottom) {
  const gapX = w / (rows + 2);
  const gapY = (bottom - top) / (rows + 1);
  const pegRows = [];
  for (let r = 2; r <= rows; r++) {
    const row = [];
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
    bucketX: (b) => w / 2 + (b - rows / 2) * gapX
  };
}
const G = 2400;
const V_MAX = 780;
const RESTITUTION = 0.28;
const TANGENTIAL_KEEP = 0.9;
const AIM_JITTER = 0.1;
const AIM_VX_MAX = 3.5;
const TAU_MIN = 0.02;
const TRANSIT_FLOOR = 0.8;
const TRANSIT_BAND = 0.22;
const PIN_LIMIT = 12;
const PIN_HOP_T = 0.09;
const ROW_TIME_CAP = 0.17;
const ROW_TIME_FLOOR = 0.095;
class Ball {
  constructor(geo, path, events = {}) {
    this.state = "falling";
    this.alpha = 1;
    this.vx = 0;
    this.vy = 0;
    this.trail = [];
    this.cum = 0;
    // decided offset so far, in half-gaps (+ = right)
    this.nextRow = 2;
    // next peg row owed a steering decision
    this.nextIdx = 0;
    // decisions consumed from the path
    this.finalLane = false;
    // past the last peg, falling into the bucket
    this.aimOff = 0;
    // cosmetic offset from the column center for this row
    this.maxVx = 0;
    // lateral speed cap
    this.stateT = 0;
    this.pinPeg = null;
    // peg the ball may be resting on
    this.pinCount = 0;
    // consecutive contacts with it
    this.overrideT = 0;
    this.geo = geo;
    this.path = path;
    this.events = events;
    this.rowTime = Math.min(ROW_TIME_CAP, Math.max(ROW_TIME_FLOOR, 1.5 / geo.rows));
    this.steerV = geo.gapX * 0.5 / this.rowTime;
    this.maxVx = this.steerV * AIM_VX_MAX;
    this.minVy = Math.max(60, (geo.gapY - 0.5 * G * this.rowTime * this.rowTime) / this.rowTime);
    this.aimOff = this.flankOff();
    this.x = geo.w / 2 + (Math.random() - 0.5) * 6;
    this.y = geo.top - 24;
    this.vy = 40;
    this.vx = (Math.random() - 0.5) * 30;
  }
  /** Aim jitter around the column center — small enough that the aim point
   *  stays in free space, never inside a peg (targets on the peg surface
   *  pin the ball against it). */
  flankOff() {
    return (Math.random() - 0.5) * this.geo.gapX * AIM_JITTER;
  }
  /** Cosmetic-only random uses; the path itself comes from the server. */
  step(dt) {
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
    const targetX = this.geo.w / 2 + this.cum * (this.geo.gapX / 2) + this.aimOff;
    const rowY = this.finalLane ? this.geo.bottom : this.geo.top + (Math.min(Math.max(this.nextRow, 2), this.geo.rows) - 1) * this.geo.gapY;
    const sRow = rowY - this.y;
    const vFall = Math.max(this.minVy, this.vy);
    const tau = sRow <= 0 ? TAU_MIN : (-vFall + Math.sqrt(vFall * vFall + 2 * G * sRow)) / G;
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
  fadeOut() {
    if (this.state === "falling") {
      this.state = "fading";
      this.stateT = 0;
    }
  }
  pushTrail() {
    this.trail.push({ x: this.x, y: this.y });
    if (this.trail.length > 9) this.trail.shift();
  }
  get speed() {
    return Math.hypot(this.vx, this.vy);
  }
  collidePegs() {
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
          this.vy = Math.max(this.vy, this.minVy * 0.7);
          if (this.pinPeg === peg) {
            this.pinCount++;
          } else {
            this.pinPeg = peg;
            this.pinCount = 1;
          }
          if (this.pinCount > PIN_LIMIT) {
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
  steerOff(peg) {
    if (peg.row < this.nextRow || this.nextIdx >= this.path.length) return false;
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
class PlinkoWorld {
  constructor(rows, w, top, bottom) {
    this.geo = buildGeometry(rows, w, top, bottom);
  }
  drop(path, events) {
    return new Ball(this.geo, path, events);
  }
}
export {
  Ball,
  PlinkoWorld,
  buildGeometry
};
