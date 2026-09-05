"use client";

import { useCallback, useEffect, useState } from "react";
import Backdrop from "@/components/Backdrop";
import PixelSymbol from "@/components/PixelSymbol";
import PixelCard from "@/components/PixelCard";
import { NavLink } from "@/components/NavButton";
import { sound } from "@/lib/sound";
import { useDeposit, useGames, useSession } from "@/lib/api";
import { useLobby, type LobbyRoom } from "@/lib/useLobby";
import { EUROPEAN_ORDER, POCKET_COLORS, pocketColor } from "@/lib/roulette";
import { Avatar } from "@/components/Avatar";
import { AccountModal } from "@/components/AccountModal";
import RadialMenu, { type RadialNode } from "@/components/RadialMenu";
import type { GameInfo } from "@/lib/types";
import { greetingPool, pickGreeting } from "@/lib/greeting";

// Design-size wheel stage, scaled to fit the viewport. Wide and short on
// purpose: the ring is an ellipse, so widescreen viewports scale it large.
const STAGE_W = 1480;
const STAGE_H = 640;

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
  // The wheel is a tree: the root ring holds one node per game type; picking
  // a type drills into its sub-ring of games/rooms, with a BACK chip to climb.
  const [drilled, setDrilled] = useState<string | null>(null);
  // Rotating situational greeting (time of day, balance, live tables).
  const [greeting, setGreeting] = useState<string | null>(null);

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
  const liveTables = lobby.rooms.filter(
    (r) => r.state !== undefined && r.state !== "waiting" && r.state !== "",
  ).length;

  // Pick a fresh greeting from the situational pool every few seconds.
  useEffect(() => {
    const now = new Date();
    const pool = greetingPool({
      hour: now.getHours(),
      displayName: session.data?.user.displayName,
      balance,
      liveTables,
      weekend: now.getDay() === 0 || now.getDay() === 6,
    });
    if (pool.length === 0) return;
    let current = pickGreeting(pool);
    setGreeting(current);
    const id = window.setInterval(() => {
      current = pickGreeting(pool, current);
      setGreeting(current);
    }, 7000);
    return () => window.clearInterval(id);
  }, [session.data?.user.displayName, balance, liveTables]);

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

  // ── the game tree ──────────────────────────────────────────────────────
  const slotsGames = (games.data ?? []).filter((g) => g.paytable);
  const tableGames = (games.data ?? []).filter((g) => !g.paytable);
  const crashRooms = lobby.rooms.filter((r) => r.gameId === "crash");
  const rouletteRooms = lobby.rooms.filter((r) => r.gameId === "roulette");
  const holdemRooms = lobby.rooms.filter((r) => r.gameId === "holdem");

  const liveCount = (rooms: LobbyRoom[]) =>
    rooms.filter((r) => r.state !== undefined && r.state !== "waiting" && r.state !== "").length;

  interface Group {
    key: string;
    label: string;
    accent: string;
    badge: string;
    art: React.ReactNode;
    children: RadialNode[];
  }

  const groups: Group[] = [];
  if (crashRooms.length > 0) {
    groups.push({
      key: "crash",
      label: "CRASH",
      accent: "#22e8ff",
      badge: `${liveCount(crashRooms)}/${crashRooms.length} LIVE`,
      art: (
        <img
          src="/sprites/space/flight-1.png"
          alt=""
          className="pixelated"
          style={{ height: 52, filter: "drop-shadow(0 0 8px rgba(34,232,255,.5))" }}
        />
      ),
      children: crashRooms.map((r) => roomNode(r)),
    });
  }
  if (rouletteRooms.length > 0) {
    groups.push({
      key: "roulette",
      label: "ROULETTE",
      accent: "#5fe08a",
      badge: `${liveCount(rouletteRooms)}/${rouletteRooms.length} LIVE`,
      art: <MiniWheel last={rouletteRooms[0] ? (rouletteRooms[0].recentCrashes ?? [])[0] : undefined} />,
      children: rouletteRooms.map((r) => roomNode(r)),
    });
  }
  if (holdemRooms.length > 0) {
    groups.push({
      key: "poker",
      label: "POKER",
      accent: "#5fe08a",
      badge: `${liveCount(holdemRooms)}/${holdemRooms.length} LIVE`,
      art: <MiniFelt seated={0} capacity={6} />,
      children: holdemRooms.map((r) => roomNode(r)),
    });
  }
  if (slotsGames.length > 0) {
    const icons = slotsGames[0].paytable?.icons.slice(0, 4) ?? [];
    groups.push({
      key: "slots",
      label: "SLOTS",
      accent: "#ff2d95",
      badge: `${slotsGames.length} MACHINES`,
      art: (
        <span style={{ display: "flex", gap: 5 }}>
          {icons.map((icon, i) => (
            <PixelSymbol key={i} index={i} icon={icon} scale={1} />
          ))}
        </span>
      ),
      children: slotsGames.map((g) => gameNode(g)),
    });
  }

  // Root ring: one node per type, single-player tables as direct leaves.
  const groupNode = (g: Group): RadialNode => ({
    key: `group:${g.key}`,
    label: g.label,
    accent: g.accent,
    badge: g.badge,
    art: g.art,
    status: "PICK ▸",
    onActivate: () => {
      sound.click();
      setDrilled(g.key);
    },
  });
  const activeGroup = groups.find((g) => g.key === drilled);
  const nodes: RadialNode[] = activeGroup
    ? activeGroup.children
    : [...groups.map(groupNode), ...tableGames.map((g) => gameNode(g))];

  // Climb back out if the drilled group emptied out.
  useEffect(() => {
    if (drilled && !activeGroup) setDrilled(null);
  }, [drilled, activeGroup]);

  // Account actions live in the header now — the wheel is games only.

  const hub = (
    <>
      {greeting && (
        <span
          key={greeting}
          style={{
            fontFamily: "var(--font-body)",
            fontSize: 15,
            color: "#22e8ff",
            textShadow: "0 0 8px rgba(34,232,255,.6)",
            whiteSpace: "nowrap",
            maxWidth: 252,
            overflow: "hidden",
            textOverflow: "ellipsis",
            animation: "logPop .45s ease-out both",
          }}
        >
          {greeting}
        </span>
      )}
      <span style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 34,
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
            fontSize: 34,
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
          fontSize: 60,
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
        {games.isLoading
          ? "OPENING THE FLOOR…"
          : games.isError
            ? "CASINO UNREACHABLE"
            : activeGroup
              ? "◀ CLICK HUB TO GO BACK"
              : "◆ PICK YOUR GAME ◆"}
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
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            {session.data && (
              <button
                type="button"
                onClick={() => {
                  sound.unlock();
                  sound.click();
                  setAccountOpen(true);
                }}
                title={session.data.user.email ?? session.data.user.displayName}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  border: "1px solid #4a3a72",
                  background: "#1d1036",
                  padding: "5px 10px",
                  cursor: "pointer",
                  maxWidth: 220,
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.borderColor = "#22e8ff";
                  e.currentTarget.style.boxShadow = "0 0 14px rgba(34,232,255,.4)";
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.borderColor = "#4a3a72";
                  e.currentTarget.style.boxShadow = "none";
                }}
              >
                <Avatar
                  userId={session.data.user.id}
                  displayName={session.data.user.displayName}
                  avatarPreset={session.data.user.avatarPreset}
                  avatarVersion={session.data.user.avatarVersion}
                  size={22}
                  ring="#22e8ff"
                />
                <span
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 11,
                    letterSpacing: 1,
                    color: "#cfc4f2",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                >
                  {session.data.user.displayName}
                </span>
              </button>
            )}
            <button
              type="button"
              onClick={doDeposit}
              disabled={deposit.isPending || !session.isSuccess}
              style={{
                border: "2px solid #ff8a1f",
                background: "#2a1406",
                color: "#ff8a1f",
                fontFamily: "var(--font-display)",
                fontSize: 11,
                letterSpacing: 1,
                padding: "8px 12px",
                whiteSpace: "nowrap",
                cursor: deposit.isPending ? "wait" : "pointer",
                opacity: deposit.isPending ? 0.6 : 1,
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.background = "#ff8a1f";
                e.currentTarget.style.color = "#06040d";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.background = "#2a1406";
                e.currentTarget.style.color = "#ff8a1f";
              }}
            >
              {deposit.isPending ? "…" : "+1000"}
            </button>
            <NavLink href="/verify">VERIFY</NavLink>
            {session.data && !session.data.user.isGuest && (
              <NavLink href="/auth/logout" hard>
                LOGOUT
              </NavLink>
            )}
            {!session.data && (
              <NavLink href="/auth/login?next=/" hard>
                LOGIN
              </NavLink>
            )}
            {session.data && (session.data.user.role === "admin" || session.data.user.role === "moderator") && (
              <NavLink href="/admin">
                STAFF
              </NavLink>
            )}
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
          </div>
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
            <RadialMenu
              nodes={nodes}
              hub={hub}
              onHubActivate={activeGroup ? () => setDrilled(null) : undefined}
            />
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

/** Live rooms: roulette gets a mini wheel, poker a felt, crash a viewfinder. */
function roomNode(room: LobbyRoom): RadialNode {
  if (room.gameId === "roulette") {
    const open = room.state === "betting_open";
    const live = room.state !== undefined && room.state !== "waiting";
    const last = (room.recentCrashes ?? [])[0];
    return {
      key: room.slug,
      label: room.name.toUpperCase(),
      accent: open ? "#5fe08a" : live ? "#22e8ff" : "#8878b8",
      href: `/rooms/${room.slug}`,
      live,
      badge: room.state ? room.state.toUpperCase() : "…",
      art: <MiniWheel last={last} />,
      status: `BETS ${room.minBet}–${room.maxBet.toLocaleString()} · ${room.playerCount}/${room.capacity}`,
      onLaunchSound: () => sound.chipClink(),
    };
  }
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

/** Miniature European wheel with the last winning pocket in the hub. */
function MiniWheel({ last }: { last: number | undefined }) {
  const seg = 360 / 37;
  return (
    <span
      style={{
        position: "relative",
        width: 60,
        height: 60,
        borderRadius: "50%",
        display: "inline-block",
        background:
          "conic-gradient(from -4.865deg, " +
          EUROPEAN_ORDER.map((p, i) => {
            const c = POCKET_COLORS[pocketColor(p)];
            return `${c} ${(i * seg).toFixed(3)}deg ${((i + 1) * seg).toFixed(3)}deg`;
          }).join(", ") +
          ")",
        boxShadow: "0 0 14px rgba(95,224,138,.35), inset 0 0 0 2px #06040d",
      }}
    >
      <span
        style={{
          position: "absolute",
          inset: 16,
          borderRadius: "50%",
          background: "linear-gradient(#1d1036,#0d0619 80%)",
          border: "1px solid #35205c",
          display: "grid",
          placeItems: "center",
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 12,
            color: last === undefined ? "#5c4f80" : pocketColor(last) === "black" ? "#ece6ff" : POCKET_COLORS[pocketColor(last)],
            textShadow: last === undefined ? "none" : `0 0 8px ${POCKET_COLORS[pocketColor(last)]}`,
          }}
        >
          {last ?? "··"}
        </span>
      </span>
    </span>
  );
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
