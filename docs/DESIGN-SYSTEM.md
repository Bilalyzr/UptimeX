# Signal Press — Editorial Instrument Design Language

The UI system behind UptimeX. Named for its two halves: **Press** — the
warm-paper, ink-and-hairline vocabulary of print/editorial design — and
**Signal** — the live-instrument layer of monitoring UIs (pulses, pings,
ECG traces, terminal panels). Use it for products that want to feel
hand-crafted and technical at once: dev tools, observability, fintech
dashboards, premium B2B SaaS.

Alternate names if you prefer: *Bone & Ink*, *Paper Terminal*, *Ledger Signal*.

---

## 1. Philosophy

1. **Paper first, ink on top.** Warm off-white canvas, near-black "ink"
   for text, borders and solid fills. Nothing pure white, nothing pure
   black — it should feel like a printed page.
2. **Hairlines over shadows.** Structure comes from 1px ink-opacity
   rules. Soft blur shadows are for hover states only; identity comes
   from **hard offset shadows** (solid, unblurred, like letterpress).
3. **Pills for action, mono for data.** Buttons are full pills (999px).
   Anything measurable — labels, counts, timestamps, statuses — is
   monospace uppercase with wide tracking.
4. **Color means something.** One accent per job: coral = act/hover,
   mint = alive/signal, ink = identity, violet = links/tertiary. Status
   colors get soft-tinted pill badges, never raw glows.
5. **Motion is mechanical, not bouncy.** 150–300ms, gentle cubic-bezier
   easing, staggered reveals. The product is an instrument: movements
   feel like needles and switches, never like toys.

## 2. Color tokens

| Token | Value | Role |
|---|---|---|
| `bg` | `#e9eee7` | Page canvas (warm sage-paper) |
| `surface` | `#fbfdfa` | Cards, panels |
| `surface-2` | `#dde5db` | Inset tracks, secondary fills |
| `ink` | `#0a090f` | Text-strong, primary buttons, terminal panels |
| `text` | `#353539` | Body text |
| `muted` | `#6e6e68` | Secondary text, mono labels |
| `border` | `#cfd8cd` | Soft dividers |
| `border-ink` | `rgba(53,53,57,.26)` | The signature hairline |
| `coral` | `#fc6756` | Interactive accent (hover, arrows, CTA glow) |
| `coral-strong` | `#f4503c` | CTA gradient end |
| `mint` | `#26f4d0` | Signal/live (dots, pulse traces, progress) |
| `green` | `#0e8f74` | Success text on light (mint is too light for text) |
| `green-soft` | `#e0f7f0` | Success badge fill |
| `amber` / `amber-soft` | `#9a7b0a` / `#faf3d7` | Warning badge pair |
| `red` / `red-soft` | `#e04431` / `#fde7e3` | Danger badge pair |
| `violet` / `violet-soft` | `#724ce8` / `#efeafe` | Links, tertiary series |
| `orange` | `#e0862f` | Mid-status dot (degraded) |

**Rule of thumb:** 90% of any screen is paper + ink + one hairline.
Coral and mint appear in small doses — a hover, a pulse, a chip.

## 3. Typography

| Role | Font | Notes |
|---|---|---|
| Body / UI | **Epilogue** (400–800) | Geometric, neutral workhorse |
| Display | **Violet Sans** (400) | Quirkier headline voice; large sizes only |
| Data | **Roboto Mono** (400/700) | Labels 10–12px uppercase, +0.10em tracking |

Type scale used: 44–60px hero, 30–38px section H2, 15–17px body,
11–13px mono micro-labels. Google Fonts one-liner:

```
family=Epilogue:wght@400;500;600;700;800&family=Violet+Sans&family=Roboto+Mono:wght@400;700
```

## 4. Shape, depth, spacing

- **Radii:** cards 18–22px, inputs/buttons 999px (pill), chips 999px,
  terminal panels 16–20px.
- **Signature shadow:** `8px 8px 0 rgba(10,9,15,.85-.9)` — solid ink
  offset, no blur (letterpress feel). Hover versions shrink to 4–6px.
- **Soft shadows** (`0 6px 20px rgba(10,9,15,.08)`) only for floating
  layers (nav pill, tooltips).
- **Spacing:** 4px base grid; sections breathe 64–80px; content max-width
  1200–1280px centered.

## 5. Signature components

- **Section label** — mono `01 CAPABILITIES` with a hairline rule running
  to the edge; the number sits in an ink pill.
- **Stat rail** — hairline-divided column of stats: mono micro-label over
  display-font value. Works horizontally on mobile.
- **Terminal panel** — ink panel, traffic-light dots, mono text, blinking
  block cursor. For closing CTAs and "live" moments.
- **Corner brackets** — 1.5px coral L-marks at panel corners (fade in on
  hover for cards).
- **Marquee strip** — full-width hairline-bounded mono ticker between
  sections; pause on hover.
- **Pulse motif** — an ECG polyline (mint stroke, glow drop-shadow)
  animating via `stroke-dashoffset`. The brand heartbeat.
- **Soft badges** — status pills: tinted fill + 30%-opacity border +
  dark text of same hue.
- **Branched nav** — trunk line with SVG branch curves that draw to the
  active item (stroke-dashoffset), gliding marker.

## 6. Motion rules

- Durations 150–300ms; easing `cubic-bezier(.22,1,.36,1)` (out) and
  `cubic-bezier(.65,0,.35,1)` (draw/wipe).
- Entrances: staggered fades/line-reveals (50–120ms offsets), clip-path
  mask wipes for headlines.
- Infinite animations are ambient only (pulse dots, marquee, ECG sweep)
  and must be slow (2–30s).
- **Every animation gated by `prefers-reduced-motion`** — reduce to
  opacity-only or static.

## 7. Do / Don't

**Do:** leave generous paper whitespace · use mono uppercase for any
number that changes · draw hairlines full-bleed · let one coral accent
carry a whole section.

**Don't:** pure white or pure black · blur shadows on identity elements ·
more than two accent colors per view · dark full pages (ink is for
panels, pills and terminal moments) · animation without a data story.
