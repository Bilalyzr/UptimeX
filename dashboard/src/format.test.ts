import { describe, expect, it } from 'vitest';
import { formatAge, formatDuration, formatMs, formatPct } from './format';

describe('formatDuration', () => {
  it('renders milliseconds below one second', () => {
    expect(formatDuration(42)).toBe('42ms');
    expect(formatDuration(999)).toBe('999ms');
  });
  it('renders seconds with one decimal', () => {
    expect(formatDuration(1500)).toBe('1.5s');
    expect(formatDuration(59_400)).toBe('59.4s');
  });
  it('renders minutes and hours and days', () => {
    expect(formatDuration(65_000)).toBe('1m 5s');
    expect(formatDuration(3_600_000)).toBe('1h 0m');
    expect(formatDuration(90_000_000)).toBe('1d 1h');
  });
  it('handles null and undefined', () => {
    expect(formatDuration(null)).toBe('—');
    expect(formatDuration(undefined)).toBe('—');
  });
});

describe('formatMs', () => {
  it('renders sub-second latencies rounded', () => {
    expect(formatMs(123.4)).toBe('123ms');
  });
  it('renders second-scale latencies', () => {
    expect(formatMs(2450)).toBe('2.45s');
  });
  it('handles null', () => {
    expect(formatMs(null)).toBe('—');
  });
});

describe('formatPct', () => {
  it('formats with fixed digits', () => {
    expect(formatPct(99.2134)).toBe('99.21%');
    expect(formatPct(100, 1)).toBe('100.0%');
  });
  it('handles null', () => {
    expect(formatPct(null)).toBe('—');
  });
});

describe('formatAge', () => {
  it('returns never for empty input', () => {
    expect(formatAge(null)).toBe('never');
  });
  it('returns just now for fresh timestamps', () => {
    expect(formatAge(new Date().toISOString())).toBe('just now');
  });
  it('renders elapsed time', () => {
    const twoMinAgo = new Date(Date.now() - 120_000).toISOString();
    expect(formatAge(twoMinAgo)).toBe('2m 0s ago');
  });
});
