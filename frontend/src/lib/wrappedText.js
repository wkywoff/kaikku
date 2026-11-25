import { plural } from './format.js';

export const WRAPPED_TINT = { r: 155, g: 31, b: 232 };

const FORMS = {
  plays: { ru: ['прослушивание', 'прослушивания', 'прослушиваний'], en: ['play', 'plays'] },
  minutes: { ru: ['минута', 'минуты', 'минут'], en: ['minute', 'minutes'] },
  times: { ru: ['раз', 'раза', 'раз'], en: ['time', 'times'] },
  tracks: { ru: ['разный трек', 'разных трека', 'разных треков'], en: ['different song', 'different songs'] },
  songs: { ru: ['трек', 'трека', 'треков'], en: ['song', 'songs'] },
  artists: { ru: ['исполнитель', 'исполнителя', 'исполнителей'], en: ['artist', 'artists'] },
  albums: { ru: ['альбом', 'альбома', 'альбомов'], en: ['album', 'albums'] },
  days: { ru: ['день подряд с музыкой', 'дня подряд с музыкой', 'дней подряд с музыкой'], en: ['day in a row with music', 'days in a row with music'] },
};

export const num = (n, lang) => (n || 0).toLocaleString(lang === 'en' ? 'en-US' : 'ru-RU');
export const word = (n, key, lang) => (lang === 'en' ? FORMS[key].en[n === 1 ? 0 : 1] : plural(n, FORMS[key].ru));
export const count = (n, key, lang) => `${num(n, lang)} ${word(n, key, lang)}`;
