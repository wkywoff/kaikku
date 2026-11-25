const UPPER = new Set(['edm', 'idm', 'ebm', 'dnb', 'r&b', 'uk', 'usa', 'ost']);

export function tagTitle(tag) {
  return String(tag || '')
    .split(/([\s-]+)/)
    .map((w) => (UPPER.has(w) ? w.toUpperCase() : w.charAt(0).toUpperCase() + w.slice(1)))
    .join('');
}

export function tagSlug(tag) {
  return String(tag || '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

export function mixTitle(mix, t) {
  if (mix.kind === 'on_repeat') return t('mix.onRepeat');
  if (mix.kind === 'forgotten') return t('mix.forgotten');
  return t('mix.tag', { tag: tagTitle(mix.tag) });
}

export function mixSubtitle(mix, t) {
  const names = mix.artists || [];
  return names.length ? t('mix.artists', { names: names.join(', ') }) : t('mix.kind');
}

export function mixDescription(mix, t) {
  if (mix.kind === 'on_repeat') return t('mix.onRepeatText');
  if (mix.kind === 'forgotten') return t('mix.forgottenText');
  return t('mix.tagText', { tag: tagTitle(mix.tag), names: (mix.artists || []).join(', ') });
}

export function mixHue(id) {
  let h = 0;
  for (const c of String(id)) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  if (id === 'on-repeat') return 340;
  if (id === 'forgotten') return 200;
  return h % 360;
}

export function mixColor(id) {
  const h = mixHue(id) / 360;
  const s = 0.55;
  const l = 0.45;
  const f = (n) => {
    const k = (n + h * 12) % 12;
    const a = s * Math.min(l, 1 - l);
    return Math.round(255 * (l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))));
  };
  return { r: f(0), g: f(8), b: f(4) };
}
