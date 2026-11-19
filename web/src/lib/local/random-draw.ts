// Crypto entropy mixed with the draw time; rejection sampling avoids modulo bias.
export function shufflePractice<T>(values: readonly T[], now: number): T[] {
  const out = [...values];
  for (let i = out.length - 1; i > 0; i--) {
    const limit = i + 1, bound = Math.floor(0x100000000 / limit) * limit; let value: number;
    do { value = (crypto.getRandomValues(new Uint32Array(1))[0]! ^ (now >>> 0)) >>> 0; } while (value >= bound);
    const j = value % limit; [out[i], out[j]] = [out[j]!, out[i]!];
  }
  return out;
}
