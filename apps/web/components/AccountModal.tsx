"use client";

import { useEffect, useRef, useState } from "react";
import { Avatar } from "@/components/Avatar";
import NameTag from "@/components/NameTag";
import NeonDialog from "@/components/NeonDialog";
import { sound } from "@/lib/sound";
import {
  useDeleteAvatar,
  usePersonalizedLobby,
  useSelfExclude,
  useSessions,
  useRevokeSession,
  useSession,
  useUpdatePreferences,
  useUpdateProfile,
  useUploadAvatar,
} from "@/lib/api";
import { AVATAR_PRESETS, type Me } from "@/lib/types";

// Keycloak hosts credentials (password, email, MFA); we link out to its
// account console rather than re-implementing credential flows. Override in
// non-dev deployments with NEXT_PUBLIC_KEYCLOAK_ACCOUNT_URL.
const KC_ACCOUNT_URL =
  process.env.NEXT_PUBLIC_KEYCLOAK_ACCOUNT_URL ??
  "http://localhost:8081/realms/retro-casino/account";

type Tab = "profile" | "security" | "lobby";

/** The ACCOUNT overlay: identity, avatar wardrobe, and session controls. */
export function AccountModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const session = useSession();
  const me = session.data ?? null;
  const [tab, setTab] = useState<Tab>("profile");
  const [note, setNote] = useState<string | null>(null);

  useEffect(() => {
    if (!note) return;
    const t = setTimeout(() => setNote(null), 2600);
    return () => clearTimeout(t);
  }, [note]);

  if (!open || !me) return null;

  const accent =
    tab === "profile" ? "#22e8ff" : tab === "security" ? "#5fe08a" : "#ffd21f";

  return (
    <NeonDialog open={open} onClose={onClose} title="◆ ACCOUNT" accent={accent} width={980}>
      <IdentityStrip me={me} />
      <TabRow tab={tab} onTab={(t) => { sound.click(); setTab(t); }} />
      {tab === "profile" ? (
        <ProfileTab key={me.user.displayName} me={me} onNote={setNote} />
      ) : tab === "security" ? (
        <SecurityTab me={me} onNote={setNote} />
      ) : (
        <LobbyTab me={me} onNote={setNote} />
      )}

      {note && (
        <div
          role="status"
          style={{
            margin: "0 20px 16px",
            border: `2px solid ${accent}`,
            background: "#06040d",
            color: accent,
            fontFamily: "var(--font-display)",
            fontSize: 14,
            letterSpacing: 2,
            padding: "10px 14px",
            animation: "notePop .2s ease-out both",
          }}
        >
          {note}
        </div>
      )}
    </NeonDialog>
  );
}

/** Big identity header: avatar, callsign, badges. */
function IdentityStrip({ me }: { me: Me }) {
  const u = me.user;
  const since = new Date(u.createdAt).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 18, padding: "18px 16px 8px" }}>
      <Avatar
        userId={u.id}
        displayName={u.displayName}
        avatarPreset={u.avatarPreset}
        avatarVersion={u.avatarVersion}
        size={96}
        ring="#22e8ff"
        glow
      />
      <div style={{ display: "flex", flexDirection: "column", gap: 6, minWidth: 0 }}>
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 24,
            letterSpacing: 2,
            color: "#ece6ff",
            textShadow: "0 0 12px rgba(157,77,255,.6)",
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          <NameTag displayName={u.displayName} title={u.title} nameEffect={u.nameEffect} titleClassName="text-[10px] px-1.5" />
        </span>
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {u.isGuest ? (
            <Badge color="#ffb15c" bg="#2a1406">GUEST — NOT SAVED</Badge>
          ) : (
            <Badge color="#5fe08a" bg="#0b2a33">REGISTERED</Badge>
          )}
          <Badge color="#22e8ff" bg="#0b2a33">{u.role.toUpperCase()}</Badge>
          {u.status !== "active" && (
            <Badge color="#f2643d" bg="#2d0a1e">{u.status.toUpperCase()}</Badge>
          )}
          <Badge color="#8878b8" bg="#150a2a">SINCE {since}</Badge>
          <a
            href="/shop"
            onClick={() => sound.click()}
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 12,
              letterSpacing: 2,
              color: "#ffd21f",
              background: "#2a2006",
              border: "1px solid #ffd21f55",
              padding: "4px 9px",
              textDecoration: "none",
            }}
          >
            🛒 THE VAULT
          </a>
        </div>
      </div>
    </div>
  );
}

function Badge({ color, bg, children }: { color: string; bg: string; children: React.ReactNode }) {
  return (
    <span
      style={{
        fontFamily: "var(--font-display)",
        fontSize: 12,
        letterSpacing: 2,
        color,
        background: bg,
        border: `1px solid ${color}55`,
        padding: "4px 9px",
      }}
    >
      {children}
    </span>
  );
}

function TabRow({ tab, onTab }: { tab: Tab; onTab: (t: Tab) => void }) {
  const tabs: { id: Tab; label: string; color: string }[] = [
    { id: "profile", label: "PROFILE", color: "#22e8ff" },
    { id: "security", label: "SECURITY", color: "#5fe08a" },
    { id: "lobby", label: "LOBBY", color: "#ffd21f" },
  ];
  return (
    <div style={{ display: "flex", gap: 8, padding: "10px 16px 0" }}>
      {tabs.map((t) => {
        const active = t.id === tab;
        return (
          <button
            key={t.id}
            type="button"
            onClick={() => onTab(t.id)}
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 16,
              letterSpacing: 3,
              padding: "10px 22px",
              cursor: "pointer",
              border: `2px solid ${active ? t.color : "#35205c"}`,
              background: active ? "#06040d" : "#150a2a",
              color: active ? t.color : "#8878b8",
              boxShadow: active ? `0 0 14px ${t.color}66` : "none",
            }}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}

function ProfileTab({ me, onNote }: { me: Me; onNote: (s: string) => void }) {
  const u = me.user;
  const update = useUpdateProfile();
  const upload = useUploadAvatar();
  const removeAvatar = useDeleteAvatar();
  // The parent keys this component on u.displayName, so a saved rename
  // remounts it with the authoritative value — no effect-driven setState.
  const [name, setName] = useState(u.displayName);
  const [error, setError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const dirty = name.trim() !== u.displayName;

  const saveName = () => {
    sound.click();
    setError(null);
    update.mutate(
      { displayName: name.trim() },
      {
        onSuccess: (m) => {
          setName(m.user.displayName);
          onNote("CALLSIGN SAVED");
          sound.bell();
        },
        onError: (e) => {
          setError(e.message);
          sound.error();
        },
      },
    );
  };

  const pickPreset = (key: string) => {
    sound.chipClink();
    setError(null);
    update.mutate(
      { avatarPreset: key },
      {
        onSuccess: () => onNote("AVATAR EQUIPPED"),
        onError: (e) => {
          setError(e.message);
          sound.error();
        },
      },
    );
  };

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    setError(null);
    if (!file.type.startsWith("image/")) {
      setError("THAT FILE IS NOT AN IMAGE");
      sound.error();
      return;
    }
    try {
      // Downscale locally to 64x64 PNG: matches the server's storage format
      // and shows exactly what other players will see.
      const bmp = await createImageBitmap(file);
      const side = Math.min(bmp.width, bmp.height);
      const canvas = document.createElement("canvas");
      canvas.width = canvas.height = 64;
      const ctx = canvas.getContext("2d");
      if (!ctx) throw new Error("no canvas");
      ctx.drawImage(bmp, (bmp.width - side) / 2, (bmp.height - side) / 2, side, side, 0, 0, 64, 64);
      const blob = await new Promise<Blob | null>((res) => canvas.toBlob(res, "image/png"));
      if (!blob) throw new Error("encode failed");
      upload.mutate(blob, {
        onSuccess: () => {
          onNote("AVATAR UPLOADED");
          sound.bell();
        },
        onError: (e) => {
          setError(e.message);
          sound.error();
        },
      });
    } catch {
      setError("COULD NOT READ THAT IMAGE");
      sound.error();
    }
  };

  const hasAvatar = !!u.avatarPreset || (u.avatarVersion ?? 0) > 0;

  return (
    <div style={{ padding: "14px 16px 16px", display: "flex", flexDirection: "column", gap: 18 }}>
      <div>
        <FieldLabel>CALLSIGN</FieldLabel>
        <div style={{ display: "flex", gap: 10 }}>
          <input
            value={name}
            maxLength={20}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && dirty && !update.isPending) saveName();
            }}
            style={{
              flex: 1,
              background: "#06040d",
              border: `2px solid ${dirty ? "#22e8ff" : "#35205c"}`,
              color: "#ece6ff",
              fontFamily: "var(--font-body)",
              fontSize: 26,
              padding: "6px 12px",
              outline: "none",
            }}
          />
          <button
            type="button"
            disabled={!dirty || update.isPending}
            onClick={saveName}
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 14,
              letterSpacing: 2,
              padding: "0 22px",
              cursor: dirty ? "pointer" : "not-allowed",
              border: "2px solid #22e8ff",
              background: dirty ? "#0b2a33" : "#150a2a",
              color: dirty ? "#22e8ff" : "#5c4f80",
            }}
          >
            {update.isPending ? "SAVING…" : "SAVE"}
          </button>
        </div>
        <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#5c4f80" }}>
          3–20 CHARS · LETTERS, DIGITS, SPACE, _ AND - · ONE CHANGE PER DAY
        </span>
      </div>

      <div>
        <FieldLabel>AVATAR</FieldLabel>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(52px, 1fr))",
            gap: 8,
            maxWidth: 460,
          }}
        >
          {AVATAR_PRESETS.map((p) => {
            const selected = u.avatarPreset === p.key;
            return (
              <button
                key={p.key}
                type="button"
                title={p.key}
                onClick={() => pickPreset(p.key)}
                style={{
                  width: 52,
                  height: 52,
                  display: "grid",
                  placeItems: "center",
                  cursor: "pointer",
                  background: "#06040d",
                  border: `2px solid ${selected ? "#22e8ff" : "#35205c"}`,
                  boxShadow: selected ? "0 0 14px rgba(34,232,255,.5)" : "none",
                }}
              >
                <img src={p.src} width={36} height={36} className="pixelated" alt={p.key} />
              </button>
            );
          })}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 12 }}>
          {u.isGuest ? (
            <a
              href="/auth/login?next=/"
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 13,
                letterSpacing: 2,
                color: "#ffb15c",
                border: "1px solid #6b4a1c",
                background: "#2a1406",
                padding: "9px 14px",
                textDecoration: "none",
              }}
            >
              ★ LOGIN TO UNLOCK UPLOADS
            </a>
          ) : (
            <>
              <input
                ref={fileRef}
                type="file"
                accept="image/png,image/jpeg,image/webp"
                style={{ display: "none" }}
                onChange={(e) => {
                  void onFile(e.target.files?.[0]);
                  e.target.value = "";
                }}
              />
              <button
                type="button"
                onClick={() => {
                  sound.click();
                  fileRef.current?.click();
                }}
                disabled={upload.isPending}
                style={uploadBtn(upload.isPending)}
              >
                {upload.isPending ? "UPLOADING…" : "⇧ UPLOAD IMAGE"}
              </button>
            </>
          )}
          {hasAvatar && !u.isGuest && (
            <button
              type="button"
              onClick={() => {
                sound.click();
                removeAvatar.mutate(undefined, {
                  onSuccess: () => {
                    onNote("AVATAR CLEARED");
                  },
                  onError: (e) => {
                    setError(e.message);
                    sound.error();
                  },
                });
              }}
              style={uploadBtn(false)}
            >
              ✕ REMOVE
            </button>
          )}
          <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#5c4f80" }}>
            UPLOADS GET PIXELATED TO 64×64
          </span>
        </div>
      </div>

      {error && (
        <div
          role="alert"
          style={{
            border: "2px solid #ff8a1f",
            background: "#06040d",
            color: "#ff8a1f",
            fontFamily: "var(--font-display)",
            fontSize: 14,
            letterSpacing: 2,
            padding: "10px 14px",
          }}
        >
          ! {error}
        </div>
      )}
    </div>
  );
}

function uploadBtn(pending: boolean): React.CSSProperties {
  return {
    fontFamily: "var(--font-display)",
    fontSize: 13,
    letterSpacing: 2,
    padding: "9px 14px",
    cursor: pending ? "wait" : "pointer",
    border: "1px solid #4a3a72",
    background: "#1d1036",
    color: "#cfc4f2",
  };
}

function FieldLabel({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        fontFamily: "var(--font-display)",
        fontSize: 13,
        letterSpacing: 3,
        color: "#8878b8",
        marginBottom: 6,
      }}
    >
      {children}
    </div>
  );
}

function SecurityTab({ me, onNote }: { me: Me; onNote: (s: string) => void }) {
  const u = me.user;
  const sessions = useSessions(true);
  const revoke = useRevokeSession();

  return (
    <div style={{ padding: "14px 16px 16px", display: "flex", flexDirection: "column", gap: 18 }}>
      <div style={{ display: "flex", gap: 24, flexWrap: "wrap" }}>
        <div style={{ minWidth: 260 }}>
          <FieldLabel>EMAIL</FieldLabel>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <span style={{ fontFamily: "var(--font-body)", fontSize: 24, color: "#ece6ff" }}>
              {u.email ?? "—"}
            </span>
            {u.email &&
              (u.emailVerified ? (
                <Badge color="#5fe08a" bg="#0b2a33">✓ VERIFIED</Badge>
              ) : (
                <Badge color="#ffb15c" bg="#2a1406">UNVERIFIED</Badge>
              ))}
          </div>
        </div>
        <div>
          <FieldLabel>PASSWORD &amp; 2-STEP</FieldLabel>
          <a
            href={KC_ACCOUNT_URL}
            target="_blank"
            rel="noreferrer"
            onClick={() => sound.click()}
            style={{
              display: "inline-block",
              fontFamily: "var(--font-display)",
              fontSize: 13,
              letterSpacing: 2,
              color: "#5fe08a",
              border: "2px solid #5fe08a",
              background: "#0b2a33",
              padding: "9px 14px",
              textDecoration: "none",
            }}
          >
            🔒 MANAGE AT KEYCLOAK ↗
          </a>
        </div>
      </div>

      {u.isGuest && (
        <div
          style={{
            border: "2px solid #ffb15c",
            background: "#1d1000",
            padding: "12px 16px",
            fontFamily: "var(--font-display)",
            fontSize: 13,
            letterSpacing: 2,
            color: "#ffb15c",
            lineHeight: 1.5,
          }}
        >
          YOU ARE PLAYING AS A GUEST — CREDITS AND STATS ARE ONLY SAVED IN THIS BROWSER.{" "}
          <a href="/auth/login?next=/" style={{ color: "#22e8ff" }}>
            LOGIN
          </a>{" "}
          TO KEEP THEM FOREVER.
        </div>
      )}

      <div>
        <FieldLabel>ACTIVE SESSIONS</FieldLabel>
        {sessions.isLoading ? (
          <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#5c4f80" }}>
            READING…
          </span>
        ) : (sessions.data?.length ?? 0) === 0 ? (
          <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#5c4f80" }}>
            NO APP SESSIONS — YOUR LOGIN LIVES IN THE BROWSER.
          </span>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {sessions.data?.map((s) => (
              <div
                key={s.id}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 12,
                  border: "1px solid #35205c",
                  background: "#06040d",
                  padding: "6px 10px",
                }}
              >
                <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#cfc4f2", flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {s.userAgent || "UNKNOWN DEVICE"}
                </span>
                <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#5c4f80" }}>
                  {s.ip ?? "—"}
                </span>
                <button
                  type="button"
                  onClick={() => {
                    sound.click();
                    revoke.mutate(s.id, {
                      onSuccess: () => onNote("SESSION REVOKED"),
                      onError: () => sound.error(),
                    });
                  }}
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 12,
                    letterSpacing: 2,
                    padding: "6px 10px",
                    cursor: "pointer",
                    border: "1px solid #f2643d",
                    background: "#2d0a1e",
                    color: "#f2643d",
                  }}
                >
                  REVOKE
                </button>
              </div>
            ))}
          </div>
        )}
        <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#5c4f80" }}>
          REVOKING YOUR CURRENT SESSION LOGS YOU OUT.
        </span>
      </div>
    </div>
  );
}

const EXCLUDE_OPTIONS: { days: number; label: string }[] = [
  { days: 1, label: "24 HOURS" },
  { days: 7, label: "7 DAYS" },
  { days: 30, label: "30 DAYS" },
  { days: 0, label: "PERMANENT" },
];

/** The LOBBY tab: personalization transparency + safer-play controls. */
function LobbyTab({ me, onNote }: { me: Me; onNote: (s: string) => void }) {
  const u = me.user;
  const session = useSession();
  const recs = usePersonalizedLobby(session.isSuccess);
  const updatePrefs = useUpdatePreferences();
  const exclude = useSelfExclude();
  const [confirmDays, setConfirmDays] = useState<number | null>(null);
  // The end date comes straight from the self-exclude response; /me doesn't
  // carry it for previously-set exclusions.
  const [excludedUntil, setExcludedUntil] = useState<string | null>(null);

  const personalizeOn = recs.data
    ? recs.data.personalized || recs.data.reason !== "toggle_off"
    : true;

  const togglePersonalize = () => {
    sound.click();
    updatePrefs.mutate(!personalizeOn, {
      onSuccess: (v) => {
        onNote(v ? "PERSONALIZED LOBBY ON" : "PERSONALIZED LOBBY OFF");
        sound.bell();
      },
      onError: () => sound.error(),
    });
  };

  const selfExcluded = u.status === "self_excluded";

  return (
    <div style={{ padding: "14px 16px 16px", display: "flex", flexDirection: "column", gap: 18 }}>
      <div>
        <FieldLabel>PERSONALIZED LOBBY</FieldLabel>
        <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
          <button
            type="button"
            onClick={togglePersonalize}
            disabled={updatePrefs.isPending}
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 15,
              letterSpacing: 2,
              padding: "9px 16px",
              cursor: "pointer",
              minWidth: 120,
              border: `2px solid ${personalizeOn ? "#ffd21f" : "#35205c"}`,
              background: personalizeOn ? "#2a2206" : "#150a2a",
              color: personalizeOn ? "#ffd21f" : "#8878b8",
              boxShadow: personalizeOn ? "0 0 14px rgba(255,210,31,.35)" : "none",
            }}
          >
            {updatePrefs.isPending ? "…" : personalizeOn ? "★ ON" : "OFF"}
          </button>
          <span style={{ fontFamily: "var(--font-body)", fontSize: 20, color: "#cfc4f2" }}>
            {personalizeOn
              ? "THE WHEEL REORDERS AROUND HOW YOU PLAY"
              : "EVERYONE SEES THE SAME STATIC LOBBY"}
          </span>
        </div>
        <span style={{ display: "block", marginTop: 8, fontFamily: "var(--font-body)", fontSize: 19, color: "#5c4f80", lineHeight: 1.45 }}>
          Uses only your play history on this casino — games you bet on and pages you open — to
          order the wheel, pick FOR YOU suggestions and mark what is busy. It never changes odds,
          payouts or offers. Turn it off anytime.
        </span>
      </div>

      <div>
        <FieldLabel>SAFER PLAY</FieldLabel>
        {selfExcluded ? (
          <div
            style={{
              border: "2px solid #5fe08a",
              background: "#0b2a33",
              padding: "12px 16px",
              fontFamily: "var(--font-display)",
              fontSize: 14,
              letterSpacing: 2,
              color: "#5fe08a",
              lineHeight: 1.6,
            }}
          >
            SELF-EXCLUSION ACTIVE{excludedUntil ? ` — UNTIL ${new Date(excludedUntil).toLocaleString()}` : ""}
            <span style={{ display: "block", fontFamily: "var(--font-body)", fontSize: 19, color: "#cfc4f2", letterSpacing: 0 }}>
              Betting is paused on your account. The lobby stays viewable, without promos or picks.
            </span>
          </div>
        ) : (
          <>
            <span style={{ display: "block", marginBottom: 10, fontFamily: "var(--font-body)", fontSize: 19, color: "#5c4f80", lineHeight: 1.45 }}>
              Need a breather? Pause all betting for a while. The floor stays viewable read-only;
              promos and personalized picks switch off too.
            </span>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              {EXCLUDE_OPTIONS.map((o) => {
                const selected = confirmDays === o.days;
                return (
                  <button
                    key={o.days}
                    type="button"
                    disabled={exclude.isPending}
                    onClick={() => {
                      sound.chipClink();
                      setConfirmDays(selected ? null : o.days);
                    }}
                    style={{
                      fontFamily: "var(--font-display)",
                      fontSize: 13,
                      letterSpacing: 2,
                      padding: "10px 14px",
                      cursor: "pointer",
                      border: `2px solid ${selected ? "#f2643d" : "#4a3a72"}`,
                      background: selected ? "#2d0a1e" : "#1d1036",
                      color: selected ? "#f2643d" : "#cfc4f2",
                    }}
                  >
                    {o.label}
                  </button>
                );
              })}
            </div>
            {confirmDays !== null && (
              <div
                style={{
                  marginTop: 12,
                  display: "flex",
                  alignItems: "center",
                  gap: 12,
                  border: "2px solid #f2643d",
                  background: "#2d0a1e",
                  padding: "10px 14px",
                  flexWrap: "wrap",
                }}
              >
                <span style={{ fontFamily: "var(--font-display)", fontSize: 13, letterSpacing: 2, color: "#f2643d" }}>
                  PAUSE BETTING {EXCLUDE_OPTIONS.find((o) => o.days === confirmDays)?.label}?
                </span>
                <button
                  type="button"
                  disabled={exclude.isPending}
                  onClick={() => {
                    sound.click();
                    exclude.mutate(confirmDays, {
                      onSuccess: (data) => {
                        setConfirmDays(null);
                        setExcludedUntil(data.statusUntil);
                        onNote("SAFER PLAY ACTIVE");
                      },
                      onError: () => sound.error(),
                    });
                  }}
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 13,
                    letterSpacing: 2,
                    padding: "8px 16px",
                    cursor: "pointer",
                    border: "2px solid #f2643d",
                    background: "#f2643d",
                    color: "#06040d",
                  }}
                >
                  {exclude.isPending ? "…" : "CONFIRM"}
                </button>
                <button
                  type="button"
                  onClick={() => {
                    sound.click();
                    setConfirmDays(null);
                  }}
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 13,
                    letterSpacing: 2,
                    padding: "8px 16px",
                    cursor: "pointer",
                    border: "1px solid #4a3a72",
                    background: "#1d1036",
                    color: "#cfc4f2",
                  }}
                >
                  CANCEL
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
