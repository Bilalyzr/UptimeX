// Marketing landing page: hero, capability grid, how-it-works, pricing and
// FAQ. All copy is grounded in the platform's actual feature set (see README
// feature table); the hero visual is a CSS/SVG dashboard mockup.
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { useAuth } from '../auth';
import { Logo } from '../components/Logo';
import { Reveal } from '../components/Reveal';

const plans = [
  {
    id: 'free',
    name: 'Free',
    price: '₹0',
    period: 'forever',
    tagline: 'Side projects and personal sites.',
    features: [
      '2 monitored endpoints',
      '60-second check interval',
      '7-day metrics history',
      'Email + webhook alerts',
      'Public status page',
    ],
    cta: 'Start free',
    highlight: false,
  },
  {
    id: 'pro',
    name: 'Pro',
    price: '₹1,499',
    period: 'per month',
    tagline: 'Production services with real users.',
    features: [
      '50 monitored endpoints',
      '30-second check interval',
      '30-day metrics history',
      'P50 / P95 / P99 analytics',
      'HMAC-signed webhook alerts',
      'Public status page',
    ],
    cta: 'Start 14-day trial',
    highlight: true,
  },
  {
    id: 'business',
    name: 'Business',
    price: '₹7,999',
    period: 'per month',
    tagline: 'Fleets where minutes cost money.',
    features: [
      '250 monitored endpoints',
      '10-second check interval',
      '90-day metrics history',
      'Failure-state-machine incident accuracy',
      'Self-monitoring heartbeat watchdog',
      'Priority support',
    ],
    cta: 'Start 14-day trial',
    highlight: false,
  },
];

const capabilities = [
  {
    title: 'Distributed probing',
    body: 'A bounded worker pool checks 50 to 1000+ HTTP/HTTPS endpoints concurrently. One slow target can never block another — every probe runs under its own deadline.',
  },
  {
    title: 'No flapping pages',
    body: 'A failure state machine requires consecutive failures before an incident opens. Single blips are recorded, not paged. One outage maps to exactly one incident, enforced by the database.',
  },
  {
    title: 'Tail latency, exposed',
    body: 'P50, P95 and P99 latency computed over explicit windows with bounded populations — the numbers averages hide are the ones you see first.',
  },
  {
    title: 'Alerts that fire once',
    body: 'HMAC-signed webhooks and optional email, with cooldown dedup per endpoint and event type. DOWN, RECOVERED and HIGH_LATENCY are first-class events.',
  },
  {
    title: 'Public status pages',
    body: 'Every organization gets a status page at /status/your-slug with live service states, 24-hour uptime and open incidents. Disable it any time.',
  },
  {
    title: 'Who monitors the monitor?',
    body: 'An independent checker-of-checkers polls the heartbeat endpoint from a separate failure domain and alerts when the platform itself goes dark.',
  },
];

const steps = [
  {
    title: 'Add an endpoint',
    body: 'URL, interval, timeout, expected status range. An SSRF guard blocks private and metadata targets before the first probe is ever sent.',
  },
  {
    title: 'Watch the signal',
    body: 'Live states, status-code distributions, latency trends and incident history — computed continuously, not on demand.',
  },
  {
    title: 'Get paged, once',
    body: 'When failures persist past the threshold, an incident opens and your webhook fires. When it recovers, you get the all-clear automatically.',
  },
];

// Live pipeline: a packet travels URL → probe → state machine → incident →
// page on a 5s loop; each station pulses as the work passes through it, the
// incident station flashes coral and the bell rings at the end.
const PIPELINE_PATH = 'M60,64 H860';
const STATIONS = [
  { x: 60, label: 'URL', pulseDelay: '0s' },
  { x: 260, label: 'PROBE', pulseDelay: '1.2s' },
  { x: 460, label: 'STATE MACHINE', pulseDelay: '2.45s' },
  { x: 660, label: 'INCIDENT', pulseDelay: '3.7s', hot: true },
  { x: 860, label: 'PAGE', pulseDelay: '4.75s' },
];

function Pipeline() {
  const svgRef = useRef<SVGSVGElement>(null);
  useEffect(() => {
    // SMIL animations ignore CSS reduced-motion — pause them explicitly.
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      svgRef.current?.pauseAnimations();
    }
  }, []);

  return (
    <svg ref={svgRef} className="pipeline-svg" viewBox="0 0 920 132" aria-label="Monitoring pipeline: from URL to page">
      {/* base track */}
      <line x1="60" y1="64" x2="860" y2="64" className="pl-track" />
      {/* marching flow dashes */}
      <line x1="60" y1="64" x2="860" y2="64" className="pl-flow" />

      {STATIONS.map((s) => (
        <g key={s.label}>
          <circle cx={s.x} cy="64" r="17" className={`pl-pulse${s.hot ? ' pl-pulse-hot' : ''}`} style={{ animationDelay: s.pulseDelay }} />
          <circle cx={s.x} cy="64" r="16" className="pl-node" />
          <text x={s.x} y="104" className={`pl-label${s.hot ? ' pl-label-hot' : ''}`}>
            {s.label}
          </text>
        </g>
      ))}

      {/* station glyphs */}
      {/* URL: address bar */}
      <g className="pl-glyph">
        <rect x="50" y="58" width="20" height="12" rx="2.5" />
        <line x1="50" y1="62" x2="70" y2="62" />
      </g>
      {/* PROBE: radar */}
      <g className="pl-glyph">
        <path d="M254 70 a8 8 0 0 1 12 -12" />
        <path d="M251 74 a12 12 0 0 1 18 -18" />
        <circle cx="262" cy="62" r="1.8" className="pl-glyph-fill" />
      </g>
      {/* STATE MACHINE: heartbeat */}
      <g className="pl-glyph">
        <polyline points="451,65 456,65 458.5,58 461.5,71 464,65 469,65" />
      </g>
      {/* INCIDENT: warning */}
      <g className="pl-glyph pl-glyph-hot">
        <path d="M660 55 l8 14 h-16 z" />
        <line x1="660" y1="60" x2="660" y2="64.5" />
        <circle cx="660" cy="67" r="0.9" className="pl-glyph-fill" />
      </g>
      {/* PAGE: bell */}
      <g className="pl-glyph pl-bell">
        <path d="M854 66 a6 6 0 0 1 12 0 z" />
        <line x1="852" y1="66" x2="868" y2="66" />
        <line x1="860" y1="57" x2="860" y2="55" />
        <circle cx="860" cy="70" r="1.6" className="pl-glyph-fill" />
      </g>

      {/* travelling packet + glow */}
      <circle r="13" className="pl-packet-glow">
        <animateMotion dur="5s" repeatCount="indefinite" path={PIPELINE_PATH} />
      </circle>
      <circle r="5.5" className="pl-packet">
        <animateMotion dur="5s" repeatCount="indefinite" path={PIPELINE_PATH} />
      </circle>
    </svg>
  );
}

const NAV_SECTIONS = [
  { id: 'capabilities', label: 'Capabilities' },
  { id: 'how', label: 'How it works' },
  { id: 'pricing', label: 'Pricing' },
  { id: 'faq', label: 'FAQ' },
];

// Closing-band ECG: one beat pattern tiled across the section width.
const CTA_ECG =
  'M0 20 H90 l10 -14 l12 26 l10 -12 H300 l10 -14 l12 26 l10 -12 H600 l10 -14 l12 26 l10 -12 H900 l10 -14 l12 26 l10 -12 H1200';

export function Landing() {
  const { me } = useAuth();
  const [active, setActive] = useState('');
  const [scrolled, setScrolled] = useState(false);

  // Scrollspy: highlight the nav link of the section in view.
  useEffect(() => {
    const sections = NAV_SECTIONS.map((s) => document.getElementById(s.id)).filter(
      (el): el is HTMLElement => el != null,
    );
    const io = new IntersectionObserver(
      (entries) => {
        entries.forEach((e) => {
          if (e.isIntersecting) setActive(e.target.id);
        });
      },
      { rootMargin: '-40% 0px -55% 0px' },
    );
    sections.forEach((s) => io.observe(s));
    return () => io.disconnect();
  }, []);

  // Scroll state: the floating pill tightens and gains shadow after lift-off.
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 24);
    onScroll();
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  return (
    <div className="landing">
      <div className={`nav-shell${scrolled ? ' nav-scrolled' : ''}`}>
        <header className="landing-nav">
          <Link to="/" className="landing-brand">
            <Logo size={28} wordmark />
          </Link>
          <nav className="landing-links">
            {NAV_SECTIONS.map((s) => (
              <a key={s.id} href={`#${s.id}`} className={active === s.id ? 'active' : undefined}>
                {s.label}
              </a>
            ))}
          </nav>
          <div className="landing-nav-actions">
            {me ? (
              <Link className="btn btn-primary" to="/app">
                Open dashboard
              </Link>
            ) : (
              <>
                <Link className="btn btn-ghost" to="/login">
                  Sign in
                </Link>
                <Link className="btn btn-primary" to="/signup">
                  Start free
                </Link>
              </>
            )}
          </div>
        </header>
      </div>

      <section className="hero">
        <div className="hero-copy">
          <div className="hero-eyebrow">Distributed health &amp; uptime monitoring</div>
          <h1 className="hero-title">
            <span className="hero-line">Know it&apos;s</span>
            <span className="hero-line">
              <span className="mark">
                down
                <svg className="mark-svg" viewBox="0 0 120 12" preserveAspectRatio="none" aria-hidden="true">
                  <path d="M2 8 Q 60 1 118 6" />
                </svg>
              </span>{' '}
              before
            </span>
            <span className="hero-line">your users do.</span>
          </h1>
          <p className="hero-sub">
            UptimeX probes your endpoints around the clock, separates transient blips from real
            incidents, and pages you exactly once — with the P99 latency averages hide.
          </p>
          <div className="hero-actions">
            <Link className="btn btn-cta btn-lg" to="/signup">
              Start monitoring free →
            </Link>
            <Link className="btn btn-lg" to="/status/acme-corp">
              See a live status page
            </Link>
          </div>
          <p className="hero-trust mono">No credit card · 2 free monitors · live in 60 seconds</p>
          <div className="hero-stats">
            <div className="hero-stat">
              <strong>10s</strong>
              <span>fastest checks</span>
            </div>
            <div className="hero-stat">
              <strong>1000+</strong>
              <span>endpoints per org</span>
            </div>
            <div className="hero-stat">
              <strong>P99</strong>
              <span>tail latency built-in</span>
            </div>
          </div>
        </div>

        <div className="scroll-cue" aria-hidden="true">
          <span className="mono">scroll</span>
          <i />
        </div>
      </section>

      <div className="marquee" aria-hidden="true">
        <div className="marquee-track">
          {[0, 1].map((dup) => (
            <div className="marquee-group" key={dup}>
              {[
                '1 OUTAGE = 1 INCIDENT',
                'P50 / P95 / P99 ANALYTICS',
                '10-SECOND CHECKS',
                'SSRF-GUARDED PROBES',
                'HMAC-SIGNED WEBHOOKS',
                'CHECKER-OF-CHECKERS',
                'AUTO-RESOLVING INCIDENTS',
              ].map((item) => (
                <span className="marquee-item" key={`${dup}-${item}`}>
                  {item} <i>→</i>
                </span>
              ))}
            </div>
          ))}
        </div>
      </div>

      <section id="capabilities" className="landing-section">
        <div className="section-label">
          <span className="section-label-num">01</span> CAPABILITIES
        </div>
        <h2>Built like infrastructure, priced like SaaS</h2>
        <p className="landing-section-sub">
          Everything below runs in the platform today — no roadmap promises.
        </p>
        <div className="cap-grid">
          {capabilities.map((c, i) => (
            <Reveal key={c.title} delay={i * 60} className="stretch">
              <div className="cap-card corners">
                <h3>{c.title}</h3>
                <p>{c.body}</p>
              </div>
            </Reveal>
          ))}
        </div>
      </section>

      <section id="how" className="landing-section how-section">
        <div className="section-label section-label-center">
          <span className="section-label-num">02</span> HOW IT WORKS
        </div>
        <h2>From URL to page in three steps</h2>
        <p className="landing-section-sub">
          A URL goes in, checks flow continuously, and the first persistent failure pages you — exactly once.
        </p>
        <Reveal>
          <div className="pipeline-card">
            <div className="pipeline-head">
              <span className="live-dot" />
              <span className="live-label">Live pipeline</span>
              <span className="pipeline-head-sub">url → probe → state machine → incident → page</span>
            </div>
            <div className="pipeline-scroll">
              <Pipeline />
            </div>
          </div>
        </Reveal>
        <div className="steps-grid">
          {steps.map((s, i) => (
            <Reveal key={s.title} delay={i * 80} className="stretch">
              <div className="step-card">
                <div className="step-num">{i + 1}</div>
                <h3>{s.title}</h3>
                <p>{s.body}</p>
              </div>
            </Reveal>
          ))}
        </div>
      </section>

      <section id="pricing" className="landing-section pricing-section">
        <div className="section-label section-label-center">
          <span className="section-label-num">03</span> PRICING
        </div>
        <h2>Simple pricing, serious monitoring</h2>
        <p className="landing-section-sub">Start free. Upgrade when your endpoints outnumber your patience.</p>
        <div className="pricing-grid">
          {plans.map((p, i) => (
            <Reveal key={p.id} delay={i * 80} className="stretch">
              <div className={`price-card${p.highlight ? ' price-card-hot' : ''}`}>
                {p.highlight && <div className="price-flag">Most popular</div>}
                <h3>{p.name}</h3>
                <div className="price-amount">
                  {p.price} <span>{p.period}</span>
                </div>
                <p className="price-tagline">{p.tagline}</p>
                <div className="price-meta">
                  <span>{p.id === 'free' ? '2' : p.id === 'pro' ? '50' : '250'} monitors</span>
                  <i />
                  <span>
                    {p.id === 'free' ? '60s' : p.id === 'pro' ? '30s' : '10s'} fastest checks
                  </span>
                  <i />
                  <span>
                    {p.id === 'free' ? '7' : p.id === 'pro' ? '30' : '90'}-day history
                  </span>
                </div>
                <ul>
                  {p.features.map((f) => (
                    <li key={f}>{f}</li>
                  ))}
                </ul>
                <Link
                  className={`btn price-cta ${p.highlight ? 'btn-primary' : ''}`}
                  to={me ? '/app/billing' : '/signup'}
                >
                  {p.cta}
                </Link>
              </div>
            </Reveal>
          ))}
        </div>
        <Reveal>
          <div className="pricing-includes">
            <span className="pricing-includes-title">Every plan ships with</span>
            <span>Public status pages</span>
            <i>·</i>
            <span>HMAC-signed webhooks</span>
            <i>·</i>
            <span>SSRF-guarded probes</span>
            <i>·</i>
            <span>Auto-resolving incidents</span>
            <i>·</i>
            <span>Checker-of-checkers watchdog</span>
          </div>
        </Reveal>
      </section>

      <section id="faq" className="landing-section">
        <div className="section-label">
          <span className="section-label-num">04</span> FAQ
        </div>
        <h2>Questions, answered</h2>
        <div className="faq-list">
          {[
            {
              q: 'How is UptimeX different from a cron pinger?',
              a: 'Consecutive-failure thresholds, one-incident-per-outage guarantees, percentile analytics over bounded populations, cooldown-deduplicated alerting and an external watchdog on the monitor itself — all enforced server-side, not bolted on.',
            },
            {
              q: 'What counts as an incident?',
              a: 'An endpoint must fail N checks in a row (default 3, configurable 1–20) before an incident opens. Transient failures are recorded in metrics but never page you. When a down endpoint succeeds again, the incident auto-resolves and a RECOVERED alert fires.',
            },
            {
              q: 'Can I monitor internal hosts?',
              a: 'The SSRF guard blocks private, link-local and cloud-metadata targets by default. Self-hosted operators can allow private targets explicitly for internal monitoring.',
            },
            {
              q: 'Is there a self-hosted option?',
              a: 'Yes — the entire platform is open infrastructure: one binary plus PostgreSQL (or SQLite), a React dashboard, healthchecked Docker Compose and CI. The SaaS layer (accounts, plans, status pages) is the same code with SAAS_MODE=true.',
            },
          ].map((item, i) => (
            <Reveal key={item.q} delay={i * 60}>
              <details>
                <summary>{item.q}</summary>
                <p>{item.a}</p>
              </details>
            </Reveal>
          ))}
        </div>
      </section>

      <Reveal>
        <section className="cta-band" aria-label="Get started">
          <span className="cta-corner c-tl" aria-hidden="true" />
          <span className="cta-corner c-tr" aria-hidden="true" />
          <span className="cta-corner c-bl" aria-hidden="true" />
          <span className="cta-corner c-br" aria-hidden="true" />
          <div className="cta-copy">
            <div className="cta-eyebrow mono">$ uptime start --free</div>
            <h2>Your endpoints are due a check.</h2>
            <p>Two monitors, zero credit card, sixty seconds to first signal.</p>
          </div>
          <div className="cta-action">
            <Link className="btn cta-btn btn-lg" to="/signup">
              Create your workspace →
            </Link>
            <span className="cta-note mono">no card · cancel anytime · ₹0 floor</span>
          </div>
          <svg className="cta-pulse" viewBox="0 0 1200 40" preserveAspectRatio="none" aria-hidden="true">
            <path className="cta-pulse-base" d={CTA_ECG} />
            <path className="cta-pulse-live" d={CTA_ECG} />
          </svg>
        </section>
      </Reveal>

      <footer className="landing-footer">
        <div className="footer-wordmark" aria-hidden="true">
          UptimeX
        </div>
        <div className="footer-grid">
          <div className="footer-brand">
            <Link to="/" className="landing-brand">
              <Logo size={26} wordmark />
            </Link>
            <p>Distributed health &amp; uptime monitoring. Know it&apos;s down before your users do.</p>
            <Link to="/status/acme-corp" className="footer-status">
              <span className="live-dot" />
              All systems operational
            </Link>
          </div>
          <div className="footer-col">
            <h4>Product</h4>
            <a href="#capabilities">Capabilities</a>
            <a href="#how">How it works</a>
            <a href="#pricing">Pricing</a>
            <Link to="/status/acme-corp">Status demo</Link>
          </div>
          <div className="footer-col">
            <h4>Resources</h4>
            <a href="https://github.com/Bilalyzr/UptimeX/blob/main/docs/API.md" target="_blank" rel="noreferrer">
              API reference ↗
            </a>
            <a
              href="https://github.com/Bilalyzr/UptimeX/blob/main/docs/ARCHITECTURE.md"
              target="_blank"
              rel="noreferrer"
            >
              Architecture ↗
            </a>
            <a
              href="https://github.com/Bilalyzr/UptimeX/blob/main/docs/BENCHMARKS.md"
              target="_blank"
              rel="noreferrer"
            >
              Benchmarks ↗
            </a>
            <a
              href="https://github.com/Bilalyzr/UptimeX/blob/main/docs/SECURITY.md"
              target="_blank"
              rel="noreferrer"
            >
              Security ↗
            </a>
          </div>
          <div className="footer-col">
            <h4>Account</h4>
            <Link to="/login">Sign in</Link>
            <Link to="/signup">Create workspace</Link>
            <Link to="/app">Dashboard</Link>
          </div>
        </div>
        <div className="footer-bottom">
          <span>© 2026 UptimeX — uptime monitoring that pages you once</span>
          <a href="https://github.com/Bilalyzr/UptimeX" target="_blank" rel="noreferrer">
            GitHub ↗
          </a>
        </div>
      </footer>
    </div>
  );
}
