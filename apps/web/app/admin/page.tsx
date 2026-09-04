"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import Backdrop from "@/components/Backdrop";
import { Avatar } from "@/components/Avatar";
import { sound } from "@/lib/sound";
import { useAdminAdjust, useAdminBan, useAdminUsers, useSession } from "@/lib/api";
import type { AdminUserRow } from "@/lib/types";

/** Staff panel: user search, bans, and credit adjustments (moderator+). */
export default function AdminPage() {
  const session = useSession();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [note, setNote] = useState<string | null>(null);

  useEffect(() => {
    const t = setTimeout(() => setDebounced(query.trim()), 250);
    return () => clearTimeout(t);
  }, [query]);

  useEffect(() => {
    if (!note) return;
    const t = setTimeout(() => setNote(null), 2600);
    return () => clearTimeout(t);
  }, [note]);

  const role = session.data?.user.role;
  const isStaff = role === "admin" || role === "moderator";

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
          padding: "22px 30px",
          gap: 18,
        }}
      >
        <header style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <Link
            href="/"
            onClick={() => sound.click()}
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 11,
              letterSpacing: 2,
              color: "#22e8ff",
              border: "1px solid #22e8ff",
              background: "#0b2a33",
              padding: "8px 12px",
              textDecoration: "none",
              textShadow: "0 0 10px rgba(34,232,255,.8)",
            }}
          >
            ◀ FLOOR
          </Link>
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 18,
              letterSpacing: 5,
              color: "#ff2d95",
              textShadow: "0 0 12px rgba(255,45,149,.9)",
            }}
          >
            STAFF CONTROL
          </span>
        </header>

        {!session.isSuccess ? (
          <Hint>READING CLEARANCE…</Hint>
        ) : !isStaff ? (
          <Hint>CLEARANCE DENIED — STAFF ROLES ONLY.</Hint>
        ) : (
          <UserRoster
            query={debounced}
            onQuery={setQuery}
            isAdmin={role === "admin"}
            onNote={(msg) => {
              setNote(msg);
              sound.bell();
            }}
            onError={(msg) => {
              setNote(msg);
              sound.error();
            }}
          />
        )}

        {note && (
          <div
            role="status"
            style={{
              position: "fixed",
              bottom: 24,
              left: "50%",
              transform: "translateX(-50%)",
              border: "2px solid #22e8ff",
              background: "rgba(6,4,13,.95)",
              color: "#22e8ff",
              fontFamily: "var(--font-display)",
              fontSize: 12,
              letterSpacing: 2,
              padding: "10px 18px",
              animation: "notePop .2s ease-out both",
              zIndex: 50,
            }}
          >
            {note}
          </div>
        )}
      </div>
    </div>
  );
}

function Hint({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        margin: "auto",
        fontFamily: "var(--font-display)",
        fontSize: 13,
        letterSpacing: 3,
        color: "#8878b8",
      }}
    >
      {children}
    </div>
  );
}

function UserRoster({
  query,
  onQuery,
  isAdmin,
  onNote,
  onError,
}: {
  query: string;
  onQuery: (s: string) => void;
  isAdmin: boolean;
  onNote: (s: string) => void;
  onError: (s: string) => void;
}) {
  const roster = useAdminUsers(query, true);
  const ban = useAdminBan();
  const adjust = useAdminAdjust();
  const [adjusting, setAdjusting] = useState<number | null>(null);
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState("");

  const doAdjust = (userId: number) => {
    const credits = Number(amount);
    if (!Number.isInteger(credits) || credits === 0 || !reason.trim()) {
      onError("AMOUNT (NON-ZERO) AND REASON REQUIRED");
      return;
    }
    adjust.mutate(
      { userId, amountCredits: credits, reason: reason.trim() },
      {
        onSuccess: () => {
          onNote("BALANCE ADJUSTED");
          setAdjusting(null);
          setAmount("");
          setReason("");
        },
        onError: (e) => onError(e.message.toUpperCase()),
      },
    );
  };

  return (
    <>
      <input
        value={query}
        onChange={(e) => onQuery(e.target.value)}
        placeholder="SEARCH NAME OR EMAIL…"
        style={{
          background: "#06040d",
          border: "2px solid #35205c",
          color: "#ece6ff",
          fontFamily: "var(--font-body)",
          fontSize: 22,
          padding: "8px 12px",
          outline: "none",
          width: 420,
        }}
      />
      <div style={{ flex: 1, overflowY: "auto", border: "2px solid #35205c", background: "rgba(15,7,32,.9)" }}>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              {["PLAYER", "KIND", "ROLE", "STATUS", "JOINED", "ACTIONS"].map((h) => (
                <th
                  key={h}
                  style={{
                    position: "sticky",
                    top: 0,
                    background: "#150a2a",
                    borderBottom: "2px solid #241640",
                    fontFamily: "var(--font-display)",
                    fontSize: 10,
                    letterSpacing: 2,
                    color: "#8878b8",
                    textAlign: "left",
                    padding: "10px 12px",
                  }}
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {roster.data?.users.map((u) => (
              <UserRow
                key={u.id}
                u={u}
                isAdmin={isAdmin}
                busy={ban.isPending || adjust.isPending}
                adjusting={adjusting === u.id}
                onAdjustToggle={() => {
                  sound.click();
                  setAdjusting(adjusting === u.id ? null : u.id);
                }}
                amount={amount}
                onAmount={setAmount}
                reason={reason}
                onReason={setReason}
                onAdjustConfirm={() => doAdjust(u.id)}
                onBan={() => {
                  sound.click();
                  ban.mutate(
                    { userId: u.id, banned: u.status !== "banned" },
                    { onError: (e) => onError(e.message.toUpperCase()) },
                  );
                }}
              />
            ))}
          </tbody>
        </table>
        {roster.data && roster.data.users.length === 0 && (
          <div style={{ padding: 18, fontFamily: "var(--font-body)", fontSize: 20, color: "#5c4f80" }}>
            NO PLAYERS MATCH.
          </div>
        )}
      </div>
    </>
  );
}

function UserRow({
  u,
  isAdmin,
  busy,
  adjusting,
  onAdjustToggle,
  amount,
  onAmount,
  reason,
  onReason,
  onAdjustConfirm,
  onBan,
}: {
  u: AdminUserRow;
  isAdmin: boolean;
  busy: boolean;
  adjusting: boolean;
  onAdjustToggle: () => void;
  amount: string;
  onAmount: (s: string) => void;
  reason: string;
  onReason: (s: string) => void;
  onAdjustConfirm: () => void;
  onBan: () => void;
}) {
  const statusColor =
    u.status === "active" ? "#5fe08a" : u.status === "banned" ? "#f2643d" : "#ffb15c";
  return (
    <>
      <tr style={{ borderBottom: "1px solid #241640" }}>
        <td style={{ padding: "8px 12px" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Avatar userId={u.id} displayName={u.displayName} size={28} ring={statusColor} />
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 11,
                letterSpacing: 1,
                color: "#ece6ff",
              }}
            >
              {u.displayName}
            </span>
          </div>
        </td>
        <td style={{ padding: "8px 12px", fontFamily: "var(--font-body)", fontSize: 19, color: u.isGuest ? "#ffb15c" : "#5fe08a" }}>
          {u.isGuest ? "GUEST" : u.email ?? "REGISTERED"}
        </td>
        <td style={{ padding: "8px 12px", fontFamily: "var(--font-display)", fontSize: 10, color: "#cfc4f2" }}>
          {u.role.toUpperCase()}
        </td>
        <td style={{ padding: "8px 12px", fontFamily: "var(--font-display)", fontSize: 10, color: statusColor }}>
          {u.status.toUpperCase()}
        </td>
        <td style={{ padding: "8px 12px", fontFamily: "var(--font-body)", fontSize: 18, color: "#5c4f80" }}>
          {new Date(u.createdAt).toLocaleDateString()}
        </td>
        <td style={{ padding: "8px 12px" }}>
          <div style={{ display: "flex", gap: 8 }}>
            <button
              type="button"
              onClick={onBan}
              disabled={busy}
              style={{
                fontFamily: "var(--font-display)",
                fontSize: 9,
                letterSpacing: 2,
                padding: "5px 9px",
                cursor: "pointer",
                border: `1px solid ${u.status === "banned" ? "#5fe08a" : "#f2643d"}`,
                background: u.status === "banned" ? "#0b2a33" : "#2d0a1e",
                color: u.status === "banned" ? "#5fe08a" : "#f2643d",
              }}
            >
              {u.status === "banned" ? "UNBAN" : "BAN"}
            </button>
            {isAdmin && (
              <button
                type="button"
                onClick={onAdjustToggle}
                disabled={busy}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 9,
                  letterSpacing: 2,
                  padding: "5px 9px",
                  cursor: "pointer",
                  border: "1px solid #ff8a1f",
                  background: "#2a1406",
                  color: "#ff8a1f",
                }}
              >
                ADJUST
              </button>
            )}
          </div>
        </td>
      </tr>
      {adjusting && (
        <tr style={{ background: "#150a2a" }}>
          <td colSpan={6} style={{ padding: "10px 12px" }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <span style={{ fontFamily: "var(--font-display)", fontSize: 9, letterSpacing: 2, color: "#8878b8" }}>
                CREDITS
              </span>
              <input
                value={amount}
                onChange={(e) => onAmount(e.target.value.replace(/[^-\d]/g, ""))}
                placeholder="+500 / -250"
                style={{
                  width: 130,
                  background: "#06040d",
                  border: "2px solid #35205c",
                  color: "#ff8a1f",
                  fontFamily: "var(--font-body)",
                  fontSize: 20,
                  padding: "4px 10px",
                  outline: "none",
                }}
              />
              <input
                value={reason}
                onChange={(e) => onReason(e.target.value)}
                placeholder="REASON (LOGGED TO AUDIT)"
                style={{
                  flex: 1,
                  background: "#06040d",
                  border: "2px solid #35205c",
                  color: "#ece6ff",
                  fontFamily: "var(--font-body)",
                  fontSize: 20,
                  padding: "4px 10px",
                  outline: "none",
                }}
              />
              <button
                type="button"
                onClick={onAdjustConfirm}
                style={{
                  fontFamily: "var(--font-display)",
                  fontSize: 9,
                  letterSpacing: 2,
                  padding: "6px 12px",
                  cursor: "pointer",
                  border: "2px solid #ff8a1f",
                  background: "#2a1406",
                  color: "#ff8a1f",
                }}
              >
                APPLY
              </button>
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
