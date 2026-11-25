import { translate } from './i18n.js';

export function recordTypeLabel(type, lang = 'ru') {
  return translate(lang, `type.${['album', 'ep', 'single', 'compile'].includes(type) ? type : 'album'}`);
}

export function albumArtists(album) {
  return album.contributors?.length ? album.contributors.map((a) => a.name).join(', ') : album.artist_name || '';
}

export function recordCategory(type) {
  if (type === 'ep') return 'ep';
  if (type === 'single') return 'single';
  return 'album';
}

export function plural(n, [one, few, many]) {
  const mod10 = n % 10;
  const mod100 = n % 100;
  if (mod10 === 1 && mod100 !== 11) return one;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return few;
  return many;
}

export function tracksCount(n, lang = 'ru') {
  if (lang === 'en') return `${n} ${n === 1 ? 'song' : 'songs'}`;
  return `${n} ${plural(n, ['трек', 'трека', 'треков'])}`;
}

export function longDuration(totalSec, lang = 'ru') {
  const s = Math.round(totalSec || 0);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const [hr, min, sec] = lang === 'en' ? ['hr', 'min', 'sec'] : ['ч', 'мин', 'сек'];
  if (h > 0) return `${h} ${hr} ${m} ${min}`;
  return `${m} ${min} ${s % 60} ${sec}`;
}

export function year(date) {
  return date && /^\d{4}/.test(date) && !date.startsWith('0000') ? date.slice(0, 4) : '';
}

export function compactNumber(n, lang = 'ru') {
  if (lang === 'en') {
    if (n >= 1e6) return `${(n / 1e6).toFixed(1).replace('.0', '')}M`;
    if (n >= 1e3) return `${Math.round(n / 1e3)}K`;
    return String(n);
  }
  if (n >= 1e6) return `${(n / 1e6).toFixed(1).replace('.0', '').replace('.', ',')} млн`;
  if (n >= 1e3) return `${Math.round(n / 1e3)} тыс.`;
  return String(n);
}

export function fansLabel(n, lang = 'ru') {
  if (!n) return '';
  if (lang === 'en') return `${compactNumber(n, lang)} ${n === 1 ? 'fan' : 'fans'} on Deezer`;
  const word = n >= 1e3 ? 'поклонников' : plural(n, ['поклонник', 'поклонника', 'поклонников']);
  return `${compactNumber(n, lang)} ${word} на Deezer`;
}

export function longDate(date, lang = 'ru') {
  if (!year(date)) return '';
  const d = new Date(`${date}T00:00:00`);
  return d.toLocaleDateString(lang === 'en' ? 'en-US' : 'ru-RU', { day: 'numeric', month: 'long', year: 'numeric' });
}

export function wrappedSeasonYear(now = new Date()) {
  const y = now.getFullYear();
  if (now.getMonth() === 11) return y;
  if (now.getMonth() === 0 && now.getDate() <= 15) return y - 1;
  return null;
}
