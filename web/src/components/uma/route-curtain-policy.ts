/** Timed games and live rooms must become usable immediately after joining. */
export function canAnimateRoute(current: string, next: string): boolean {
  if (current === next) return false;
  if (/^\/rooms\/(?!new(?:\/|$))[^/]+/.test(next) || /^\/challenges\//.test(next)) return false;
  if (/^\/events\/(?!new(?:\/|$))[^/]+/.test(next)) return false;
  return !/^\/practice\//.test(next) || /^\/practice\/local-[^/]+$/.test(next);
}
