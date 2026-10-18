import { get } from 'svelte/store';
import { t } from './i18n.js';
import { api } from './api.js';
import { paths, navigate } from './stores/nav.js';
import { playlists, savedAlbumIds, followedIds, toggleSaveAlbum, toggleFollow, addToPlaylist, createPlaylist } from './stores/collection.js';
import { toast } from './stores/toast.js';
import { refresh, queueRow, playArtistRadio, mixRow } from './library.js';
import { rowFromDeezerTrack, rowFromCollectionTrack } from './rows.js';
import { shareItem, linkItems } from './share.js';
import ListPlus from '@lucide/svelte/icons/list-plus';
import ListEnd from '@lucide/svelte/icons/list-end';
import Plus from '@lucide/svelte/icons/plus';
import CirclePlus from '@lucide/svelte/icons/circle-plus';
import CircleCheck from '@lucide/svelte/icons/circle-check';
import UserRound from '@lucide/svelte/icons/user-round';
import UserPlus from '@lucide/svelte/icons/user-plus';
import UserCheck from '@lucide/svelte/icons/user-check';
import Radio from '@lucide/svelte/icons/radio';

// Меню карточек (MediaCard: правый клик, долгое нажатие, «…»): то, что в
// Spotify можно сделать с альбомом, плейлистом, артистом или миксом, не
// открывая его. Треки грузятся только по нажатию на пункт.

const report = (e) => toast(e.message, 'error');

// Треки места — строками списка, с отметкой, что уже скачано (refresh).
async function loaded(load) {
  const rows = await load();
  await refresh(rows.map((r) => r.deezerId));
  return rows;
}

const albumRows = (id) =>
  loaded(async () => {
    const { album, tracks } = await api.discoverAlbum(id);
    return tracks.map((tr) => rowFromDeezerTrack(tr, album));
  });
const playlistRows = (id) => loaded(async () => (await api.getPlaylist(id)).tracks.map(rowFromCollectionTrack));
const mixRows = (id) => loaded(async () => (await api.mix(id)).tracks.map(mixRow));

function queueItem(load) {
  const tr = get(t);
  return {
    label: tr('menu.addToQueue'),
    icon: ListEnd,
    onClick: () =>
      load()
        .then((rows) => {
          rows.forEach(queueRow);
          toast(tr('menu.queued'));
        })
        .catch(report),
  };
}

// «Добавить в плейлист»: все треки места — в новый или свой плейлист.
function playlistItem(load, skipId = null) {
  const tr = get(t);
  const ids = async () => (await load()).map((r) => r.deezerId).filter(Boolean);
  const mine = get(playlists).mine.filter((p) => p.id !== skipId);
  return {
    label: tr('menu.addToPlaylist'),
    icon: ListPlus,
    submenu: [
      {
        label: tr('menu.newPlaylist'),
        icon: Plus,
        onClick: async () => {
          try {
            const p = await createPlaylist(await ids());
            if (p) navigate(paths.playlist(p.id));
          } catch (e) {
            report(e);
          }
        },
      },
      ...(mine.length ? [{ separator: true }] : []),
      ...mine.map((p) => ({ label: p.title, onClick: () => ids().then((list) => addToPlaylist(p, list), report) })),
    ],
  };
}

/** album: { id, title, artist_id? } */
export function albumMenuItems(album) {
  const tr = get(t);
  const saved = get(savedAlbumIds).has(album.id);
  const items = [
    queueItem(() => albumRows(album.id)),
    playlistItem(() => albumRows(album.id)),
    { label: tr(saved ? 'album.unsave' : 'album.save'), icon: saved ? CircleCheck : CirclePlus, onClick: () => toggleSaveAlbum(album.id) },
    shareItem(paths.album(album.id), album.title),
  ];
  if (album.artist_id) items.push({ separator: true }, { label: tr('menu.goToArtist'), icon: UserRound, href: paths.artist(album.artist_id) });
  return items;
}

/** playlist: { id, title, is_public, mine } */
export function playlistMenuItems(playlist) {
  const items = [queueItem(() => playlistRows(playlist.id)), playlistItem(() => playlistRows(playlist.id), playlist.id)];
  if (playlist.is_public || playlist.mine) items.push(shareItem(paths.playlist(playlist.id), playlist.title));
  return items;
}

/** artist: { id, name } */
export function artistMenuItems(artist) {
  const tr = get(t);
  const following = get(followedIds).has(artist.id);
  return [
    { label: tr(following ? 'artist.following' : 'artist.follow'), icon: following ? UserCheck : UserPlus, onClick: () => toggleFollow(artist.id) },
    { label: tr('artist.radio'), icon: Radio, onClick: () => playArtistRadio(artist.id).catch(report) },
    ...linkItems(paths.artist(artist.id), artist.name),
  ];
}

/** mix: { id } — свой у каждого, поэтому без «Поделиться». */
export function mixMenuItems(mix) {
  return [queueItem(() => mixRows(mix.id)), playlistItem(() => mixRows(mix.id))];
}
