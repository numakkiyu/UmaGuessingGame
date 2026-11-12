/** Moderate RMS matching with a hard peak ceiling; never amplify silence/noise. */
export function audioPlaybackGain(channels: readonly Float32Array[]): number {
  let energy = 0, peak = 0, samples = 0;
  for (const channel of channels) for (const sample of channel) {
    if (!Number.isFinite(sample)) return 0;
    energy += sample * sample; peak = Math.max(peak, Math.abs(sample)); samples++;
  }
  if (!samples || peak < 0.0001) return 1;
  const rms = Math.sqrt(energy / samples);
  return Math.min(3, 0.12 / Math.max(rms, 0.02), 0.85 / peak);
}
