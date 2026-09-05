// Situational lobby greetings: time-of-day buckets with random flavour
// lines, plus situation overrides (thin/stacked balance, live tables,
// weekends, the player's own callsign). Pure functions so the pool is
// deterministic given the context and easy to eyeball in review.

export interface GreetingContext {
  /** Local hour 0-23. */
  hour: number;
  displayName?: string;
  balance?: number;
  /** How many rooms currently have a round running. */
  liveTables?: number;
  weekend?: boolean;
}

// Time-of-day flavour lines, ≤ 40 chars so they never crowd the hub.
const BUCKETS: { from: number; to: number; lines: string[] }[] = [
  {
    from: 0,
    to: 5,
    lines: [
      "THE DEAD HOURS PAY DOUBLE",
      "THE WHEEL NEVER SLEEPS",
      "MIDNIGHT ODDS ARE THE BEST ODDS",
      "SHH — THE FLOOR IS YOURS",
    ],
  },
  {
    from: 5,
    to: 8,
    lines: [
      "UP EARLY? LUCK IS AN EARLY BIRD",
      "MORNING ODDS FAVOR THE BRAVE",
      "FRESH SEEDS, FRESH LUCK",
      "THE COFFEE IS STRONG. SO IS THE RTP",
    ],
  },
  {
    from: 8,
    to: 12,
    lines: [
      "GOOD MORNING, HIGH ROLLER",
      "BREAKFAST OF CHAMPIONS: LOOSE SLOTS",
      "THE CHIPS ARE POLISHED FOR YOU",
      "EARLY BIRDS PICK THE BEST SEATS",
    ],
  },
  {
    from: 12,
    to: 14,
    lines: [
      "LUNCH BREAK? THE WHEEL SNACKS TOO",
      "MIDDAY MADNESS ON THE MID FLOOR",
      "A QUICK SPIN BETWEEN BITES",
    ],
  },
  {
    from: 14,
    to: 18,
    lines: [
      "GOOD AFTERNOON, SHARP EYES",
      "THE AFTERNOON SHIFT RUNS HOT",
      "A NAP IS JUST A PAUSE BETWEEN SPINS",
      "THE SLOW HOURS BUILD THE BIG WINS",
    ],
  },
  {
    from: 18,
    to: 22,
    lines: [
      "GOOD EVENING, HIGH ROLLER",
      "PRIME TIME — THE NEON IS ON",
      "EVENINGS WERE MADE FOR ROULETTE",
      "THE NIGHT FLOOR IS WAKING UP",
    ],
  },
  {
    from: 22,
    to: 24,
    lines: [
      "LADY LUCK KEEPS LATE HOURS",
      "THE HOUSE ALWAYS WINS. ALLEGEDLY",
      "LAST CALL? THE FLOOR SAYS OTHERWISE",
    ],
  },
];

function timeLines(hour: number): string[] {
  for (const b of BUCKETS) {
    if (hour >= b.from && hour < b.to) return b.lines;
  }
  return BUCKETS[2].lines;
}

/** The full greeting pool for a context: flavour + situational overrides. */
export function greetingPool(ctx: GreetingContext): string[] {
  const pool = [...timeLines(ctx.hour)];

  const name = ctx.displayName?.trim().toUpperCase();
  if (name) {
    const short = name.length > 14 ? name.slice(0, 13) + "…" : name;
    pool.push(`WELCOME BACK, ${short}`, `THE HOUSE REMEMBERS YOU, ${short}`);
  }

  if (ctx.balance !== undefined) {
    if (ctx.balance < 100) {
      pool.push("LOOKING THIN — THE KIOSK GIVES +1000", "A FRESH +1000 AWAITS AT THE KIOSK");
    } else if (ctx.balance >= 10000) {
      pool.push("BIG STACKS GO TO THE WHALE ROOM", "THAT STACK DESERVES A ROULETTE TABLE");
    }
  }

  if ((ctx.liveTables ?? 0) > 0) {
    pool.push(
      `${ctx.liveTables} TABLES ARE RUNNING HOT`,
      `${ctx.liveTables} LIVE TABLES CALLING YOUR NAME`,
    );
  }

  if (ctx.weekend) {
    pool.push("WEEKEND RULES: SAME EDGE, MORE STYLE");
  }

  return pool;
}

/** Random pick that avoids repeating the last line when there's a choice. */
export function pickGreeting(pool: string[], avoid?: string): string {
  if (pool.length === 0) return "";
  if (pool.length === 1) return pool[0];
  let next = pool[Math.floor(Math.random() * pool.length)];
  let guard = 0;
  while (next === avoid && guard++ < 8) {
    next = pool[Math.floor(Math.random() * pool.length)];
  }
  return next;
}
