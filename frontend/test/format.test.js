import { test } from 'node:test';
import assert from 'node:assert/strict';
import { messages } from '../src/lib/i18n.js';
import {
  albumArtists,
  plural,
  tracksCount,
  releasesCount,
  longDuration,
  year,
  shortDate,
  compactNumber,
  fansLabel,
  recordCategory,
  recordTypeLabel,
  wrappedSeasonYear,
} from '../src/lib/format.js';

test('russian plurals', () => {
  const forms = ['трек', 'трека', 'треков'];
  const cases = { 1: 'трек', 2: 'трека', 4: 'трека', 5: 'треков', 11: 'треков', 12: 'треков', 14: 'треков', 21: 'трек', 22: 'трека', 25: 'треков', 101: 'трек', 111: 'треков' };
  for (const [n, want] of Object.entries(cases)) assert.equal(plural(Number(n), forms), want, n);
  assert.equal(tracksCount(13), '13 треков');
  assert.equal(releasesCount(1), '1 релиз');
  assert.equal(releasesCount(3), '3 релиза');
  assert.equal(releasesCount(11), '11 релизов');
});

test('album duration', () => {
  assert.equal(longDuration(4464), '1 ч 14 мин');
  assert.equal(longDuration(2232), '37 мин 12 сек');
});

test('album artists: every contributor of a joint release, else the main artist', () => {
  const joint = { artist_name: 'Left', contributors: [{ id: '30', name: 'Left' }, { id: '31', name: 'Right' }] };
  assert.equal(albumArtists(joint), 'Left, Right');
  assert.equal(albumArtists({ artist_name: 'Solo' }), 'Solo');
  assert.equal(albumArtists({ artist_name: 'Solo', contributors: [] }), 'Solo');
  assert.equal(albumArtists({}), '');
});

test('year ignores Deezer placeholder dates', () => {
  assert.equal(year('2013-05-17'), '2013');
  assert.equal(year('0000-00-00'), '');
  assert.equal(year(undefined), '');
});

test('date a song was added', () => {
  assert.match(shortDate('2026-08-14T12:00:00Z'), /^14 авг\. 2026/);
  assert.equal(shortDate('2026-08-14T12:00:00Z', 'en'), 'Aug 14, 2026');
  assert.equal(shortDate(null), '');
});

test('compact numbers and fans', () => {
  assert.equal(compactNumber(5219978), '5,2 млн');
  assert.equal(compactNumber(2000000), '2 млн');
  assert.equal(compactNumber(886377), '886 тыс.');
  assert.equal(fansLabel(3), '3 поклонника на Deezer');
  assert.equal(fansLabel(0), '');
});

test('release categories for filters', () => {
  assert.equal(recordCategory('compile'), 'album');
  assert.equal(recordCategory('ep'), 'ep');
  assert.equal(recordCategory('single'), 'single');
  assert.equal(recordCategory(undefined), 'album');
  assert.equal(recordTypeLabel('single'), 'Сингл');
});

test('english formatting', () => {
  assert.equal(tracksCount(1, 'en'), '1 song');
  assert.equal(tracksCount(13, 'en'), '13 songs');
  assert.equal(releasesCount(1, 'en'), '1 release');
  assert.equal(releasesCount(2, 'en'), '2 releases');
  assert.equal(longDuration(4464, 'en'), '1 hr 14 min');
  assert.equal(compactNumber(5219978, 'en'), '5.2M');
  assert.equal(fansLabel(886377, 'en'), '886K fans on Deezer');
  assert.equal(recordTypeLabel('single', 'en'), 'Single');
});

test('every russian string has an english translation and vice versa', () => {
  const ru = Object.keys(messages.ru).sort();
  const en = Object.keys(messages.en).sort();
  assert.deepEqual(ru.filter((k) => !en.includes(k)), [], 'нет перевода на английский');
  assert.deepEqual(en.filter((k) => !ru.includes(k)), [], 'лишние английские ключи');
});

test('wrappedSeasonYear: Wrapped on the home page only from 1 December to 15 January', () => {
  assert.equal(wrappedSeasonYear(new Date(2026, 10, 30)), null);
  assert.equal(wrappedSeasonYear(new Date(2026, 11, 1)), 2026);
  assert.equal(wrappedSeasonYear(new Date(2026, 11, 31, 23, 59)), 2026);
  assert.equal(wrappedSeasonYear(new Date(2027, 0, 1)), 2026);
  assert.equal(wrappedSeasonYear(new Date(2027, 0, 15, 22)), 2026);
  assert.equal(wrappedSeasonYear(new Date(2027, 0, 16)), null);
  assert.equal(wrappedSeasonYear(new Date(2026, 6, 10)), null);
});
