import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseRoute, paths } from '../src/lib/route.js';

test('every path builder round-trips through parseRoute', () => {
  const cases = [
    [paths.home(), { view: 'home' }],
    [paths.search('drake'), { view: 'search', q: 'drake' }],
    [paths.settings(), { view: 'settings' }],
    [paths.liked(), { view: 'liked' }],
    [paths.playlist('7f1c2b'), { view: 'playlist', id: '7f1c2b' }],
    [paths.search(''), { view: 'search', q: '' }],
    [paths.search('касания болезненны'), { view: 'search', q: 'касания болезненны' }],
    [paths.artist('27'), { view: 'artist', id: '27' }],
    [paths.artist('27', 'album'), { view: 'artist', id: '27', disc: 'album' }],
    [paths.discography('27'), { view: 'discography', id: '27', filter: 'all' }],
    [paths.discography('27', 'single'), { view: 'discography', id: '27', filter: 'single' }],
    [paths.discography('27', 'ep'), { view: 'discography', id: '27', filter: 'ep' }],
    [paths.album('302127'), { view: 'album', id: '302127' }],
    [paths.albumTrack(paths.album('302127'), '3135556'), { view: 'album', id: '302127', track: '3135556' }],
    [paths.wrapped(), { view: 'wrapped' }],
    [paths.wrapped(2025), { view: 'wrapped', year: '2025' }],
    [paths.album('k3'), { view: 'album', id: 'k3' }],
    [paths.albumTrack(paths.album('k3'), 'k12'), { view: 'album', id: 'k3', track: 'k12' }],
    [paths.artist('k7'), { view: 'artist', id: 'k7' }],
    [paths.uploads(), { view: 'uploads' }],
    [paths.upload('k3'), { view: 'upload', id: 'k3' }],
    [paths.user('ann b'), { view: 'user', id: 'ann b' }],
  ];
  for (const [path, want] of cases) {
    const url = new URL(path, 'https://music.example');
    assert.deepEqual(parseRoute(url.pathname, url.search), want, path);
  }
});

test('old /discover links still open the right page', () => {
  assert.deepEqual(parseRoute('/discover/artist/27'), { view: 'artist', id: '27' });
  assert.deepEqual(parseRoute('/discover/album/9'), { view: 'album', id: '9' });
  assert.deepEqual(parseRoute('/discover', '?q=x'), { view: 'search', q: 'x' });
});

test('an unknown discography filter shows everything', () => {
  assert.deepEqual(parseRoute('/artist/27/discography/bogus'), { view: 'discography', id: '27', filter: 'all' });
  assert.deepEqual(parseRoute('/artist/27/discography'), { view: 'discography', id: '27', filter: 'all' });
});

test('/library is the library page', () => {
  assert.deepEqual(parseRoute('/library'), { view: 'library' });
  assert.equal(paths.library(), '/library');
});

test('unknown paths fall back to home', () => {
  assert.deepEqual(parseRoute('/nope/whatever'), { view: 'home' });
  assert.deepEqual(parseRoute('/library/artist'), { view: 'home' });
  assert.deepEqual(parseRoute('/', '?invite=abc'), { view: 'home' });
});


test('Last.fm returns to /lastfm — the settings page links the account', () => {
  assert.deepEqual(parseRoute('/lastfm', '?token=abc'), { view: 'settings', lastfm: true });
});

test('/mix/<id> is a mix', () => {
  assert.deepEqual(parseRoute('/mix/tag-russian-hip-hop'), { view: 'mix', id: 'tag-russian-hip-hop' });
  assert.equal(paths.mix('on-repeat'), '/mix/on-repeat');
  assert.deepEqual(parseRoute('/mix'), { view: 'home' });
});

test('/admin/missing is the admin list of songs that were not found', () => {
  assert.deepEqual(parseRoute('/admin/missing'), { view: 'missing' });
  assert.equal(paths.missing(), '/admin/missing');
  assert.deepEqual(parseRoute('/admin'), { view: 'home' });
});

test('/genre/<slug> is a genre page', () => {
  assert.deepEqual(parseRoute('/genre/russian-hip-hop'), { view: 'genre', id: 'russian-hip-hop' });
  assert.equal(paths.genre('r-b'), '/genre/r-b');
});
