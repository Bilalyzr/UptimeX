import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import type { Distribution, TrendPoint } from '../types';
import { DISTRIBUTION_COLORS, DISTRIBUTION_LABELS, DISTRIBUTION_ORDER } from '../format';

// LatencyTrend renders avg/p95/p99 over time buckets. Clarity first: thin
// lines, tabular axes, tooltips with exact values (PRD §24).
export function LatencyTrend({ trend }: { trend: TrendPoint[] }) {
  const data = trend.map((p) => ({
    ...p,
    timeLabel: new Date(p.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
  }));
  return (
    <div data-testid="latency-trend">
      <div className="latency-legend" style={{ marginBottom: 8 }}>
        <span>
          <span className="dot" style={{ background: '#38bdf8' }} />
          avg
        </span>
        <span>
          <span className="dot" style={{ background: '#f59e0b' }} />
          p95
        </span>
        <span>
          <span className="dot" style={{ background: '#ef4444' }} />
          p99
        </span>
      </div>
      <ResponsiveContainer width="100%" height={220}>
        <LineChart data={data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="#22304f" strokeDasharray="3 3" vertical={false} />
          <XAxis dataKey="timeLabel" stroke="#8ea0bd" fontSize={11} tickLine={false} />
          <YAxis
            stroke="#8ea0bd"
            fontSize={11}
            tickLine={false}
            width={54}
            tickFormatter={(v: number) => `${v}ms`}
          />
          <Tooltip
            contentStyle={{
              background: '#111a2e',
              border: '1px solid #22304f',
              borderRadius: 8,
              fontSize: 12,
            }}
            labelStyle={{ color: '#e2e8f0' }}
            formatter={(value: number | string, name) => [`${Number(value).toFixed(1)}ms`, name]}
          />
          <Line type="monotone" dataKey="avg_ms" name="avg" stroke="#38bdf8" strokeWidth={1.5} dot={false} />
          <Line type="monotone" dataKey="p95_ms" name="p95" stroke="#f59e0b" strokeWidth={1.5} dot={false} />
          <Line type="monotone" dataKey="p99_ms" name="p99" stroke="#ef4444" strokeWidth={1.5} dot={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

// DistributionBars renders the HTTP status-code distribution with counts
// and proportional bars; 200/201 exact plus families and error classes.
export function DistributionBars({ distribution }: { distribution: Distribution }) {
  const total = Object.values(distribution).reduce((a, b) => a + b, 0);
  if (total === 0) {
    return <div className="muted">No observations in this window yet.</div>;
  }
  const keys = Object.keys(distribution).sort(
    (a, b) => DISTRIBUTION_ORDER.indexOf(a) - DISTRIBUTION_ORDER.indexOf(b),
  );
  return (
    <div data-testid="distribution">
      {keys.map((k) => {
        const count = distribution[k];
        const pct = (count / total) * 100;
        return (
          <div className="dist-row" key={k}>
            <span className="dist-label">{DISTRIBUTION_LABELS[k] ?? k}</span>
            <div className="dist-bar-bg">
              <div
                className="dist-bar"
                style={{ width: `${Math.max(pct, 1.2)}%`, background: DISTRIBUTION_COLORS[k] ?? '#64748b' }}
              />
            </div>
            <span className="dist-count">
              {count} · {pct.toFixed(1)}%
            </span>
          </div>
        );
      })}
    </div>
  );
}
