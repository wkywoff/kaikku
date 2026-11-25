<script context="module">
  import { LRU } from '../lru.js';
  const cache = new LRU(20);
  const expandedLists = new LRU(20);
</script>

<script>
  import { api } from '../api.js';
  import { player, isPlaying as playerPlaying } from '../stores/player.js';
  import { navStore, paths, link, fromHistory } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint, LIKED_TINT } from '../stores/theme.js';
  import { setRouteTitle } from '../stores/title.js';
  import { toast } from '../stores/toast.js';
  import { refresh, playRows, playingContext, isContext, playAlbum } from '../library.js';
  import { likedIds, savedAlbumIds, collectionLoaded } from '../stores/collection.js';
  import { rowFromCollectionTrack } from '../rows.js';
  import { recordTypeLabel, year } from '../format.js';
  import { albumMenuItems } from '../cardMenu.js';
  import { cardsPerRow } from '../layout.js';
  import MediaCard from '../components/MediaCard.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';

  const FIRST_SONGS = 10;
  const NAME_SLOT = '{name}';

  let artist = null;
  let tracks = [];
  let albums = [];
  let error = '';
  let expanded = (fromHistory() && expandedLists.get($navStore.id)) || { songs: false, releases: false };
  let grid;
  let gridWidth = 0;

  pageTint().none(LIKED_TINT);

  $: id = $navStore.id;
  $: load(id);

  async function load(artistId) {
    error = '';
    const cached = cache.get(artistId);
    if (cached) apply(cached);
    try {
      const data = await api.artistLiked(artistId);
      if (artistId !== $navStore.id) return;
      cache.set(artistId, data);
      apply(data);
    } catch (e) {
      if (!cached) error = e.message;
    }
  }

  function toggleList(list) {
    expanded = { ...expanded, [list]: !expanded[list] };
    expandedLists.set(id, expanded);
  }

  function apply(data) {
    artist = data.artist;
    albums = data.albums;
    tracks = data.tracks.map(rowFromCollectionTrack);
    refresh(tracks.map((r) => r.deezerId));
  }

  $: songs = $collectionLoaded ? tracks.filter((r) => $likedIds.has(r.deezerId)) : tracks;
  $: releases = $collectionLoaded ? albums.filter((a) => $savedAlbumIds.has(a.id)) : albums;
  $: shownSongs = expanded.songs ? songs : songs.slice(0, FIRST_SONGS);
  $: perRow = gridWidth && grid ? cardsPerRow(grid) : 2;
  $: shownReleases = expanded.releases ? releases : releases.slice(0, perRow);

  $: title = artist ? $t('artist.likedSongs', { name: artist.name }) : '';
  $: [beforeName, afterName] = $t('artist.likedSongs').split(NAME_SLOT);
  $: if (artist) setRouteTitle(`artistLiked:${id}`, title);

  $: context = { kind: 'liked', id, title };
  $: playingHere = isContext($playingContext, 'liked', id);
  $: playingCtx = $playerPlaying ? $playingContext : null;

  function onPlay() {
    if (playingHere) return player.toggle();
    playRows(songs, null, context);
  }

  function onPlayRelease(albumId) {
    playAlbum(albumId).catch((e) => toast(e.message, 'error'));
  }

  const subtitle = (a, language) => [year(a.release_date), recordTypeLabel(a.record_type, language)].filter(Boolean).join(' • ');
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if artist}
  <div class="bar page-pad">
    {#if songs.length}
      <PlayButton size={48} playing={playingHere && $playerPlaying} on:click={onPlay} />
    {/if}
    <h1>
      {songs.length ? beforeName : ''}<a href={paths.artist(artist.id)} use:link>{artist.name}</a>{songs.length ? afterName : ''}
    </h1>
  </div>

  {#if songs.length}
    <section class="page-pad">
      <TrackList rows={shownSongs} queue={songs} showCover showAlbum showAdded {context} on:changed={() => load(id)} />
      {#if songs.length > FIRST_SONGS}
        <button class="more" on:click={() => toggleList('songs')}>
          {expanded.songs ? $t('disc.showLess') : $t('artist.seeMore')}
        </button>
      {/if}
    </section>
  {/if}

  {#if releases.length}
    <section class="page-pad">
      <div class="section-head">
        <h2>{$t('artist.likedReleases')}</h2>
        {#if releases.length > perRow}
          <button class="show-all" on:click={() => toggleList('releases')}>
            {expanded.releases ? $t('disc.showLess') : $t('disc.showAll')}
          </button>
        {/if}
      </div>
      <div class="grid" bind:this={grid} bind:clientWidth={gridWidth}>
        {#each shownReleases as a (a.id)}
          <MediaCard
            href={paths.album(a.id)}
            menu={() => albumMenuItems(a)}
            image={a.cover_url}
            hidden={a.hidden}
            title={a.title}
            subtitle={subtitle(a, $lang)}
            onPlay={() => onPlayRelease(a.id)}
            playing={isContext(playingCtx, 'album', a.id)}
          />
        {/each}
      </div>
    </section>
  {/if}

  {#if !songs.length && !releases.length}
    <div class="page-pad">
      <p>{$t('liked.empty')}</p>
      <p class="muted">{$t('liked.emptyHint')}</p>
    </div>
  {/if}
{:else}
  <div class="page-pad">
    <div class="sk-bar"></div>
  </div>
{/if}

<style>
  .bar {
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 16px;
    padding-top: 20px;
    padding-bottom: 16px;
    background: var(--bg);
    box-shadow: 0 0 0 100vmax var(--bg);
    clip-path: inset(0 -100vmax);
  }
  h1 {
    min-width: 0;
    font-size: 24px;
    line-height: 1.2;
    overflow-wrap: anywhere;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  @media (hover: hover) {
    h1 a:hover {
      text-decoration: underline;
    }
  }
  section {
    margin-bottom: 40px;
  }
  h2 {
    font-size: 24px;
  }
  .section-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 8px;
  }
  .show-all,
  .more {
    font-size: 14px;
    font-weight: 700;
    color: var(--text-dim);
  }
  .show-all {
    flex-shrink: 0;
  }
  .more {
    margin-top: 8px;
    padding: 8px 12px;
  }
  @media (hover: hover) {
    .show-all:hover,
    .more:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  .sk-bar {
    height: 32px;
    width: 240px;
    margin-top: 24px;
    border-radius: 8px;
    background: var(--bg-card);
  }
  @media (max-width: 768px) {
    .bar {
      padding-top: 12px;
    }
    h1 {
      font-size: 18px;
    }
    h2 {
      font-size: 20px;
    }
    .more {
      padding: 8px;
    }
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
  }
</style>
