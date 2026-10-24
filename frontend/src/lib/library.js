import { writable, get } from 'svelte/store';
import { api } from './api.js';
import { player, select } from './stores/player.js';
import { rowFromDeezerTrack, rowFromCollectionTrack } from './rows.js';
import { canEdit, me } from './stores/me.js';

// Какие треки Deezer уже скачаны на сервер — одно на всё приложение:
//
//   libraryState: { [deezerTrackId]: { trackId } }
//
// Нужно только для того, чтобы скачанное играло прямо из медиатеки. Всё
// остальное играет потоком, пока сервер его скачивает (player.js →
// resolveSource), — ждать ничего не нужно.
export const libraryState = writable({});

/** Подтянуть, что из списка Deezer ID уже скачано (страница альбома, поиск, топ артиста). */
export async function refresh(ids) {
  const unique = [...new Set(ids.filter(Boolean))];
  if (unique.length === 0) return;
  try {
    const found = await api.ingestLookup(unique);
    const known = {};
    for (const id of unique) if (found[id]?.track_id) known[id] = { trackId: found[id].track_id };
    if (Object.keys(known).length) libraryState.update((m) => ({ ...m, ...known }));
  } catch {
    /* не критично: такие треки просто сыграют потоком */
  }
}

// --- воспроизведение строк из списков ---
//
// Строка (row) — трек в любом списке:
//   { deezerId?, libraryTrackId?, title, artists: [{name, href}], coverUrl?,
//     album?: {title, href}, durationMs, explicit?, itemId? (в плейлисте) }

export function trackIdOf(row, state = get(libraryState)) {
  return row.libraryTrackId || (row.deezerId && state[row.deezerId]?.trackId) || null;
}

/** Играет ли сейчас эта строка (current — элемент очереди плеера). */
export function isCurrentRow(row, current, state = get(libraryState)) {
  if (!current) return false;
  // Трек, который играет потоком, ещё без id медиатеки — узнаём по Deezer ID.
  if (current.deezerId && row.deezerId === current.deezerId) return true;
  return !!current.id && trackIdOf(row, state) === current.id;
}

// context — откуда включили (для истории): { kind: 'album'|'playlist'|'artist'|'liked'|'mix', id }.
export function playerEntry(row, trackId, context = null) {
  return {
    id: trackId,
    deezerId: row.deezerId || null,
    title: row.title,
    artists: row.artists || [], // [{name, href}] — каждый кликабелен
    artist: (row.artists || []).map((a) => a.name).join(', '), // текстом — для Media Session
    artistHref: row.artists?.[0]?.href || null,
    album: row.album?.title || '',
    albumHref: row.album?.href || null,
    coverUrl: row.coverUrl || null,
    durationMs: row.durationMs,
    context,
  };
}

/**
 * Играть rows начиная с index (null — с начала, а с перемешиванием — со
 * случайного трека: Play альбома). В очередь встают все строки списка:
 * скачанные играют из медиатеки, остальные — потоком, пока сервер их
 * скачивает, так что переходы между треками работают по всему списку.
 */
// chosen: false — включили место целиком (Play альбома, радио), а не этот
// трек: не сыгравший трек тогда пропускается (stores/player.js, trackFailed).
export function playRows(rows, index, context = null, { chosen = true } = {}) {
  const state = get(libraryState);
  const entries = [];
  let start = -1;
  const skipHidden = !get(canEdit); // треки скрытых релизов не играют (кроме режима админа)
  rows.forEach((r, i) => {
    const id = trackIdOf(r, state);
    if (!id && !r.deezerId) return;
    if (r.hidden && skipHidden) return;
    if (i === index) start = entries.length;
    entries.push(playerEntry(r, id, context));
  });
  if (index === null) start = entries.length ? null : -1;
  if (start === -1) return;
  return player.playQueue(entries, start, { chosen });
}

// --- откуда сейчас играет ---
//
// У элемента очереди есть context — откуда его включили ({ kind: album |
// playlist | artist | liked, id }; добавленное в очередь вручную — без
// него). По нему кнопки Play в шапках и на карточках знают, играют ли они:
// альбом не «играет», если его трек звучит из плейлиста или радио.

/** Откуда играет текущий трек: { kind, id } или null. */
export const playingContext = select(player, (s) => [s.queue, s.index], (s) => s.queue[s.index]?.context || null);

/** ctx — это место (kind, id)? */
export function isContext(ctx, kind, id = '') {
  return !!ctx && ctx.kind === kind && String(ctx.id ?? '') === String(id ?? '');
}

/**
 * Play в шапке или на карточке: если отсюда уже играет — пауза/продолжить,
 * иначе — с начала. loadRows отдаёт строки (треки) этого места.
 */
export async function playFrom(kind, id, loadRows) {
  const s = get(player);
  if (isContext(s.queue[s.index]?.context, kind, id)) return player.toggle();
  const rows = await loadRows();
  if (!rows.length) return;
  await refresh(rows.map((r) => r.deezerId));
  return playRows(rows, null, { kind, id }, { chosen: false });
}

export const playAlbum = (id) =>
  playFrom('album', id, async () => {
    const { album, tracks } = await api.discoverAlbum(id);
    return tracks.map((tr) => rowFromDeezerTrack(tr, album));
  });
export const playArtistTop = (id) => playFrom('artist', id, async () => (await api.discoverArtist(id)).top.map((tr) => rowFromDeezerTrack(tr)));
export const playPlaylist = (id) => playFrom('playlist', id, async () => (await api.getPlaylist(id)).tracks.map(rowFromCollectionTrack));
export const playLiked = () => playFrom('liked', '', async () => (await api.likedTracks()).tracks.map(rowFromCollectionTrack));
export const playMix = (id) => playFrom('mix', id, async () => (await api.mix(id)).tracks.map(mixRow));

/** Трек микса — строка: номер — место в миксе, а не в альбоме. */
export const mixRow = (tr) => ({ ...rowFromDeezerTrack(tr), trackNo: null, discNo: null });

/** Строку — в конец очереди (контекстное меню). */
export function queueRow(row) {
  player.addToQueue(playerEntry(row, trackIdOf(row)));
}

/** Строку — сразу после текущего трека («Играть следующим»). */
export function playNextRow(row) {
  player.playNext(playerEntry(row, trackIdOf(row)));
}

/** Элемент очереди плеера → строка (для меню трека в плеере). */
export function rowFromEntry(entry) {
  return {
    deezerId: entry.deezerId || null,
    libraryTrackId: entry.id || null,
    title: entry.title,
    artists: entry.artists?.length ? entry.artists : [{ name: entry.artist, href: entry.artistHref || null }],
    album: entry.album ? { title: entry.album, href: entry.albumHref || null } : null,
    coverUrl: entry.coverUrl || null,
    durationMs: entry.durationMs,
  };
}

/** Deezer ID основного исполнителя элемента очереди или строки. */
export function mainArtistId(entry) {
  const a = entry?.artists?.[0];
  if (a?.id) return String(a.id);
  const href = a?.href || entry?.artistHref || '';
  return href.match(/^\/artist\/(\d+|k[1-9]\d*)/)?.[1] || null;
}

// Радио артиста как строки списка; exclude — Deezer ID, которых не надо.
async function radioRows(artistId, exclude = new Set()) {
  const { tracks } = await api.artistRadio(artistId);
  const rows = tracks.map((tr) => rowFromDeezerTrack(tr)).filter((r) => !exclude.has(r.deezerId));
  await refresh(rows.map((r) => r.deezerId));
  return rows;
}

/**
 * Трек из поиска: он сам — сразу, следом — похожие (радио его исполнителя,
 * как «Радио по треку»), а не следующий результат выдачи: выдача — не
 * плейлист. Радио грузится, пока трек уже играет, и встаёт в очередь, если
 * человек за это время не включил другое.
 */
export async function playWithSimilar(row) {
  const artistId = row.deezerId ? mainArtistId(row) : null;
  const context = artistId ? { kind: 'artist', id: artistId } : null;
  const similar = artistId ? radioRows(artistId, new Set([row.deezerId])).catch(() => []) : Promise.resolve([]);
  await playRows([row], 0, context);
  const rows = await similar;
  const s = get(player);
  if (!rows.length || s.queue.length !== 1 || s.queue[0].deezerId !== row.deezerId) return;
  const state = get(libraryState);
  return player.appendToQueue(rows.map((r) => playerEntry(r, trackIdOf(r, state), context)));
}

/** «Радио»: на странице артиста — его радио; из меню трека — этот трек и радио его исполнителя. */
export async function playArtistRadio(artistId, firstRow = null) {
  const rows = await radioRows(artistId, new Set(firstRow ? [firstRow.deezerId] : []));
  return playRows(firstRow ? [firstRow, ...rows] : rows, firstRow ? 0 : null, { kind: 'artist', id: artistId }, { chosen: false });
}

// Радио, чарт и поиск Deezer отдают у трека только основного исполнителя —
// когда трек начинает играть, дозапрашиваем всех (один раз на трек).
async function completeArtists(entry) {
  if (!entry.deezerId || entry.artistsChecked || (entry.artists?.length || 0) > 1) return;
  entry.artistsChecked = true;
  try {
    const tr = await api.discoverTrack(entry.deezerId);
    if (!tr.contributors || tr.contributors.length < 2) return;
    const artists = rowFromDeezerTrack(tr).artists;
    player.patchEntry(entry, { artists, artist: artists.map((a) => a.name).join(', ') });
  } catch {
    entry.artistsChecked = false; // в следующий раз попробуем снова
  }
}

// История, автовоспроизведение, полные исполнители — хуки плеера (stores/player.js).
player.setHooks({
  onTrackStart(entry) {
    completeArtists(entry);
    if (entry.deezerId && get(me)?.lastfm_scrobble) api.lastfmNowPlaying(entry.deezerId).catch(() => {});
  },
  onPlayed(entry, play) {
    if (entry.deezerId) play.recorded = api.recordPlay(entry.deezerId, entry.context).catch(() => null);
  },
  // Сколько трек реально играл — к записи прослушивания (минуты в итогах года).
  onListened(play, ms) {
    play.recorded?.then((r) => r?.id && api.playListened(r.id, ms).catch(() => {}));
  },
  onScrobble(entry, startedAt) {
    if (entry.deezerId && get(me)?.lastfm_scrobble) api.lastfmScrobble(entry.deezerId, startedAt).catch(() => {});
  },
  // Очередь кончилась: радио исполнителя последнего трека, без того, что
  // уже было в очереди.
  async autoplay(last, queue) {
    const artistId = mainArtistId(last);
    if (!artistId) return [];
    const rows = await radioRows(artistId, new Set(queue.map((e) => e.deezerId).filter(Boolean)));
    const state = get(libraryState);
    return rows.map((r) => playerEntry(r, trackIdOf(r, state), { kind: 'artist', id: artistId }));
  },
});
