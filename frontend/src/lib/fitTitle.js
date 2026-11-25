export function fitFontSize(avail, unit, min, max) {
  if (!(avail > 0) || !(unit > 0)) return min;
  return Math.max(min, Math.min(max, Math.floor(avail / unit)));
}
