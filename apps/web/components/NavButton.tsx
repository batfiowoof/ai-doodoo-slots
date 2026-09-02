"use client";

import Link from "next/link";
import type { CSSProperties, ReactNode } from "react";

type Variant = "default" | "back" | "sound";

const BASE: CSSProperties = {
  whiteSpace: "nowrap",
  cursor: "pointer",
  fontFamily: "var(--font-display)",
  fontSize: 11,
  letterSpacing: "1px",
  padding: "9px 12px",
  border: "1px solid #4a3a72",
  background: "#1d1036",
  color: "#cfc4f2",
};

const VARIANTS: Record<Variant, CSSProperties> = {
  default: {},
  back: {
    color: "#22e8ff",
    background: "#0b2a33",
    border: "1px solid #1c5f6b",
  },
  sound: {
    color: "#ffb15c",
    background: "#2a1406",
    border: "1px solid #6b4a1c",
  },
};

function styleFor(variant: Variant, hover: boolean): CSSProperties {
  const s = { ...BASE, ...VARIANTS[variant] };
  if (!hover) return s;
  if (variant === "sound") {
    s.borderColor = "#ff8a1f";
    s.boxShadow = "0 0 14px rgba(255,138,31,.4)";
  } else {
    s.borderColor = "#22e8ff";
    s.boxShadow = "0 0 14px rgba(34,232,255,.4)";
    if (variant === "default") s.color = "#22e8ff";
  }
  return s;
}

function hoverProps(variant: Variant) {
  return {
    onMouseEnter: (e: React.MouseEvent<HTMLElement>) => {
      Object.assign(e.currentTarget.style, styleFor(variant, true));
    },
    onMouseLeave: (e: React.MouseEvent<HTMLElement>) => {
      Object.assign(e.currentTarget.style, styleFor(variant, false));
    },
  };
}

/** Header nav action shared by both screens. */
export function NavButton({
  variant = "default",
  onClick,
  children,
  disabled,
}: {
  variant?: Variant;
  onClick?: () => void;
  children: ReactNode;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      style={styleFor(variant, false)}
      {...hoverProps(variant)}
    >
      {children}
    </button>
  );
}

/** Same chrome as NavButton, but navigates. */
export function NavLink({
  href,
  variant = "default",
  children,
}: {
  href: string;
  variant?: Variant;
  children: ReactNode;
}) {
  return (
    <Link href={href} style={styleFor(variant, false)} {...hoverProps(variant)}>
      {children}
    </Link>
  );
}
