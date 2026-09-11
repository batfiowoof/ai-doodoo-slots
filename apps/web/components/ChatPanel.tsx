"use client";

// CASINO WIRE — the global chat panel. Fixed to the right of the dock
// cluster; portals are unnecessary here because it renders at viewport
// level already (it is only mounted from SocialDock, never inside the
// scaled stage).

import { useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { Avatar } from "./Avatar";
import NameTag from "./NameTag";
import { parseChatBody, emoteVisual } from "@/lib/emotes";
import { fmtCredits } from "@/lib/social";
import { sound } from "@/lib/sound";
import type { ChatMessage, Me } from "@/lib/types";

const ACCENT = "#ff2d95";

function nameColor(role: string): string {
  if (role === "admin") return "#ff2d95";
  if (role === "moderator") return "#ffb15c";
  return "#22e8ff";
}

export default function ChatPanel({
  messages,
  me,
  onlineCount,
  status,
  onSend,
  onEmoteClick,
  onRain,
  onOpenPlayer,
  onModerate,
  onClose,
}: {
  messages: ChatMessage[];
  me: Me | null;
  onlineCount: number;
  status: string;
  onSend: (body: string) => void;
  onEmoteClick: () => void;
  onRain: (credits: number) => void;
  onOpenPlayer: (userId: number) => void;
  onModerate: (kind: "delete" | "mute10" | "mute60", msg: ChatMessage) => void;
  onClose: () => void;
}) {
  const isStaff = me?.user.role === "admin" || me?.user.role === "moderator";
  const [text, setText] = useState("");
  const [menu, setMenu] = useState<{ x: number; y: number; msg: ChatMessage } | null>(null);
  const [rainOpen, setRainOpen] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);

  // Stick to the newest line; chat is a firehose, not a scroller.
  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages.length]);

  const send = () => {
    const body = text.trim().slice(0, 256);
    if (!body) return;
    onSend(body);
    setText("");
  };

  return (
    <>
      <div
        style={{
          position: "fixed",
          right: 104,
          bottom: 24,
          width: 400,
          maxWidth: "calc(100vw - 140px)",
          height: 540,
          maxHeight: "calc(100vh - 48px)",
          zIndex: 80,
          display: "flex",
          flexDirection: "column",
          background: "rgba(15,7,32,.96)",
          border: `2px solid ${ACCENT}`,
          boxShadow: `0 0 50px ${ACCENT}55, inset 0 0 0 1px #241640`,
          animation: "chatSlide .22s cubic-bezier(.2,1.2,.4,1) both",
        }}
      >
        {/* header */}
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            padding: "10px 14px",
            borderBottom: "2px solid #241640",
            background: "#150a2a",
            flex: "none",
          }}
        >
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 15,
              letterSpacing: 4,
              color: ACCENT,
              textShadow: `0 0 12px ${ACCENT}`,
            }}
          >
            CASINO WIRE
          </span>
          <span
            title={status}
            style={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              background: status === "open" ? "#5fe08a" : "#ff8a1f",
              boxShadow: `0 0 8px ${status === "open" ? "#5fe08a" : "#ff8a1f"}`,
            }}
          />
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 11,
              color: "#8878b8",
              letterSpacing: 1,
            }}
          >
            {onlineCount} ONLINE
          </span>
          <span style={{ flex: 1 }} />
          <button
            type="button"
            onClick={() => {
              sound.click();
              setRainOpen(true);
            }}
            style={{
              border: "1px solid #ff8a1f",
              background: "#2a1406",
              color: "#ffb15c",
              fontFamily: "var(--font-display)",
              fontSize: 11,
              letterSpacing: 1,
              padding: "5px 10px",
              cursor: "pointer",
            }}
          >
            ☔ RAIN
          </button>
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
              fontSize: 12,
              padding: "5px 10px",
              cursor: "pointer",
            }}
          >
            ✕
          </button>
        </div>

        {/* message list */}
        <div ref={listRef} style={{ flex: 1, overflowY: "auto", padding: "10px 12px", display: "flex", flexDirection: "column", gap: 6 }}>
          {messages.length === 0 && (
            <span
              style={{
                margin: "auto",
                fontFamily: "var(--font-body)",
                fontSize: 18,
                color: "#8878b8",
                textAlign: "center",
                letterSpacing: 1,
              }}
            >
              Nobody has spoken yet.
              <br />
              Be the first voice on the wire.
            </span>
          )}
          {messages.map((m) => {
            const mine = me != null && m.userId === me.user.id;
            const mention =
              !mine && me != null && m.body.toLowerCase().includes(me.user.displayName.toLowerCase());
            if (m.kind === "system") {
              return (
                <div
                  key={m.id}
                  style={{
                    textAlign: "center",
                    padding: "3px 8px",
                    fontFamily: "var(--font-body)",
                    fontSize: 17,
                    color: "#ffd36e",
                    textShadow: "0 0 8px rgba(255,211,110,.4)",
                    letterSpacing: 0.5,
                  }}
                >
                  ★ <span style={{ color: nameColor(m.role), fontFamily: "var(--font-display)", fontSize: 12 }}>{m.displayName}</span>{" "}
                  {m.body} ★
                </div>
              );
            }
            return (
              <div
                key={m.id}
                onContextMenu={
                  isStaff
                    ? (e) => {
                        e.preventDefault();
                        setMenu({ x: e.clientX, y: e.clientY, msg: m });
                      }
                    : undefined
                }
                style={{
                  display: "flex",
                  gap: 8,
                  alignItems: "flex-start",
                  padding: "4px 6px",
                  background: mention ? "rgba(255,45,149,.12)" : "transparent",
                  borderLeft: mention ? `3px solid ${ACCENT}` : "3px solid transparent",
                  borderRadius: 2,
                }}
              >
                <Avatar
                  userId={m.userId}
                  displayName={m.displayName}
                  avatarPreset={m.avatarPreset}
                  avatarVersion={m.avatarVersion}
                  size={26}
                  ring={nameColor(m.role)}
                />
                <div style={{ minWidth: 0, flex: 1 }}>
                  <button
                    type="button"
                    onClick={() => onOpenPlayer(m.userId)}
                    style={{
                      background: "none",
                      border: "none",
                      padding: 0,
                      cursor: "pointer",
                      fontFamily: "var(--font-display)",
                      fontSize: 12,
                      letterSpacing: 1,
                      color: nameColor(m.role),
                    }}
                  >
                    <NameTag
                      displayName={m.displayName}
                      title={m.title}
                      nameEffect={m.nameEffect}
                      titleClassName="text-[7px] px-0.5"
                    />
                    {m.role !== "player" ? " ★" : ""}
                  </button>
                  <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 2, fontFamily: "var(--font-body)", fontSize: 19, color: "#dcd4f5", lineHeight: 1.25, overflowWrap: "anywhere" }}>
                    {parseChatBody(m.body).map((tok, i) =>
                      tok.kind === "text" ? (
                        <span key={i}>{tok.text}</span>
                      ) : (
                        <InlineEmote key={i} id={tok.id} />
                      )
                    )}
                  </div>
                </div>
                {mine && (
                  <span style={{ fontFamily: "var(--font-display)", fontSize: 9, color: "#5a4a88", alignSelf: "center" }}>YOU</span>
                )}
              </div>
            );
          })}
        </div>

        {/* input row */}
        <div style={{ display: "flex", gap: 8, padding: "10px 12px", borderTop: "2px solid #241640", flex: "none" }}>
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") send();
            }}
            maxLength={256}
            placeholder="Speak to the floor… (:gg: for inline emotes)"
            style={{
              flex: 1,
              background: "#0c0718",
              border: "1px solid #35205c",
              color: "#dcd4f5",
              fontFamily: "var(--font-body)",
              fontSize: 18,
              padding: "8px 10px",
              outline: "none",
            }}
          />
          <button
            type="button"
            onClick={() => {
              sound.click();
              onEmoteClick();
            }}
            title="Emote wheel"
            style={{
              width: 40,
              border: "2px solid #ff2d95",
              background: "#160b28",
              fontSize: 18,
              cursor: "pointer",
            }}
          >
            🎉
          </button>
          <button
            type="button"
            onClick={() => {
              sound.unlock();
              send();
            }}
            style={{
              border: "2px solid #22e8ff",
              background: "#0b2a33",
              color: "#22e8ff",
              fontFamily: "var(--font-display)",
              fontSize: 12,
              letterSpacing: 1,
              padding: "0 14px",
              cursor: "pointer",
            }}
          >
            SEND
          </button>
        </div>
      </div>

      {/* staff context menu */}
      {menu && (
        <div
          onClick={() => setMenu(null)}
          onContextMenu={(e) => {
            e.preventDefault();
            setMenu(null);
          }}
          style={{ position: "fixed", inset: 0, zIndex: 95 }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              position: "fixed",
              left: Math.min(menu.x, window.innerWidth - 190),
              top: Math.min(menu.y, window.innerHeight - 130),
              width: 180,
              background: "#150a2a",
              border: "1px solid #ffb15c",
              boxShadow: "0 0 24px rgba(255,177,92,.4)",
              display: "flex",
              flexDirection: "column",
              animation: "rosterPop .12s ease-out both",
            }}
          >
            {(
              [
                ["delete", "DELETE MESSAGE"],
                ["mute10", "MUTE 10 MIN"],
                ["mute60", "MUTE 1 HOUR"],
              ] as const
            ).map(([kind, label]) => (
              <button
                key={kind}
                type="button"
                onClick={() => {
                  sound.click();
                  onModerate(kind, menu.msg);
                  setMenu(null);
                }}
                style={{
                  background: "none",
                  border: "none",
                  borderBottom: "1px solid #241640",
                  color: kind === "delete" ? "#ff2d95" : "#ffb15c",
                  fontFamily: "var(--font-display)",
                  fontSize: 11,
                  letterSpacing: 1,
                  padding: "10px 12px",
                  cursor: "pointer",
                  textAlign: "left",
                }}
              >
                {label}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* rain dialog */}
      {rainOpen && (
        <RainDialog
          onClose={() => setRainOpen(false)}
          onRain={(credits) => {
            setRainOpen(false);
            onRain(credits);
          }}
        />
      )}
    </>
  );
}

function InlineEmote({ id }: { id: string }) {
  const visual = emoteVisual(id);
  if (!visual) return <span style={{ color: "#5a4a88" }}>:{id}:</span>;
  if (visual.type === "img") {
    return <img src={visual.src} width={22} height={22} className="pixelated" alt={id} style={{ verticalAlign: "middle" }} />;
  }
  return <span style={{ fontSize: 20, lineHeight: 1 }}>{visual.emoji}</span>;
}

function RainDialog({ onClose, onRain }: { onClose: () => void; onRain: (credits: number) => void }) {
  const [credits, setCredits] = useState("500");
  const inputStyle: CSSProperties = {
    width: "100%",
    background: "#0c0718",
    border: "1px solid #35205c",
    color: "#ffb15c",
    fontFamily: "var(--font-display)",
    fontSize: 20,
    padding: "10px 12px",
    outline: "none",
    textAlign: "center",
  };
  return createPortal(
    <div
      onClick={onClose}
      style={{ position: "fixed", inset: 0, zIndex: 90, background: "rgba(6,4,13,.86)", display: "grid", placeItems: "center" }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 380,
          maxWidth: "calc(100vw - 48px)",
          background: "#0f0720",
          border: "2px solid #ff8a1f",
          boxShadow: "0 0 60px rgba(255,138,31,.4)",
          padding: 22,
          animation: "bigPop .25s cubic-bezier(.2,1.4,.4,1) both",
          display: "flex",
          flexDirection: "column",
          gap: 14,
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 20,
            letterSpacing: 5,
            color: "#ff8a1f",
            textShadow: "0 0 14px #ff8a1f",
            textAlign: "center",
          }}
        >
          ☔ MAKE IT RAIN
        </span>
        <p style={{ margin: 0, fontFamily: "var(--font-body)", fontSize: 18, color: "#b9aede", textAlign: "center" }}>
          Split a pot of credits across every player online — even the ones not watching this table.
        </p>
        <div style={{ display: "flex", gap: 8, justifyContent: "center" }}>
          {[100, 500, 1000].map((v) => (
            <button
              key={v}
              type="button"
              onClick={() => {
                sound.click();
                setCredits(String(v));
              }}
              style={{
                border: "2px solid #ff8a1f",
                background: Number(credits) === v ? "#ff8a1f" : "#2a1406",
                color: Number(credits) === v ? "#06040d" : "#ffb15c",
                fontFamily: "var(--font-display)",
                fontSize: 13,
                padding: "8px 14px",
                cursor: "pointer",
              }}
            >
              {fmtCredits(v)}
            </button>
          ))}
        </div>
        <input
          value={credits}
          onChange={(e) => setCredits(e.target.value.replace(/[^0-9]/g, "").slice(0, 7))}
          style={inputStyle}
        />
        <button
          type="button"
          onClick={() => {
            const v = Number(credits);
            if (!Number.isFinite(v) || v < 10) {
              sound.error();
              return;
            }
            sound.unlock();
            onRain(v);
          }}
          style={{
            border: "2px solid #ff8a1f",
            background: "#2a1406",
            color: "#ffb15c",
            fontFamily: "var(--font-display)",
            fontSize: 15,
            letterSpacing: 2,
            padding: "12px",
            cursor: "pointer",
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.background = "#ff8a1f";
            e.currentTarget.style.color = "#06040d";
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.background = "#2a1406";
            e.currentTarget.style.color = "#ffb15c";
          }}
        >
          RAIN {fmtCredits(Number(credits) || 0)} CREDITS
        </button>
      </div>
    </div>,
    document.body
  );
}
