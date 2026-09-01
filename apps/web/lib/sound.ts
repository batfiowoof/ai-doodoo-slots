"use client";

// Synthesized sound effects via WebAudio — no asset files, no network.
// The AudioContext resumes on the first user gesture (the spin button).
class SoundManager {
  private ctx: AudioContext | null = null;
  private master: GainNode | null = null;
  private _muted = false;

  get muted(): boolean {
    return this._muted;
  }

  setMuted(m: boolean): void {
    this._muted = m;
    if (this.master && this.ctx) {
      this.master.gain.setTargetAtTime(m ? 0 : 1, this.ctx.currentTime, 0.01);
    }
  }

  /** Call from a user-gesture handler the first time. */
  unlock(): void {
    this.ensure();
  }

  private ensure(): AudioContext | null {
    if (typeof window === "undefined") return null;
    if (!this.ctx) {
      const Ctor =
        window.AudioContext ??
        (window as unknown as { webkitAudioContext?: typeof AudioContext })
          .webkitAudioContext;
      if (!Ctor) return null;
      this.ctx = new Ctor();
      this.master = this.ctx.createGain();
      this.master.gain.value = this._muted ? 0 : 1;
      this.master.connect(this.ctx.destination);
    }
    if (this.ctx.state === "suspended") void this.ctx.resume();
    return this.ctx;
  }

  /** One oscillator blip. */
  private blip(
    freq: number,
    durMs: number,
    opts: {
      type?: OscillatorType;
      gain?: number;
      delayMs?: number;
      slideTo?: number;
    } = {},
  ): void {
    const ctx = this.ensure();
    if (!ctx || !this.master) return;
    const { type = "square", gain = 0.04, delayMs = 0, slideTo } = opts;
    const t0 = ctx.currentTime + delayMs / 1000;
    const osc = ctx.createOscillator();
    const g = ctx.createGain();
    osc.type = type;
    osc.frequency.setValueAtTime(freq, t0);
    if (slideTo !== undefined) {
      osc.frequency.linearRampToValueAtTime(slideTo, t0 + durMs / 1000);
    }
    g.gain.setValueAtTime(0, t0);
    g.gain.linearRampToValueAtTime(gain, t0 + 0.008);
    g.gain.exponentialRampToValueAtTime(0.0001, t0 + durMs / 1000);
    osc.connect(g);
    g.connect(this.master);
    osc.start(t0);
    osc.stop(t0 + durMs / 1000 + 0.02);
  }

  click(): void {
    this.blip(880, 30, { gain: 0.03 });
  }

  error(): void {
    this.blip(180, 120, { type: "sawtooth", gain: 0.05, slideTo: 110 });
    this.blip(120, 160, {
      type: "sawtooth",
      gain: 0.05,
      delayMs: 120,
      slideTo: 80,
    });
  }

  /** Rising sweep while the reels accelerate. */
  spinStart(): void {
    this.blip(160, 260, { type: "triangle", gain: 0.05, slideTo: 640 });
  }

  /** Thud when a reel lands. */
  reelStop(index: number): void {
    this.blip(150 - index * 12, 70, { type: "triangle", gain: 0.07 });
  }

  /** Ticking counter during the win count-up. */
  winTick(step: number): void {
    this.blip(900 + (step % 5) * 120, 30, { gain: 0.03 });
  }

  /** Win jingle; richer for multiple lines. */
  winJingle(lines: number): void {
    const base = [523, 659, 784, 1047];
    const seq = lines > 1 ? [...base, 784, 1047, 1319] : base;
    seq.forEach((f, i) => {
      this.blip(f, 110, { gain: 0.05, delayMs: i * 90 });
    });
  }
}

// Module singleton.
export const sound = new SoundManager();
