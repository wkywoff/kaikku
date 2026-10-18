import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseTrackList, spotifyLink, parseCsv } from '../src/lib/importList.js';

test('lines "Artist — Title" with dashes of any kind, numbering and bare titles', () => {
  const list = parseTrackList(`1. Daft Punk - Get Lucky
2) The Weeknd, Daft Punk – Starboy

Буря — Огни города - Remastered
Just A Title`);
  assert.deepEqual(list, [
    { title: 'Get Lucky', artists: ['Daft Punk'] },
    { title: 'Starboy', artists: ['The Weeknd', 'Daft Punk'] },
    { title: 'Огни города - Remastered', artists: ['Буря'] },
    { title: 'Just A Title', artists: [] },
  ]);
});

test('an Exportify CSV: quoted fields, commas inside quotes, durations', () => {
  const csv = `"Track URI","Track Name","Album Name","Artist Name(s)","Release Date","Duration (ms)"
"spotify:track:1","Get Lucky (feat. Pharrell Williams, Nile Rodgers)","Random Access Memories","Daft Punk,Pharrell Williams,Nile Rodgers","2013-05-17","369626"
"spotify:track:2","Say ""Hi""","X","Someone","2020","1000"
`;
  assert.deepEqual(parseTrackList(csv), [
    { title: 'Get Lucky (feat. Pharrell Williams, Nile Rodgers)', artists: ['Daft Punk', 'Pharrell Williams', 'Nile Rodgers'], duration_ms: 369626 },
    { title: 'Say "Hi"', artists: ['Someone'], duration_ms: 1000 },
  ]);
  assert.deepEqual(parseCsv('a,"b\nc",d\r\n1,2,3'), [['a', 'b\nc', 'd'], ['1', '2', '3']]);
});

test('Spotify links are recognized, other text is not', () => {
  assert.ok(spotifyLink('https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=abc'));
  assert.ok(spotifyLink('https://open.spotify.com/intl-de/album/4m2880jivSbbyEGAKfITCa'));
  assert.ok(spotifyLink('spotify:playlist:37i9dQZF1DXcBWIGoYBM5M'));
  assert.equal(spotifyLink('Daft Punk - Get Lucky'), null);
  assert.equal(spotifyLink('https://open.spotify.com/track/2Foc5Q5nqNiosCNqttzHof'), null);
});
