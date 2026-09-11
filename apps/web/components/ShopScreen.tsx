"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import {
  ProfileError,
  useDeposit,
  useEquipCosmetics,
  useSession,
  useShopInventory,
  useShopItems,
  useShopPurchase,
} from "@/lib/api";
import { sound } from "@/lib/sound";
import Backdrop from "@/components/Backdrop";
import NameTag from "@/components/NameTag";
import PixelCard from "@/components/PixelCard";
import { getEmote } from "@/lib/emotes";
import {
  BALLS,
  FRAMES,
  PACKS,
  THEMES,
  nameEffectClass,
} from "@/lib/cosmetics";
import type { ShopItem } from "@/lib/types";

// THE VAULT — the cosmetics shop. Spend credits on titles, name effects,
// card skins, frames, plinko balls, profile themes and emote packs. Every
// purchase is a server transaction; this screen only shows catalog state
// and equips what the ledger says is owned.

const ACCENT = "#ffd21f";
const GOOD = "#5fe08a";

type SlotKey =
  | "title"
  | "nameEffect"
  | "cardSkin"
  | "avatarFrame"
  | "plinkoBall"
  | "profileTheme";

const TABS = [
  { key: "title", label: "TITLES", accent: "#ffd21f" },
  { key: "name_effect", label: "NAME FX", accent: "#2de2ff" },
  { key: "card_skin", label: "CARD SKINS", accent: "#ff2d95" },
  { key: "avatar_frame", label: "FRAMES", accent: "#b026ff" },
  { key: "plinko_ball", label: "BALLS", accent: "#39ff14" },
  { key: "profile_theme", label: "THEMES", accent: "#ff8833" },
  { key: "emote_pack", label: "PACKS", accent: "#5fe08a" },
  { key: "inventory", label: "INVENTORY", accent: "#e8e8ff" },
] as const;

type TabKey = (typeof TABS)[number]["key"];

const SLOT_BY_KIND: Record<string, SlotKey> = {
  title: "title",
  name_effect: "nameEffect",
  card_skin: "cardSkin",
  avatar_frame: "avatarFrame",
  plinko_ball: "plinkoBall",
  profile_theme: "profileTheme",
};

const RARITY: Record<string, { border: string; text: string; glow: string }> = {
  common: { border: "border-zinc-600", text: "text-zinc-400", glow: "" },
  rare: { border: "border-cyan-500", text: "text-cyan-300", glow: "" },
  epic: { border: "border-fuchsia-500", text: "text-fuchsia-300", glow: "" },
  legendary: { border: "border-yellow-400", text: "text-yellow-200", glow: "fx-legendary" },
};

function credits(n: number): string {
  return n.toLocaleString("en-US");
}

export default function ShopScreen() {
  const session = useSession();
  const catalog = useShopItems();
  const inventory = useShopInventory(true);
  const purchase = useShopPurchase();
  const equip = useEquipCosmetics();
  const deposit = useDeposit();

  const [tab, setTab] = useState<TabKey>("title");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [balanceFlash, setBalanceFlash] = useState(false);

  const me = session.data;
  const balance = me?.balanceCredits ?? 0;
  const items = useMemo(() => catalog.data?.items ?? [], [catalog.data]);
  const owned = useMemo(
    () => new Set((inventory.data?.items ?? []).map((i) => i.itemId)),
    [inventory.data],
  );

  const equipped = (slot: SlotKey): string =>
    (me?.user as unknown as Record<string, string> | undefined)?.[slot] ?? "";

  const shown = useMemo(() => {
    if (tab === "inventory") return items.filter((i) => owned.has(i.id));
    return items.filter((i) => i.kind === tab);
  }, [items, owned, tab]);

  const selected: ShopItem | undefined =
    shown.find((i) => i.id === selectedId) ?? shown[0];

  const flashBalance = () => {
    setBalanceFlash(false);
    requestAnimationFrame(() => setBalanceFlash(true));
    setTimeout(() => setBalanceFlash(false), 800);
  };

  const buy = (item: ShopItem) => {
    if (!me) return;
    if (balance < item.priceCredits) {
      sound.error();
      setNotice({ kind: "err", text: "NOT ENOUGH CREDITS — VISIT THE CAGE" });
      return;
    }
    sound.unlock();
    sound.chipToss();
    purchase.mutate(item.id, {
      onSuccess: () => {
        sound.chipClink();
        sound.bell();
        flashBalance();
        setNotice({ kind: "ok", text: `${item.name} SECURED — IT'S YOURS` });
      },
      onError: (err) => {
        sound.error();
        const code = err instanceof ProfileError ? err.code : "error";
        setNotice({
          kind: "err",
          text: code === "insufficient_funds" ? "NOT ENOUGH CREDITS" : "THE VAULT JAMMED — TRY AGAIN",
        });
      },
    });
  };

  const toggleEquip = (item: ShopItem) => {
    if (!me) return;
    const slot = SLOT_BY_KIND[item.kind];
    if (!slot) return;
    const isOn = equipped(slot) === item.id;
    sound.unlock();
    equip.mutate(
      { [slot]: isOn ? "" : item.id },
      {
        onSuccess: () => {
          if (isOn) {
            sound.click();
            setNotice({ kind: "ok", text: `${item.name} STOWED` });
          } else {
            sound.bell();
            setNotice({ kind: "ok", text: `${item.name} EQUIPPED` });
          }
        },
        onError: () => {
          sound.error();
          setNotice({ kind: "err", text: "THE VAULT JAMMED — TRY AGAIN" });
        },
      },
    );
  };

  const doDeposit = () => {
    sound.unlock();
    sound.click();
    deposit.mutate(undefined, {
      onSuccess: () => {
        sound.bell();
        flashBalance();
        setNotice({ kind: "ok", text: "+1,000 CREDITS FROM THE CAGE" });
      },
      onError: () => {
        sound.error();
        setNotice({ kind: "err", text: "THE CAGE IS CLOSED — TRY AGAIN SOON" });
      },
    });
  };

  const itemState = (item: ShopItem): "owned" | "equipped" | "buy" => {
    if (!owned.has(item.id)) return "buy";
    const slot = SLOT_BY_KIND[item.kind];
    if (!slot) return "owned";
    return equipped(slot) === item.id ? "equipped" : "owned";
  };

  return (
    <div className="relative min-h-dvh overflow-hidden bg-[#0a0614] text-zinc-100">
      <Backdrop />
      <div className="relative mx-auto flex min-h-dvh w-full max-w-6xl flex-col gap-4 px-4 py-5 md:px-8">
        {/* Header: back, sign, balance */}
        <header className="flex flex-wrap items-center justify-between gap-3">
          <Link
            href="/"
            onClick={() => sound.click()}
            className="border-2 border-zinc-600 bg-black/60 px-3 py-1.5 font-display text-sm tracking-widest text-zinc-300 transition-colors hover:border-fuchsia-400 hover:text-fuchsia-300"
          >
            &larr; BACK
          </Link>
          <h1
            className="font-display text-3xl tracking-[0.3em] md:text-4xl"
            style={{ color: ACCENT, textShadow: "0 0 18px rgba(255,210,31,0.55)" }}
          >
            THE VAULT
          </h1>
          <div className="flex items-center gap-2">
            <div className="border-2 border-lime-500/70 bg-black/70 px-3 py-1.5 font-display text-sm text-lime-300">
              <span className="mr-2 text-[10px] text-lime-600">CREDITS</span>
              <span data-testid="shop-credits" className={balanceFlash ? "fx-balance-flash inline-block" : ""}>
                {credits(balance)}
              </span>
            </div>
            <button
              onClick={doDeposit}
              disabled={deposit.isPending}
              className="border-2 border-lime-500/70 bg-black/60 px-3 py-1.5 font-display text-xs tracking-widest text-lime-300 transition-colors hover:bg-lime-500/20 disabled:opacity-50"
            >
              + TOP UP
            </button>
          </div>
        </header>

        {/* Tab rail */}
        <nav className="flex flex-wrap gap-2">
          {TABS.map((t) => {
            const on = t.key === tab;
            return (
              <button
                key={t.key}
                onClick={() => {
                  sound.click();
                  setTab(t.key);
                  setSelectedId(null);
                }}
                className={`border-2 px-3 py-1.5 font-display text-xs tracking-widest transition-all ${
                  on ? "bg-white/10 text-white" : "border-zinc-700 bg-black/50 text-zinc-400 hover:text-zinc-100"
                }`}
                style={on ? { borderColor: t.accent, color: t.accent, boxShadow: `0 0 12px ${t.accent}55` } : undefined}
              >
                {t.label}
              </button>
            );
          })}
        </nav>

        {notice && (
          <div
            className={`border-2 px-3 py-2 font-display text-xs tracking-widest ${
              notice.kind === "ok"
                ? "border-lime-500 text-lime-300"
                : "border-red-500 text-red-300"
            }`}
          >
            {notice.text}
          </div>
        )}

        <div className="grid flex-1 grid-cols-1 gap-4 pb-24 lg:grid-cols-[1fr_320px]">
          {/* Item grid */}
          <section className="grid grid-cols-1 content-start gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {catalog.isLoading || session.isLoading ? (
              <div className="col-span-full py-16 text-center font-display text-sm tracking-widest text-zinc-500">
                OPENING THE VAULT…
              </div>
            ) : shown.length === 0 ? (
              <div className="col-span-full py-16 text-center font-display text-sm tracking-widest text-zinc-500">
                {tab === "inventory" ? "EMPTY — GO SPEND SOMETHING" : "NOTHING HERE YET"}
              </div>
            ) : (
              shown.map((item) => {
                const rarity = RARITY[item.rarity] ?? RARITY.common;
                const state = itemState(item);
                const isSel = selected?.id === item.id;
                return (
                  <button
                    key={item.id}
                    onClick={() => {
                      sound.click();
                      setSelectedId(item.id);
                    }}
                    className={`relative border-2 bg-black/60 p-3 text-left transition-transform hover:-translate-y-0.5 ${rarity.border} ${rarity.glow} ${
                      isSel ? "ring-2 ring-white/70" : ""
                    }`}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <span className={`font-display text-sm leading-tight ${rarity.text}`}>{item.name}</span>
                      <span className={`shrink-0 font-display text-[8px] uppercase tracking-widest ${rarity.text}`}>
                        {item.rarity}
                      </span>
                    </div>
                    <p className="mt-1 line-clamp-2 min-h-8 text-xs text-zinc-400">{item.blurb}</p>
                    <div className="mt-2 flex items-center justify-between">
                      {state === "buy" ? (
                        <span className="font-display text-xs text-amber-300">{credits(item.priceCredits)} CR</span>
                      ) : (
                        <span className="font-display text-xs text-zinc-500">OWNED</span>
                      )}
                      {state === "buy" ? (
                        <span
                          role="button"
                          tabIndex={0}
                          onClick={(e) => {
                            e.stopPropagation();
                            buy(item);
                          }}
                          className="border-2 border-lime-500 px-2 py-1 font-display text-[10px] tracking-widest text-lime-300 hover:bg-lime-500/20"
                        >
                          BUY
                        </span>
                      ) : state === "owned" ? (
                        <span
                          role="button"
                          tabIndex={0}
                          onClick={(e) => {
                            e.stopPropagation();
                            toggleEquip(item);
                          }}
                          className="border-2 border-cyan-400 px-2 py-1 font-display text-[10px] tracking-widest text-cyan-300 hover:bg-cyan-500/20"
                        >
                          EQUIP
                        </span>
                      ) : (
                        <span
                          role="button"
                          tabIndex={0}
                          onClick={(e) => {
                            e.stopPropagation();
                            toggleEquip(item);
                          }}
                          className="border-2 px-2 py-1 font-display text-[10px] tracking-widest"
                          style={{ borderColor: GOOD, color: GOOD, boxShadow: `0 0 8px ${GOOD}66` }}
                        >
                          EQUIPPED
                        </span>
                      )}
                    </div>
                  </button>
                );
              })
            )}
          </section>

          {/* Preview stage */}
          <aside className="h-fit border-2 border-zinc-700 bg-black/60 p-4 lg:sticky lg:top-5">
            <div className="mb-3 font-display text-[10px] tracking-[0.3em] text-zinc-500">PREVIEW</div>
            <Preview item={selected} />
            {selected && (
              <div className="mt-4 border-t border-zinc-800 pt-3">
                <div className="font-display text-sm" style={{ color: ACCENT }}>
                  {selected.name}
                </div>
                <p className="mt-1 text-xs text-zinc-400">{selected.blurb}</p>
                <div className="mt-3">
                  {itemState(selected) === "buy" ? (
                    <button
                      onClick={() => buy(selected)}
                      disabled={purchase.isPending || !me}
                      className="w-full border-2 border-lime-500 py-2 font-display text-xs tracking-[0.25em] text-lime-300 transition-colors hover:bg-lime-500/20 disabled:opacity-50"
                    >
                      {purchase.isPending ? "…" : `BUY — ${credits(selected.priceCredits)} CR`}
                    </button>
                  ) : itemState(selected) === "owned" ? (
                    <button
                      onClick={() => toggleEquip(selected)}
                      disabled={equip.isPending}
                      className="w-full border-2 border-cyan-400 py-2 font-display text-xs tracking-[0.25em] text-cyan-300 transition-colors hover:bg-cyan-500/20 disabled:opacity-50"
                    >
                      EQUIP
                    </button>
                  ) : (
                    <button
                      onClick={() => toggleEquip(selected)}
                      disabled={equip.isPending}
                      className="w-full border-2 py-2 font-display text-xs tracking-[0.25em] disabled:opacity-50"
                      style={{ borderColor: GOOD, color: GOOD }}
                    >
                      EQUIPPED — CLICK TO STOW
                    </button>
                  )}
                </div>
              </div>
            )}
          </aside>
        </div>
      </div>
    </div>
  );
}

/** Live preview of the selected (or hovered) item, wearing the player's name. */
function Preview({ item }: { item: ShopItem | undefined }) {
  if (!item) {
    return (
      <div className="flex h-44 items-center justify-center font-display text-xs tracking-widest text-zinc-600">
        PICK AN ITEM
      </div>
    );
  }
  switch (item.kind) {
    case "title":
      return (
        <div className="flex h-44 items-center justify-center">
          <NameTag displayName="YOU" title={item.id} className="text-2xl" titleClassName="text-xs px-2 py-0.5" />
        </div>
      );
    case "name_effect":
      return (
        <div className="flex h-44 flex-col items-center justify-center gap-3">
          <span className={`text-3xl ${nameEffectClass(item.id)}`} data-name="YOU">
            YOU
          </span>
          <span className="text-[10px] text-zinc-500">…on every name you own</span>
        </div>
      );
    case "card_skin":
      return (
        <div className="flex h-44 items-center justify-center gap-4">
          <PixelCard code="As" scale={4} skin={item.id} />
          <PixelCard code="back" scale={4} skin={item.id} />
        </div>
      );
    case "avatar_frame": {
      const f = FRAMES[item.id];
      return (
        <div className="flex h-44 items-center justify-center">
          <div className={`${f?.className ?? ""} overflow-hidden rounded-full`}>
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src="/sprites/star.png" alt="avatar preview" className="h-20 w-20 [image-rendering:pixelated]" />
          </div>
        </div>
      );
    }
    case "plinko_ball": {
      const b = BALLS[item.id];
      if (!b) return null;
      return (
        <div className="flex h-44 items-center justify-center">
          <div
            className="h-16 w-16 rounded-full"
            style={{
              background: `radial-gradient(circle at 32% 30%, ${b.shine} 0%, ${b.body} 55%, #000c 130%)`,
              boxShadow: `0 0 24px 6px ${b.glow}`,
            }}
          />
        </div>
      );
    }
    case "profile_theme": {
      const t = THEMES[item.id];
      return (
        <div className={`flex h-44 items-center justify-center border-2 border-zinc-700 ${t?.className ?? ""}`}>
          <span className="font-display text-xs tracking-[0.3em] text-white/70">YOUR PROFILE</span>
        </div>
      );
    }
    case "emote_pack": {
      const ids = PACKS[item.id]?.emotes ?? [];
      return (
        <div className="flex h-44 flex-wrap content-center justify-center gap-2">
          {ids.map((id) => {
            const emote = getEmote(id);
            return (
              <span
                key={id}
                className="flex h-11 w-11 items-center justify-center border-2 border-zinc-700 bg-black/60 text-xl"
                title={emote?.label ?? id}
              >
                {emote?.emoji ?? "?"}
              </span>
            );
          })}
        </div>
      );
    }
    default:
      return null;
  }
}
