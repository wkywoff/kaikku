<script>
  // Плейлист: своя обложка или мозаика обложек, автор, треки. Владелец
  // меняет данные (название, описание, обложка, публичный), перетаскивает
  // треки и убирает их через меню «…»; добавляют треки из любого списка:
  // «…» → «Добавить в плейлист». Лупа — поиск внутри плейлиста.
  import { api } from '../api.js';
  import { player, isPlaying as playerPlaying, shuffleOn } from '../stores/player.js';
  import { navStore, navigate, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { refresh, playRows, playingContext, isContext, queueRow } from '../library.js';
  import { refreshPlaylists } from '../stores/collection.js';
  import { toast } from '../stores/toast.js';
  import { rowFromCollectionTrack, filterRows } from '../rows.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { shareItem } from '../share.js';
  import { tracksCount, longDuration } from '../format.js';
  import Hero from '../components/Hero.svelte';
  import Artwork from '../components/Artwork.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';
  import PlaylistDialog from '../components/PlaylistDialog.svelte';
  import FilterBox from '../components/FilterBox.svelte';
  import SortButton from '../components/SortButton.svelte';
  import { sortRows } from '../sort.js';
  import { sortPrefs } from '../stores/sort.js';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import LogOut from '@lucide/svelte/icons/log-out';
  import { me } from '../stores/me.js';
  import ListEnd from '@lucide/svelte/icons/list-end';
  import Pencil from '@lucide/svelte/icons/pencil';
  import { setRouteTitle } from '../stores/title.js';
  import Shuffle from '@lucide/svelte/icons/shuffle';
  import Search from '@lucide/svelte/icons/search';

  let playlist = null;
  let rows = [];
  let loading = true;
  let error = '';
  let editing = false;
  const tint = pageTint();
  let filter = '';
  $: sort = $sortPrefs.playlist;
  $: shown = sortRows(filterRows(rows, filter), sort.key, sort.desc, $lang);
  // Перетаскивать — только в своём порядке, как в Spotify.
  $: customOrder = sort.key === 'custom';
  $: sortOptions = [
    { key: 'custom', label: $t('sort.custom') },
    { key: 'title', label: $t('sort.title') },
    { key: 'artist', label: $t('sort.artist') },
    { key: 'album', label: $t('sort.album') },
    { key: 'added', label: $t('sort.added'), desc: true },
    { key: 'duration', label: $t('sort.duration') },
  ];

  $: load($navStore.id);

  let seq = 0;
  async function load(id) {
    const my = ++seq;
    error = '';
    try {
      const data = await api.getPlaylist(id);
      if (my !== seq) return;
      playlist = data.playlist;
      rows = data.tracks.map(rowFromCollectionTrack);
      tint.from(playlist.covers[0] || null);
      refresh(rows.map((r) => r.deezerId));
    } catch (e) {
      if (my === seq) error = e.message.includes('not found') ? $t('playlist.notFound') : e.message;
    } finally {
      if (my === seq) loading = false;
    }
  }

  // Вкладка: «Название – плейлист от автора | kaikku».
  $: if (playlist) setRouteTitle(`playlist:${$navStore.id}`, $t('title.playlist', { title: playlist.title, owner: playlist.owner.username }));

  $: playingHere = !!playlist && isContext($playingContext, 'playlist', playlist.id);
  $: totalSec = rows.reduce((s, r) => s + (r.durationMs || 0) / 1000, 0);

  function onPlay() {
    if (playingHere) return player.toggle();
    if (rows.length) playRows(rows, 0, { kind: 'playlist', id: playlist.id, title: playlist.title });
  }

  // Перетащили трек: сразу на новом месте, на сервер — «встать перед
  // следующим» (или в конец). Не вышло — перечитываем плейлист.
  async function onReorder({ from, to }) {
    const next = [...rows];
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    rows = next;
    try {
      await api.movePlaylistItem(playlist.id, moved.itemId, next[to + 1]?.itemId || null);
      refreshPlaylists();
    } catch (e) {
      toast(e.message, 'error');
      load(playlist.id);
    }
  }

  function moreMenu(e) {
    const items = [];
    if (rows.length) {
      items.push({
        label: $t('menu.addToQueue'),
        icon: ListEnd,
        onClick: () => {
          rows.forEach(queueRow);
          toast($t('menu.queued'));
        },
      });
    }
    if (playlist.is_public) items.push(shareItem(paths.playlist(playlist.id), playlist.title));
    // Соавтор может выйти сам — плейлист пропадёт из его медиатеки.
    if (!playlist.mine && playlist.can_edit) items.push({ separator: true }, { label: $t('playlist.leave'), icon: LogOut, danger: true, onClick: leave });
    if (items.length) openMenu(e, items);
  }

  async function leave() {
    try {
      await api.removeCollaborator(playlist.id, $me.id);
      refreshPlaylists();
      toast($t('playlist.left', { name: playlist.title }));
      navigate(paths.library(), { replace: true });
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  async function removeRow(row) {
    rows = rows.filter((r) => r !== row);
    try {
      await api.removeFromPlaylist(playlist.id, row.itemId);
      refreshPlaylists();
    } catch (e) {
      toast(e.message, 'error');
      load(playlist.id);
    }
  }

  function onSaved(e) {
    editing = false;
    playlist = { ...playlist, ...e.detail };
    tint.from(playlist.covers[0] || null); // обложка могла смениться
    refreshPlaylists();
  }

  function onDeleted() {
    editing = false;
    refreshPlaylists();
    navigate(paths.home(), { replace: true });
  }
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if playlist}
  <div class="page-pad">
    <Hero kind={$t(playlist.is_public ? 'playlist.publicKind' : 'playlist.kind')} title={playlist.title}>
      <Artwork
        slot="art"
        images={playlist.covers}
        iconSize={72}
        sizes="(max-width: 768px) min(62vw, 240px), 232px"
        tileSizes="(max-width: 768px) min(31vw, 120px), 116px"
      />
      <svelte:fragment slot="meta">
        {#if playlist.description}<div class="description">{playlist.description}</div>{/if}
        <span class="owner">{#each [playlist.owner, ...(playlist.collaborators || [])] as u, i (u.id)}{#if i > 0}{', '}{/if}<a href={paths.user(u.username)} use:link>{u.username}</a>{/each}</span>
        {#if rows.length}<span class="dot">•</span><span class="muted-meta">{tracksCount(rows.length, $lang)}, {longDuration(totalSec, $lang)}</span>{/if}
      </svelte:fragment>
    </Hero>
  </div>

  <div class="actions page-pad">
    {#if rows.length}
      <PlayButton size={56} playing={playingHere && $playerPlaying} label={playingHere && $playerPlaying ? $t('player.pause') : $t('player.play')} on:click={onPlay} />
      <button class="icon-btn big" class:on={$shuffleOn} title={$t('player.shuffle')} aria-pressed={$shuffleOn} on:click={() => player.toggleShuffle()}>
        <Shuffle size={28} />
      </button>
    {/if}
    {#if playlist.mine}
      <button class="icon-btn" title={$t('playlist.edit')} aria-label={$t('playlist.edit')} on:click={() => (editing = true)}><Pencil size={22} /></button>
    {/if}
    {#if rows.length || playlist.is_public || (!playlist.mine && playlist.can_edit)}
      <button class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: playlist.title })} on:click={moreMenu}><Ellipsis size={24} /></button>
    {/if}
    {#if rows.length}
      <div class="filter-slot">
        <FilterBox bind:value={filter} placeholder={$t('playlist.search')} />
        <SortButton options={sortOptions} value={sort} on:change={(e) => sortPrefs.update((p) => ({ ...p, playlist: e.detail }))} />
      </div>
    {/if}
  </div>

  <div class="page-pad">
    {#if rows.length && !shown.length}
      <p class="muted no-match">{$t('list.noMatch', { q: filter })}</p>
    {:else if rows.length}
      <TrackList rows={shown} showCover showAlbum numbered reorderable={playlist.can_edit && !filter && customOrder} playlist={{ id: playlist.id, mine: playlist.can_edit }} showAddedBy={playlist.collaborative} context={{ kind: 'playlist', id: playlist.id, title: playlist.title }} on:remove={(e) => removeRow(e.detail)} on:reorder={(e) => onReorder(e.detail)} on:changed={() => load(playlist.id)} />
    {:else}
      <div class="empty">
        <h2>{$t('playlist.empty')}</h2>
        <p class="muted">{$t('playlist.emptyHint')}</p>
        <a class="pill" href={paths.search()} use:link><Search size={16} /> {$t('playlist.find')}</a>
      </div>
    {/if}
  </div>

  {#if editing}
    <PlaylistDialog {playlist} on:close={() => (editing = false)} on:saved={onSaved} on:deleted={onDeleted} on:collaborators={() => (load(playlist.id), refreshPlaylists())} />
  {/if}
{:else if loading}
  <div class="page-pad skeleton">
    <div class="sk-art"></div>
    <div class="sk-lines"><div></div><div></div></div>
  </div>
{/if}

<style>
  .description {
    width: 100%;
    margin-bottom: 6px;
    color: rgba(255, 255, 255, 0.7);
    font-weight: 400;
  }
  .owner {
    font-weight: 700;
  }
  @media (hover: hover) {
    .owner a:hover {
      text-decoration: underline;
    }
  }
  .dot {
    margin: 0 4px;
  }
  .muted-meta {
    color: rgba(255, 255, 255, 0.7);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 24px;
    padding-top: 20px;
    padding-bottom: 24px;
    min-height: 56px;
  }
  .icon-btn.big {
    width: 44px;
    height: 44px;
  }
  .filter-slot {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 16px;
  }
  .no-match {
    padding: 24px 0;
  }
  .empty {
    padding: 32px 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
  .empty h2 {
    font-size: 22px;
  }
  .skeleton {
    display: flex;
    align-items: flex-end;
    gap: 24px;
    padding-top: 48px;
  }
  .sk-art {
    width: 232px;
    height: 232px;
    border-radius: 6px;
    background: var(--bg-card);
  }
  .sk-lines div {
    height: 48px;
    width: 360px;
    max-width: 50vw;
    border-radius: 8px;
    background: var(--bg-card);
    margin-top: 12px;
  }
  @media (max-width: 768px) {
    .actions {
      gap: 16px;
    }
    .skeleton {
      flex-direction: column;
      align-items: center;
    }
  }
</style>
