"use client";

// Casino chip: striped edge ring, inset face, neon denomination. Pure CSS —
// the edge stripes come from a repeating conic gradient so the chip reads at
// any size without sprites.

const CHIP_COLORS: Record<string, { face: string; stripe: string; rim: string }> = {
  pink: { face: "#ff2d95", stripe: "#fff", rim: "#8c1460" },
  cyan: { face: "#22e8ff", stripe: "#062028", rim: "#0e6273" },
  orange: { face: "#ff8a1f", stripe: "#2a1406", rim: "#8c4a0a" },
  green: { face: "#5fe08a", stripe: "#062012", rim: "#1e7a44" },
  purple: { face: "#a45cff", stripe: "#fff", rim: "#4d1e8c" },
};

export interface ChipProps {
  /** Label pressed into the face (denomination or action). */
  label: string;
  color?: keyof typeof CHIP_COLORS;
  /** Face diameter in px. */
  size?: number;
  selected?: boolean;
  disabled?: boolean;
  onClick?: () => void;
  title?: string;
}

export default function Chip({
  label,
  color = "pink",
  size = 56,
  selected = false,
  disabled = false,
  onClick,
  title,
}: ChipProps) {
  const c = CHIP_COLORS[color] ?? CHIP_COLORS.pink;
  const stripes = 12;

  const face: React.CSSProperties = {
    width: size,
    height: size,
    borderRadius: "50%",
    background: `
      radial-gradient(circle at 34% 30%, ${c.face}, ${c.rim} 130%)`,
    boxShadow: selected
      ? `0 0 18px ${c.face}cc, 0 0 40px ${c.face}55, inset 0 -3px 6px rgba(0,0,0,.5)`
      : "0 4px 10px rgba(0,0,0,.55), inset 0 -3px 6px rgba(0,0,0,.45)",
    position: "relative",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    flexShrink: 0,
  };

  const edgeStyle: React.CSSProperties = {
    position: "absolute",
    inset: 0,
    borderRadius: "50%",
    background: `repeating-conic-gradient(${c.stripe} 0deg ${(360 / stripes) * 0.42}deg, transparent ${(360 / stripes) * 0.42}deg ${360 / stripes}deg)`,
    opacity: 0.9,
    mask: "radial-gradient(circle, transparent 62%, #000 63%, #000 92%, transparent 93%)",
    WebkitMask:
      "radial-gradient(circle, transparent 62%, #000 63%, #000 92%, transparent 93%)",
  };

  const innerRing: React.CSSProperties = {
    position: "absolute",
    inset: size * 0.16,
    borderRadius: "50%",
    border: `2px dashed ${c.stripe}`,
    opacity: 0.75,
  };

  const labelStyle: React.CSSProperties = {
    position: "relative",
    fontFamily: "var(--font-display)",
    fontSize: Math.max(10, size * 0.24),
    letterSpacing: 0.5,
    color: color === "cyan" || color === "green" ? "#062028" : "#fff",
    textShadow: "0 1px 0 rgba(0,0,0,.4)",
    pointerEvents: "none",
  };

  if (!onClick) {
    return (
      <span style={face} title={title}>
        <span style={edgeStyle} />
        <span style={innerRing} />
        <span style={labelStyle}>{label}</span>
      </span>
    );
  }

  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      disabled={disabled}
      style={{
        ...face,
        border: "none",
        cursor: disabled ? "wait" : "pointer",
        opacity: disabled ? 0.45 : 1,
        outline: selected ? `3px solid #ece6ff` : "none",
        outlineOffset: 2,
        transition: "transform .12s ease, box-shadow .12s ease",
      }}
      onMouseEnter={(e) => {
        if (disabled) return;
        e.currentTarget.style.transform = "translateY(-4px) scale(1.06)";
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.transform = "translateY(0) scale(1)";
      }}
    >
      <span style={edgeStyle} />
      <span style={innerRing} />
      <span style={labelStyle}>{label}</span>
    </button>
  );
}

/** A short vertical stack of chips for bet displays. */
export function ChipStack({
  amount,
  color = "pink",
  chipSize = 34,
  max = 4,
}: {
  amount: number;
  color?: keyof typeof CHIP_COLORS;
  chipSize?: number;
  max?: number;
}) {
  const count = Math.max(1, Math.min(max, Math.ceil(Math.log10(Math.max(amount, 2)) * 1.4)));
  return (
    <span style={{ position: "relative", display: "inline-block", width: chipSize, height: chipSize + (count - 1) * 7 }}>
      {Array.from({ length: count }, (_, i) => (
        <span
          key={i}
          style={{
            position: "absolute",
            bottom: i * 7,
            left: 0,
            filter: i === count - 1 ? "none" : "brightness(.82)",
          }}
        >
          <Chip label="" color={color} size={chipSize} />
        </span>
      ))}
    </span>
  );
}
