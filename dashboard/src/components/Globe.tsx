// Hero 3D model: the monitoring observatory globe — a solid sphere with a
// depth-faded wireframe, three tilted orbital probe rings carrying travelling
// satellites, distributed probe nodes with radar pings and packet arcs.
// Pure canvas with orthographic 3D projection (zero dependencies). Renders
// one static frame under prefers-reduced-motion.
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
  { lat: 22, lon: 78, phase: 0.12 },
  { lat: -36, lon: 175, phase: 0.66 },
  { lat: 48, lon: -100, phase: 0.4 },
  { lat: 8, lon: 20, phase: 0.58 },
  { lat: -14, lon: -55, phase: 0.28 },
  { lat: 44, lon: 12, phase: 0.82 },
];

const ARCS: Array<[number, number]> = [
  [0, 2], [1, 10], [4, 5], [3, 11], [0, 9], [6, 7], [14, 4], [17, 2],
];

// Orbital probe rings: radius factor, plane basis (tilt via two 3D vectors),
// satellite speed and phase.
const RINGS = [
  {
    u: [0.97, 0.12, 0.2], v: [-0.16, 0.95, 0.26], rr: 1.38,
    speed: 0.05, phase: 0.1,
  },
  {
    u: [0.3, 0.95, -0.08], v: [0.9, -0.26, -0.34], rr: 1.56,
    speed: -0.038, phase: 0.55,
  },
  {
    u: [0.55, -0.2, 0.81], v: [0.62, 0.72, -0.32], rr: 1.22,
    speed: 0.065, phase: 0.85,
  },
];

const INK = '53, 53, 57';
const MINT = '38, 244, 208';
const CORAL = '252, 103, 86';
const VIOLET = '114, 76, 232';

type P3 = { x: number; y: number; z: number };

export function Globe({ size = 520 }: { size?: number }) {
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
    const R = size / 2 - 34; // leaves room for the outer ring
    const cx = size / 2;
    const cy = size / 2;

    const project = (lat: number, lon: number, rotDeg: number): P3 => {
      const phi = (lat * Math.PI) / 180;
      const lam = ((lon + rotDeg) * Math.PI) / 180;
      return {
        x: cx + Math.cos(phi) * Math.sin(lam) * R,
        y: cy - Math.sin(phi) * R,
        z: Math.cos(phi) * Math.cos(lam),
      };
    };

    // Stroke a polyline split into front (z >= 0) and back halves so the far
    // side of the sphere reads dimmer — two batched paths, no per-segment
    // state churn.
    const strokeSplit = (pts: P3[], frontColor: string, backColor: string, width: number) => {
      for (const wantFront of [false, true]) {
        ctx.beginPath();
        let drawing = false;
        for (const p of pts) {
          if ((p.z >= 0) === wantFront) {
            if (!drawing) {
              ctx.moveTo(p.x, p.y);
              drawing = true;
            } else {
              ctx.lineTo(p.x, p.y);
            }
          } else {
            drawing = false;
          }
        }
        ctx.strokeStyle = wantFront ? frontColor : backColor;
        ctx.lineWidth = width;
        ctx.stroke();
      }
    };

    const ringPoint = (ring: (typeof RINGS)[number], theta: number): P3 => {
      const { u, v, rr } = ring;
      const px = Math.cos(theta) * u[0] + Math.sin(theta) * v[0];
      const py = Math.cos(theta) * u[1] + Math.sin(theta) * v[1];
      const pz = Math.cos(theta) * u[2] + Math.sin(theta) * v[2];
      return { x: cx + px * R * rr, y: cy + py * R * rr, z: pz };
    };

    const draw = (rot: number, t: number) => {
      ctx.clearRect(0, 0, size, size);

      // Solid body: soft radial tint so the sphere reads as a volume.
      const grad = ctx.createRadialGradient(cx - R * 0.35, cy - R * 0.4, R * 0.1, cx, cy, R);
      grad.addColorStop(0, 'rgba(38, 244, 208, 0.10)');
      grad.addColorStop(0.65, 'rgba(38, 244, 208, 0.03)');
      grad.addColorStop(1, 'rgba(53, 53, 57, 0.05)');
      ctx.beginPath();
      ctx.arc(cx, cy, R, 0, Math.PI * 2);
      ctx.fillStyle = grad;
      ctx.fill();
      ctx.strokeStyle = `rgba(${INK}, 0.55)`;
      ctx.lineWidth = 1.4;
      ctx.stroke();

      // Wireframe (20° mesh), equator emphasized.
      for (let lat = -80; lat <= 80; lat += 20) {
        const pts: P3[] = [];
        for (let a = 0; a <= 360; a += 5) pts.push(project(lat, a, rot));
        const eq = lat === 0;
        strokeSplit(pts, `rgba(${INK}, ${eq ? 0.4 : 0.26})`, `rgba(${INK}, ${eq ? 0.16 : 0.09})`, eq ? 1.3 : 1);
      }
      for (let lon = 0; lon < 360; lon += 20) {
        const pts: P3[] = [];
        for (let a = -90; a <= 90; a += 5) pts.push(project(a, lon, rot));
        strokeSplit(pts, `rgba(${INK}, 0.26)`, `rgba(${INK}, 0.09)`, 1);
      }

      // Orbital probe rings + travelling satellites.
      RINGS.forEach((ring, i) => {
        const pts: P3[] = [];
        for (let a = 0; a <= 360; a += 5) pts.push(ringPoint(ring, (a * Math.PI) / 180));
        strokeSplit(pts, `rgba(${INK}, 0.34)`, `rgba(${INK}, 0.1)`, 1);
        const sat = ringPoint(ring, t * 0.001 * 14 * ring.speed + ring.phase * Math.PI * 2);
        if (sat.z > -0.05) {
          const depth = 0.5 + sat.z * 0.5;
          ctx.beginPath();
          ctx.arc(sat.x, sat.y, 3, 0, Math.PI * 2);
          ctx.fillStyle = `rgba(${CORAL}, ${0.5 + depth * 0.5})`;
          ctx.fill();
          ctx.beginPath();
          ctx.arc(sat.x, sat.y, 6.5, 0, Math.PI * 2);
          ctx.strokeStyle = `rgba(${INK}, ${0.3 * depth})`;
          ctx.lineWidth = 1;
          ctx.stroke();
          void i;
        }
      });

      // Packet arcs between probe nodes.
      ARCS.forEach(([ai, bi], i) => {
        const A = project(NODES[ai].lat, NODES[ai].lon, rot);
        const B = project(NODES[bi].lat, NODES[bi].lon, rot);
        if (A.z <= 0.05 || B.z <= 0.05) return;
        const mx = (A.x + B.x) / 2;
        const my = (A.y + B.y) / 2;
        const dx = mx - cx;
        const dy = my - cy;
        const len = Math.hypot(dx, dy) || 1;
        const qx = mx + (dx / len) * R * 0.5;
        const qy = my + (dy / len) * R * 0.5;
        ctx.beginPath();
        ctx.moveTo(A.x, A.y);
        ctx.quadraticCurveTo(qx, qy, B.x, B.y);
        ctx.strokeStyle = `rgba(${VIOLET}, 0.5)`;
        ctx.lineWidth = 1.4;
        ctx.stroke();
        const p = (t / 1600 + i * 0.13) % 1;
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
        const ring = (t / 1900 + n.phase) % 1;
        ctx.beginPath();
        ctx.arc(p.x, p.y, 3 + ring * 17, 0, Math.PI * 2);
        ctx.strokeStyle = `rgba(${MINT}, ${(1 - ring) * 0.55 * depth})`;
        ctx.lineWidth = 1.4;
        ctx.stroke();
        ctx.beginPath();
        ctx.arc(p.x, p.y, 2.6, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(${MINT}, ${0.55 + depth * 0.45})`;
        ctx.fill();
        ctx.beginPath();
        ctx.arc(p.x, p.y, 4.8, 0, Math.PI * 2);
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
      drawWireSafe();
      rot += dt * 0.008;
      raf = requestAnimationFrame(frame);
    };
    const drawWireSafe = () => draw(rot, performance.now());
    if (reduced) {
      draw(20, 0);
    } else {
      raf = requestAnimationFrame(frame);
    }
    return () => cancelAnimationFrame(raf);
  }, [size]);

  return (
    <canvas
      ref={ref}
      className="globe-canvas"
      style={{ width: '100%', maxWidth: size, height: 'auto', aspectRatio: '1 / 1' }}
      role="img"
      aria-label="Rotating globe with distributed monitoring probes and orbital satellites"
    />
  );
}
