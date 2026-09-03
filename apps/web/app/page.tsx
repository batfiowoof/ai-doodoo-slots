"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import Backdrop from "@/components/Backdrop";
import PixelSymbol from "@/components/PixelSymbol";
import { NavLink } from "@/components/NavButton";
import { sound } from "@/lib/sound";
import { useDeposit, useGames, useSession } from "@/lib/api";
import { useLobby, type LobbyRoom } from "@/lib/useLobby";
import PixelCard from "@/components/PixelCard";
import type { GameInfo } from "@/lib/types";

const TRACK_W = 1220;

/** The arcade floor: one cabinet card per machine, plus the deposit kiosk. */
export default function GameMenu() {
  const session = useSession();
  const games = useGames();
  const deposit = useDeposit();
  const lobby = useLobby();
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
              LIVE TABLES
            </div>
          </div>

          {lobby.rooms.length > 0 && (
            <div
              style={{
                width: TRACK_W,
                display: "flex",
                justifyContent: "center",
                flexWrap: "wrap",
                gap: 22,
                transform: `scale(${menuScale})`,
                transformOrigin: "center center",
              }}
            >
              {lobby.rooms.map((r) => (
                <LiveTableCard key={r.slug} room={r} />
              ))}
            </div>
          )}

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

// ---------- live tables ----------

// Shared card chrome: every live-table card lifts and glows on hover.
const CARD_BASE: React.CSSProperties = {
  width: 376,
  boxSizing: "border-box",
  flex: "0 0 376px",
  display: "block",
  padding: 20,
  background: "linear-gradient(#0d0619,#170c2b)",
  border: "2px solid #35205c",
  cursor: "pointer",
  color: "inherit",
  textDecoration: "none",
  transition: "transform 160ms ease-out, box-shadow 160ms ease-out, border-color 160ms ease-out",
};

function cardHover(accent: string) {
  return {
    onMouseEnter: (e: React.MouseEvent<HTMLElement>) => {
      e.currentTarget.style.transform = "translateY(-5px)";
      e.currentTarget.style.borderColor = accent;
      e.currentTarget.style.boxShadow = `0 0 44px ${accent}59`;
    },
    onMouseLeave: (e: React.MouseEvent<HTMLElement>) => {
      e.currentTarget.style.transform = "none";
      e.currentTarget.style.borderColor = "#35205c";
      e.currentTarget.style.boxShadow = "none";
    },
  };
}

function CardHeader({ name, accent, right }: { name: string; accent: string; right: React.ReactNode }) {
  return (
    <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12 }}>
      <span
        style={{
          fontFamily: "var(--font-display)",
          fontSize: 17,
          letterSpacing: 2,
          color: accent,
          textShadow: `0 0 10px ${accent}80`,
        }}
      >
        {name.toUpperCase()}
      </span>
      <span style={{ fontFamily: "var(--font-body)", fontSize: 19, color: "#8878b8" }}>{right}</span>
    </div>
  );
}

function StateBadge({ state }: { state: string }) {
  const live = state !== "waiting" && state !== "" && state !== "idle";
  const showdown = state === "showdown";
  const color = showdown ? "#ff2d95" : live ? "#22e8ff" : "#8878b8";
  return (
    <span
      style={{
        fontFamily: "var(--font-display)",
        fontSize: 11,
        letterSpacing: 2,
        padding: "4px 10px",
        border: `1px solid ${color}`,
        color,
        textShadow: live ? `0 0 10px ${color}` : "none",
        animation: live ? "hintBlink 1.6s steps(1) infinite" : undefined,
      }}
    >
      {state === "" ? "…" : state.toUpperCase()}
    </span>
  );
}

/** Dispatches a lobby room to its game's card. */
function LiveTableCard({ room }: { room: LobbyRoom }) {
  if (room.gameId === "holdem") return <PokerTableCard room={room} />;
  return <CrashTableCard room={room} />;
}

/** Hold'em room card: a miniature oval table with live seats and phase. */
function PokerTableCard({ room }: { room: LobbyRoom }) {
  const live = room.state !== undefined && room.state !== "waiting";
  const seated = Math.min(room.playerCount, room.capacity);
  const bb = room.minBet;
  const sb = bb / 2;

  return (
    <Link
      href={`/rooms/${room.slug}`}
      onClick={() => {
        sound.unlock();
        sound.chipClink();
      }}
      style={{
        ...CARD_BASE,
        border: `2px solid ${live ? "#22e8ff" : "#35205c"}`,
        boxShadow: live ? "0 0 34px rgba(34,232,255,.3)" : "0 0 24px rgba(157,77,255,.15)",
      }}
      {...cardHover("#22e8ff")}
    >
      <CardHeader
        name={room.name}
        accent={live ? "#22e8ff" : "#cfc4f2"}
        right={`${room.playerCount}/${room.capacity} SEATED`}
      />

      {/* Miniature table */}
      <div style={{ marginTop: 14, position: "relative", height: 132 }}>
        {/* Wood rim */}
        <div
          style={{
            position: "absolute",
            inset: 0,
            borderRadius: "50%",
            background: "linear-gradient(160deg, #7a4c26, #5c3a1e 45%, #38200e)",
            boxShadow: "0 10px 26px rgba(0,0,0,.55), inset 0 1px 0 rgba(255,200,140,.35)",
          }}
        />
        {/* Felt */}
        <div
          style={{
            position: "absolute",
            inset: 7,
            borderRadius: "50%",
            background: "linear-gradient(#15503a, #0b352a)",
            boxShadow: "inset 0 0 0 2px rgba(34,232,255,.3), inset 0 0 40px rgba(0,0,0,.5)",
          }}
        />
        {/* Dashed inlay */}
        <div
          style={{
            position: "absolute",
            inset: 14,
            borderRadius: "50%",
            border: "1px dashed rgba(159,216,192,.3)",
          }}
        />
        {/* Board slots */}
        <div
          style={{
            position: "absolute",
            left: "50%",
            top: "44%",
            transform: "translate(-50%, -50%)",
            display: "flex",
            gap: 5,
          }}
        >
          {[0, 1, 2, 3, 4].map((i) => (
            <span
              key={i}
              style={{
                width: 15,
                height: 21,
                border: "1px dashed rgba(159,216,192,.4)",
                background: "rgba(0,0,0,.22)",
              }}
            />
          ))}
        </div>
        {/* Held hole cards at the near edge */}
        <div
          style={{
            position: "absolute",
            left: "50%",
            bottom: -8,
            transform: "translateX(-50%)",
            display: "flex",
            filter: "drop-shadow(0 4px 5px rgba(0,0,0,.5))",
          }}
        >
          <span style={{ transform: "rotate(-10deg)" }}>
            <PixelCard code="back" scale={1} />
          </span>
          <span style={{ transform: "rotate(10deg)", marginLeft: -10 }}>
            <PixelCard code="back" scale={1} />
          </span>
        </div>
        {/* Seat dots on the far arc: filled = seated */}
        {Array.from({ length: room.capacity }, (_, i) => {
          const n = Math.max(room.capacity, 1);
          const theta = Math.PI - ((i + 1) * Math.PI) / (n + 1);
          const x = 50 + 44 * Math.cos(theta);
          const y = 46 - 36 * Math.sin(theta);
          const taken = i < seated;
          return (
            <span
              key={i}
              title={`seat ${i + 1}`}
              style={{
                position: "absolute",
                left: `${x}%`,
                top: `${y}%`,
                transform: "translate(-50%, -50%)",
                width: 9,
                height: 9,
                borderRadius: "50%",
                background: taken ? "#5fe08a" : "transparent",
                border: taken ? "none" : "1px dashed rgba(136,120,184,.7)",
                boxShadow: taken ? "0 0 8px rgba(95,224,138,.8)" : "none",
              }}
            />
          );
        })}
      </div>

      <div
        style={{
          marginTop: 16,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <span style={{ fontFamily: "var(--font-body)", fontSize: 19, color: "#ff8a1f" }}>
          BLINDS {sb}/{bb}
        </span>
        <StateBadge state={room.state ?? "…"} />
      </div>
    </Link>
  );
}

/** Crash room card: live multiplier, recent-crash sparkline, bet window. */
function CrashTableCard({ room }: { room: LobbyRoom }) {
  const live = room.state === "running";
  const open = room.state === "betting_open";
  const accent = live ? "#22e8ff" : open ? "#5fe08a" : "#cfc4f2";
  const crashes = (room.recentCrashes ?? []).slice(-10);
  const last = crashes[crashes.length - 1];

  // Sparkline of recent crashes, log-ish scaled so 1.0× sits low.
  const w = 316;
  const h = 66;
  const max = Math.max(2, ...crashes);
  const pts = crashes.map(
    (m, i) =>
      `${(i / Math.max(crashes.length - 1, 1)) * (w - 8) + 4},${
        h - 5 - (Math.log(m + 0.2) / Math.log(max + 0.2)) * (h - 12)
      }`,
  );

  return (
    <Link
      href={`/rooms/${room.slug}`}
      onClick={() => {
        sound.unlock();
        sound.click();
      }}
      style={{
        ...CARD_BASE,
        border: `2px solid ${live ? "#22e8ff" : open ? "#1c5f6b" : "#35205c"}`,
        boxShadow: live ? "0 0 34px rgba(34,232,255,.3)" : "0 0 24px rgba(157,77,255,.15)",
      }}
      {...cardHover(accent)}
    >
      <CardHeader name={room.name} accent={accent} right={`${room.playerCount} IN`} />

      {/* Multiplier headline / bet window */}
      <div
        style={{
          marginTop: 12,
          height: 58,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          padding: "0 6px",
        }}
      >
        {live && room.multiplier ? (
          <>
            <span
              style={{
                fontFamily: "var(--font-body)",
                fontSize: 52,
                lineHeight: 1,
                color: "#ff8a1f",
                textShadow: "0 0 18px rgba(255,138,31,.8)",
              }}
            >
              {room.multiplier.toFixed(2)}×
            </span>
            <StateBadge state="in flight" />
          </>
        ) : open ? (
          <>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 20,
                letterSpacing: 3,
                color: "#5fe08a",
                textShadow: "0 0 14px rgba(95,224,138,.7)",
                animation: "hintBlink 1.2s steps(1) infinite",
              }}
            >
              PLACE YOUR BETS
            </span>
            <StateBadge state="bets open" />
          </>
        ) : (
          <>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 18,
                letterSpacing: 3,
                color: "#5c4f80",
              }}
            >
              STARTING SOON
            </span>
            <StateBadge state="idle" />
          </>
        )}
      </div>

      {/* Recent crashes sparkline */}
      <div
        style={{
          marginTop: 10,
          padding: 8,
          background: "#06040d",
          boxShadow: "inset 0 0 0 1px #241640",
        }}
      >
        {crashes.length >= 2 ? (
          <svg width="100%" height={h} viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none">
            <line x1="4" y1={h - 5 - (Math.log(1.2) / Math.log(max + 0.2)) * (h - 12)} x2={w - 4} y2={h - 5 - (Math.log(1.2) / Math.log(max + 0.2)) * (h - 12)} stroke="#35205c" strokeDasharray="3 5" strokeWidth="1" />
            <polyline
              points={pts.join(" ")}
              fill="none"
              stroke="#5fe08a"
              strokeWidth="2"
              style={{ filter: "drop-shadow(0 0 4px rgba(95,224,138,.9))" }}
            />
            {last !== undefined && pts.length > 0 && (
              <circle
                cx={w - 4}
                cy={h - 5 - (Math.log(last + 0.2) / Math.log(max + 0.2)) * (h - 12)}
                r="3.5"
                fill={last >= 2 ? "#5fe08a" : "#f2643d"}
                style={{ filter: "drop-shadow(0 0 5px currentColor)" }}
              />
            )}
          </svg>
        ) : (
          <div
            style={{
              height: h,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              fontFamily: "var(--font-body)",
              fontSize: 16,
              color: "#5c4f80",
            }}
          >
            FIRST ROUND SOON
          </div>
        )}
      </div>

      <div
        style={{
          marginTop: 14,
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <span style={{ fontFamily: "var(--font-body)", fontSize: 19, color: "#ff8a1f" }}>
          BETS {room.minBet}–{room.maxBet.toLocaleString()}
        </span>
        <span style={{ fontFamily: "var(--font-display)", fontSize: 12, letterSpacing: 2, color: "#ff2d95" }}>
          ENTER ▸
        </span>
      </div>
    </Link>
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
