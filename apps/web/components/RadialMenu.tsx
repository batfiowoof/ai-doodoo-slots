"use client";

// The lobby wheel. Nodes fan out of a central hub onto an outer ring; account
// satellites ride a smaller inner orbit. Geometry and motion live here — the
// lobby screen passes in node/satellite content and the hub face.
//
// The hub click is a prize-wheel shuffle: the whole game ring orbits the hub
// by a few random turns (radSpinTo on the ring container) while every node
// anchor counter-rotates by the same amount (radCounterSpinTo), so the tiles
// stay upright and land on a fresh random slot each spin.

import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
} from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { createPortal } from "react-dom";
import { sound } from "@/lib/sound";

export interface RadialNode {
  key: string;
  label: string;
  accent: string;
  /** Node face (icon strip, card fan, sprite…). */
  art?: ReactNode;
  /** Bottom line of small print. */
  status?: string;
  /** Corner tag, e.g. a live multiplier or room state. */
  badge?: string;
  /** Pulsing border + badge treatment for live rooms. */
  live?: boolean;
  /** Navigation target. Without one, the node is a button satellite. */
  href?: string;
  /** Auth routes navigate with a plain anchor (no client-side launch). */
  hard?: boolean;
  /** Button-satellite action (account modal, deposit kiosk). */
  onActivate?: () => void;
  disabled?: boolean;
  /** Launch sound; defaults to sound.click(). */
  onLaunchSound?: () => void;
}

// Design-size stage; the lobby scales it to fit the viewport.
const STAGE_W = 1240;
const STAGE_H = 880;
const CX = STAGE_W / 2;
const CY = STAGE_H / 2;
const HUB_D = 280;
const SAT_R = 225;
const NODE_W = 200;
const NODE_H = 136;
// Satellites are casino chips: a round face with the label stacked below.
const SAT_D = 94;
const SAT_W = 132;
const SAT_H = 144;

// Prize-wheel orbit timing; the ring and its counter-rotating anchors must
// share duration + easing so tiles land upright.
const ORBIT_MS = 1700;
const ORBIT_EASE = "cubic-bezier(.32,.08,.26,1)";
const SPIN_ANIM = `radSpinTo ${ORBIT_MS}ms ${ORBIT_EASE} 1 forwards`;
const COUNTER_ANIM = `radCounterSpinTo ${ORBIT_MS}ms ${ORBIT_EASE} 1 forwards`;

function gameRadius(n: number): number {
  if (n <= 3) return 296;
  if (n <= 5) return 316;
  if (n <= 6) return 332;
  if (n <= 8) return 356;
  return 372;
}

function slot(i: number, n: number, r: number): { x: number; y: number } {
  const deg = -90 + (i * 360) / n;
  const rad = (deg * Math.PI) / 180;
  return { x: CX + r * Math.cos(rad), y: CY + r * Math.sin(rad) };
}

interface PlacedNode {
  node: RadialNode;
  x: number;
  y: number;
  delay: number;
  satellite: boolean;
}

export default function RadialMenu({
  nodes,
  satellites,
  hub,
}: {
  nodes: RadialNode[];
  satellites: RadialNode[];
  hub: ReactNode;
}) {
  const router = useRouter();
  const [rot, setRot] = useState(0);
  const [spin, setSpin] = useState<{ delta: number } | null>(null);
  const [launchKey, setLaunchKey] = useState<string | null>(null);
  const timers = useRef<number[]>([]);
  const spinning = spin !== null;

  useEffect(
    () => () => {
      timers.current.forEach((t) => window.clearTimeout(t));
    },
    [],
  );

  const r = gameRadius(nodes.length);
  // Slot coordinates are stage-absolute here; the orbit containers below are
  // anchored at the hub center, so children get center-relative offsets.
  const placedGames: PlacedNode[] = nodes.map((node, i) => ({
    node,
    ...slot(i, nodes.length, r),
    delay: i * 70,
    satellite: false,
  }));
  // Satellites interleave on the inner orbit, offset half a game step. They
  // stay put while the game ring shuffles — the wheel spins, the table doesn't.
  const placedSats: PlacedNode[] = satellites.map((node, i) => {
    const deg = -90 + ((i + 0.5) * 360) / Math.max(satellites.length, 1);
    const rad = (deg * Math.PI) / 180;
    return {
      node,
      x: CX + SAT_R * Math.cos(rad),
      y: CY + SAT_R * Math.sin(rad),
      delay: nodes.length * 70 + 60 + i * 60,
      satellite: true,
    };
  });

  const spinWheel = () => {
    if (spinning || launchKey) return;
    sound.unlock();
    sound.click();
    const n = Math.max(nodes.length, 1);
    const step = 360 / n;
    // A couple of full turns plus a random whole slot: the cards reshuffle
    // every spin, and always land evenly on the ring.
    const delta =
      (2 + Math.floor(Math.random() * 2)) * 360 + (1 + Math.floor(Math.random() * (n - 1))) * step;
    setSpin({ delta });
    if (!window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      for (let t = 1; t <= 14; t++) {
        timers.current.push(window.setTimeout(() => sound.winTick(t), t * 110));
      }
    }
    timers.current.push(
      window.setTimeout(() => {
        setRot((prev) => prev + delta);
        setSpin(null);
        sound.bell();
      }, ORBIT_MS + 50),
    );
  };

  const launch = (node: RadialNode) => {
    if (!node.href || launchKey) return;
    if (node.onLaunchSound) node.onLaunchSound();
    else sound.click();
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reduce) {
      router.push(node.href);
      return;
    }
    setLaunchKey(node.key);
    timers.current.push(window.setTimeout(() => router.push(node.href!), 430));
  };

  const activate = (node: RadialNode, e?: MouseEvent) => {
    if (node.disabled) return;
    sound.unlock();
    if (!node.href) {
      if (node.onLaunchSound) node.onLaunchSound();
      else sound.click();
      node.onActivate?.();
      return;
    }
    if (node.hard) {
      if (node.onLaunchSound) node.onLaunchSound();
      else sound.click();
      return; // plain anchor: the browser takes it from here
    }
    e?.preventDefault();
    launch(node);
  };

  const orbitVars = {
    ["--spin-from" as string]: `${rot}deg`,
    ["--spin-to" as string]: `${rot + (spin?.delta ?? 0)}deg`,
  };

  return (
    <div style={{ position: "relative", width: STAGE_W, height: STAGE_H, flex: "none" }}>
      {/* Spoke lines hub → node, under everything. They rotate with the ring
          so they always point where the cards are. */}
      <div
        style={{
          position: "absolute",
          inset: 0,
          animation: "radRingIn .9s ease-out both",
          pointerEvents: "none",
          zIndex: 0,
        }}
      >
        <svg
          width={STAGE_W}
          height={STAGE_H}
          style={{
            position: "absolute",
            inset: 0,
            transform: `rotate(${rot}deg)`,
            animation: spinning ? SPIN_ANIM : undefined,
            ...orbitVars,
          }}
          aria-hidden
        >
          {[...placedGames, ...placedSats].map(({ x, y, satellite }) => (
            <line
              key={`spoke-${x}-${y}`}
              x1={CX}
              y1={CY}
              x2={x}
              y2={y}
              stroke={satellite ? "#241640" : "#2a1848"}
              strokeWidth={satellite ? 1 : 2}
              strokeDasharray={satellite ? "3 9" : undefined}
            />
          ))}
        </svg>
      </div>

      {/* Decorative rings: dashed cyan drift, crisp conic ticks, dashed pink
          counter-drift. They draw in, then rotate forever. */}
      <div
        style={{
          position: "absolute",
          left: CX,
          top: CY,
          width: 0,
          height: 0,
          zIndex: 1,
          animation: "radRingIn .8s cubic-bezier(.2,1.2,.4,1) both",
          pointerEvents: "none",
        }}
      >
        <Ring r={286} style={{ border: "2px dashed #22e8ff55", animation: "radSpin 80s linear infinite" }} />
        <Ring
          r={r}
          style={{
            background:
              "repeating-conic-gradient(from 0deg, rgba(34,232,255,0) 0deg 11deg, rgba(34,232,255,.6) 11deg 12.2deg)",
            maskImage: "radial-gradient(closest-side, transparent 85.5%, #000 86.5%, #000 96.5%, transparent 97.5%)",
            WebkitMaskImage:
              "radial-gradient(closest-side, transparent 85.5%, #000 86.5%, #000 96.5%, transparent 97.5%)",
            animation: "radSpin 16s linear infinite",
          }}
        />
        <Ring r={404} style={{ border: "2px dashed #ff2d9544", animation: "radSpin 110s linear infinite reverse" }} />
      </div>

      {/* Hub whirl wrapper: one-shot 720° easter egg; 720 ≡ 0 so it lands clean. */}
      <div
        style={{
          position: "absolute",
          left: CX,
          top: CY,
          width: 0,
          height: 0,
          zIndex: 5,
          animation: spinning ? "radSpin720 1.4s cubic-bezier(.3,.08,.25,1) 1" : undefined,
        }}
      >
        <button
          type="button"
          onClick={spinWheel}
          title="SPIN THE HOUSE"
          style={{
            position: "absolute",
            left: -HUB_D / 2,
            top: -HUB_D / 2,
            width: HUB_D,
            height: HUB_D,
            borderRadius: "50%",
            border: "none",
            background: "linear-gradient(#1d1036,#0d0619 70%)",
            cursor: "pointer",
            padding: 0,
            animation: "bigPop .35s cubic-bezier(.2,1.4,.4,1) both, radHubGlow 4.2s ease-in-out .35s infinite",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            gap: 6,
          }}
        >
          {hub}
        </button>
      </div>

      {/* The game ring: one rotating container, nodes counter-rotated inside
          so the cards orbit the hub but never tilt. */}
      <div
        style={{
          position: "absolute",
          left: CX,
          top: CY,
          width: 0,
          height: 0,
          zIndex: 2,
          transform: `rotate(${rot}deg)`,
          animation: spinning ? SPIN_ANIM : undefined,
          ...orbitVars,
        }}
      >
        {placedGames.map(({ node, x, y, delay }) => (
          <WheelNode
            key={node.key}
            node={node}
            x={x - CX}
            y={y - CY}
            delay={delay}
            orbit
            counterRot={rot}
            spinning={spinning}
            launchKey={launchKey}
            onActivate={activate}
          />
        ))}
      </div>

      {/* Action chips: not part of the shuffle. */}
      <div style={{ position: "absolute", left: CX, top: CY, width: 0, height: 0, zIndex: 2 }}>
        {placedSats.map(({ node, x, y, delay }) => (
          <WheelNode
            key={node.key}
            node={node}
            x={x - CX}
            y={y - CY}
            delay={delay}
            orbit={false}
            counterRot={0}
            spinning={spinning}
            launchKey={launchKey}
            onActivate={activate}
          />
        ))}
      </div>

      {/* Warp flash on launch — portaled so the stage's scale() can't dim it. */}
      {launchKey &&
        createPortal(
          <div
            style={{
              position: "fixed",
              inset: 0,
              background: "#f2f9ff",
              zIndex: 80,
              pointerEvents: "none",
              animation: "radFlash .43s ease-out both",
            }}
          />,
          document.body,
        )}
    </div>
  );
}

function Ring({ r, style }: { r: number; style?: CSSProperties }) {
  return (
    <div
      style={{
        position: "absolute",
        left: -r,
        top: -r,
        width: r * 2,
        height: r * 2,
        borderRadius: "50%",
        ...style,
      }}
    />
  );
}

function WheelNode({
  node,
  x,
  y,
  delay,
  orbit,
  counterRot,
  spinning,
  launchKey,
  onActivate,
}: {
  node: RadialNode;
  x: number;
  y: number;
  delay: number;
  /** Inside the rotating ring container: counter-rotate to stay upright. */
  orbit: boolean;
  counterRot: number;
  spinning: boolean;
  launchKey: string | null;
  onActivate: (node: RadialNode, e?: MouseEvent) => void;
}) {
  const [hover, setHover] = useState(false);
  const dx = -x;
  const dy = -y;
  const w = orbit ? NODE_W : SAT_W;
  const h = orbit ? NODE_H : SAT_H;

  const launching = launchKey === node.key;
  const retracting = launchKey !== null && !launching;

  const anchor: CSSProperties = {
    position: "absolute",
    left: x,
    top: y,
    width: 0,
    height: 0,
    transform: orbit ? `rotate(${-counterRot}deg)` : undefined,
    animation: orbit && spinning ? COUNTER_ANIM : undefined,
  };

  const layer: CSSProperties = {
    position: "absolute",
    left: -w / 2,
    top: -h / 2,
    width: w,
    height: h,
    transition: "transform 160ms cubic-bezier(.2,.85,.3,1)",
    transform: hover && !launchKey ? "translateY(-6px) scale(1.09)" : undefined,
    animation: launching
      ? "radLaunch .45s cubic-bezier(.45,-.25,.65,.4) forwards"
      : retracting
        ? "radRetract .32s ease-in both"
        : undefined,
  };
  const fan: CSSProperties = {
    width: "100%",
    height: "100%",
    ["--dx" as string]: `${dx}px`,
    ["--dy" as string]: `${dy}px`,
    animation: `radNodeIn .55s cubic-bezier(.2,1.4,.4,1) ${delay}ms both${
      spinning && !orbit ? ", radWobble .5s ease-in-out infinite" : ""
    }`,
  };

  const tile: CSSProperties = orbit
    ? {
        width: "100%",
        height: "100%",
        boxSizing: "border-box",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 0,
        padding: "10px 12px 8px",
        background: "linear-gradient(#170c2b,#0d0619)",
        border: `2px solid ${hover || (node.live && !orbit) ? node.accent : "#35205c"}`,
        color: "inherit",
        textDecoration: "none",
        cursor: node.disabled ? "default" : "pointer",
        opacity: node.disabled ? 0.55 : 1,
        boxShadow: hover ? `0 0 34px ${node.accent}80` : `0 0 22px ${node.accent}26`,
        ["--pulse" as string]: `${node.accent}99`,
        animation: node.live ? "radLivePulse 1.7s ease-in-out infinite" : undefined,
      }
    : {
        width: "100%",
        height: "100%",
        boxSizing: "border-box",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "flex-start",
        gap: 7,
        background: "transparent",
        border: "none",
        padding: 0,
        color: "inherit",
        textDecoration: "none",
        cursor: node.disabled ? "default" : "pointer",
        opacity: node.disabled ? 0.55 : 1,
      };

  // Round chip face with a dashed edge print, like a poker chip.
  const chip: CSSProperties = {
    width: SAT_D,
    height: SAT_D,
    borderRadius: "50%",
    position: "relative",
    display: "grid",
    placeItems: "center",
    border: `2px solid ${hover ? node.accent : "#4a3a72"}`,
    background: "linear-gradient(#1d1036,#0d0619 80%)",
    boxShadow: hover ? `0 0 32px ${node.accent}80` : `0 0 16px ${node.accent}26`,
    transition: "border-color 160ms ease-out, box-shadow 160ms ease-out",
    flex: "none",
  };
  const chipRing: CSSProperties = {
    position: "absolute",
    inset: 5,
    borderRadius: "50%",
    border: `1px dashed ${hover ? `${node.accent}aa` : "#4a3a72"}`,
    pointerEvents: "none",
  };

  const label = (
    <span
      style={{
        fontFamily: "var(--font-display)",
        fontSize: orbit ? 13 : 11,
        letterSpacing: 1,
        lineHeight: orbit ? 1.3 : undefined,
        color: hover ? "#fff" : orbit ? node.accent : "#cfc4f2",
        textShadow: hover ? `0 0 10px ${node.accent}` : `0 0 8px ${node.accent}59`,
        overflow: "hidden",
        textOverflow: orbit ? undefined : "ellipsis",
        whiteSpace: orbit ? "normal" : "nowrap",
        flex: orbit ? 1 : undefined,
        minWidth: orbit ? 0 : undefined,
        maxWidth: orbit ? "100%" : SAT_W,
        textAlign: "center",
      }}
    >
      {node.label}
    </span>
  );

  const inner = orbit ? (
    <>
      <span
        style={{
          width: "100%",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 6,
        }}
      >
        {label}
        {node.badge && (
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 10,
              letterSpacing: 1,
              color: node.accent,
              textShadow: `0 0 8px ${node.accent}`,
              border: `1px solid ${node.accent}`,
              padding: "2px 5px",
              animation: node.live ? "hintBlink 1.5s steps(1) infinite" : undefined,
            }}
          >
            {node.badge}
          </span>
        )}
      </span>
      {node.art && (
        <span
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flex: 1,
            minHeight: 0,
            animation: hover ? "radIconBob .5s ease-in-out" : undefined,
          }}
        >
          {node.art}
        </span>
      )}
      {node.status && (
        <span
          style={{
            fontFamily: "var(--font-body)",
            fontSize: 16,
            lineHeight: 1.1,
            color: hover ? "#cfc4f2" : "#8878b8",
            whiteSpace: "nowrap",
          }}
        >
          {node.status}
        </span>
      )}
    </>
  ) : (
    <>
      <span style={chip}>
        <span style={chipRing} />
        {node.art ?? (
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 24,
              color: node.accent,
              textShadow: `0 0 10px ${node.accent}`,
            }}
          >
            {node.label.slice(0, 1)}
          </span>
        )}
      </span>
      {label}
      {node.status && (
        <span
          style={{
            fontFamily: "var(--font-body)",
            fontSize: 14,
            lineHeight: 1.1,
            color: node.accent,
            textAlign: "center",
          }}
        >
          {node.status}
        </span>
      )}
    </>
  );

  const enter = (e: MouseEvent) => {
    if (node.disabled) return;
    sound.unlock();
    sound.winTick(0);
    setHover(true);
    e.stopPropagation();
  };

  return (
    <div
      style={{
        ...anchor,
        zIndex: launching ? 6 : node.live && orbit ? 3 : 2,
      }}
    >
      <div style={layer}>
        <div style={fan}>
          {node.href ? (
            node.hard ? (
              <a
                href={node.href}
                style={tile}
                onMouseEnter={enter}
                onMouseLeave={() => setHover(false)}
                onClick={() => onActivate(node)}
              >
                {inner}
              </a>
            ) : (
              <Link
                href={node.href}
                style={tile}
                onMouseEnter={enter}
                onMouseLeave={() => setHover(false)}
                onClick={(e) => onActivate(node, e)}
              >
                {inner}
              </Link>
            )
          ) : (
            <button
              type="button"
              style={tile}
              disabled={node.disabled}
              onMouseEnter={enter}
              onMouseLeave={() => setHover(false)}
              onClick={() => onActivate(node)}
            >
              {inner}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
