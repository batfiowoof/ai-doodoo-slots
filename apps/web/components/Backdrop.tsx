const GRID_FLOOR = {
  position: "absolute",
  left: 0,
  right: 0,
  bottom: 0,
  height: "46%",
  overflow: "hidden",
  pointerEvents: "none",
} as const;

const GRID_INNER: React.CSSProperties = {
  position: "absolute",
  inset: 0,
  transform: "perspective(420px) rotateX(74deg)",
  transformOrigin: "bottom center",
  backgroundImage:
    "repeating-linear-gradient(to right, rgba(34,232,255,.35) 0 1px, transparent 1px 72px),repeating-linear-gradient(to bottom, rgba(255,45,149,.28) 0 1px, transparent 1px 56px)",
};

const HORIZON: React.CSSProperties = {
  position: "absolute",
  left: 0,
  right: 0,
  bottom: "46%",
  height: 1,
  background: "#22e8ff",
  boxShadow: "0 0 22px 4px rgba(34,232,255,.55)",
  pointerEvents: "none",
};

const BLOOM: React.CSSProperties = {
  position: "absolute",
  left: "50%",
  top: "50%",
  width: 1400,
  height: 1000,
  transform: "translate(-50%,-50%)",
  background:
    "radial-gradient(ellipse at 50% 50%, rgba(255,138,31,.18), rgba(255,45,149,.11) 42%, transparent 68%)",
  pointerEvents: "none",
};

const SCANLINES: React.CSSProperties = {
  position: "absolute",
  inset: 0,
  zIndex: 10,
  pointerEvents: "none",
  background:
    "repeating-linear-gradient(to bottom, rgba(0,0,0,.22) 0 2px, rgba(0,0,0,0) 2px 4px)",
};

const VIGNETTE: React.CSSProperties = {
  position: "absolute",
  inset: 0,
  zIndex: 10,
  pointerEvents: "none",
  background:
    "radial-gradient(ellipse at 50% 50%, transparent 68%, rgba(6,4,13,.32) 100%)",
};

const FLICKER: React.CSSProperties = {
  position: "absolute",
  inset: 0,
  zIndex: 10,
  pointerEvents: "none",
  background: "#22e8ff",
  opacity: 0.03,
  mixBlendMode: "overlay",
  animation: "crtFlicker 5s steps(1) infinite",
};

/**
 * Shared synthwave ground: perspective grid floor, horizon line, sunset
 * bloom, then the CRT layers (scanlines, vignette, flicker) above the
 * content. Purely decorative; never intercepts a click.
 */
export default function Backdrop() {
  return (
    <>
      <div style={GRID_FLOOR}>
        <div style={GRID_INNER} />
      </div>
      <div style={HORIZON} />
      <div style={BLOOM} />
      <div style={SCANLINES} />
      <div style={VIGNETTE} />
      <div style={FLICKER} />
    </>
  );
}
