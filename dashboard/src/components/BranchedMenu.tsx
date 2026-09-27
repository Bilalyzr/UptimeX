// Branched menu navigation — adapted from ReactBits "Branched Menu"
// (reactbits.dev/micro/branched-menu): a trunk line on the left, collapsible
// section heads, and child items that branch off the trunk on curved SVG
// lines. The active item's branch draws itself in the accent color and a
// marker glides along the trunk. Reworked for react-router (values are
// paths; active state comes from the router) and UptimeX design tokens.
import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';

export interface BranchedChild {
  value: string; // route path
  label: string;
  icon?: ReactNode;
}

export interface BranchedSection {
  label: string;
  children: BranchedChild[];
}

const PAD = 6;
const MARK = 16;
const TRUNK = 2;
const INDENT = 40;

// '/app' must not prefix-match every route; deeper paths match by prefix.
const isActivePath = (active: string, value: string) =>
  value === '/app' ? active === '/app' : active === value || active.startsWith(`${value}/`);

export function BranchedMenu({
  sections,
  activePath,
  defaultOpen = [0, 1],
}: {
  sections: BranchedSection[];
  activePath: string;
  defaultOpen?: number[];
}) {
  const [open, setOpen] = useState<Set<number>>(() => new Set(defaultOpen));
  const navRef = useRef<HTMLElement | null>(null);
  const heads = useRef<Array<HTMLButtonElement | null>>([]);
  const markerRef = useRef<HTMLSpanElement | null>(null);
  const navigate = useNavigate();

  const activeSection = sections.findIndex((s) => s.children.some((k) => isActivePath(activePath, k.value)));
  const markerShown = activeSection >= 0 && open.has(activeSection);

  // Glide the trunk marker to the active section head.
  useLayoutEffect(() => {
    const place = (glide: boolean) => {
      const m = markerRef.current;
      const el = heads.current[activeSection];
      if (!m) return;
      const on = markerShown && el;
      if (!glide) m.style.transition = 'none';
      if (on) m.style.top = `${el.offsetTop + (el.offsetHeight - MARK) / 2}px`;
      m.toggleAttribute('data-on', Boolean(on));
      if (!glide) {
        void m.offsetHeight;
        m.style.transition = '';
      }
    };
    place(true);
    let first = true;
    const ro = new ResizeObserver(() => {
      if (first) {
        first = false;
        return;
      }
      place(false);
    });
    if (navRef.current) ro.observe(navRef.current);
    return () => ro.disconnect();
  }, [activeSection, markerShown, sections]);

  const toggle = (i: number) => {
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });
  };

  const rowY = (k: number) => PAD + k * 32 + 32 / 2;
  const r = Math.min(8, 32 / 2 - 2);
  const endX = INDENT - 8;
  const branch = (k: number) => `M ${TRUNK} ${rowY(k) - r} A ${r} ${r} 0 0 0 ${TRUNK + r} ${rowY(k)} H ${endX}`;
  const reach = (k: number) =>
    `M ${TRUNK} 0 V ${rowY(k) - r} A ${r} ${r} 0 0 0 ${TRUNK + r} ${rowY(k)} H ${endX}`;
  const length = (k: number) => rowY(k) - r + (Math.PI * r) / 2 + (endX - TRUNK - r);

  return (
    <nav ref={navRef} className="branched-menu" aria-label="Dashboard sections">
      <span ref={markerRef} className="branched-menu__marker" aria-hidden="true" />
      {sections.map((item, i) => {
        const kids = item.children;
        const isOpen = open.has(i);
        const bodyH = PAD * 2 + kids.length * 32;
        return (
          <div key={item.label} className="branched-menu__section" data-open={isOpen ? '' : undefined}>
            <button
              ref={(el) => {
                heads.current[i] = el;
              }}
              type="button"
              className="branched-menu__head"
              aria-expanded={isOpen}
              onClick={() => toggle(i)}
            >
              {item.label}
            </button>
            <div className="branched-menu__body">
              <div className="branched-menu__fold">
                <div className="branched-menu__tree" style={{ height: bodyH }}>
                  <svg className="branched-menu__lines" width={INDENT} height={bodyH} aria-hidden="true">
                    <path className="branched-menu__base" d={`M ${TRUNK} 0 V ${rowY(kids.length - 1) - r}`} />
                    {kids.map((kid, k) => (
                      <path key={kid.value} className="branched-menu__base" d={branch(k)} />
                    ))}
                    {kids.map((kid, k) => (
                      <path
                        key={kid.value}
                        className="branched-menu__reach"
                        d={reach(k)}
                        style={{
                          strokeDasharray: length(k),
                          strokeDashoffset: isActivePath(activePath, kid.value) ? 0 : length(k),
                        }}
                      />
                    ))}
                  </svg>
                  {kids.map((kid) => {
                    const active = isActivePath(activePath, kid.value);
                    return (
                      <button
                        key={kid.value}
                        type="button"
                        className="branched-menu__item"
                        aria-current={active ? 'page' : undefined}
                        data-active={active ? '' : undefined}
                        tabIndex={isOpen ? 0 : -1}
                        onClick={() => navigate(kid.value)}
                      >
                        {kid.icon ? (
                          <span className="branched-menu__icon" aria-hidden="true">
                            {kid.icon}
                          </span>
                        ) : null}
                        <span className="branched-menu__label">{kid.label}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            </div>
          </div>
        );
      })}
    </nav>
  );
}
