import { useCallback, useEffect, useRef, useState } from 'react';

// usePolling fetches a resource on an interval with loading/error state and
// cleanup. The fetcher is kept in a ref so re-renders never restart the loop.
export function usePolling<T>(fetcher: () => Promise<T>, intervalMs: number) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const intervalRef = useRef(intervalMs);
  intervalRef.current = intervalMs;

  const tick = useCallback(async () => {
    try {
      const result = await fetcherRef.current();
      setData(result);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void tick();
    const id = window.setInterval(() => void tick(), intervalRef.current);
    return () => window.clearInterval(id);
  }, [tick, intervalMs]);

  return { data, error, loading, refresh: tick };
}
