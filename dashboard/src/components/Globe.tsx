// Live 3D wireframe monitoring globe: a rotating sphere (real 3D projection
// on <canvas>, zero dependencies) with distributed probe nodes, radar pings
// and packet arcs — the product's "distributed probing" story as a visual.
// Renders one static frame under prefers-reduced-motion.
import { useEffect, useRef } from 'react';

type Node3D = { lat: number; lon: number; phase: number };

const NODES: Node3D[] = [
  { lat: 40, lon: -74, phase: 0.0 },
  { lat: 51, lon: 0, phase: 0.15 },
  { lat: 52, lon: 13, phase: 0.55 },
  { lat: 19, lon: 72, phase: 0.3 },
  { lat: 35, lon: 139, phase: 0.7 },
  { lat: 1, lon: 103, phase: 0.45 },
  { lat: -33, lon: 151, phase: 0.85 },
  { lat: -23, lon: -46, phase: 0.2 },
  { lat: -1, lon: -78, phase: 0.6 },
  { lat: 37, lon: -122, phase: 0.35 },
  { lat: 55, lon: 37, phase: 0.9 },
  { lat: 30, lon: 31, phase: 0.05 },
  { lat: -26, lon: 28, phase: 0.5 },
  { lat: 64, lon: -21, phase: 0.75 },
];

// Arc routes between probe nodes: [nodeIndex, nodeIndex]
const ARCS: Array<[number, number]> = [
  [0, 2],
  [1, 10],
  [4, 5],
  [3, 11],
  [0, 9],
  [6, 7],
];

const INK = '53, 53, 57';
const MINT = '38, 244, 208';
const CORAL = '252, 103, 86';
const VIOLET = '114, 76, 232';

export function Globe({ size = 340 }: { size?: number }) {
  const ref = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = size * dpr;
    canvas.height = size * dpr;
    ctx.scale(dpr, dpr);

    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const R = size / 2 - 12;
    const cx = size / 2;
    const cy = size / 2;

    // Orthographic projection of a lat/lon point rotated around the Y axis.
    const project = (lat: number, lon: number, rotDeg: number) => {
      const phi = (lat * Math.PI) / 180;
      const lam = ((lon + rotDeg) * Math.PI) / 180;
      const x = Math.cos(phi) * Math.sin(lam);
      const y = Math.sin(phi);
      const z = Math.cos(phi) * Math.cos(lam);
      return { x: cx + x * R, y: cy - y * R, z };
    };

    const drawWire = (rot: number, t: number) => {
      ctx.clearRect(0, 0, size, size);

      // Sphere silhouette.
      ctx.beginPath();
      ctx.arc(cx, cy, R, 0, Math.PI * 2);
      ctx.strokeStyle = `rgba(${INK}, 0.5)`;
      ctx.lineWidth = 1.2;
      ctx.stroke();

      // Latitude rings.
      for (let lat = -60; lat <= 60; lat += 30) {
        ctx.beginPath();
        for (let a = 0; a <= 360; a += 6) {
          const p = project(lat, a, rot);
          if (a === 0) ctx.moveTo(p.x, p.y);
          else ctx.lineTo(p.x, p.y);
        }
        ctx.strokeStyle = `rgba(${INK}, 0.16)`;
        ctx.lineWidth = 1;
        ctx.stroke();
      }
      // Longitude meridians.
      for (let lon = 0; lon < 360; lon += 30) {
        ctx.beginPath();
        for (let a = -90; a <= 90; a += 6) {
          const p = project(a, lon, rot);
          if (a === -90) ctx.moveTo(p.x, p.y);
          else ctx.lineTo(p.x, p.y);
        }
        ctx.strokeStyle = `rgba(${INK}, 0.16)`;
        ctx.lineWidth = 1;
        ctx.stroke();
      }

      // Packet arcs between nodes (front hemisphere only), with a travelling
      // coral packet along each.
      ARCS.forEach(([ai, bi], i) => {
        const A = project(NODES[ai].lat, NODES[ai].lon, rot);
        const B = project(NODES[bi].lat, NODES[bi].lon, rot);
        if (A.z <= 0.05 || B.z <= 0.05) return;
        const mx = (A.x + B.x) / 2;
        const my = (A.y + B.y) / 2;
        const dx = mx - cx;
        const dy = my - cy;
        const len = Math.hypot(dx, dy) || 1;
        const lift = 1.32;
        const qx = mx + (dx / len) * (R * (lift - 1) * 1.4);
        const qy = my + (dy / len) * (R * (lift - 1) * 1.4);
        ctx.beginPath();
        ctx.moveTo(A.x, A.y);
        ctx.quadraticCurveTo(qx, qy, B.x, B.y);
        ctx.strokeStyle = `rgba(${VIOLET}, 0.5)`;
        ctx.lineWidth = 1.4;
        ctx.stroke();
        // Travelling packet.
        const p = (t / 1600 + i * 0.18) % 1;
        const ix = (1 - p) * (1 - p) * A.x + 2 * (1 - p) * p * qx + p * p * B.x;
        const iy = (1 - p) * (1 - p) * A.y + 2 * (1 - p) * p * qy + p * p * B.y;
        ctx.beginPath();
        ctx.arc(ix, iy, 2.6, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(${CORAL}, 0.95)`;
        ctx.fill();
      });

      // Probe nodes with radar pings.
      NODES.forEach((n) => {
        const p = project(n.lat, n.lon, rot);
        if (p.z <= 0.05) return;
        const depth = 0.35 + p.z * 0.65;
        const ring = ((t / 1900 + n.phase) % 1);
        if (ring < 1) {
          ctx.beginPath();
          ctx.arc(p.x, p.y, 3 + ring * 16, 0, Math.PI * 2);
          ctx.strokeStyle = `rgba(${MINT}, ${(1 - ring) * 0.55 * depth})`;
          ctx.lineWidth = 1.4;
          ctx.stroke();
        }
        ctx.beginPath();
        ctx.arc(p.x, p.y, 2.6, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(${MINT}, ${0.55 + depth * 0.45})`;
        ctx.fill();
        ctx.beginPath();
        ctx.arc(p.x, p.y, 4.6, 0, Math.PI * 2);
        ctx.strokeStyle = `rgba(${INK}, ${0.35 * depth})`;
        ctx.lineWidth = 1;
        ctx.stroke();
      });
    };

    let raf = 0;
    let rot = 0;
    let last = performance.now();
    const frame = (t: number) => {
      const dt = Math.min(t - last, 50);
      last = t;
      drawWire(rot, t);
      rot += dt * 0.009; // ~9°/s — slow, ambient
      raf = requestAnimationFrame(frame);
    };
    if (reduced) {
      drawWire(20, 0); // one static frame
    } else {
      raf = requestAnimationFrame(frame);
    }
    return () => cancelAnimationFrame(raf);
  }, [size]);

  return (
    <canvas
      ref={ref}
      className="globe-canvas"
      style={{ width: size, height: size }}
      aria-label="Rotating globe with distributed monitoring probes"
      role="img"
    />
  );
}
