"use client";

import { useEffect, useState } from "react";
import Backdrop from "@/components/Backdrop";
import Cabinet, { type OverlayState } from "@/components/Cabinet";
import HistoryTable from "@/components/HistoryTable";
import { NavButton, NavLink } from "@/components/NavButton";
import Paytable from "@/components/Paytable";
import VerifyClient from "@/components/VerifyClient";
import { Avatar } from "@/components/Avatar";
import NeonDialog from "@/components/NeonDialog";
import { sound } from "@/lib/sound";
import { useBets, useFairCurrent, useGames, useSession } from "@/lib/api";
import type { GameInfo } from "@/lib/types";

type Panel = "paytable" | "history" | "info" | "verify" | null;

const PANEL_TITLES: Record<Exclude<Panel, null>, string> = {
  paytable: "PAYTABLE",
  history: "SPIN HISTORY",
  info: "HOW IT WORKS",
  verify: "VERIFY A SPIN",
};

/** The machine: header, scaled cabinet + lever, footer, overlay panels. */
export default function MachineScreen({ gameId }: { gameId: string }) {
  const session = useSession();
  const fair = useFairCurrent(session.isSuccess);
  const games = useGames();
  const bets = useBets();

  const info = games.data?.find((g) => g.id === gameId);
  const pt = info?.paytable ?? null;

  const [panel, setPanel] = useState<Panel>(null);
  const [scale, setScale] = useState(1);
  const [muted, setMuted] = useState(false);
  const [overlay, setOverlay] = useState<OverlayState | null>(null);
  // Celebrations whose big-win takeover the player already tapped away.
  const [dismissedKeys, setDismissedKeys] = useState<number[]>([]);

  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth;
      const h = window.innerHeight;
      setScale(Math.min(1, (h - 190) / 800, (w - 60) / 790));
    };
    onResize();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const openPanel = (p: Panel) => {
    sound.click();
    setPanel(p);
  };

  const toggleMute = () => {
    const next = !muted;
    setMuted(next);
    sound.setMuted(next);
    if (!next) {
      sound.unlock();
      sound.click();
    }
  };

  const me = session.data;
  const subtitle = info
    ? `${info.name} · ${pt?.reels ?? "·"}×${pt?.rows ?? "·"} · RTP ${(info.theoreticalRtp * 100).toFixed(2)}%`
    : "···";

  return (
    <div style={{ position: "fixed", inset: 0, overflow: "hidden", background: "#06040d" }}>
      <Backdrop />

      <div
        style={{
          position: "relative",
          zIndex: 5,
          display: "flex",
          flexDirection: "column",
          height: "100%",
        }}
      >
        <header
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 16,
            padding: "14px 22px",
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
            <NavLink href="/" variant="back">
              ◂ GAME MENU
            </NavLink>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 19, color: "#8878b8" }}>
              {subtitle}
            </span>
          </div>
          <nav style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <NavButton onClick={() => openPanel("paytable")}>PAYTABLE</NavButton>
            <NavButton onClick={() => openPanel("history")}>HISTORY</NavButton>
            <NavButton onClick={() => openPanel("info")}>HOW IT WORKS</NavButton>
            <NavButton onClick={() => openPanel("verify")}>VERIFY</NavButton>
            <NavButton variant="sound" onClick={toggleMute}>
              {muted ? "SND OFF" : "SND ON"}
            </NavButton>
          </nav>
        </header>

        <div
          style={{
            flex: 1,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            minHeight: 0,
          }}
        >
          <div style={{ transform: `scale(${scale})`, transformOrigin: "center center" }}>
            <Cabinet
              key={gameId}
              gameId={gameId}
              inert={panel !== null}
              onOverlay={setOverlay}
              bigWinDismissed={overlay ? dismissedKeys.includes(overlay.spinKey) : false}
            />
          </div>
        </div>

        <div
          style={{
            padding: "10px 22px 14px",
            display: "flex",
            justifyContent: "space-between",
            fontFamily: "var(--font-body)",
            fontSize: 17,
            color: "#5c4f80",
          }}
        >
          <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
            {me && (
              <Avatar
                userId={me.user.id}
                displayName={me.user.displayName}
                avatarPreset={me.user.avatarPreset}
                avatarVersion={me.user.avatarVersion}
                size={20}
                ring="#35205c"
              />
            )}
            {me ? (me.user.isGuest ? "guest" : me.user.displayName) : "····"}
            {fair.data
              ? ` · seed ${fair.data.serverSeedHash.slice(0, 8)}… · nonce ${fair.data.nonce}`
              : ""}
          </span>
          <span>SPACE = SPIN · {bets.data?.bets.length ?? 0} SPINS THIS SESSION</span>
        </div>
      </div>

      {/* Coin fountain */}
      {overlay?.coins && (
        <div
          style={{
            position: "absolute",
            inset: 0,
            zIndex: 8,
            pointerEvents: "none",
            overflow: "hidden",
          }}
        >
          {Array.from({ length: 34 }, (_, i) => (
            <span
              key={i}
              style={{
                position: "absolute",
                bottom: "6%",
                left: `${((i * 137 + 23) % 96) + 2}%`,
                width: 16,
                height: 16,
                borderRadius: "50%",
                background:
                  "radial-gradient(circle at 34% 30%, #fff8d8, #ff8a1f 55%, #a1450a)",
                boxShadow: "0 0 12px rgba(255,138,31,.8)",
                animation: `coinFly ${1500 + ((i * 61) % 700)}ms cubic-bezier(.3,.05,.6,1) ${(i * 83) % 900}ms forwards`,
              }}
            />
          ))}
        </div>
      )}

      {/* Big win takeover */}
      {overlay?.bigWin && !dismissedKeys.includes(overlay.spinKey) && (
        <div
          onClick={() =>
            setDismissedKeys((keys) =>
              keys.includes(overlay.spinKey) ? keys : [...keys, overlay.spinKey],
            )
          }
          style={{
            position: "absolute",
            inset: 0,
            zIndex: 9,
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            gap: 20,
            cursor: "pointer",
            animation: "bigStrobe .5s steps(1) infinite",
          }}
        >
          <div
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 96,
              letterSpacing: 10,
              color: "#fff",
              textShadow:
                "0 0 14px #ff2d95, 0 0 44px #ff2d95, 0 0 90px rgba(34,232,255,.7)",
              animation: "bigPop .5s cubic-bezier(.2,1.4,.4,1) both",
            }}
          >
            BIG WIN
          </div>
          <div
            style={{
              fontFamily: "var(--font-body)",
              fontSize: 84,
              lineHeight: 1,
              color: "#ff8a1f",
              textShadow: "0 0 20px rgba(255,138,31,.85)",
            }}
          >
            {overlay.winShown.toLocaleString()}
          </div>
          <div
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 14,
              letterSpacing: 3,
              color: "#22e8ff",
            }}
          >
            {overlay.summary} · TAP TO CONTINUE
          </div>
        </div>
      )}

      {/* Overlay panels */}
      <NeonDialog
        open={panel !== null}
        onClose={() => setPanel(null)}
        title={panel ? PANEL_TITLES[panel] : ""}
        accent="#ff2d95"
        width={1040}
      >
        <div style={{ padding: 18 }} key={panel}>
          {panel === "paytable" && <Paytable gameId={gameId} />}
          {panel === "history" && <HistoryTable />}
          {panel === "info" && info && <HowItWorks game={info} />}
          {panel === "verify" && <VerifyClient />}
        </div>
      </NeonDialog>
    </div>
  );
}

function HowItWorks({ game }: { game: GameInfo }) {
  const pt = game.paytable;
  const rows: Array<[string, string]> = [
    [
      "GRID",
      pt ? `${pt.reels} reels × ${pt.rows} rows · ${pt.symbols.length} symbols` : "···",
    ],
    [
      "WIN MODE",
      pt
        ? pt.mode === "scatter"
          ? "scatter — N of a symbol anywhere on the grid"
          : `${pt.paylines} paylines, counted from the left`
        : "···",
    ],
    ["BET STEPS", pt ? `${pt.betSteps.join(" · ")} credits` : "···"],
    ["TOP UP", "+1000 credits from the kiosk, once per hour"],
  ];

  return (
    <div>
      <p
        style={{
          margin: 0,
          fontFamily: "var(--font-body)",
          fontSize: 22,
          lineHeight: 1.35,
          color: "#ece6ff",
          textWrap: "pretty",
        }}
      >
        Every spin is decided by the server and derived from your seed pair —
        verify any bet from the VERIFY page with the server seed revealed on
        rotation. Outcomes are provably fair; the client only renders them.
      </p>
      <div style={{ marginTop: 18, display: "flex", flexDirection: "column", gap: 10 }}>
        {rows.map(([label, value]) => (
          <div
            key={label}
            style={{
              display: "flex",
              gap: 14,
              padding: "10px 0",
              borderTop: "1px solid #1b1030",
            }}
          >
            <span
              style={{
                width: 220,
                fontFamily: "var(--font-display)",
                fontSize: 14,
                letterSpacing: 2,
                color: "#8878b8",
              }}
            >
              {label}
            </span>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#ece6ff" }}>
              {value}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
