import {
  Area,
  CartesianGrid,
  ComposedChart,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import type { Distribution, TrendPoint } from '../types';
import { DISTRIBUTION_COLORS, DISTRIBUTION_LABELS, DISTRIBUTION_ORDER } from '../format';

// Light-theme chart palette (kept in sync with styles.css tokens).
const CHART = {
  avg: '#0e8f74', // teal (mint family)
  p95: '#fc6756', // coral
  p99: '#724ce8', // violet
  grid: '#dcdcd0', // warm hairline
  axis: '#6e6e68', // warm gray
};

// LatencyTrend renders avg/p95/p99 over time buckets. Clarity first: thin
// lines, tabular axes, tooltips with exact values (PRD §24). The avg series
// carries a soft gradient fill to anchor the trend at a glance.
export function LatencyTrend({ trend }: { trend: TrendPoint[] }) {
  const data = trend.map((p) => ({
    ...p,
    timeLabel: new Date(p.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
  }));
  return (
    <div data-testid="latency-trend">
      <div className="latency-legend" style={{ marginBottom: 8 }}>
        <span>
          <span className="dot" style={{ background: CHART.avg }} />
          avg
        </span>
        <span>
          <span className="dot" style={{ background: CHART.p95 }} />
          p95
        </span>
        <span>
          <span className="dot" style={{ background: CHART.p99 }} />
          p99
        </span>
      </div>
      <ResponsiveContainer width="100%" height={220}>
        <ComposedChart data={data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="avgFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={CHART.avg} stopOpacity={0.22} />
              <stop offset="100%" stopColor={CHART.avg} stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke={CHART.grid} strokeDasharray="4 4" vertical={false} />
          <XAxis dataKey="timeLabel" stroke={CHART.axis} fontSize={11} tickLine={false} axisLine={false} dy={6} />
          <YAxis
            stroke={CHART.axis}
            fontSize={11}
            tickLine={false}
            axisLine={false}
            width={54}
            tickFormatter={(v: number) => `${v}ms`}
          />
          <Tooltip
            contentStyle={{
              background: '#fafaf6',
              border: '1px solid #353539',
              borderRadius: 12,
              fontSize: 12,
              boxShadow: '0 8px 24px rgba(10, 9, 15, 0.12)',
              padding: '8px 12px',
            }}
            labelStyle={{ color: '#0a090f', fontWeight: 600, marginBottom: 4 }}
            itemStyle={{ color: '#6e6e68' }}
            cursor={{ stroke: '#858580', strokeDasharray: '4 4' }}
            formatter={(value: number | string, name) => [`${Number(value).toFixed(1)}ms`, name]}
          />
          <Area
            type="monotone"
            dataKey="avg_ms"
            name="avg"
            stroke="none"
            fill="url(#avgFill)"
            isAnimationActive={false}
          />
          <Line type="monotone" dataKey="avg_ms" name="avg" stroke={CHART.avg} strokeWidth={2.5} dot={false} activeDot={{ r: 4 }} />
          <Line type="monotone" dataKey="p95_ms" name="p95" stroke={CHART.p95} strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
          <Line type="monotone" dataKey="p99_ms" name="p99" stroke={CHART.p99} strokeWidth={2} strokeDasharray="5 3" dot={false} activeDot={{ r: 4 }} />
        </ComposedChart>
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
