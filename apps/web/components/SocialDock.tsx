"use client";

// The social dock: one lobby-mode socket feeding chat, the emote wheel,
// the online roster, the leaderboard, big-win banners, rain storms and
// tips — on every screen. Rendered once from the root layout, outside the
// scaled stage; overlays portal to the body.

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useQueryClient } from "@tanstack/react-query";
import ChatPanel from "./ChatPanel";
import EmoteWheel from "./EmoteWheel";
import LeaderboardDialog from "./LeaderboardDialog";
import PlayerCard from "./PlayerCard";
import { Avatar } from "./Avatar";
import { pixelClip } from "./Pixel";
import { useSession } from "@/lib/api";
import { useCasinoSocket, type CasinoEnvelope } from "@/lib/useCasinoSocket";
import { useChatHistory } from "@/lib/social";
import { emoteVisual } from "@/lib/emotes";
import { sound } from "@/lib/sound";
import type { ChatMessage } from "@/lib/types";

interface EmoteEvt {
  userId: number;
  displayName: string;
  avatarPreset: string;
  avatarVersion: number;
  emoteId: string;
}

interface BigWinEvt {
  userId: number;
  displayName: string;
  gameId: string;
  betCredits: number;
  payoutCredits: number;
  multiplier: number;
}

interface RainEvt {
  fromUserId: number;
  fromName: string;
  totalCredits: number;
  shareCredits: number;
  recipientCount: number;
}

interface TipEvt {
  fromUserId: number;
  fromName: string;
  toUserId: number;
  toName: string;
  credits: number;
}

interface RosterEntry {
  userId: number;
  displayName: string;
  avatarPreset: string;
  avatarVersion: number;
  role: string;
  room: string;
}

interface FloatEmote {
  key: number;
  emoteId: string;
  name: string;
  mine: boolean;
  drift: number;
}

interface Toast {
  key: number;
  text: string;
  accent: string;
}

let keySeq = 1;

function roomLabel(slug: string): string {
  if (!slug) return "LOBBY";
  return slug.replace(/-[a-z0-9]+$/i, "").toUpperCase();
}

export default function SocialDock() {
  const session = useSession();
  const qc = useQueryClient();
  const me = session.data ?? null;

  const [chatOpen, setChatOpen] = useState(false);
  const [unread, setUnread] = useState(0);
  const [wheelOpen, setWheelOpen] = useState(false);
  const [rosterOpen, setRosterOpen] = useState(false);
  const [boardOpen, setBoardOpen] = useState(false);
  const [playerId, setPlayerId] = useState<number | null>(null);
  const [liveMessages, setLiveMessages] = useState<ChatMessage[]>([]);
  const [historyOpened, setHistoryOpened] = useState(false);
  const [roster, setRoster] = useState<RosterEntry[]>([]);
  const [online, setOnline] = useState(0);
  const [floats, setFloats] = useState<FloatEmote[]>([]);
  const [coins, setCoins] = useState<{ key: number; left: number; size: number; dur: number; delay: number }[]>([]);
  const [banner, setBanner] = useState<BigWinEvt | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);

  const chatOpenRef = useRef(false);
  const lastPopRef = useRef(0);
  const bannerTimer = useRef<number | undefined>(undefined);

  const chat = useChatHistory(historyOpened);
  useEffect(() => {
    chatOpenRef.current = chatOpen;
  }, [chatOpen]);

  // History replay + live feed, deduped by message id (a system line can be
  // persisted and broadcast while history is in flight).
  const messages = (() => {
    if (!chat.data) return liveMessages;
    const ids = new Set(liveMessages.map((m) => m.id));
    return [...chat.data.filter((m) => !ids.has(m.id)), ...liveMessages];
  })();

  const popThrottled = () => {
    const now = Date.now();
    if (now - lastPopRef.current > 400) {
      lastPopRef.current = now;
      sound.chatPop();
    }
  };

  const pushToast = (text: string, accent = "#22e8ff") => {
    const key = keySeq++;
    setToasts((t) => [...t.slice(-3), { key, text, accent }]);
    window.setTimeout(() => setToasts((t) => t.filter((x) => x.key !== key)), 4200);
  };

  const handleMessage = (msg: CasinoEnvelope) => {
    const p = msg.payload as Record<string, unknown> | undefined;
    switch (msg.type) {
      case "chat_message": {
        const m = p as unknown as ChatMessage;
        setLiveMessages((old) => [...old.slice(-199), m]);
        if (chatOpenRef.current) {
          popThrottled();
        } else {
          setUnread((u) => u + 1);
          popThrottled();
        }
        if (
          me != null &&
          m.userId !== me.user.id &&
          m.kind === "chat" &&
          m.body.toLowerCase().includes(me.user.displayName.toLowerCase())
        ) {
          sound.turnAlert();
        }
        break;
      }
      case "chat_deleted": {
        const { messageId } = p as { messageId: number };
        setLiveMessages((old) => old.filter((m) => m.id !== messageId));
        break;
      }
      case "emote": {
        const e = p as unknown as EmoteEvt;
        const mine = me != null && e.userId === me.user.id;
        const key = keySeq++;
        setFloats((f) => [
          ...f.slice(-11),
          { key, emoteId: e.emoteId, name: e.displayName, mine, drift: Math.round(Math.random() * 120 - 40) },
        ]);
        window.setTimeout(() => setFloats((f) => f.filter((x) => x.key !== key)), 3800);
        if (!mine) popThrottled();
        break;
      }
      case "big_win": {
        const w = p as unknown as BigWinEvt;
        setBanner(w);
        sound.bigWin();
        window.clearTimeout(bannerTimer.current);
        bannerTimer.current = window.setTimeout(() => setBanner(null), 4200);
        void qc.invalidateQueries({ queryKey: ["leaderboard"] });
        break;
      }
      case "rain": {
        const r = p as unknown as RainEvt;
        const storm = Array.from({ length: 26 }, () => ({
          key: keySeq++,
          left: Math.random() * 96,
          size: 16 + Math.random() * 20,
          dur: 1.7 + Math.random() * 1.3,
          delay: Math.random() * 0.9,
        }));
        setCoins((c) => [...c, ...storm]);
        sound.rainStorm();
        pushToast(`☔ ${r.fromName} rained ${r.totalCredits.toLocaleString()} across ${r.recipientCount} players`, "#ff8a1f");
        window.setTimeout(() => setCoins((c) => c.filter((x) => !storm.some((s) => s.key === x.key))), 3400);
        void qc.invalidateQueries({ queryKey: ["me"] });
        break;
      }
      case "tip": {
        const t = p as unknown as TipEvt;
        pushToast(`💸 ${t.fromName} tipped ${t.toName} ${t.credits.toLocaleString()} credits`, "#5fe08a");
        if (me != null && (t.toUserId === me.user.id || t.fromUserId === me.user.id)) {
          if (t.toUserId === me.user.id) sound.tipReceived();
          void qc.invalidateQueries({ queryKey: ["me"] });
        }
        break;
      }
      case "lobby_summary": {
        const s = p as unknown as { connectedPlayers: number; roster?: RosterEntry[] };
        setOnline(s.connectedPlayers);
        if (s.roster) setRoster(s.roster);
        break;
      }
      case "error": {
        const e = p as { code?: string } | undefined;
        if (e?.code === "muted") pushToast("You are muted. The floor is watching.", "#ff2d95");
        else if (e?.code === "insufficient_credits") pushToast("Not enough credits.", "#ff8a1f");
        else if (e?.code === "rain_too_small") pushToast("Too many players online for that pot.", "#ff8a1f");
        break;
      }
    }
  };

  const socket = useCasinoSocket(handleMessage);

  const openChat = () => {
    sound.unlock();
    sound.click();
    setChatOpen((v) => {
      const next = !v;
      if (next) {
        setUnread(0);
        setHistoryOpened(true);
      }
      return next;
    });
  };

  const pickEmote = (emoteId: string) => {
    setWheelOpen(false);
    socket.send("send_emote", { emoteId });
  };

  const moderate = (kind: "delete" | "mute10" | "mute60", m: ChatMessage) => {
    if (kind === "delete") socket.send("chat_delete", { messageId: m.id });
    else socket.send("chat_mute", { userId: m.userId, minutes: kind === "mute10" ? 10 : 60, reason: "floor" });
  };

  return (
    <>
      {/* dock chips — one horizontal pixel row along the bottom-right */}
      <div
        style={{
          position: "fixed",
          right: 20,
          bottom: 20,
          zIndex: 80,
          display: "flex",
          flexDirection: "row",
          gap: 12,
        }}
      >
        <DockChip icon="💬" label="CHAT" accent="#ff2d95" badge={unread} active={chatOpen} onClick={openChat} title="Casino Wire chat" />
        <DockChip icon="🏆" label="TOP" accent="#ff8a1f" onClick={() => {
          sound.unlock();
          sound.leaderboardOpen();
          setBoardOpen(true);
        }} title="The floor's finest" />
        <DockChip icon="🎉" label="EMOTE" accent="#22e8ff" active={wheelOpen} onClick={() => {
          sound.unlock();
          sound.click();
          setWheelOpen((v) => !v);
        }} title="Emote wheel" />
        <DockChip icon={String(online)} label="ONLINE" accent="#5fe08a" onClick={() => {
          sound.click();
          setRosterOpen((v) => !v);
        }} title="Who's on the floor" />
      </div>

      {/* toasts */}
      <div
        style={{
          position: "fixed",
          right: 104,
          bottom: 580,
          zIndex: 84,
          display: "flex",
          flexDirection: "column",
          gap: 8,
          alignItems: "flex-end",
          pointerEvents: "none",
        }}
      >
        {toasts.map((t) => (
          <div
            key={t.key}
            style={{
              maxWidth: 380,
              padding: "8px 14px",
              background: "rgba(6,4,13,.92)",
              border: `2px solid ${t.accent}`,
              color: t.accent,
              fontFamily: "var(--font-display)",
              fontSize: 12,
              letterSpacing: 1,
              animation: "notePop .2s ease-out both",
            }}
          >
            {t.text}
          </div>
        ))}
      </div>

      {chatOpen && (
        <ChatPanel
          messages={messages}
          me={me}
          onlineCount={online}
          status={socket.status}
          onSend={(body) => socket.send("send_chat", { body })}
          onEmoteClick={() => setWheelOpen((v) => !v)}
          onRain={(credits) => socket.send("make_it_rain", { credits })}
          onOpenPlayer={(userId) => setPlayerId(userId)}
          onModerate={moderate}
          onClose={() => setChatOpen(false)}
        />
      )}

      <EmoteWheel open={wheelOpen} onPick={pickEmote} />

      {/* online roster */}
      {rosterOpen && (
        <div
          onClick={() => setRosterOpen(false)}
          style={{ position: "fixed", inset: 0, zIndex: 81 }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              position: "fixed",
              right: 20,
              bottom: 104,
              width: 280,
              maxHeight: 380,
              overflowY: "auto",
              background: "rgba(15,7,32,.97)",
              border: "2px solid #5fe08a",
              boxShadow: "0 0 40px rgba(95,224,138,.3)",
              padding: 10,
              animation: "rosterPop .16s ease-out both",
            }}
          >
            <span style={{ display: "block", fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 3, color: "#5fe08a", padding: "2px 4px 8px" }}>
              ON THE FLOOR — {online}
            </span>
            {roster.length === 0 && (
              <span style={{ fontFamily: "var(--font-body)", fontSize: 17, color: "#8878b8", padding: 4 }}>
                Just the dealers tonight.
              </span>
            )}
            {roster.map((r) => (
              <div key={r.userId} style={{ display: "flex", alignItems: "center", gap: 8, padding: "4px 4px" }}>
                <Avatar userId={r.userId} displayName={r.displayName} avatarPreset={r.avatarPreset} avatarVersion={r.avatarVersion} size={26} ring={r.role !== "player" ? "#ffb15c" : "#35205c"} />
                <button
                  type="button"
                  onClick={() => {
                    setRosterOpen(false);
                    setPlayerId(r.userId);
                  }}
                  style={{
                    background: "none",
                    border: "none",
                    padding: 0,
                    cursor: "pointer",
                    fontFamily: "var(--font-display)",
                    fontSize: 12,
                    letterSpacing: 1,
                    color: r.role !== "player" ? "#ffb15c" : "#dcd4f5",
                    maxWidth: 130,
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                >
                  {r.displayName}
                </button>
                <span style={{ flex: 1 }} />
                <span style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 1, color: "#5a4a88" }}>
                  {roomLabel(r.room)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}

      <LeaderboardDialog open={boardOpen} onClose={() => setBoardOpen(false)} meUserId={me?.user.id ?? null} onOpenPlayer={(userId) => setPlayerId(userId)} />

      <PlayerCard
        userId={playerId}
        me={me ? { id: me.user.id, displayName: me.user.displayName, balanceCredits: me.balanceCredits } : null}
        onClose={() => setPlayerId(null)}
        onTip={(toUserId, credits) => {
          socket.send("send_tip", { toUserId, credits });
          sound.chipClink();
          setPlayerId(null);
          pushToast(`💸 Tip sent: ${credits.toLocaleString()} credits`, "#5fe08a");
        }}
      />

      {/* floating emotes */}
      {floats.length > 0 &&
        createPortal(
          <div style={{ position: "fixed", inset: 0, zIndex: 85, pointerEvents: "none", overflow: "hidden" }}>
            {floats.map((f) => (
              <div
                key={f.key}
                style={{
                  position: "absolute",
                  right: 90,
                  bottom: 90,
                  transform: `translateX(${f.drift}px)`,
                  display: "flex",
                  flexDirection: "column",
                  alignItems: "center",
                  gap: 2,
                }}
              >
                <div style={{ animation: "emoteFloat 3.6s ease-out both", display: "flex", flexDirection: "column", alignItems: "center", gap: 2 }}>
                  <EmoteGlyph id={f.emoteId} />
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 10,
                      letterSpacing: 1,
                      color: f.mine ? "#ff2d95" : "#22e8ff",
                      background: "rgba(6,4,13,.8)",
                      padding: "2px 6px",
                      border: `1px solid ${f.mine ? "#ff2d9566" : "#22e8ff66"}`,
                      animation: "emoteName 3.6s ease both",
                    }}
                  >
                    {f.name}
                  </span>
                </div>
              </div>
            ))}
          </div>,
          document.body
        )}

      {/* rain storm */}
      {coins.length > 0 &&
        createPortal(
          <div style={{ position: "fixed", inset: 0, zIndex: 87, pointerEvents: "none", overflow: "hidden" }}>
            {coins.map((c) => (
              <div
                key={c.key}
                style={{
                  position: "absolute",
                  left: `${c.left}vw`,
                  top: 0,
                  fontSize: c.size,
                  filter: "drop-shadow(0 0 6px rgba(255,138,31,.7))",
                  animation: `coinRain ${c.dur}s linear ${c.delay}s both`,
                }}
              >
                🪙
              </div>
            ))}
          </div>,
          document.body
        )}

      {/* big-win banner */}
      {banner &&
        createPortal(
          <div
            style={{
              position: "fixed",
              top: 76,
              left: "50%",
              zIndex: 86,
              pointerEvents: "none",
              animation: "winBanner 4.2s cubic-bezier(.2,1.2,.4,1) both",
              whiteSpace: "nowrap",
            }}
          >
            <div
              style={{
                display: "inline-flex",
                alignItems: "center",
                gap: 12,
                padding: "14px 26px",
                background: "linear-gradient(90deg, #2a1406, #1d1036, #2a1406)",
                border: "3px solid #ff8a1f",
                boxShadow: "0 0 46px rgba(255,138,31,.75), inset 0 0 0 1px #06040d",
              }}
            >
              <span style={{ fontSize: 26 }}>👑</span>
              <span
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 17,
                  letterSpacing: 2,
                  color: "#ffb15c",
                  textShadow: "0 0 12px rgba(255,138,31,.9)",
                }}
              >
                {banner.displayName} HIT {banner.multiplier.toFixed(2)}× ON {banner.gameId.toUpperCase()}
              </span>
              <span style={{ fontFamily: "var(--font-display)", fontSize: 17, color: "#5fe08a", textShadow: "0 0 12px rgba(95,224,138,.9)" }}>
                +{banner.payoutCredits.toLocaleString()} CR
              </span>
              <span style={{ fontSize: 26 }}>💰</span>
            </div>
          </div>,
          document.body
        )}
    </>
  );
}

function EmoteGlyph({ id }: { id: string }) {
  const visual = emoteVisual(id);
  if (!visual) return <span style={{ fontSize: 44 }}>🎲</span>;
  if (visual.type === "img") {
    return <img src={visual.src} width={56} height={56} className="pixelated" alt="" />;
  }
  return <span style={{ fontSize: 48, filter: "drop-shadow(0 0 10px rgba(34,232,255,.5))" }}>{visual.emoji}</span>;
}

function DockChip({
  icon,
  label,
  accent,
  badge,
  active,
  onClick,
  title,
}: {
  icon: string;
  label: string;
  accent: string;
  badge?: number;
  active?: boolean;
  onClick: () => void;
  title: string;
}) {
  const [hover, setHover] = useState(false);
  const lit = hover || active;
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        position: "relative",
        border: "none",
        padding: 0,
        background: "none",
        cursor: "pointer",
        transform: hover ? "scale(1.08)" : "scale(1)",
        transition: "transform .1s ease, filter .12s ease",
        filter: lit ? `drop-shadow(0 0 20px ${accent}cc)` : `drop-shadow(0 0 8px ${accent}44)`,
      }}
    >
      {/* accent frame layer + dark stepped core = the pixel border */}
      <span
        style={{
          display: "block",
          background: accent,
          clipPath: pixelClip(4),
          padding: 3,
        }}
      >
        <span
          style={{
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            gap: 1,
            width: 64,
            height: 64,
            clipPath: pixelClip(4),
            background: active
              ? `radial-gradient(circle at 34% 30%, ${accent}55, #120a24 70%)`
              : "radial-gradient(circle at 34% 30%, #2b1a4d, #120a24 70%)",
          }}
        >
          <span style={{ fontSize: 22, lineHeight: 1 }}>{icon}</span>
          <span style={{ fontFamily: "var(--font-display)", fontSize: 8, letterSpacing: 1, color: accent }}>{label}</span>
        </span>
      </span>
      {badge != null && badge > 0 && (
        <span
          style={{
            position: "absolute",
            top: -7,
            right: -7,
            minWidth: 22,
            height: 22,
            background: "#ff2d95",
            clipPath: pixelClip(3),
            color: "#06040d",
            fontFamily: "var(--font-display)",
            fontSize: 11,
            display: "grid",
            placeItems: "center",
            padding: "0 4px",
            animation: "dockBadge .25s cubic-bezier(.2,1.6,.4,1) both",
          }}
        >
          {badge > 99 ? "99+" : badge}
        </span>
      )}
    </button>
  );
}
