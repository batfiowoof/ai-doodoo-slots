"use client";

// Shared neon dialog: dimmed viewport backdrop, glowing panel, sticky header.
// Portals to the body so dialogs render at true viewport size even inside
// the scale()-wrapped game surfaces (the AccountModal trick, generalized).

import { useEffect, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { sound } from "@/lib/sound";

export default function NeonDialog({
  open,
  onClose,
  title,
  accent = "#22e8ff",
  width = 1000,
  dismissOnBackdrop = true,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  accent?: string;
  width?: number;
  dismissOnBackdrop?: boolean;
  children: ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div
      onClick={dismissOnBackdrop ? onClose : undefined}
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(6,4,13,.86)",
        zIndex: 90,
        display: "grid",
        placeItems: "center",
        padding: 24,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width,
          maxWidth: "calc(100vw - 48px)",
          maxHeight: "calc(100vh - 48px)",
          overflowY: "auto",
          background: "#0f0720",
          border: `2px solid ${accent}`,
          boxShadow: `0 0 60px ${accent}59, inset 0 0 0 1px #241640`,
          animation: "bigPop .3s cubic-bezier(.2,1.4,.4,1) both",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 12,
            background: "#150a2a",
            borderBottom: "2px solid #241640",
            padding: "14px 20px",
            position: "sticky",
            top: 0,
            zIndex: 2,
          }}
        >
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 20,
              letterSpacing: 6,
              color: accent,
              textShadow: `0 0 14px ${accent}`,
            }}
          >
            {title}
          </span>
          <button
            type="button"
            onClick={() => {
              sound.click();
              onClose();
            }}
            style={{
              border: "1px solid #35205c",
              background: "transparent",
              color: "#8878b8",
              fontFamily: "var(--font-display)",
              fontSize: 14,
              letterSpacing: 2,
              padding: "10px 16px",
              cursor: "pointer",
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.borderColor = accent;
              e.currentTarget.style.color = accent;
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.borderColor = "#35205c";
              e.currentTarget.style.color = "#8878b8";
            }}
          >
            CLOSE ✕
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  );
}
