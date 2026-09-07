"use client";

import { useEffect, useRef, useState } from "react";
import { sound } from "../lib/sound";
import Chip from "./Chip";

// Shared bet console: preset chips, a free-form amount field, and ½ / 2× /
// MAX shorthands. Every stake surface (slots, blackjack, crash, roulette)
// renders this so a custom amount can be typed anywhere; the server still
// enforces the real range, this clamps early and yells on overflow.

const CHIP_COLOR_BY_INDEX = ["pink", "cyan", "orange", "green", "purple"] as const;

export interface BetInputProps {
  value: number;
  onChange: (v: number) => void;
  /** Preset denominations rendered as chips (UI only — any in-range amount plays). */
  steps?: number[];
  min?: number;
  max?: number;
  /** When set, MAX and manual entry never exceed the player's balance. */
  balance?: number;
  accent?: string;
  disabled?: boolean;
  /** Hide the ½ / 2× / MAX row (tight consoles). */
  compact?: boolean;
  testIdPrefix?: string;
}

export default function BetInput({
  value,
  onChange,
  steps = [5, 10, 25, 50, 100],
  min = 1,
  max = 1_000_000,
  balance,
  accent = "#ff8a1f",
  disabled = false,
  compact = false,
  testIdPrefix = "bet",
}: BetInputProps) {
  const ceiling = Math.min(max, balance ?? max);
  const [text, setText] = useState(String(value));
  const [shake, setShake] = useState(false);
  const focused = useRef(false);

  useEffect(() => {
    if (!focused.current) setText(String(value));
  }, [value]);

  const reject = () => {
    sound.error();
    setShake(true);
    setTimeout(() => setShake(false), 350);
  };

  // Parses the field and commits a clamped amount; out-of-range input gets
  // the shake + error sound, then lands on the nearest legal value.
  const commit = (raw: string) => {
    const n = Math.floor(Number(raw));
    if (!Number.isFinite(n) || raw.trim() === "" || n <= 0) {
      reject();
      setText(String(value));
      return;
    }
    if (n < min || n > ceiling) {
      reject();
      const clamped = Math.max(min, Math.min(ceiling, n));
      setText(String(clamped));
      onChange(clamped);
      return;
    }
    setText(String(n));
    onChange(n);
  };

  const set = (n: number) => {
    const clamped = Math.max(min, Math.min(ceiling, Math.floor(n)));
    setText(String(clamped));
    onChange(clamped);
  };

  const field: React.CSSProperties = {
    flex: 1,
    minWidth: 110,
    background: "#06040d",
    border: `2px solid ${accent}55`,
    borderRadius: 8,
    color: accent,
    fontFamily: "var(--font-display)",
    fontSize: 22,
    padding: "6px 12px",
    textAlign: "center",
    outline: "none",
    textShadow: `0 0 10px ${accent}88`,
  };

  const quick: React.CSSProperties = {
    fontFamily: "var(--font-display)",
    fontSize: 12,
    letterSpacing: 1,
    color: "#cfc4f2",
    background: "#1d1036",
    border: "1px solid #4a3a72",
    borderRadius: 6,
    padding: "5px 10px",
    cursor: disabled ? "wait" : "pointer",
  };

  return (
    <div
      data-testid={`${testIdPrefix}-input`}
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 8,
        animation: shake ? "betShake .35s ease" : undefined,
      }}
    >
      <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
        <input
          value={text}
          disabled={disabled}
          inputMode="numeric"
          data-testid={`${testIdPrefix}-field`}
          style={{ ...field, opacity: disabled ? 0.5 : 1 }}
          onChange={(e) => setText(e.target.value.replace(/[^0-9]/g, ""))}
          onFocus={(e) => {
            focused.current = true;
            e.currentTarget.select();
          }}
          onBlur={(e) => {
            focused.current = false;
            commit(e.currentTarget.value);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              (e.currentTarget as HTMLInputElement).blur();
            }
          }}
        />
        {!compact && (
          <>
            <button type="button" style={quick} disabled={disabled} data-testid={`${testIdPrefix}-half`} onClick={() => { sound.click(); set(value / 2); }}>
              ½
            </button>
            <button type="button" style={quick} disabled={disabled} data-testid={`${testIdPrefix}-double`} onClick={() => { sound.click(); set(value * 2); }}>
              2×
            </button>
            <button type="button" style={quick} disabled={disabled} data-testid={`${testIdPrefix}-max`} onClick={() => { sound.chipClink(); set(ceiling); }}>
              MAX
            </button>
          </>
        )}
      </div>
      <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "center" }}>
        {steps.map((s, i) => (
          <Chip
            key={s}
            label={String(s)}
            color={CHIP_COLOR_BY_INDEX[i % CHIP_COLOR_BY_INDEX.length]}
            size={44}
            selected={value === s}
            disabled={disabled || s > ceiling}
            onClick={() => {
              sound.chipClink();
              set(s);
            }}
          />
        ))}
      </div>
    </div>
  );
}
