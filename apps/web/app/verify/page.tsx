import type { Metadata } from "next";
import Link from "next/link";
import Backdrop from "@/components/Backdrop";
import VerifyClient from "@/components/VerifyClient";

export const metadata: Metadata = {
  title: "Verify — Retro Casino",
  description:
    "Recompute a slot outcome in your browser from the revealed server seed, client seed, and nonce. No server involved.",
};

export default function VerifyPage() {
  return (
    <div style={{ position: "fixed", inset: 0, overflow: "hidden", background: "#06040d" }}>
      <Backdrop />
      <div
        style={{
          position: "relative",
          zIndex: 5,
          height: "100%",
          overflow: "auto",
          maxWidth: 940,
          margin: "0 auto",
          padding: "18px 26px 40px",
          boxSizing: "border-box",
        }}
      >
        <header
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 16,
            padding: "0 0 18px",
          }}
        >
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 22,
              letterSpacing: 3,
              color: "#ff2d95",
              textShadow: "0 0 10px rgba(255,45,149,.9)",
            }}
          >
            VERIFY A SPIN
          </span>
          <Link
            href="/"
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 11,
              letterSpacing: 1,
              padding: "9px 12px",
              whiteSpace: "nowrap",
              border: "1px solid #1c5f6b",
              background: "#0b2a33",
              color: "#22e8ff",
            }}
          >
            ◂ BACK TO THE FLOOR
          </Link>
        </header>

        <div
          style={{
            background: "#0f0720",
            border: "2px solid #22e8ff",
            boxShadow: "0 0 60px rgba(34,232,255,.35)",
            padding: 18,
          }}
        >
          <VerifyClient />
        </div>
      </div>
    </div>
  );
}
