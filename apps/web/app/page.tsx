"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import Backdrop from "@/components/Backdrop";
import PixelSymbol from "@/components/PixelSymbol";
import { NavLink } from "@/components/NavButton";
import { sound } from "@/lib/sound";
import { useDeposit, useGames, useSession } from "@/lib/api";
import type { GameInfo } from "@/lib/types";

const TRACK_W = 1220;

/** The arcade floor: one cabinet card per machine, plus the deposit kiosk. */
export default function GameMenu() {
  const session = useSession();
  const games = useGames();
  const deposit = useDeposit();
  const [depositNote, setDepositNote] = useState<string | null>(null);
  const [menuScale, setMenuScale] = useState(1);
  const [muted, setMuted] = useState(false);

  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth;
      const h = window.innerHeight;
      setMenuScale(Math.min(1, (w - 60) / TRACK_W, (h - 250) / 470));
    };
    onResize();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const balance = session.data?.balanceCredits;

  const doDeposit = useCallback(() => {
    sound.unlock();
    sound.click();
    deposit.mutate(undefined, {
      onSuccess: (data) => {
        if (data.claimed) sound.bell();
        setDepositNote(
          data.claimed
            ? `+${data.amountCredits.toLocaleString()} CREDITS`
            : "ALREADY CLAIMED — BACK IN UNDER AN HOUR",
        );
        window.setTimeout(() => setDepositNote(null), 2600);
      },
      onError: () => {
        sound.error();
        setDepositNote("KIOSK UNAVAILABLE");
        window.setTimeout(() => setDepositNote(null), 2600);
      },
    });
  }, [deposit]);

  const toggleMute = () => {
    const next = !muted;
    setMuted(next);
    sound.setMuted(next);
    if (!next) {
      sound.unlock();
      sound.click();
    }
  };

  return (
    <div style={{ position: "fixed", inset: 0, overflow: "hidden", background: "#06040d" }}>
      <Backdrop />

      <div
        style={{
          position: "relative",
          zIndex: 5,
          height: "100%",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
      >
        <header
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 16,
            padding: "18px 26px",
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 22,
                letterSpacing: 3,
                color: "#ff2d95",
                textShadow: "0 0 10px rgba(255,45,149,.9)",
              }}
            >
              RETRO
            </span>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 22,
                letterSpacing: 3,
                color: "#22e8ff",
                textShadow: "0 0 10px rgba(34,232,255,.9)",
              }}
            >
              CASINO
            </span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 11,
                letterSpacing: 2,
                color: "#8878b8",
              }}
            >
              CREDITS
            </span>
            <span
              data-testid="credits"
              style={{
                fontFamily: "var(--font-body)",
                fontSize: 34,
                lineHeight: 1,
                color: "#ff8a1f",
                textShadow: "0 0 12px rgba(255,138,31,.7)",
                background: "#06040d",
                boxShadow: "inset 0 0 0 1px #35205c",
                padding: "2px 14px",
              }}
            >
              {balance === undefined ? "····" : balance.toLocaleString()}
            </span>
            {session.data && !session.data.user.isGuest && (
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 11,
                  letterSpacing: 1,
                  color: "#cfc4f2",
                  maxWidth: 160,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
                title={session.data.user.email ?? session.data.user.displayName}
              >
                {session.data.user.displayName}
              </span>
            )}
            {session.data?.user.isGuest && (
              <NavLink href="/auth/login?next=/">LOGIN</NavLink>
            )}
            {session.data && !session.data.user.isGuest && (
              <NavLink href="/auth/logout">LOGOUT</NavLink>
            )}
            <NavLink href="/verify">VERIFY</NavLink>
            <button
              type="button"
              onClick={toggleMute}
              style={{
                border: "1px solid #6b4a1c",
                background: "#2a1406",
                color: "#ffb15c",
                fontFamily: "var(--font-display)",
                fontSize: 11,
                letterSpacing: "1px",
                padding: "10px 12px",
                whiteSpace: "nowrap",
                cursor: "pointer",
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.borderColor = "#ff8a1f";
                e.currentTarget.style.boxShadow = "0 0 14px rgba(255,138,31,.4)";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.borderColor = "#6b4a1c";
                e.currentTarget.style.boxShadow = "none";
              }}
            >
              {muted ? "SND OFF" : "SND ON"}
            </button>
            <button
              type="button"
              onClick={doDeposit}
              disabled={deposit.isPending || !session.isSuccess}
              style={{
                border: "2px solid #ff8a1f",
                background: "#2a1406",
                color: "#ff8a1f",
                fontFamily: "var(--font-display)",
                fontSize: 12,
                letterSpacing: 2,
                padding: "11px 14px",
                whiteSpace: "nowrap",
                cursor: deposit.isPending ? "wait" : "pointer",
                opacity: deposit.isPending ? 0.6 : 1,
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.background = "#ff8a1f";
                e.currentTarget.style.color = "#06040d";
                e.currentTarget.style.boxShadow = "0 0 22px rgba(255,138,31,.6)";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.background = "#2a1406";
                e.currentTarget.style.color = "#ff8a1f";
                e.currentTarget.style.boxShadow = "none";
              }}
            >
              {deposit.isPending ? "…" : "DEPOSIT +1000"}
            </button>
          </div>
        </header>

        {depositNote && (
          <div
            role="status"
            style={{
              alignSelf: "center",
              margin: "0 0 12px",
              padding: "10px 18px",
              background: "rgba(6,4,13,.9)",
              border: "2px solid #22e8ff",
              fontFamily: "var(--font-display)",
              fontSize: 14,
              letterSpacing: 2,
              color: "#22e8ff",
              animation: "notePop .2s ease-out both",
            }}
          >
            {depositNote}
          </div>
        )}

        <div
          style={{
            flex: 1,
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            gap: 26,
            padding: "0 26px 30px",
          }}
        >
          <div style={{ textAlign: "center" }}>
            <div
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 15,
                letterSpacing: 6,
                color: "#8878b8",
              }}
            >
              PICK YOUR MACHINE
            </div>
          </div>

          {games.isLoading && (
            <p style={{ fontFamily: "var(--font-display)", fontSize: 14, letterSpacing: 3, color: "#5c4f80" }}>
              OPENING THE FLOOR…
            </p>
          )}
          {games.isError && (
            <p style={{ fontFamily: "var(--font-display)", fontSize: 14, letterSpacing: 3, color: "#ff8a1f" }}>
              CASINO UNREACHABLE
            </p>
          )}

          {games.data && (
            <div
              style={{
                width: TRACK_W,
                display: "flex",
                justifyContent: "center",
                gap: 22,
                transform: `scale(${menuScale})`,
                transformOrigin: "center center",
              }}
            >
              {games.data.map((g) => (
                <MachineCard key={g.id} game={g} />
              ))}
            </div>
          )}

          <div style={{ fontFamily: "var(--font-body)", fontSize: 19, color: "#5c4f80" }}>
            play-money arcade · no cash value · no deposits · no cash-out
          </div>
        </div>
      </div>
    </div>
  );
}

function MachineCard({ game }: { game: GameInfo }) {
  const pt = game.paytable;
  const icons = pt?.icons ?? [];

  return (
    <Link
      href={`/play/${game.id}`}
      onClick={() => {
        sound.unlock();
        sound.click();
      }}
      style={{
        width: 376,
        boxSizing: "border-box",
        flex: "0 0 376px",
        padding: 20,
        background: "linear-gradient(#170c2b,#0d0619)",
        border: "2px solid #35205c",
        boxShadow: "0 0 44px rgba(157,77,255,.22)",
        cursor: "pointer",
        transition: "transform 160ms ease-out, box-shadow 160ms ease-out, border-color 160ms ease-out",
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.transform = "translateY(-5px)";
        e.currentTarget.style.borderColor = "#22e8ff";
        e.currentTarget.style.boxShadow = "0 0 54px rgba(34,232,255,.34)";
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.transform = "none";
        e.currentTarget.style.borderColor = "#35205c";
        e.currentTarget.style.boxShadow = "0 0 44px rgba(157,77,255,.22)";
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "baseline",
          justifyContent: "space-between",
          gap: 12,
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 19,
            letterSpacing: 2,
            color: "#fff",
            textShadow: "0 0 12px rgba(255,45,149,.8)",
          }}
        >
          {game.name.toUpperCase()}
        </span>
        <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#22e8ff" }}>
          {pt ? `${pt.reels} × ${pt.rows}` : "···"}
        </span>
      </div>
      <div style={{ marginTop: 6, fontFamily: "var(--font-body)", fontSize: 20, color: "#8878b8" }}>
        {pt
          ? pt.mode === "scatter"
            ? `SCATTER PAYS · ${pt.symbols.length} SYMBOLS`
            : `${pt.paylines} LINES · ${pt.symbols.length} SYMBOLS`
          : "···"}
      </div>
      <div
        style={{
          marginTop: 14,
          padding: 12,
          background: "#06040d",
          boxShadow: "inset 0 0 0 1px #241640",
          display: "flex",
          flexWrap: "nowrap",
          gap: 8,
          justifyContent: "center",
        }}
      >
        {icons.map((icon, i) => (
          <PixelSymbol key={i} index={i} icon={icon} scale={1} />
        ))}
      </div>
      <div style={{ marginTop: 14, display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#ff8a1f" }}>
          RTP {(game.theoreticalRtp * 100).toFixed(2)}%
        </span>
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 12,
            letterSpacing: 2,
            color: "#ff2d95",
          }}
        >
          PLAY ▸
        </span>
      </div>
    </Link>
  );
}
