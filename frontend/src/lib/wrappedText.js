import { plural } from './format.js';

// Числа и склонения для итогов года (WrappedPage, WrappedAlbumsPage):
// «1 прослушивание», «2 раза», «5 минут».
const FORMS = {
  plays: { ru: ['прослушивание', 'прослушивания', 'прослушиваний'], en: ['play', 'plays'] },
  minutes: { ru: ['минута', 'минуты', 'минут'], en: ['minute', 'minutes'] },
  times: { ru: ['раз', 'раза', 'раз'], en: ['time', 'times'] },
  tracks: { ru: ['разный трек', 'разных трека', 'разных треков'], en: ['different song', 'different songs'] },
  artists: { ru: ['исполнитель', 'исполнителя', 'исполнителей'], en: ['artist', 'artists'] },
  albums: { ru: ['альбом', 'альбома', 'альбомов'], en: ['album', 'albums'] },
  days: { ru: ['день подряд с музыкой', 'дня подряд с музыкой', 'дней подряд с музыкой'], en: ['day in a row with music', 'days in a row with music'] },
};

/** 12345 → «12 345» (ru) или «12,345» (en). */
export const num = (n, lang) => (n || 0).toLocaleString(lang === 'en' ? 'en-US' : 'ru-RU');
/** Слово в нужной форме: word(2, 'plays', 'ru') → «прослушивания». */
export const word = (n, key, lang) => (lang === 'en' ? FORMS[key].en[n === 1 ? 0 : 1] : plural(n, FORMS[key].ru));
/** Число со словом: count(213, 'plays', 'en') → «213 plays». */
export const count = (n, key, lang) => `${num(n, lang)} ${word(n, key, lang)}`;
