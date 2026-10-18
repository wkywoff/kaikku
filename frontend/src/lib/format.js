// Подписи для экранов: тип релиза, длительность альбома, склонения. Все
// функции с текстом принимают язык ('ru' | 'en').
import { translate } from './i18n.js';

export function recordTypeLabel(type, lang = 'ru') {
  return translate(lang, `type.${['album', 'ep', 'single', 'compile'].includes(type) ? type : 'album'}`);
}

/** Категория для фильтров: сборники считаются альбомами. */
export function recordCategory(type) {
  if (type === 'ep') return 'ep';
  if (type === 'single') return 'single';
  return 'album';
}

/** plural(5, ['трек', 'трека', 'треков']) → 'треков' */
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

/** 4464 → '1 ч 14 мин' / '1 hr 14 min', 2232 → '37 мин 12 сек' / '37 min 12 sec' */
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

/** 5219978 → '5,2 млн' / '5.2M', 886377 → '886 тыс.' / '886K' */
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

/** '2013-05-17' → '17 мая 2013 г.' / 'May 17, 2013' */
export function longDate(date, lang = 'ru') {
  if (!year(date)) return '';
  const d = new Date(`${date}T00:00:00`);
  return d.toLocaleDateString(lang === 'en' ? 'en-US' : 'ru-RU', { day: 'numeric', month: 'long', year: 'numeric' });
}

/**
 * Год, итоги которого показываются на главной, — только в сезон, как у
 * Spotify: с 1 декабря (итоги этого года) по 15 января (прошлого). В
 * остальное время — null: итоги открываются из меню аккаунта.
 */
export function wrappedSeasonYear(now = new Date()) {
  const y = now.getFullYear();
  if (now.getMonth() === 11) return y;
  if (now.getMonth() === 0 && now.getDate() <= 15) return y - 1;
  return null;
}
