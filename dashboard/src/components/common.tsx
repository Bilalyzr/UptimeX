import type { ReactNode } from 'react';

export function StatCard(props: { label: string; value: ReactNode; hint?: string; color?: string }) {
  return (
    <div className="card stat-card">
      <div className="label">{props.label}</div>
      <div className="value" style={props.color ? { color: props.color } : undefined}>
        {props.value}
      </div>
      {props.hint ? <div className="hint">{props.hint}</div> : null}
    </div>
  );
}

export function Badge({ state }: { state: string }) {
  return <span className={`badge badge-${state}`}>{state}</span>;
}

export function Loading({ label = 'Loading…', skeleton = true }: { label?: string; skeleton?: boolean }) {
  if (skeleton) {
    return <div className="loading skeleton" aria-label={label} role="status" />;
  }
  return <div className="loading muted">{label}</div>;
}

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="empty">
      <h3>{title}</h3>
      {hint ? <p className="muted">{hint}</p> : null}
    </div>
  );
}

export function ErrorBanner({ message }: { message: string }) {
  return <div className="error-banner">Failed to load data: {message}</div>;
}

export function WindowPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <label className="row" style={{ gap: 6 }}>
      <span className="muted" style={{ fontSize: 12 }}>
        Window
      </span>
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="1h">1 hour</option>
        <option value="6h">6 hours</option>
        <option value="24h">24 hours</option>
        <option value="7d">7 days</option>
      </select>
    </label>
  );
}
