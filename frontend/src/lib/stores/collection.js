import { writable, get } from 'svelte/store';
import { api } from '../api.js';
import { toast } from './toast.js';
import { translate } from '../i18n.js';
import { lang } from './settings.js';

// Личная медиатека текущего пользователя: что лайкнуто, какие альбомы
// сохранены, на кого подписан, плейлисты (свои и чужие публичные). Грузится после входа
// (App.svelte); лайк и сохранение меняются сразу на экране, а если сервер
// ответил ошибкой — откатываются.

export const likedIds = writable(new Set());
export const savedAlbumIds = writable(new Set());
export const followedIds = writable(new Set());
export const playlists = writable({ mine: [], others: [] });
export const collectionLoaded = writable(false);

const tr = (key, vars) => translate(get(lang), key, vars);

export async function loadCollection() {
  try {
    const [ids, lists] = await Promise.all([api.collection(), api.playlists()]);
    likedIds.set(new Set(ids.liked));
    savedAlbumIds.set(new Set(ids.albums));
    followedIds.set(new Set(ids.artists || []));
    playlists.set(lists);
  } catch {
    /* без медиатеки сайт всё равно работает — кнопки просто «не лайкнуто» */
  }
  collectionLoaded.set(true);
}

export async function refreshPlaylists() {
  try {
    playlists.set(await api.playlists());
  } catch {
    /* оставляем старый список */
  }
}

function toggleIn(store, id, on) {
  store.update((set) => {
    const next = new Set(set);
    if (on) next.add(id);
    else next.delete(id);
    return next;
  });
}

export async function toggleLike(deezerId) {
  const on = !get(likedIds).has(deezerId);
  toggleIn(likedIds, deezerId, on);
  try {
    await (on ? api.like(deezerId) : api.unlike(deezerId));
    toast(tr(on ? 'collection.liked' : 'collection.unliked'));
  } catch (e) {
    toggleIn(likedIds, deezerId, !on);
    toast(e.message, 'error');
  }
}

export async function toggleSaveAlbum(deezerId) {
  const on = !get(savedAlbumIds).has(deezerId);
  toggleIn(savedAlbumIds, deezerId, on);
  try {
    await (on ? api.saveAlbum(deezerId) : api.unsaveAlbum(deezerId));
    toast(tr(on ? 'collection.albumSaved' : 'collection.albumRemoved'));
  } catch (e) {
    toggleIn(savedAlbumIds, deezerId, !on);
    toast(e.message, 'error');
  }
}

export async function toggleFollow(deezerArtistId) {
  const on = !get(followedIds).has(deezerArtistId);
  toggleIn(followedIds, deezerArtistId, on);
  try {
    await (on ? api.follow(deezerArtistId) : api.unfollow(deezerArtistId));
    toast(tr(on ? 'collection.followed' : 'collection.unfollowed'));
  } catch (e) {
    toggleIn(followedIds, deezerArtistId, !on);
    toast(e.message, 'error');
  }
}

export async function addToPlaylist(playlist, deezerIds) {
  try {
    await api.addToPlaylist(playlist.id, deezerIds);
    toast(tr('collection.addedTo', { name: playlist.title }));
    refreshPlaylists();
    return true;
  } catch (e) {
    toast(e.message, 'error');
    return false;
  }
}

/**
 * Новый плейлист (по умолчанию «Мой плейлист №N»), сразу с треками, если
 * переданы. download: false — не ставить их на скачивание сразу (микс
 * целиком: скачаются, когда заиграют).
 */
export async function createPlaylist(deezerIds = [], fields = {}, { download } = {}) {
  const n = get(playlists).mine.length + 1;
  try {
    const p = await api.createPlaylist({ title: tr('playlist.defaultTitle', { n }), ...fields });
    if (deezerIds.length) await api.addToPlaylist(p.id, deezerIds, { download });
    await refreshPlaylists();
    toast(tr('collection.created', { name: p.title }));
    return p;
  } catch (e) {
    toast(e.message, 'error');
    return null;
  }
}
