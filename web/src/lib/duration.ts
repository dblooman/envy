// Installation durations use Go's time.Duration syntax (for example, 8h0m0s).
export function durationSeconds(value: string): number | null {
  const units: Record<string, number> = {
    h: 3600,
    m: 60,
    s: 1,
    ms: 0.001,
    us: 0.000001,
    µs: 0.000001,
    μs: 0.000001,
    ns: 0.000000001,
  };
  const parts = [
    ...value.matchAll(/(\d+(?:\.\d+)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/g),
  ];
  if (!parts.length || parts.map((p) => p[0]).join("") !== value) return null;
  const seconds = parts.reduce(
    (total, part) => total + Number(part[1]) * units[part[2]],
    0,
  );
  return seconds > 0 && Number.isFinite(seconds) ? seconds : null;
}
export function lifetimeOptions(maximum?: string) {
  const max = maximum ? durationSeconds(maximum) : null;
  const values = ["1h", "4h", "8h", "24h", ...(maximum ? [maximum] : [])];
  const seen = new Set<number>();
  return values
    .filter((value) => {
      const seconds = durationSeconds(value);
      if (!seconds || (max !== null && seconds > max) || seen.has(seconds))
        return false;
      seen.add(seconds);
      return true;
    })
    .sort((a, b) => durationSeconds(a)! - durationSeconds(b)!);
}
