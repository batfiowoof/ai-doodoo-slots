"use client";

import { useCallback, useEffect, useState } from "react";
import Backdrop from "@/components/Backdrop";
import PixelSymbol from "@/components/PixelSymbol";
import PixelCard from "@/components/PixelCard";
import { sound } from "@/lib/sound";
import { useDeposit, useGames, useSession } from "@/lib/api";
import { useLobby, type LobbyRoom } from "@/lib/useLobby";
import { Avatar } from "@/components/Avatar";
import { AccountModal } from "@/components/AccountModal";
import RadialMenu, { type RadialNode } from "@/components/RadialMenu";
import type { GameInfo } from "@/lib/types";

// Design-size wheel stage, scaled to fit the viewport.
const STAGE_W = 1240;
const STAGE_H = 880;

/** The casino floor: every game and live table on one big radial wheel. */
export default function GameMenu() {
  const session = useSession();
  const games = useGames();
  const deposit = useDeposit();
  const lobby = useLobby();
  const [depositNote, setDepositNote] = useState<string | null>(null);
  const [stageScale, setStageScale] = useState(1);
  const [muted, setMuted] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);

  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth;
      const h = window.innerHeight;
      setStageScale(Math.min(1, (w - 40) / STAGE_W, (h - 170) / STAGE_H));
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

  // Outer ring: machines and tables first, then the live rooms.
  const nodes: RadialNode[] = [
    ...(games.data ?? []).map((g) => gameNode(g)),
    ...lobby.rooms.map((r) => roomNode(r)),
  ];

  // Inner orbit: account + kiosk actions.
  const satellites: RadialNode[] = [];
  if (session.data) {
    satellites.push({
      key: "account",
      label: "ACCOUNT",
      accent: "#22e8ff",
      art: (
        <Avatar
          userId={session.data.user.id}
          displayName={session.data.user.displayName}
          avatarPreset={session.data.user.avatarPreset}
          avatarVersion={session.data.user.avatarVersion}
          size={46}
          ring="#22e8ff"
        />
      ),
      status: session.data.user.displayName.toUpperCase(),
      onActivate: () => setAccountOpen(true),
    });
    satellites.push({
      key: "deposit",
      label: "DEPOSIT",
      status: "+1000",
      accent: "#ff8a1f",
      art: <img src="/sprites/money-bag.png" width={38} height={38} className="pixelated" alt="" />,
      onActivate: doDeposit,
      disabled: deposit.isPending || !session.isSuccess,
      onLaunchSound: () => {}, // the kiosk call plays its own cues
    });
    if (!session.data.user.isGuest) {
      satellites.push({
        key: "logout",
        label: "LOGOUT",
        accent: "#8878b8",
        href: "/auth/logout",
        hard: true,
        art: <img src="/sprites/horseshoe.png" width={38} height={38} className="pixelated" alt="" />,
      });
    }
  } else {
    satellites.push({
      key: "login",
      label: "LOGIN",
      accent: "#22e8ff",
      href: "/auth/login?next=/",
      hard: true,
      art: <img src="/sprites/star.png" width={38} height={38} className="pixelated" alt="" />,
    });
  }
  satellites.push({
    key: "verify",
    label: "VERIFY",
    accent: "#5fe08a",
    href: "/verify",
    art: <img src="/sprites/key.png" width={38} height={38} className="pixelated" alt="" />,
  });
  if (
    session.data &&
    (session.data.user.role === "admin" || session.data.user.role === "moderator")
  ) {
    satellites.push({
      key: "staff",
      label: "STAFF",
      accent: "#ff2d95",
      href: "/admin",
      art: <img src="/sprites/crown.png" width={38} height={38} className="pixelated" alt="" />,
    });
  }

  const hub = (
    <>
      <span style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 30,
            letterSpacing: 4,
            color: "#ff2d95",
            textShadow: "0 0 14px rgba(255,45,149,.9)",
          }}
        >
          RETRO
        </span>
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 30,
            letterSpacing: 4,
            color: "#22e8ff",
            textShadow: "0 0 14px rgba(34,232,255,.9)",
          }}
        >
          CASINO
        </span>
      </span>
      <span
        style={{
          fontFamily: "var(--font-display)",
          fontSize: 10,
          letterSpacing: 4,
          color: "#8878b8",
        }}
      >
        CREDITS
      </span>
      <span
        data-testid="credits"
        style={{
          fontFamily: "var(--font-body)",
          fontSize: 54,
          lineHeight: 1,
          color: "#ff8a1f",
          textShadow: "0 0 16px rgba(255,138,31,.7)",
        }}
      >
        {balance === undefined ? "····" : balance.toLocaleString()}
      </span>
      <span
        style={{
          fontFamily: "var(--font-display)",
          fontSize: 10,
          letterSpacing: 3,
          color: games.isError ? "#ff8a1f" : "#8878b8",
          animation: games.isError ? undefined : "hintBlink 1.6s steps(1) infinite",
        }}
      >
        {games.isLoading ? "OPENING THE FLOOR…" : games.isError ? "CASINO UNREACHABLE" : "◆ PICK YOUR GAME ◆"}
      </span>
    </>
  );

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
            padding: "14px 26px",
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 18,
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
                fontSize: 18,
                letterSpacing: 3,
                color: "#22e8ff",
                textShadow: "0 0 10px rgba(34,232,255,.9)",
              }}
            >
              CASINO
            </span>
          </div>
          <button
            type="button"
            onClick={toggleMute}
            style={{
              border: "1px solid #6b4a1c",
              background: "#2a1406",
              color: "#ffb15c",
              fontFamily: "var(--font-display)",
              fontSize: 12,
              letterSpacing: "1px",
              padding: "10px 14px",
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
        </header>

        <AccountModal open={accountOpen} onClose={() => setAccountOpen(false)} />

        {depositNote && (
          <div
            role="status"
            style={{
              position: "absolute",
              left: "50%",
              top: 64,
              transform: "translateX(-50%)",
              zIndex: 20,
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

        {/* minHeight/minWidth 0: let the stage box overflow the flex area so
            the scaled wheel stays centered instead of stretching the column.
            placeContent centers the oversized implicit row (the item alone
            only centers within the row track), and no overflow clip here —
            clipping would flip alignment to "safe" and pin the stage on top. */}
        <div
          style={{
            flex: 1,
            display: "grid",
            placeItems: "center",
            placeContent: "center",
            minHeight: 0,
            minWidth: 0,
          }}
        >
          <div style={{ transform: `scale(${stageScale})`, transformOrigin: "center center" }}>
            <RadialMenu nodes={nodes} satellites={satellites} hub={hub} />
          </div>
        </div>

        <div
          style={{
            textAlign: "center",
            padding: "0 26px 14px",
            fontFamily: "var(--font-body)",
            fontSize: 17,
            color: "#5c4f80",
          }}
        >
          play-money arcade · no cash value · no deposits · no cash-out
        </div>
      </div>
    </div>
  );
}

// ---------- wheel node builders ----------

/** Machines get their paytable icons; table games get a little card fan. */
function gameNode(game: GameInfo): RadialNode {
  const pt = game.paytable;
  if (!pt) {
    return {
      key: game.id,
      label: game.name.toUpperCase(),
      accent: "#ff8a1f",
      href: `/play/${game.id}`,
      art: (
        <span style={{ display: "flex", paddingTop: 4 }}>
          <span style={{ transform: "rotate(-9deg)" }}>
            <PixelCard code="As" scale={2} />
          </span>
          <span style={{ transform: "rotate(4deg)", marginLeft: -18 }}>
            <PixelCard code="Td" scale={2} />
          </span>
          <span style={{ transform: "rotate(14deg)", marginLeft: -18 }}>
            <PixelCard code="back" scale={2} />
          </span>
        </span>
      ),
      status: "HIT · STAND · DOUBLE",
      onLaunchSound: () => sound.click(),
    };
  }
  return {
    key: game.id,
    label: game.name.toUpperCase(),
    accent: "#ff2d95",
    href: `/play/${game.id}`,
    art: (
      <span style={{ display: "flex", gap: 5 }}>
        {pt.icons.slice(0, 4).map((icon, i) => (
          <PixelSymbol key={i} index={i} icon={icon} scale={1} />
        ))}
      </span>
    ),
    status: `RTP ${(game.theoreticalRtp * 100).toFixed(2)}% · ${pt.reels}×${pt.rows}`,
    onLaunchSound: () => sound.lever(),
  };
}

/** Live rooms: poker gets a miniature felt, crash a live flight viewfinder. */
function roomNode(room: LobbyRoom): RadialNode {
  if (room.gameId === "holdem") {
    const live = room.state !== undefined && room.state !== "waiting";
    const seated = Math.min(room.playerCount, room.capacity);
    return {
      key: room.slug,
      label: room.name.toUpperCase(),
      accent: live ? "#22e8ff" : "#5fe08a",
      href: `/rooms/${room.slug}`,
      live,
      badge: room.state ? room.state.toUpperCase() : "…",
      art: <MiniFelt seated={seated} capacity={room.capacity} />,
      status: `BLINDS ${room.minBet / 2}/${room.minBet} · ${room.playerCount}/${room.capacity} SEATED`,
      onLaunchSound: () => sound.chipClink(),
    };
  }
  const live = room.state === "running";
  const open = room.state === "betting_open";
  const m = room.multiplier ?? 1;
  return {
    key: room.slug,
    label: room.name.toUpperCase(),
    accent: live ? tierGlow(m) : open ? "#5fe08a" : "#8878b8",
    href: `/rooms/${room.slug}`,
    live,
    badge: live ? `${m.toFixed(2)}×` : open ? "BOARDING" : (room.state?.toUpperCase() ?? "…"),
    art: (
      <img
        src={`/sprites/space/${live ? "flight-tilt" : "flight-1"}.png`}
        alt=""
        className="pixelated"
        style={{
          height: live ? 62 : 48,
          animation: live ? "shipBob 1.1s ease-in-out infinite alternate" : undefined,
          filter: `drop-shadow(0 0 8px ${tierGlow(m)})`,
        }}
      />
    ),
    status: `BETS ${room.minBet}–${room.maxBet.toLocaleString()}`,
    onLaunchSound: () => sound.boost(),
  };
}

/** Miniature poker felt with seat dots, lifted from the old lobby card. */
function MiniFelt({ seated, capacity }: { seated: number; capacity: number }) {
  const n = Math.max(capacity, 1);
  return (
    <span style={{ position: "relative", width: 108, height: 58, display: "inline-block" }}>
      <span
        style={{
          position: "absolute",
          inset: 0,
          borderRadius: "50%",
          background: "linear-gradient(160deg, #7a4c26, #5c3a1e 45%, #38200e)",
        }}
      />
      <span
        style={{
          position: "absolute",
          inset: 4,
          borderRadius: "50%",
          background: "linear-gradient(#15503a, #0b352a)",
          boxShadow: "inset 0 0 0 2px rgba(34,232,255,.3)",
        }}
      />
      {Array.from({ length: n }, (_, i) => {
        const theta = Math.PI - ((i + 1) * Math.PI) / (n + 1);
        const x = 50 + 42 * Math.cos(theta);
        const y = 46 - 34 * Math.sin(theta);
        const taken = i < seated;
        return (
          <span
            key={i}
            style={{
              position: "absolute",
              left: `${x}%`,
              top: `${y}%`,
              transform: "translate(-50%, -50%)",
              width: 7,
              height: 7,
              borderRadius: "50%",
              background: taken ? "#5fe08a" : "transparent",
              border: taken ? "none" : "1px dashed rgba(136,120,184,.7)",
              boxShadow: taken ? "0 0 6px rgba(95,224,138,.8)" : "none",
            }}
          />
        );
      })}
    </span>
  );
}

// Glow colour tiers shared with the crash room's readouts.
function tierGlow(m: number): string {
  return m >= 25 ? "#f2643d" : m >= 10 ? "#ff2d95" : m >= 5 ? "#ffb15c" : m >= 2 ? "#5fe08a" : "#22e8ff";
}
