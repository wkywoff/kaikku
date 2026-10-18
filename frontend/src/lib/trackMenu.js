import { get } from 'svelte/store';
import { t } from './i18n.js';
import { paths, navigate } from './stores/nav.js';
import { likedIds, playlists, toggleLike, addToPlaylist, createPlaylist } from './stores/collection.js';
import { canEdit } from './stores/me.js';
import { toast } from './stores/toast.js';
import { queueRow, playNextRow, mainArtistId, playArtistRadio } from './library.js';
import { shareItem } from './share.js';
import ListPlus from '@lucide/svelte/icons/list-plus';
import Plus from '@lucide/svelte/icons/plus';
import CirclePlus from '@lucide/svelte/icons/circle-plus';
import CircleCheck from '@lucide/svelte/icons/circle-check';
import ListEnd from '@lucide/svelte/icons/list-end';
import ListStart from '@lucide/svelte/icons/list-start';
import UserRound from '@lucide/svelte/icons/user-round';
import Disc3 from '@lucide/svelte/icons/disc-3';
import Trash2 from '@lucide/svelte/icons/trash-2';
import Pencil from '@lucide/svelte/icons/pencil';
import Radio from '@lucide/svelte/icons/radio';

// Пункты контекстного меню трека (правый клик / «…» в TrackList) — только
// то, что работает: плейлисты, лайк, очередь, радио, переходы, правка (админ).
//   playlist — { id, mine } если список — плейлист; onRemove — убрать из него;
//   onEdit — открыть правку (только в режиме админа).
export function trackMenuItems(row, { playlist = null, onRemove = null, onEdit = null } = {}) {
  const tr = get(t);
  const items = [];

  if (row.deezerId) {
    const mine = get(playlists).mine;
    items.push({
      label: tr('menu.addToPlaylist'),
      icon: ListPlus,
      submenu: [
        {
          label: tr('menu.newPlaylist'),
          icon: Plus,
          onClick: async () => {
            const p = await createPlaylist([row.deezerId]);
            if (p) navigate(paths.playlist(p.id));
          },
        },
        ...(mine.length ? [{ separator: true }] : []),
        ...mine.map((p) => ({ label: p.title, onClick: () => addToPlaylist(p, [row.deezerId]) })),
      ],
    });
    const liked = get(likedIds).has(row.deezerId);
    items.push({ label: tr(liked ? 'menu.unlike' : 'menu.like'), icon: liked ? CircleCheck : CirclePlus, onClick: () => toggleLike(row.deezerId) });
  }
  if (playlist?.mine && row.itemId && onRemove) {
    items.push({ label: tr('menu.removeFromPlaylist'), icon: Trash2, onClick: onRemove });
  }
  items.push({
    label: tr('menu.playNext'),
    icon: ListStart,
    onClick: () => {
      playNextRow(row);
      toast(tr('menu.playingNext'));
    },
  });
  items.push({
    label: tr('menu.addToQueue'),
    icon: ListEnd,
    onClick: () => {
      queueRow(row);
      toast(tr('menu.queued'));
    },
  });

  // Радио: этот трек, потом похожие (радио его исполнителя).
  const radioArtist = row.deezerId ? mainArtistId(row) : null;
  if (radioArtist) {
    items.push({
      label: tr('menu.radio'),
      icon: Radio,
      onClick: () => playArtistRadio(radioArtist, row).catch((e) => toast(e.message, 'error')),
    });
  }

  // Ссылка на трек — страница его альбома с этим треком (выделен).
  if (row.album?.href && row.deezerId) items.push(shareItem(paths.albumTrack(row.album.href, row.deezerId), row.title));

  const artists = (row.artists || []).filter((a) => a.href);
  const go = [];
  if (artists.length === 1) go.push({ label: tr('menu.goToArtist'), icon: UserRound, href: artists[0].href });
  else if (artists.length > 1) go.push({ label: tr('menu.goToArtist'), icon: UserRound, submenu: artists.map((a) => ({ label: a.name, href: a.href })) });
  if (row.album?.href) go.push({ label: tr('menu.goToAlbum'), icon: Disc3, href: row.album.href });
  if (go.length) items.push({ separator: true }, ...go);

  if (onEdit && get(canEdit)) items.push({ separator: true }, { label: tr('edit.trackTitle'), icon: Pencil, onClick: onEdit });
  return items;
}
