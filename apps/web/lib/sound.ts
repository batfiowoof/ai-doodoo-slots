"use client";

// Layered WebAudio arcade sound: chiptune squares, mechanical noise, Vegas
// bells. No asset files, no network. One AudioContext, master gain 0.9,
// created/resumed on the first user gesture.
class SoundManager {
  private ctx: AudioContext | null = null;
  private master: GainNode | null = null;
  private noiseBuf: AudioBuffer | null = null;
  private whir: {
    src: AudioBufferSourceNode;
    osc: OscillatorNode;
    g: GainNode;
    og: GainNode;
  } | null = null;
  private _muted = false;

  get muted(): boolean {
    return this._muted;
  }

  setMuted(m: boolean): void {
    this._muted = m;
    if (this.master && this.ctx) {
      this.master.gain.setTargetAtTime(m ? 0 : 0.9, this.ctx.currentTime, 0.02);
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
      this.master.gain.value = this._muted ? 0 : 0.9;
      this.master.connect(this.ctx.destination);
    }
    if (this.ctx.state === "suspended") void this.ctx.resume();
    return this.ctx;
  }

  /** Filtered white noise burst — the mechanical layer. */
  private hit(
    dur = 0.06,
    o: {
      gain?: number;
      freq?: number;
      q?: number;
      delay?: number;
      type?: BiquadFilterType;
    } = {},
  ): void {
    const ctx = this.ensure();
    if (!ctx || !this.master) return;
    const { gain = 0.12, freq = 1400, q = 1, delay = 0, type = "bandpass" } = o;
    if (!this.noiseBuf) {
      const len = Math.floor(ctx.sampleRate * 1.2);
      this.noiseBuf = ctx.createBuffer(1, len, ctx.sampleRate);
      const d = this.noiseBuf.getChannelData(0);
      for (let i = 0; i < len; i++) d[i] = Math.random() * 2 - 1;
    }
    const t0 = ctx.currentTime + delay;
    const src = ctx.createBufferSource();
    src.buffer = this.noiseBuf;
    src.loop = true;
    const f = ctx.createBiquadFilter();
    const g = ctx.createGain();
    f.type = type;
    f.frequency.value = freq;
    f.Q.value = q;
    g.gain.setValueAtTime(gain, t0);
    g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
    src.connect(f);
    f.connect(g);
    g.connect(this.master);
    src.start(t0);
    src.stop(t0 + dur + 0.02);
  }

  /** One chiptune blip. */
  private tone(
    freq: number,
    dur: number,
    o: {
      type?: OscillatorType;
      gain?: number;
      delay?: number;
      slideTo?: number;
    } = {},
  ): void {
    const ctx = this.ensure();
    if (!ctx || !this.master) return;
    const { type = "square", gain = 0.05, delay = 0, slideTo } = o;
    const t0 = ctx.currentTime + delay;
    const osc = ctx.createOscillator();
    const g = ctx.createGain();
    osc.type = type;
    osc.frequency.setValueAtTime(freq, t0);
    if (slideTo !== undefined) {
      osc.frequency.exponentialRampToValueAtTime(Math.max(20, slideTo), t0 + dur);
    }
    g.gain.setValueAtTime(0, t0);
    g.gain.linearRampToValueAtTime(gain, t0 + 0.006);
    g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
    osc.connect(g);
    g.connect(this.master);
    osc.start(t0);
    osc.stop(t0 + dur + 0.02);
  }

  click(): void {
    this.tone(1200, 0.03, { gain: 0.05 });
    this.hit(0.03, { gain: 0.08, freq: 3000 });
  }

  error(): void {
    this.tone(180, 0.14, { type: "sawtooth", gain: 0.07, slideTo: 90 });
    this.tone(120, 0.2, {
      type: "sawtooth",
      gain: 0.07,
      delay: 0.13,
      slideTo: 60,
    });
  }

  /** Mechanical ratchet as the lever travels, then a low thunk. */
  lever(): void {
    for (let i = 0; i < 7; i++) {
      this.hit(0.03, {
        gain: 0.1 - i * 0.008,
        freq: 2600 - i * 220,
        q: 6,
        delay: i * 0.045,
      });
    }
    this.tone(90, 0.22, { type: "triangle", gain: 0.1, delay: 0.3, slideTo: 55 });
  }

  /** Looping reel whir: band-passed noise + rising sawtooth bed. */
  startWhir(): void {
    const ctx = this.ensure();
    if (!ctx || !this.master || this.whir) return;
    const src = ctx.createBufferSource();
    if (!this.noiseBuf) {
      const len = Math.floor(ctx.sampleRate * 1.2);
      this.noiseBuf = ctx.createBuffer(1, len, ctx.sampleRate);
      const d = this.noiseBuf.getChannelData(0);
      for (let i = 0; i < len; i++) d[i] = Math.random() * 2 - 1;
    }
    src.buffer = this.noiseBuf;
    src.loop = true;
    const f = ctx.createBiquadFilter();
    const g = ctx.createGain();
    const osc = ctx.createOscillator();
    const og = ctx.createGain();
    f.type = "bandpass";
    f.frequency.value = 420;
    f.Q.value = 1.4;
    g.gain.value = 0;
    g.gain.setTargetAtTime(0.09, ctx.currentTime, 0.08);
    osc.type = "sawtooth";
    osc.frequency.setValueAtTime(70, ctx.currentTime);
    osc.frequency.linearRampToValueAtTime(190, ctx.currentTime + 1.2);
    og.gain.value = 0.02;
    src.connect(f);
    f.connect(g);
    g.connect(this.master);
    osc.connect(og);
    og.connect(this.master);
    src.start();
    osc.start();
    this.whir = { src, osc, g, og };
  }

  stopWhir(): void {
    if (!this.whir || !this.ctx) return;
    const { src, osc, g, og } = this.whir;
    this.whir = null;
    const t = this.ctx.currentTime;
    g.gain.setTargetAtTime(0, t, 0.05);
    og.gain.setTargetAtTime(0, t, 0.05);
    src.stop(t + 0.3);
    osc.stop(t + 0.3);
  }

  /** Reel lands: relay clack + low thunk, pitched down per reel. */
  reelStop(index: number): void {
    this.hit(0.07, { gain: 0.16, freq: 1800 - index * 160, q: 2 });
    this.tone(150 - index * 12, 0.09, { type: "triangle", gain: 0.12 });
    this.tone(75 - index * 6, 0.14, { type: "sine", gain: 0.1 });
  }

  /**
   * Anticipation while a live reel is held: a rising whine, an accelerating
   * heartbeat, and a bell shimmer that brightens with the level.
   */
  anticipate(level: number, durMs: number): void {
    const dur = durMs / 1000;
    const base = 260 + level * 120;
    this.tone(base, dur, {
      gain: 0.03 + level * 0.008,
      slideTo: base * 3.4,
    });
    this.tone(base * 1.5, dur, {
      type: "triangle",
      gain: 0.018,
      slideTo: base * 4.6,
    });
    let t = 0.05;
    let gapMs = 210 - level * 25;
    while (t < dur - 0.05) {
      this.hit(0.07, { gain: 0.1, freq: 220, q: 1.2, delay: t });
      this.tone(72, 0.1, { type: "sine", gain: 0.09, delay: t });
      gapMs = Math.max(80, gapMs * 0.86);
      t += gapMs / 1000;
    }
    for (let i = 0; i <= level; i++) {
      this.tone(1568 + i * 262, 0.18, { type: "sine", gain: 0.03, delay: i * 0.09 });
    }
  }

  /** Vegas bell ding, two sine partials + a bright tick. */
  bell(delay = 0): void {
    this.tone(1568, 0.5, { type: "sine", gain: 0.07, delay });
    this.tone(2349, 0.35, { type: "sine", gain: 0.03, delay });
    this.hit(0.05, { gain: 0.06, freq: 4200, delay });
  }

  /** Ticking counter during the win count-up. */
  winTick(step: number): void {
    this.tone(1000 + (step % 6) * 110, 0.03, { gain: 0.045 });
  }

  /** Win jingle; richer for multiple lines. */
  jackpot(lines: number): void {
    const base = [523, 659, 784, 1047, 1319];
    const seq = lines > 2 ? [...base, 1568, 2093] : base;
    seq.forEach((f, i) => {
      this.tone(f, 0.16, { gain: 0.06, delay: i * 0.085 });
      this.tone(f * 2, 0.1, {
        type: "triangle",
        gain: 0.025,
        delay: i * 0.085,
      });
    });
    this.bell(seq.length * 0.085);
  }

  /** Big win: three bells, a six-note square run, a sawtooth swell. */
  bigWin(): void {
    [0, 0.12, 0.24].forEach((d) => this.bell(d));
    [523, 784, 1047, 1319, 1568, 2093].forEach((f, i) =>
      this.tone(f, 0.22, { gain: 0.06, delay: 0.4 + i * 0.1 }),
    );
    this.tone(65, 1.2, { type: "sawtooth", gain: 0.07, delay: 0.4, slideTo: 130 });
  }

  // ----- crash-room space sounds -----

  private engine: {
    src: AudioBufferSourceNode;
    f: BiquadFilterNode;
    osc: OscillatorNode;
    og: GainNode;
    g: GainNode;
  } | null = null;

  /** Ignition: sub rumble, a rising saw, and a wide noise swell. */
  launch(): void {
    this.tone(38, 1.7, { type: "sine", gain: 0.14, slideTo: 90 });
    this.tone(70, 1.4, { type: "sawtooth", gain: 0.07, slideTo: 240 });
    this.hit(1.3, { gain: 0.16, freq: 320, q: 0.6, type: "lowpass" });
    this.hit(0.25, { gain: 0.1, freq: 2600 });
  }

  /** Looping engine bed under the flight; pitch rides the multiplier. */
  engineStart(): void {
    const ctx = this.ensure();
    if (!ctx || !this.master || this.engine) return;
    if (!this.noiseBuf) {
      const len = Math.floor(ctx.sampleRate * 1.2);
      this.noiseBuf = ctx.createBuffer(1, len, ctx.sampleRate);
      const d = this.noiseBuf.getChannelData(0);
      for (let i = 0; i < len; i++) d[i] = Math.random() * 2 - 1;
    }
    const src = ctx.createBufferSource();
    src.buffer = this.noiseBuf;
    src.loop = true;
    const f = ctx.createBiquadFilter();
    f.type = "lowpass";
    f.frequency.value = 500;
    f.Q.value = 0.8;
    const g = ctx.createGain();
    g.gain.value = 0;
    g.gain.setTargetAtTime(0.075, ctx.currentTime, 0.15);
    const osc = ctx.createOscillator();
    osc.type = "sawtooth";
    osc.frequency.value = 66;
    const og = ctx.createGain();
    og.gain.value = 0.022;
    src.connect(f);
    f.connect(g);
    g.connect(this.master);
    osc.connect(og);
    og.connect(this.master);
    src.start();
    osc.start();
    this.engine = { src, f, osc, og, g };
  }

  /** Track the multiplier: filter opens and the growl rises. */
  enginePitch(m: number): void {
    if (!this.engine || !this.ctx) return;
    const l = Math.log(Math.max(1, m));
    const t = this.ctx.currentTime;
    this.engine.f.frequency.setTargetAtTime(500 + l * 520, t, 0.2);
    this.engine.osc.frequency.setTargetAtTime(66 + l * 46, t, 0.2);
    this.engine.g.gain.setTargetAtTime(0.07 + Math.min(0.05, l * 0.014), t, 0.3);
  }

  engineStop(): void {
    if (!this.engine || !this.ctx) return;
    const { src, osc, g, og } = this.engine;
    this.engine = null;
    const t = this.ctx.currentTime;
    g.gain.setTargetAtTime(0, t, 0.08);
    og.gain.setTargetAtTime(0, t, 0.08);
    src.stop(t + 0.4);
    osc.stop(t + 0.4);
  }

  /** Catastrophic disassembly: boom, crackle, and a falling saw. */
  explosion(): void {
    this.hit(0.55, { gain: 0.34, freq: 130, q: 0.5, type: "lowpass" });
    this.hit(0.3, { gain: 0.2, freq: 1100, q: 0.8, delay: 0.05 });
    this.tone(200, 0.9, { type: "sawtooth", gain: 0.12, slideTo: 28 });
    this.tone(48, 1.1, { type: "sine", gain: 0.16, slideTo: 26 });
    for (let i = 0; i < 5; i++) {
      this.hit(0.09, {
        gain: 0.1 - i * 0.015,
        freq: 900 + Math.random() * 900,
        delay: 0.15 + i * 0.11,
      });
    }
  }

  /** Player eject: bright two-note confirm, distinct from the bell. */
  cashout(): void {
    this.tone(880, 0.09, { gain: 0.06 });
    this.tone(1318, 0.2, { gain: 0.06, delay: 0.09 });
    this.tone(1760, 0.16, { type: "triangle", gain: 0.03, delay: 0.09 });
  }

  /** T-minus blip; the final second jumps an octave. */
  countdownBeep(final = false): void {
    this.tone(final ? 1320 : 880, 0.07, { gain: 0.07 });
  }

  /** Milestone zap, pitched up by tier. */
  milestone(m: number): void {
    const f = m >= 25 ? 2093 : m >= 10 ? 1568 : m >= 5 ? 1175 : 880;
    this.tone(f, 0.1, { type: "square", gain: 0.05 });
    this.tone(f * 1.5, 0.16, { type: "square", gain: 0.045, delay: 0.08 });
    this.hit(0.08, { gain: 0.06, freq: 5200, delay: 0.02 });
  }

  /** Throttle stage bump: short filtered whoosh. */
  boost(): void {
    this.tone(320, 0.26, { type: "triangle", gain: 0.05, slideTo: 1500 });
    this.hit(0.22, { gain: 0.09, freq: 1400, q: 1.2 });
  }
}

// Module singleton.
export const sound = new SoundManager();
