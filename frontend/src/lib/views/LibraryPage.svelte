<script context="module">
  // Последние ответы — чтобы «назад» в медиатеку показывал её сразу.
  let cachedAlbums = null;
  let cachedArtists = [];
  let cachedTimes = { added: {}, played: {} };
</script>

<script>
  // Медиатека, как «Your Library» в Spotify: «Любимые треки», свои
  // плейлисты, сохранённые альбомы и исполнители из подписок (фильтр-чипы),
  // создать плейлист и импорт. Раньше всё это было на главной. Тем, кто
  // может загружать свои релизы, — ещё «Загрузить» (/upload).
  import { api } from '../api.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { paths, link, navigate } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { playingContext, isContext, playAlbum, playArtistTop, playPlaylist, playLiked } from '../library.js';
  import { likedIds, savedAlbumIds, followedIds, playlists, collectionLoaded, createPlaylist } from '../stores/collection.js';
  import { recordTypeLabel, tracksCount } from '../format.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { albumMenuItems, artistMenuItems, playlistMenuItems } from '../cardMenu.js';
  import { sortLibrary } from '../sort.js';
  import { sortPrefs } from '../stores/sort.js';
  import SortButton from '../components/SortButton.svelte';
  import MediaCard from '../components/MediaCard.svelte';
  import Artwork from '../components/Artwork.svelte';
  import AccountMenu from '../components/AccountMenu.svelte';
  import Search from '@lucide/svelte/icons/search';
  import Plus from '@lucide/svelte/icons/plus';
  import Import from '@lucide/svelte/icons/import';
  import Upload from '@lucide/svelte/icons/upload';
  import { me } from '../stores/me.js';

  pageTint().none();

  // Первые карточки (что видно на экране) — без отложенной загрузки: при
  // возврате «назад» обложки из кеша показываются сразу, а не мигают пустыми.
  const EAGER = 8;

  let albums = cachedAlbums || [];
  let artists = cachedArtists;
  let loading = !cachedAlbums;
  let error = '';
  let filter = 'all'; // all | playlists | albums | artists

  async function load() {
    try {
      albums = cachedAlbums = (await api.savedAlbums()).albums;
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }
  load();
  api
    .followedArtists()
    .then((d) => (artists = cachedArtists = d.artists))
    .catch(() => {});
  // Когда что добавлено и слушалось — для сортировки; не пришло — порядок по типам.
  let times = cachedTimes;
  api
    .libraryTimes()
    .then((d) => (times = cachedTimes = d))
    .catch(() => {});

  // Убрали из медиатеки или отписались на их страницах — карточки пропадают сразу.
  $: shownAlbums = albums.filter((a) => $savedAlbumIds.has(a.id));
  $: shownArtists = artists.filter((a) => $followedIds.has(a.id));
  $: mine = $playlists.mine;
  $: likedCount = $likedIds.size;
  // Всё одной сеткой в выбранном порядке, «Любимые треки» — всегда первыми.
  $: items = sortLibrary(
    [
      ...(filter === 'all' || filter === 'playlists' ? mine.map((p) => ({ kind: 'playlist', key: `playlist:${p.id}`, name: p.title, p })) : []),
      ...(filter === 'all' || filter === 'albums' ? shownAlbums.map((a) => ({ kind: 'album', key: `album:${a.id}`, name: a.title, a })) : []),
      ...(filter === 'all' || filter === 'artists' ? shownArtists.map((a) => ({ kind: 'artist', key: `artist:${a.id}`, name: a.name, a })) : []),
    ],
    $sortPrefs.library,
    times,
    $lang,
  );
  $: sortOptions = [
    { key: 'recent', label: $t('sort.recent') },
    { key: 'added', label: $t('sort.recentlyAdded') },
    { key: 'alpha', label: $t('sort.alpha') },
  ];
  $: empty = !loading && $collectionLoaded && !likedCount && !mine.length && !shownAlbums.length && !shownArtists.length;

  // Какая карточка сейчас играет — по тому, откуда включена музыка (library.js).
  $: playingCtx = $playerPlaying ? $playingContext : null;
  $: isPlaying = (kind, id = '') => isContext(playingCtx, kind, id);

  const run = (start) => start().catch((e) => (error = e.message));

  async function newPlaylist() {
    const p = await createPlaylist();
    if (p) navigate(paths.playlist(p.id));
  }

  let importing = false;

  const filters = [
    ['all', 'home.all'],
    ['playlists', 'home.playlists'],
    ['albums', 'home.albums'],
    ['artists', 'home.artists'],
  ];
</script>

<div class="page-pad library">
  <div class="top">
    <h1>{$t('library.title')}</h1>
    <!-- На телефоне верхней панели нет — аккаунт здесь, справа вверху. -->
    <div class="phone-account"><AccountMenu /></div>
    <div class="top-actions">
      <button class="pill" on:click={() => (importing = true)}><Import size={16} /> {$t('home.import')}</button>
      <button class="pill" on:click={newPlaylist}><Plus size={16} /> {$t('home.create')}</button>
      {#if $me?.can_upload}
        <a class="pill" href={paths.uploads()} use:link><Upload size={16} /> {$t('upload.button')}</a>
      {/if}
    </div>
  </div>

  {#if importing}
    {#await import('../components/ImportDialog.svelte') then m}
      <svelte:component this={m.default} on:close={() => (importing = false)} on:created={(e) => ((importing = false), navigate(paths.playlist(e.detail.id)))} />
    {/await}
  {/if}

  {#if error}<p class="error-text">{error}</p>{/if}

  {#if empty}
    <div class="empty">
      <h2>{$t('home.emptyTitle')}</h2>
      <p class="muted">{$t('home.emptyText')}</p>
      <a class="pill" href={paths.search()} use:link><Search size={16} /> {$t('home.emptyCta')}</a>
    </div>
  {:else}
    <div class="chips">
      {#each filters as [key, label]}
        {#if key !== 'artists' || shownArtists.length}
          <button class="pill" class:active={filter === key} on:click={() => (filter = key)}>{$t(label)}</button>
        {/if}
      {/each}
      <span class="sort-slot">
        <SortButton options={sortOptions} value={{ key: $sortPrefs.library, desc: true }} directional={false}
          on:change={(e) => sortPrefs.update((p) => ({ ...p, library: e.detail.key }))} />
      </span>
    </div>

    <div class="grid">
      {#if filter === 'all' || filter === 'playlists'}
        <MediaCard
          href={paths.liked()}
          title={$t('liked.title')}
          subtitle={`${$t('playlist.kind')} • ${tracksCount(likedCount, $lang)}`}
          onPlay={likedCount ? () => run(playLiked) : null}
          playing={isPlaying('liked')}
        >
          <Artwork slot="art" kind="liked" />
        </MediaCard>
      {/if}
      {#each items as it, i (it.key)}
        {#if it.kind === 'playlist'}
          <MediaCard
            href={paths.playlist(it.p.id)}
            menu={() => playlistMenuItems(it.p)}
            title={it.p.title}
            subtitle={`${$t('playlist.kind')} • ${tracksCount(it.p.track_count, $lang)}`}
            onPlay={it.p.track_count ? () => run(() => playPlaylist(it.p.id)) : null}
            playing={isPlaying('playlist', it.p.id)}
          >
            <Artwork slot="art" images={it.p.covers} iconSize={48} eager={i < EAGER} />
          </MediaCard>
        {:else if it.kind === 'album'}
          <MediaCard
            href={paths.album(it.a.id)}
            menu={() => albumMenuItems(it.a)}
            image={it.a.cover_url}
            eager={i < EAGER}
            hidden={it.a.hidden}
            title={it.a.title}
            subtitle={`${recordTypeLabel(it.a.record_type, $lang)} • ${it.a.artist_name}`}
            onPlay={() => run(() => playAlbum(it.a.id))}
            playing={isPlaying('album', it.a.id)}
          />
        {:else}
          <MediaCard
            href={paths.artist(it.a.id)}
            menu={() => artistMenuItems(it.a)}
            image={it.a.picture_url}
            eager={i < EAGER}
            round
            title={it.a.name}
            subtitle={$t('artist.kind')}
            onPlay={() => run(() => playArtistTop(it.a.id))}
            playing={isPlaying('artist', it.a.id)}
          />
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .library {
    padding-top: 24px;
    padding-bottom: 24px;
  }
  .top {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 16px;
  }
  .phone-account {
    display: none;
  }
  h1 {
    font-size: 32px;
  }
  .top-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .chips {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin: 24px 0 16px;
  }
  .sort-slot {
    margin-left: auto;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  .empty {
    padding: 48px 0 16px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
    max-width: 480px;
  }
  @media (max-width: 768px) {
    .library {
      padding-top: 16px;
    }
    h1 {
      font-size: 26px;
    }
    .phone-account {
      display: block;
    }
    .top {
      row-gap: 12px;
    }
    .top-actions {
      grid-column: 1 / -1;
    }
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
  }
</style>
