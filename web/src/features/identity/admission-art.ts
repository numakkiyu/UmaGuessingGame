const pairs = [["riceshower", "mihonobourbon"], ["biwahayahide", "naritataishin"]] as const;
let previousPair = -1;
export function nextAdmissionPair() {
  previousPair = previousPair < 0 ? Math.floor(Math.random() * pairs.length) : (previousPair + 1) % pairs.length;
  return pairs[previousPair]!;
}
