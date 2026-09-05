"use client";

// The lobby wheel. Nodes fan out of a central hub onto an elliptical ring —
// an ellipse because viewports are widescreen and a circle wastes the
// corners. Geometry and motion live here; the lobby screen passes in the
// node content and the hub face.
//
// The hub click is a shuffle: the cards retract into the hub, then re-deal
// to fresh random seats (the deal order rotates by a random offset). That
// reads the same as a prize-wheel spin but works on any ring geometry.

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
  /** Corner tag, e.g. a live count or room state. */
  badge?: string;
  /** Pulsing border + badge treatment for live rooms. */
  live?: boolean;
  /** Navigation target. Without one, the node is a button (groups, actions). */
  href?: string;
  /** Auth routes navigate with a plain anchor (no client-side launch). */
  hard?: boolean;
  /** Button action (group drill-down). */
  onActivate?: () => void;
  disabled?: boolean;
  /** Launch sound; defaults to sound.click(). */
  onLaunchSound?: () => void;
}

// Design-size stage; the lobby scales it to fit the viewport.
const STAGE_W = 1480;
const STAGE_H = 640;
const CX = STAGE_W / 2;
const CY = STAGE_H / 2;
const HUB_D = 300;
const NODE_W = 212;
const NODE_H = 150;

// Ellipse per node count: widescreen, so horizontal room is cheap.
function gameEllipse(n: number): { rx: number; ry: number } {
  if (n <= 3) return { rx: 540, ry: 208 };
  if (n <= 5) return { rx: 575, ry: 222 };
  if (n <= 8) return { rx: 600, ry: 234 };
  return { rx: 615, ry: 240 };
}

function slot(i: number, n: number, rx: number, ry: number): { x: number; y: number } {
  const deg = -90 + (i * 360) / n;
  const rad = (deg * Math.PI) / 180;
  return { x: CX + rx * Math.cos(rad), y: CY + ry * Math.sin(rad) };
}

export default function RadialMenu({
  nodes,
  hub,
  onHubActivate,
}: {
  nodes: RadialNode[];
  hub: ReactNode;
  /** Overrides the hub shuffle — used to climb back up the game tree. */
  onHubActivate?: () => void;
}) {
  const router = useRouter();
  const [spinning, setSpinning] = useState(false);
  const [offset, setOffset] = useState(0);
  const [launchKey, setLaunchKey] = useState<string | null>(null);
  const anchors = useRef(new Map<string, HTMLDivElement>());
  const spinAnims = useRef<Animation[]>([]);
  const timers = useRef<number[]>([]);

  useEffect(
    () => () => {
      timers.current.forEach((t) => window.clearTimeout(t));
    },
    [],
  );

  const n = Math.max(nodes.length, 1);
  const { rx, ry } = gameEllipse(n);
  // Seat order accumulates a random offset on every shuffle, so the same
  // nodes land on fresh, evenly spaced seats each time.
  const placed = nodes.map((node, i) => {
    const seat = (i + offset) % n;
    return { node, ...slot(seat, n, rx, ry), delay: seat * 70 };
  });

  // The hub shuffle: every card orbits the ellipse — a couple of laps plus a
  // random whole seat — decelerating onto fresh, evenly spaced positions.
  // Tiles never tilt: they translate along the ring, so the old
  // rotate-the-container trick (circle-only) isn't needed.
  const SPIN_MS = 1700;
  const spinWheel = () => {
    if (spinning || launchKey || n < 2) return;
    sound.unlock();
    sound.click();
    const k = 1 + Math.floor(Math.random() * (n - 1));
    const laps = 2 + Math.floor(Math.random() * 2);
    const stepDeg = 360 / n;
    const easeOut = (t: number) => 1 - Math.pow(1 - t, 4);
    const anims: Animation[] = [];
    placed.forEach((p, i) => {
      const el = anchors.current.get(p.node.key);
      if (!el) return;
      const a0 = -90 + ((i + offset) % n) * stepDeg;
      const a1 = a0 + laps * 360 + k * stepDeg;
      const frames: Keyframe[] = [];
      const STEPS = 30;
      for (let sIdx = 0; sIdx <= STEPS; sIdx++) {
        const t = sIdx / STEPS;
        const ang = a0 + (a1 - a0) * easeOut(t);
        const rad = (ang * Math.PI) / 180;
        const x = CX + rx * Math.cos(rad);
        const y = CY + ry * Math.sin(rad);
        frames.push({ transform: `translate(${x.toFixed(2)}px, ${y.toFixed(2)}px)` });
      }
      anims.push(el.animate(frames, { duration: SPIN_MS, fill: "forwards" }));
    });
    spinAnims.current = anims;
    setSpinning(true);
    for (let t = 1; t <= 14; t++) {
      timers.current.push(window.setTimeout(() => sound.winTick(t), (t * SPIN_MS) / 16));
    }
    timers.current.push(
      window.setTimeout(() => {
        // The animations hold the landing spot (fill: forwards); commit the
        // new seats to React, then release the animations onto the identical
        // inline transforms — zero visual seam.
        anims.forEach((a) => a.cancel());
        spinAnims.current = [];
        setOffset((o) => (o + k) % n);
        sound.bell();
      }, SPIN_MS + 30),
    );
    timers.current.push(window.setTimeout(() => setSpinning(false), SPIN_MS + 80));
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
      {/* Spoke lines hub → node, under everything. They fade while the wheel
          shuffles, then redraw to the new seats. */}
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
            opacity: spinning ? 0.15 : 1,
            transition: "opacity .3s ease-out",
          }}
          aria-hidden
        >
          {placed.map(({ x, y }) => (
            <line key={`spoke-${x}-${y}`} x1={CX} y1={CY} x2={x} y2={y} stroke="#2a1848" strokeWidth={2} />
          ))}
        </svg>
      </div>

      {/* Decorative rings: a slowly rotating dashed circle inside the ellipse,
          plus static elliptical tick and rim rings. */}
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
        <Ring
          r={150}
          style={{ border: "2px dashed #22e8ff55", animation: "radSpin 80s linear infinite" }}
        />
        <div
          style={{
            position: "absolute",
            left: -rx,
            top: -ry,
            width: rx * 2,
            height: ry * 2,
            borderRadius: "50%",
            background:
              "repeating-conic-gradient(from 0deg, rgba(34,232,255,0) 0deg 11deg, rgba(34,232,255,.6) 11deg 12.2deg)",
            maskImage: "radial-gradient(ellipse closest-side, transparent 85%, #000 86%, #000 97%, transparent 98%)",
            WebkitMaskImage:
              "radial-gradient(ellipse closest-side, transparent 85%, #000 86%, #000 97%, transparent 98%)",
          }}
        />
        <Ellipse rx={rx + 44} ry={ry + 40} style={{ border: "2px dashed #ff2d9544" }} />
      </div>

      {/* Hub whirl while shuffling: one-shot 720°; 720 ≡ 0 so it lands clean. */}
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
          onClick={() => {
            if (onHubActivate) {
              sound.unlock();
              sound.click();
              onHubActivate();
              return;
            }
            spinWheel();
          }}
          title={onHubActivate ? "BACK TO THE FLOOR" : "SHUFFLE THE FLOOR"}
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

      {/* The game ring. Anchors register themselves so the shuffle can orbit
          them along the ellipse. */}
      {placed.map(({ node, x, y, delay }) => (
        <WheelNode
          key={node.key}
          node={node}
          x={x}
          y={y}
          delay={delay}
          launchKey={launchKey}
          onActivate={activate}
          anchorRef={(el) => {
            if (el) anchors.current.set(node.key, el);
          }}
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

function Ellipse({ rx, ry, style }: { rx: number; ry: number; style?: CSSProperties }) {
  return (
    <div
      style={{
        position: "absolute",
        left: -rx,
        top: -ry,
        width: rx * 2,
        height: ry * 2,
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
  launchKey,
  onActivate,
  anchorRef,
}: {
  node: RadialNode;
  x: number;
  y: number;
  delay: number;
  launchKey: string | null;
  onActivate: (node: RadialNode, e?: MouseEvent) => void;
  /** Registered so the hub shuffle can orbit this card along the ellipse. */
  anchorRef: (el: HTMLDivElement | null) => void;
}) {
  const [hover, setHover] = useState(false);
  const dx = CX - x;
  const dy = CY - y;

  const launching = launchKey === node.key;
  const launchRetract = launchKey !== null && !launching;

  // The anchor is the card's seat on the ellipse. Positioned by transform so
  // the shuffle's Web Animations can glide it around the ring.
  const anchor: CSSProperties = {
    position: "absolute",
    left: 0,
    top: 0,
    width: 0,
    height: 0,
    transform: `translate(${x}px, ${y}px)`,
    willChange: "transform",
    zIndex: launching ? 6 : node.live ? 3 : 2,
  };

  const layer: CSSProperties = {
    position: "absolute",
    left: -NODE_W / 2,
    top: -NODE_H / 2,
    width: NODE_W,
    height: NODE_H,
    transition: "transform 160ms cubic-bezier(.2,.85,.3,1)",
    transform: hover && !launchKey ? "translateY(-6px) scale(1.09)" : undefined,
    animation: launching
      ? "radLaunch .45s cubic-bezier(.45,-.25,.65,.4) forwards"
      : launchRetract
        ? "radRetract .32s ease-in both"
        : undefined,
  };
  const fan: CSSProperties = {
    width: "100%",
    height: "100%",
    ["--dx" as string]: `${dx}px`,
    ["--dy" as string]: `${dy}px`,
    animation: `radNodeIn .55s cubic-bezier(.2,1.4,.4,1) ${delay}ms both`,
  };

  const tile: CSSProperties = {
    width: "100%",
    height: "100%",
    boxSizing: "border-box",
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 0,
    padding: "12px 14px 10px",
    background: "linear-gradient(#170c2b,#0d0619)",
    border: `2px solid ${hover || node.live ? node.accent : "#35205c"}`,
    color: "inherit",
    textDecoration: "none",
    cursor: node.disabled ? "default" : "pointer",
    opacity: node.disabled ? 0.55 : 1,
    boxShadow: hover ? `0 0 36px ${node.accent}80` : `0 0 22px ${node.accent}26`,
    ["--pulse" as string]: `${node.accent}99`,
    animation: node.live ? "radLivePulse 1.7s ease-in-out infinite" : undefined,
  };

  return (
    <div style={anchor} ref={anchorRef}>
      <div style={layer}>
        <div style={fan}>
          {node.href ? (
            node.hard ? (
              <a
                href={node.href}
                style={tile}
                onMouseEnter={(e) => {
                  if (node.disabled) return;
                  sound.unlock();
                  sound.winTick(0);
                  setHover(true);
                  e.stopPropagation();
                }}
                onMouseLeave={() => setHover(false)}
                onClick={() => onActivate(node)}
              >
                {inner(node, hover)}
              </a>
            ) : (
              <Link
                href={node.href}
                style={tile}
                onMouseEnter={(e) => {
                  if (node.disabled) return;
                  sound.unlock();
                  sound.winTick(0);
                  setHover(true);
                  e.stopPropagation();
                }}
                onMouseLeave={() => setHover(false)}
                onClick={(e) => onActivate(node, e)}
              >
                {inner(node, hover)}
              </Link>
            )
          ) : (
            <button
              type="button"
              style={tile}
              disabled={node.disabled}
              onMouseEnter={(e) => {
                if (node.disabled) return;
                sound.unlock();
                sound.winTick(0);
                setHover(true);
                e.stopPropagation();
              }}
              onMouseLeave={() => setHover(false)}
              onClick={() => onActivate(node)}
            >
              {inner(node, hover)}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function inner(node: RadialNode, hover: boolean): ReactNode {
  return (
    <>
      <span
        style={{
          width: "100%",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 8,
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 15,
            letterSpacing: 1,
            lineHeight: 1.3,
            color: hover ? "#fff" : node.accent,
            textShadow: hover ? `0 0 10px ${node.accent}` : `0 0 8px ${node.accent}59`,
            overflow: "hidden",
            flex: 1,
            minWidth: 0,
          }}
        >
          {node.label}
        </span>
        {node.badge && (
          <span
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 12,
              letterSpacing: 1,
              color: node.accent,
              textShadow: `0 0 8px ${node.accent}`,
              border: `1px solid ${node.accent}`,
              padding: "3px 7px",
              whiteSpace: "nowrap",
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
            fontSize: 18,
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
}
