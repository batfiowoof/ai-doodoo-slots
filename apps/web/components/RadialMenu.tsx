"use client";

// The lobby wheel. Nodes fan out of a central hub onto an outer ring; account
// satellites ride a smaller inner orbit. Geometry and motion live here — the
// lobby screen passes in node/satellite content and the hub face.

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
const SAT_W = 140;
const SAT_H = 62;

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
  const [spinning, setSpinning] = useState(false);
  const [launchKey, setLaunchKey] = useState<string | null>(null);
  const timers = useRef<number[]>([]);

  useEffect(
    () => () => {
      timers.current.forEach((t) => window.clearTimeout(t));
    },
    [],
  );

  const r = gameRadius(nodes.length);
  const placed: PlacedNode[] = nodes.map((node, i) => ({
    node,
    ...slot(i, nodes.length, r),
    delay: i * 70,
    satellite: false,
  }));
  // Satellites interleave on the inner orbit, offset half a game step.
  satellites.forEach((node, i) => {
    const deg = -90 + ((i + 0.5) * 360) / Math.max(satellites.length, 1);
    const rad = (deg * Math.PI) / 180;
    placed.push({
      node,
      x: CX + SAT_R * Math.cos(rad),
      y: CY + SAT_R * Math.sin(rad),
      delay: nodes.length * 70 + 60 + i * 60,
      satellite: true,
    });
  });

  const spin = () => {
    if (spinning || launchKey) return;
    sound.unlock();
    sound.click();
    setSpinning(true);
    for (let t = 1; t <= 12; t++) {
      timers.current.push(window.setTimeout(() => sound.winTick(t), t * 95));
    }
    timers.current.push(
      window.setTimeout(() => {
        setSpinning(false);
        sound.bell();
      }, 1500),
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

  return (
    <div style={{ position: "relative", width: STAGE_W, height: STAGE_H, flex: "none" }}>
      {/* Spoke lines hub → node, under everything. */}
      <svg
        width={STAGE_W}
        height={STAGE_H}
        style={{ position: "absolute", inset: 0, animation: "radRingIn .9s ease-out both" }}
        aria-hidden
      >
        {placed.map(({ x, y, satellite }) => (
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
          onClick={spin}
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

      {placed.map(({ node, x, y, delay, satellite }) => (
        <WheelNode
          key={node.key}
          node={node}
          x={x}
          y={y}
          delay={delay}
          satellite={satellite}
          spinning={spinning}
          launchKey={launchKey}
          onActivate={activate}
        />
      ))}

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
  satellite,
  spinning,
  launchKey,
  onActivate,
}: {
  node: RadialNode;
  x: number;
  y: number;
  delay: number;
  satellite: boolean;
  spinning: boolean;
  launchKey: string | null;
  onActivate: (node: RadialNode, e?: MouseEvent) => void;
}) {
  const [hover, setHover] = useState(false);
  const dx = CX - x;
  const dy = CY - y;
  const w = satellite ? SAT_W : NODE_W;
  const h = satellite ? SAT_H : NODE_H;

  const launching = launchKey === node.key;
  const retracting = launchKey !== null && !launching;

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
      spinning ? ", radWobble .5s ease-in-out infinite" : ""
    }`,
  };

  const tile: CSSProperties = {
    width: "100%",
    height: "100%",
    boxSizing: "border-box",
    display: "flex",
    flexDirection: satellite ? "row" : "column",
    alignItems: "center",
    justifyContent: satellite ? (node.art ? "flex-start" : "center") : "space-between",
    gap: satellite ? 10 : 0,
    padding: satellite ? "0 12px" : "10px 12px 8px",
    background: satellite ? "linear-gradient(#1d1036,#120a26)" : "linear-gradient(#170c2b,#0d0619)",
    border: `2px solid ${hover || (node.live && !satellite) ? node.accent : "#35205c"}`,
    color: "inherit",
    textDecoration: "none",
    cursor: node.disabled ? "default" : "pointer",
    opacity: node.disabled ? 0.55 : 1,
    boxShadow: hover ? `0 0 34px ${node.accent}80` : `0 0 22px ${node.accent}26`,
    ["--pulse" as string]: `${node.accent}99`,
    animation: node.live && !satellite ? "radLivePulse 1.7s ease-in-out infinite" : undefined,
  };

  const label = (
    <span
      style={{
        fontFamily: "var(--font-display)",
        fontSize: satellite ? 11 : 13,
        letterSpacing: 1,
        lineHeight: satellite ? undefined : 1.3,
        color: hover ? "#fff" : satellite ? "#cfc4f2" : node.accent,
        textShadow: hover ? `0 0 10px ${node.accent}` : `0 0 8px ${node.accent}59`,
        overflow: "hidden",
        textOverflow: satellite ? "ellipsis" : undefined,
        whiteSpace: satellite ? "nowrap" : "normal",
        flex: satellite ? undefined : 1,
        minWidth: satellite ? undefined : 0,
        maxWidth: satellite ? (node.art ? 86 : "100%") : "100%",
      }}
    >
      {node.label}
    </span>
  );

  const inner = satellite ? (
    node.art ? (
      <>
        {node.art}
        <span style={{ display: "flex", flexDirection: "column", gap: 1, minWidth: 0 }}>
          {label}
          {node.status && (
            <span style={{ fontFamily: "var(--font-body)", fontSize: 15, lineHeight: 1, color: node.accent }}>
              {node.status}
            </span>
          )}
        </span>
      </>
    ) : (
      <span
        style={{
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          gap: 2,
          width: "100%",
        }}
      >
        {label}
        {node.status && (
          <span style={{ fontFamily: "var(--font-body)", fontSize: 15, lineHeight: 1, color: node.accent }}>
            {node.status}
          </span>
        )}
      </span>
    )
  ) : (
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
        position: "absolute",
        left: x,
        top: y,
        width: 0,
        height: 0,
        zIndex: launching ? 6 : node.live && !satellite ? 3 : satellite ? 2 : 2,
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
