"use client";

// Crash flight scene: parallax starfield, sprite rocket with throttle
// states, camera zoom-out, arcade chaos (shake / warp flashes / glitch
// bursts) and an oscilloscope altitude gauge — all on one fixed-backing
// canvas, stretched with image-rendering: pixelated like the rest of the
// arcade. The engine never invents game state: React feeds phase +
// multiplier ticks and the scene extrapolates between them with the same
// growth law the backend uses (m = e^(0.12 t)).

export type ScenePhase = "idle" | "betting" | "locked" | "running" | "settled";

const GROWTH = 0.12;
const TAU = Math.PI * 2;

// Fixed logical resolution, stretched to the panel by CSS.
const W = 1280;
const H = 640;
const GAUGE_H = 56;
// Frames are 280×280 aligned canvases where the ship occupies ~60% of the
// height; this base is sized so the drawn SHIP matches the old 105px target.
const SHIP_BASE_H = 170;

const SPRITES = [
  "flight-1",
  "flight-2",
  "flight-3",
  "flight-4",
  "flight-tilt",
  "crash-1",
  "crash-2",
  "crash-3",
  "crash-4",
] as const;
type SpriteName = (typeof SPRITES)[number];

// Exhaust anchor (fraction of the frame) + base rotation, measured from the
// sprite bounding boxes. The tilt sprite ships pre-rotated ~38°.
const SPRITE_META: Record<SpriteName, { ex: number; ey: number; rot: number }> = {
  "flight-1": { ex: 0.5, ey: 0.66, rot: 0.66 },
  "flight-2": { ex: 0.5, ey: 0.76, rot: 0.66 },
  "flight-3": { ex: 0.5, ey: 0.82, rot: 0.66 },
  "flight-4": { ex: 0.5, ey: 0.88, rot: 0.66 },
  "flight-tilt": { ex: 0.36, ey: 0.84, rot: 0.02 },
  "crash-1": { ex: 0.47, ey: 0.83, rot: -0.08 },
  "crash-2": { ex: 0.51, ey: 0.84, rot: -0.08 },
  "crash-3": { ex: 0.5, ey: 0.5, rot: 0 },
  "crash-4": { ex: 0.5, ey: 0.5, rot: 0 },
};

interface Star {
  x: number;
  y: number;
  depth: number; // 0.25 far · 0.55 mid · 1 near
  size: number;
  hue: number; // 0 white · 1 cyan · 2 warm
  tw: number; // twinkle phase
}

interface Particle {
  x: number;
  y: number;
  vx: number;
  vy: number;
  life: number; // seconds remaining
  ttl: number;
  size: number;
  kind: "exhaust" | "smoke" | "debris" | "spark";
  shade: number; // 0..1 colour pick within kind
}

interface Ring {
  x: number;
  y: number;
  r: number;
  v: number;
  alpha: number;
  color: string; // "r,g,b"
}

interface Streak {
  x: number;
  y: number;
  len: number;
  sp: number;
  a: number;
}

interface Rock {
  c: HTMLCanvasElement;
  x: number;
  y: number;
  depth: number;
  rot: number;
  vr: number;
}

interface Planet {
  c: HTMLCanvasElement;
  x: number;
  y: number;
  size: number;
  speed: number;
}

function rand(a: number, b: number): number {
  return a + Math.random() * (b - a);
}

export class CrashScene {
  private canvas: HTMLCanvasElement;
  private ctx: CanvasRenderingContext2D;
  private raf = 0;
  private alive = false;
  private last = 0;
  private time = 0; // seconds since start

  private phase: ScenePhase = "idle";
  private phaseAt = 0; // time the phase was entered
  private tickM = 1; // last server multiplier
  private tickAt = 0; // time of that tick
  private crashM = 1;
  private autoTarget: number | null = null;

  private stars: Star[] = [];
  private particles: Particle[] = [];
  private rings: Ring[] = [];
  private trail: { t: number; m: number }[] = [];

  private trauma = 0; // 0..1, drives shake
  private glitchUntil = -1;
  private nextGlitchAt = 0;
  private punch = 0; // milestone zoom punch 0..1

  private sprites = new Map<SpriteName, HTMLImageElement>();
  private planets: Planet[] = [];
  private rocks: Rock[] = [];
  private streaks: Streak[] = [];
  private gaugeFont: string | null = null;

  private reduced = false;

  constructor(canvas: HTMLCanvasElement) {
    this.canvas = canvas;
    canvas.width = W;
    canvas.height = H;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("crash scene: no 2d context");
    this.ctx = ctx;
    this.ctx.imageSmoothingEnabled = false;

    if (typeof window !== "undefined" && window.matchMedia) {
      this.reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    }

    for (let i = 0; i < 150; i++) {
      const depth = [0.25, 0.55, 1][i % 3];
      this.stars.push({
        x: Math.random() * W,
        y: Math.random() * (H - GAUGE_H),
        depth,
        size: depth > 0.8 ? 2 : Math.random() < 0.3 ? 2 : 1,
        hue: Math.random() < 0.72 ? 0 : Math.random() < 0.6 ? 1 : 2,
        tw: Math.random() * TAU,
      });
    }

    this.loadSprites();
    // A few ringed planets at different depths…
    const palettes = [
      ["#8a5cc9", "#b07ae0", "#6f46a8", "#c78ae8", "#7d51b5", "#9c66d4"],
      ["#2e7f8f", "#5fc4d4", "#1f5c68", "#8fdcea", "#37909f", "#6fd4e2"],
      ["#a8502e", "#d47a4a", "#7c3a20", "#e89a66", "#8f4a2a", "#c46838"],
    ];
    const spots = [
      { x: W * 0.82, y: H * 0.22, size: 104, speed: 0.045 },
      { x: W * 0.3, y: H * 0.14, size: 66, speed: 0.03 },
      { x: W * 0.6, y: H * 0.55, size: 44, speed: 0.02 },
    ];
    palettes.forEach((bands, i) => {
      this.planets.push({
        c: this.makePlanet(bands),
        x: spots[i].x,
        y: spots[i].y,
        size: spots[i].size,
        speed: spots[i].speed,
      });
    });
    // …and a drifting asteroid belt.
    for (let i = 0; i < 9; i++) {
      this.rocks.push({
        c: this.makeAsteroid(i % 3),
        x: Math.random() * W,
        y: Math.random() * (H - GAUGE_H),
        depth: rand(0.35, 1),
        rot: Math.random() * TAU,
        vr: rand(-0.5, 0.5),
      });
    }
  }

  private loadSprites(): void {
    if (typeof window === "undefined") return;
    for (const name of SPRITES) {
      const img = new Image();
      // versioned so sprite-art updates bust browser caches
      img.src = `/sprites/space/${name}.png?v=3`;
      img.decoding = "async";
      this.sprites.set(name, img);
    }
  }

  // A little ringed pixel planet, drawn small then upscaled hard.
  private makePlanet(bands: string[]): HTMLCanvasElement {
    const s = 52;
    const c = document.createElement("canvas");
    c.width = s;
    c.height = s / 2;
    const g = c.getContext("2d")!;
    const cx = s / 4;
    const cy = s / 4;
    const r = 9;
    for (let i = -r; i < r; i++) {
      g.fillStyle = bands[(i + r) % bands.length];
      const wide = Math.sqrt(Math.max(0, r * r - i * i));
      g.fillRect(Math.round(cx - wide), Math.round(cy + i), Math.round(wide * 2), 1);
    }
    // shadow crescent
    g.globalCompositeOperation = "source-atop";
    g.fillStyle = "rgba(6,4,13,.72)";
    g.beginPath();
    g.arc(cx - r * 0.55, cy - r * 0.3, r * 1.05, 0, TAU);
    g.fill();
    g.globalCompositeOperation = "source-over";
    // rings
    g.lineWidth = 1;
    g.strokeStyle = "#22e8ff";
    g.globalAlpha = 0.85;
    g.beginPath();
    g.ellipse(cx, cy, 13.5, 4.2, -0.32, 0, TAU);
    g.stroke();
    g.strokeStyle = "#ff2d95";
    g.globalAlpha = 0.5;
    g.beginPath();
    g.ellipse(cx, cy, 15.5, 5, -0.32, 0, TAU);
    g.stroke();
    return c;
  }

  // A lumpy pixel asteroid with crater pits; three silhouettes.
  private makeAsteroid(variant: number): HTMLCanvasElement {
    const s = 14;
    const c = document.createElement("canvas");
    c.width = s;
    c.height = s;
    const g = c.getContext("2d")!;
    const base = ["#6b5f9e", "#7c6a4a", "#5c5470"][variant];
    const dark = ["#3f3760", "#54462e", "#3a344c"][variant];
    // silhouette: irregular radius per direction
    const pts = 8;
    const radii = Array.from({ length: pts }, () => rand(4, 6.5));
    g.fillStyle = base;
    for (let i = 0; i < pts; i++) {
      const a0 = (i / pts) * TAU;
      const a1 = ((i + 1) / pts) * TAU;
      g.beginPath();
      g.moveTo(s / 2, s / 2);
      g.arc(s / 2, s / 2, radii[i], a0, a1);
      g.fill();
    }
    // craters + shade
    for (let i = 0; i < 4; i++) {
      g.fillStyle = dark;
      g.fillRect(Math.round(rand(3, s - 5)), Math.round(rand(3, s - 5)), 2, 2);
    }
    g.fillStyle = "rgba(6,4,13,.5)";
    g.fillRect(2, s - 5, s - 4, 3);
    return c;
  }

  start(): void {
    if (this.alive) return;
    this.alive = true;
    this.last = performance.now();
    const loop = (now: number) => {
      if (!this.alive) return;
      const dt = Math.min(0.05, (now - this.last) / 1000);
      this.last = now;
      this.time += dt;
      this.step(dt);
      this.draw();
      this.raf = requestAnimationFrame(loop);
    };
    this.raf = requestAnimationFrame(loop);
  }

  destroy(): void {
    this.alive = false;
    cancelAnimationFrame(this.raf);
  }

  setPhase(phase: ScenePhase): void {
    if (this.phase === phase) return;
    const prev = this.phase;
    this.phase = phase;
    this.phaseAt = this.time;
    if (phase === "betting" || phase === "idle") {
      this.tickM = 1;
      this.tickAt = this.time;
      this.trail = [];
      this.trauma = 0;
      this.glitchUntil = -1;
      this.crashM = 1;
      if (prev === "settled") this.particles.length = 0;
    }
    if (phase === "locked") {
      this.addTrauma(this.reduced ? 0.1 : 0.35);
    }
    if (phase === "running") {
      this.tickAt = this.time;
      this.trail = [{ t: this.time, m: 1 }];
      this.addTrauma(this.reduced ? 0.15 : 0.5);
      this.burstIgnition();
    }
  }

  /** Server tick — the scene extrapolates between these. */
  setMultiplier(m: number): void {
    if (this.phase !== "running") return;
    this.tickM = Math.max(1, m);
    this.tickAt = this.time;
    this.trail.push({ t: this.time, m: this.tickM });
    if (this.trail.length > 900) this.trail.splice(0, this.trail.length - 900);
  }

  setCrash(m: number): void {
    this.crashM = Math.max(1, m);
    this.tickM = this.crashM;
    this.tickAt = this.time;
    this.trail.push({ t: this.time, m: this.crashM });
    if (!this.reduced) {
      this.addTrauma(1);
      this.burstExplosion();
    }
  }

  setAutoTarget(t: number | null): void {
    this.autoTarget = t && t > 1.01 ? t : null;
  }

  /** Milestone crossing (2x, 5x …): ring + zoom punch + shake kick. */
  milestone(m: number): void {
    if (this.reduced) return;
    const col =
      m >= 25 ? "242,100,61" : m >= 10 ? "255,45,149" : m >= 5 ? "255,177,92" : "95,224,138";
    this.punch = 1;
    this.addTrauma(0.35);
    const p = this.shipPos();
    this.rings.push({ x: p.x, y: p.y, r: 12, v: 900, alpha: 0.8, color: col });
  }

  private addTrauma(v: number): void {
    this.trauma = Math.min(1, this.trauma + v);
  }

  /** Current display multiplier: server ticks extrapolated by the growth law. */
  private displayM(): number {
    if (this.phase === "running") {
      const dt = Math.min(0.25, this.time - this.tickAt);
      return this.tickM * Math.exp(GROWTH * dt);
    }
    if (this.phase === "settled") return this.crashM;
    return 1;
  }

  // ---------- ship geometry ----------

  /** Camera zoom-out: the ship starts big and shrinks as m climbs. */
  private shipScale(): number {
    const m = this.displayM();
    const p = Math.min(1, Math.log(m) / Math.log(40));
    return (1.95 - 1.25 * Math.sqrt(p)) * (1 - this.punch * 0.09);
  }

  private shipPos(): { x: number; y: number } {
    if (this.phase === "running" || this.phase === "settled") {
      const m = this.displayM();
      const p = Math.min(1, Math.log(m) / Math.log(60));
      // bezier arc from the pad up toward centre-right
      const x = (1 - p) * (1 - p) * (W * 0.2) + 2 * (1 - p) * p * (W * 0.3) + p * p * (W * 0.62);
      const y = (1 - p) * (1 - p) * (H * 0.72) + 2 * (1 - p) * p * (H * 0.3) + p * p * (H * 0.26);
      const wob = this.phase === "running" ? Math.min(6, Math.log(m) * 1.7) : 2;
      return {
        x: x + Math.sin(this.time * 7.3) * wob * 0.5,
        y: y + Math.cos(this.time * 5.1) * wob * 0.5,
      };
    }
    // docked on the pad
    return { x: W * 0.2, y: H * 0.7 };
  }

  private shipScaleNow(): number {
    if (this.phase === "running") return this.shipScale();
    if (this.phase === "locked") return 1.6;
    if (this.phase === "settled") return Math.max(0.85, this.shipScale() * 0.9);
    return 1.3;
  }

  private spriteFor(): SpriteName {
    const t = this.time - this.phaseAt;
    if (this.phase === "betting" || this.phase === "idle") return "flight-1";
    if (this.phase === "locked") return "flight-2";
    if (this.phase === "settled") {
      if (t < 0.3) return "crash-1";
      if (t < 0.6) return "crash-2";
      if (t < 1.05) return "crash-3";
      return "crash-4";
    }
    const m = this.displayM();
    if (m >= 8) return "flight-tilt";
    if (m >= 4) return "flight-4";
    if (m >= 2) return "flight-3";
    return "flight-2";
  }

  // ---------- particles ----------

  private spawnExhaust(dt: number): void {
    if (this.phase !== "running" && this.phase !== "locked") return;
    const name = this.spriteFor();
    const meta = SPRITE_META[name];
    const img = this.sprites.get(name);
    const dh = SHIP_BASE_H * this.shipScaleNow();
    const dw = dh * ((img?.width || 90) / (img?.height || 130));
    const p = this.shipPos();
    const rot = meta.rot;
    // exhaust anchor in rotated ship space
    const lx = (meta.ex - 0.5) * dw;
    const ly = (meta.ey - 0.5) * dh;
    const ex = p.x + lx * Math.cos(rot) - ly * Math.sin(rot);
    const ey = p.y + lx * Math.sin(rot) + ly * Math.cos(rot);
    // flow opposite the nose: local down (0,1) rotated by `rot`
    const dx = -Math.sin(rot);
    const dy = Math.cos(rot);
    const power = this.phase === "locked" ? 0.55 : 0.6 + this.shipScaleNow() * 0.4;
    const rate = this.phase === "locked" ? 140 : 55 + this.displayM() * 12;
    let count = rate * dt + Math.random();
    while (count >= 1) {
      count -= 1;
      const sp = rand(120, 260) * power;
      this.particles.push({
        x: ex + rand(-3, 3),
        y: ey + rand(-3, 3),
        vx: dx * sp + rand(-26, 26),
        vy: dy * sp + rand(-26, 26),
        life: rand(0.25, 0.6),
        ttl: 0.6,
        size: rand(2, 5) * (0.7 + power * 0.6),
        kind: "exhaust",
        shade: Math.random(),
      });
    }
  }

  private burstIgnition(): void {
    if (this.reduced) return;
    const p = this.shipPos();
    for (let i = 0; i < 46; i++) {
      this.particles.push({
        x: p.x + rand(-30, 30),
        y: p.y + rand(10, 40),
        vx: rand(-160, 160),
        vy: rand(20, 150),
        life: rand(0.5, 1.4),
        ttl: 1.4,
        size: rand(3, 8),
        kind: "smoke",
        shade: Math.random(),
      });
    }
  }

  private burstExplosion(): void {
    const p = this.shipPos();
    for (let i = 0; i < 90; i++) {
      const a = Math.random() * TAU;
      const sp = rand(60, 520);
      this.particles.push({
        x: p.x,
        y: p.y,
        vx: Math.cos(a) * sp,
        vy: Math.sin(a) * sp,
        life: rand(0.5, 1.8),
        ttl: 1.8,
        size: rand(2, 7),
        kind: Math.random() < 0.62 ? "debris" : "spark",
        shade: Math.random(),
      });
    }
    this.rings.push({ x: p.x, y: p.y, r: 8, v: 1300, alpha: 0.9, color: "255,138,31" });
  }

  // ---------- simulation ----------

  private step(dt: number): void {
    const m = this.displayM();

    // turbulence + running rumble feed the shake
    if (this.phase === "running") {
      this.trauma = Math.min(1, this.trauma + dt * (0.06 + Math.log(m) * 0.045));
    }
    if (this.phase === "locked") {
      this.trauma = Math.min(1, this.trauma + dt * 0.9);
    }
    this.trauma = Math.max(0, this.trauma - dt * (this.phase === "locked" ? 0.4 : 0.9));
    this.punch = Math.max(0, this.punch - dt * 3.2);

    // glitch bursts at 10x+
    if (this.phase === "running" && m >= 10 && !this.reduced) {
      if (this.time >= this.nextGlitchAt && this.glitchUntil < this.time) {
        this.glitchUntil = this.time + rand(0.1, 0.24);
        this.nextGlitchAt = this.time + rand(1.4, 3.2);
      }
    }

    this.spawnExhaust(dt);

    for (let i = this.particles.length - 1; i >= 0; i--) {
      const pt = this.particles[i];
      pt.life -= dt;
      if (pt.life <= 0) {
        this.particles.splice(i, 1);
        continue;
      }
      pt.x += pt.vx * dt;
      pt.y += pt.vy * dt;
      const damp = pt.kind === "smoke" ? 0.6 : pt.kind === "debris" ? 1.4 : 2.6;
      pt.vx -= pt.vx * damp * dt;
      pt.vy -= pt.vy * damp * dt;
      if (pt.kind === "smoke") {
        pt.vy -= 26 * dt; // buoyant
        pt.size += dt * 9;
      }
    }
    if (this.particles.length > 320) this.particles.splice(0, this.particles.length - 320);

    for (let i = this.rings.length - 1; i >= 0; i--) {
      const r = this.rings[i];
      r.r += r.v * dt;
      r.alpha -= dt * 1.4;
      if (r.alpha <= 0) this.rings.splice(i, 1);
    }

    // star drift speed
    let v: number;
    if (this.phase === "running") v = 46 + 1050 * Math.pow(Math.min(1, Math.log(m) / Math.log(50)), 1.7);
    else if (this.phase === "locked") v = 90;
    else if (this.phase === "settled") v = 26;
    else v = 13;
    if (this.punch > 0) v *= 1.5;

    // speed lines: the hotter the engine, the more streaks blow past
    if (this.phase === "running" && m > 1.8 && !this.reduced) {
      const heat = Math.log(m);
      let spawn = (heat - 0.55) * 10 * dt + Math.random();
      while (spawn >= 1) {
        spawn -= 1;
        this.streaks.push({
          x: rand(0, W + 200),
          y: rand(-60, H - GAUGE_H),
          len: rand(60, 170) * (1 + heat * 0.14),
          sp: v * rand(2.2, 3.4),
          a: rand(0.22, 0.6),
        });
      }
    }
    for (let i = this.streaks.length - 1; i >= 0; i--) {
      const s = this.streaks[i];
      s.x -= s.sp * dt * 0.62;
      s.y += s.sp * dt * 0.79;
      if (s.y > H || s.x < -s.len) this.streaks.splice(i, 1);
    }
    if (this.streaks.length > 60) this.streaks.splice(0, this.streaks.length - 60);

    for (const s of this.stars) {
      const d = v * s.depth;
      s.x -= d * dt * 0.62;
      s.y += d * dt * 0.79;
      if (s.y > H - GAUGE_H + 4) {
        s.y -= H - GAUGE_H + 8;
        s.x = Math.random() * W;
      }
      if (s.x < -4) {
        s.x += W + 8;
        s.y = Math.random() * (H - GAUGE_H);
      }
    }

    // planets + asteroids parallax crawl
    for (const p of this.planets) {
      p.x -= v * p.speed * dt;
      p.y += v * p.speed * 0.66 * dt;
      if (p.x < -p.size * 1.4 || p.y > H) {
        p.x = W + rand(60, 500);
        p.y = rand(10, H * 0.55);
      }
    }
    for (const rk of this.rocks) {
      rk.x -= v * rk.depth * 0.4 * dt;
      rk.y += v * rk.depth * 0.5 * dt;
      rk.rot += rk.vr * dt;
      if (rk.y > H - GAUGE_H + 20 || rk.x < -40) {
        rk.x = rand(0, W + 260);
        rk.y = rand(-40, -10);
        rk.depth = rand(0.35, 1);
      }
    }
  }

  // ---------- drawing ----------

  private draw(): void {
    const ctx = this.ctx;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.imageSmoothingEnabled = false;

    // deep space ground
    ctx.fillStyle = "#06040d";
    ctx.fillRect(0, 0, W, H);

    // camera shake
    let sx = 0;
    let sy = 0;
    if (this.trauma > 0 && !this.reduced) {
      const s = this.trauma * this.trauma * 16;
      sx = Math.round(rand(-s, s));
      sy = Math.round(rand(-s, s));
    }
    ctx.translate(sx, sy);

    this.drawStars();
    this.drawPlanets();
    this.drawRocks();
    this.drawParticles(true);
    this.drawPad();
    this.drawShip();
    this.drawParticles(false);
    this.drawRings();

    ctx.setTransform(1, 0, 0, 1, 0, 0);
    if (this.glitchUntil > this.time) this.drawGlitch();
    this.drawGauge();
  }

  private starSpeed(): number {
    const m = this.displayM();
    if (this.phase === "running")
      return 46 + 1050 * Math.pow(Math.min(1, Math.log(m) / Math.log(50)), 1.7);
    if (this.phase === "locked") return 90;
    if (this.phase === "settled") return 26;
    return 13;
  }

  private drawStars(): void {
    const ctx = this.ctx;
    const running = this.phase === "running";
    const speed = this.starSpeed();
    for (const s of this.stars) {
      const tw = 0.55 + 0.45 * Math.sin(this.time * 3 + s.tw);
      const col = s.hue === 0 ? "236,230,255" : s.hue === 1 ? "34,232,255" : "255,177,92";
      const streak = running ? Math.min(2 + speed * s.depth * 0.055, 90) : 0;
      ctx.fillStyle = `rgba(${col},${(0.35 + 0.65 * tw) * (0.4 + s.depth * 0.6)})`;
      if (streak > 3) {
        ctx.fillRect(Math.round(s.x), Math.round(s.y), Math.round(s.size), Math.round(streak));
      } else {
        ctx.fillRect(Math.round(s.x), Math.round(s.y), s.size, s.size);
      }
    }
  }

  private drawPlanets(): void {
    const ctx = this.ctx;
    for (const p of this.planets) {
      ctx.drawImage(p.c, Math.round(p.x), Math.round(p.y), p.size, p.size / 2);
    }
  }

  private drawRocks(): void {
    const ctx = this.ctx;
    for (const rk of this.rocks) {
      const size = 10 + rk.depth * 22;
      ctx.save();
      ctx.translate(Math.round(rk.x), Math.round(rk.y));
      ctx.rotate(rk.rot);
      ctx.drawImage(rk.c, -size / 2, -size / 2, size, size);
      ctx.restore();
    }
    // speed lines: thin bright streaks along the flight vector
    if (this.streaks.length > 0) {
      ctx.lineWidth = 2;
      for (const s of this.streaks) {
        ctx.strokeStyle = `rgba(200,240,255,${s.a})`;
        ctx.beginPath();
        ctx.moveTo(Math.round(s.x), Math.round(s.y));
        ctx.lineTo(Math.round(s.x + s.len * 0.62), Math.round(s.y - s.len * 0.79));
        ctx.stroke();
      }
    }
  }

  private drawPad(): void {
    if (this.phase !== "betting" && this.phase !== "idle" && this.phase !== "locked") return;
    const ctx = this.ctx;
    const px = W * 0.2;
    const py = H * 0.7 + 58;
    // platform
    ctx.fillStyle = "#1a0d2e";
    ctx.fillRect(px - 74, py, 148, 12);
    ctx.fillStyle = "#241640";
    ctx.fillRect(px - 74, py + 12, 148, 4);
    // hazard stripes
    for (let i = 0; i < 6; i++) {
      ctx.fillStyle = i % 2 ? "#ff8a1f" : "#35205c";
      ctx.fillRect(px - 60 + i * 20, py + 2, 10, 3);
    }
    // beacon lights blink in sequence during betting
    const on = Math.floor(this.time * 3) % 3;
    for (let i = 0; i < 3; i++) {
      const lit = i === on && this.phase !== "locked";
      const bx = px - 70 + i * 62;
      ctx.fillStyle = lit ? (i === 1 ? "#5fe08a" : "#ff2d95") : "#35205c";
      ctx.fillRect(bx, py - 6, 5, 5);
    }
  }

  private drawShip(): void {
    const name = this.spriteFor();
    const img = this.sprites.get(name);
    if (!img || !img.complete || img.naturalWidth === 0) return;
    const ctx = this.ctx;
    const meta = SPRITE_META[name];
    const p = this.shipPos();
    const dh = SHIP_BASE_H * this.shipScaleNow();
    const dw = dh * (img.width / img.height);

    let rot = meta.rot;
    if (this.phase === "running" || this.phase === "locked") {
      rot += Math.sin(this.time * 9.1) * 0.03;
    }
    if (this.phase === "settled") {
      const t = this.time - this.phaseAt;
      rot += Math.min(0.35, t * 0.4); // tumbling
    }

    const alpha =
      this.phase === "settled"
        ? Math.max(0.35, 1 - Math.max(0, this.time - this.phaseAt - 1.2) * 0.5)
        : 1;

    ctx.save();
    ctx.translate(Math.round(p.x), Math.round(p.y));
    ctx.rotate(rot);
    ctx.globalAlpha = alpha;

    // chromatic fringe while glitching
    if (this.glitchUntil > this.time) {
      ctx.globalCompositeOperation = "lighter";
      ctx.globalAlpha = alpha * 0.4;
      ctx.drawImage(img, -dw / 2 + 4, -dh / 2, dw, dh);
      ctx.drawImage(img, -dw / 2 - 4, -dh / 2, dw, dh);
      ctx.globalCompositeOperation = "source-over";
      ctx.globalAlpha = alpha;
    }
    ctx.drawImage(img, Math.round(-dw / 2), Math.round(-dh / 2), Math.round(dw), Math.round(dh));
    ctx.restore();
    ctx.globalAlpha = 1;
  }

  private drawParticles(smokeLayer: boolean): void {
    const ctx = this.ctx;
    for (const pt of this.particles) {
      const isSmoke = pt.kind === "smoke";
      if (smokeLayer !== isSmoke) continue;
      const a = Math.max(0, pt.life / pt.ttl);
      if (pt.kind === "exhaust") {
        ctx.fillStyle =
          pt.shade < 0.4
            ? `rgba(255,216,107,${a})`
            : pt.shade < 0.75
              ? `rgba(255,122,31,${a})`
              : `rgba(242,61,61,${a})`;
      } else if (pt.kind === "smoke") {
        ctx.fillStyle = `rgba(120,110,140,${a * 0.4})`;
      } else if (pt.kind === "debris") {
        ctx.fillStyle = pt.shade < 0.5 ? `rgba(255,138,31,${a})` : `rgba(107,95,158,${a})`;
      } else {
        ctx.fillStyle = `rgba(255,230,150,${a})`;
      }
      const s = Math.max(1, Math.round(pt.size * (pt.kind === "spark" ? a : 1)));
      ctx.fillRect(Math.round(pt.x), Math.round(pt.y), s, s);
    }
  }

  private drawRings(): void {
    const ctx = this.ctx;
    for (const r of this.rings) {
      ctx.strokeStyle = `rgba(${r.color},${Math.max(0, r.alpha)})`;
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.arc(Math.round(r.x), Math.round(r.y), Math.round(r.r), 0, TAU);
      ctx.stroke();
    }
  }

  private drawGlitch(): void {
    const ctx = this.ctx;
    for (let i = 0; i < 3; i++) {
      const y = Math.floor(rand(0, H - GAUGE_H));
      const h = Math.floor(rand(4, 26));
      const dx = Math.round(rand(-14, 14));
      ctx.drawImage(this.canvas, 0, y, W, h, dx, y, W, h);
    }
  }

  // ---------- altitude gauge ----------

  private drawGauge(): void {
    const ctx = this.ctx;
    if (!this.gaugeFont) {
      this.gaugeFont =
        typeof window !== "undefined"
          ? `13px ${getComputedStyle(this.canvas).fontFamily}`
          : "13px monospace";
    }
    const gy = H - GAUGE_H;
    ctx.fillStyle = "rgba(13,6,25,.88)";
    ctx.fillRect(0, gy, W, GAUGE_H);
    ctx.fillStyle = "#35205c";
    ctx.fillRect(0, gy, W, 2);

    const top = gy + 8;
    const bot = H - 8;
    // headroom above the current multiplier so the trace tip rides below
    // the top edge and visibly climbs into fresh space
    const yMax = Math.max(2, this.displayM() * 1.45);
    const yFor = (m: number) => bot - (Math.log(Math.max(1, m)) / Math.log(yMax)) * (bot - top);

    // multiplier gridlines
    ctx.font = this.gaugeFont;
    ctx.textAlign = "left";
    for (const g of [2, 5, 10, 25, 50, 100]) {
      if (g > yMax) break;
      const y = Math.round(yFor(g));
      ctx.fillStyle = "rgba(53,32,92,.7)";
      ctx.fillRect(0, y, W, 1);
      ctx.fillStyle = "#5c4f80";
      ctx.fillText(`${g}×`, 6, y - 3);
    }

    // auto-eject marker
    if (this.autoTarget && this.autoTarget < yMax) {
      const y = Math.round(yFor(this.autoTarget));
      ctx.strokeStyle = "rgba(95,224,138,.75)";
      ctx.setLineDash([6, 6]);
      ctx.lineWidth = 1;
      ctx.beginPath();
      ctx.moveTo(0, y + 0.5);
      ctx.lineTo(W, y + 0.5);
      ctx.stroke();
      ctx.setLineDash([]);
      ctx.fillStyle = "#5fe08a";
      ctx.textAlign = "right";
      ctx.fillText(`AUTO ${this.autoTarget.toFixed(2)}×`, W - 6, y - 4);
      ctx.textAlign = "left";
    }

    // flight trace: a sliding window that keeps scrolling left, so the
    // curve is always in motion no matter how long the round runs
    const running = this.phase === "running";
    const crashed = this.phase === "settled";
    const pts = this.trail;
    if (pts.length >= 2) {
      const tipT = running ? this.time : pts[pts.length - 1].t;
      // the window grows with elapsed time, so the trace keeps zooming
      // out: the tip pulls back from the right edge and the whole flight
      // compresses continuously instead of pinning in the corner
      const win = Math.max(8, (tipT - pts[0].t) * 1.22);
      const tLeft = tipT - win;
      const xFor = (t: number) => ((t - tLeft) / win) * (W - 16) + 8;
      ctx.strokeStyle = crashed ? "#f2643d" : "#22e8ff";
      ctx.lineWidth = 2;
      ctx.shadowColor = crashed ? "rgba(242,100,61,.8)" : "rgba(34,232,255,.8)";
      ctx.shadowBlur = 6;
      ctx.beginPath();
      let started = false;
      for (let i = 0; i < pts.length; i++) {
        if (pts[i].t < tLeft) continue;
        const x = xFor(pts[i].t);
        const y = yFor(pts[i].m);
        if (!started) {
          // enter from the left edge so the line has no floating start
          const prev = pts[Math.max(0, i - 1)];
          const px = Math.max(8, xFor(prev.t));
          ctx.moveTo(px, y);
          ctx.lineTo(Math.round(x), Math.round(y));
          started = true;
        } else {
          ctx.lineTo(Math.round(x), Math.round(y));
        }
      }
      const tipX = xFor(tipT);
      const tipM = this.displayM();
      if (running) ctx.lineTo(Math.round(tipX), Math.round(yFor(tipM)));
      ctx.stroke();
      ctx.shadowBlur = 0;

      // glowing tip
      const tipY = yFor(tipM);
      ctx.fillStyle = crashed ? "#f2643d" : "#eafeff";
      ctx.fillRect(Math.round(tipX) - 2, Math.round(tipY) - 2, 4, 4);
      if (running && !this.reduced) {
        ctx.fillStyle = "rgba(34,232,255,.3)";
        ctx.fillRect(Math.round(tipX) - 5, Math.round(tipY) - 5, 10, 10);
      }
    } else {
      ctx.fillStyle = "#5c4f80";
      ctx.font = `15px ${this.gaugeFont.slice(this.gaugeFont.indexOf(" ") + 1)}`;
      ctx.fillText(
        this.phase === "betting" || this.phase === "idle" ? "ALTITUDE TRACE — AWAITING LAUNCH" : "",
        8,
        gy + 34,
      );
    }
  }
}
