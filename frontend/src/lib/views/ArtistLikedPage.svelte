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
  import MediaCard from '../components/MediaCard.svelte';
  import CardGrid from '../components/CardGrid.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import SectionHead from '../components/SectionHead.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import StickyBar from '../components/StickyBar.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';

  const FIRST_SONGS = 10;
  const NAME_SLOT = '{name}';

  let artist = null;
  let tracks = [];
  let albums = [];
  let error = '';
  let expanded = (fromHistory() && expandedLists.get($navStore.id)) || { songs: false, releases: false };
  let perRow = 2;

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
  <StickyBar>
    <svelte:fragment slot="lead">
      {#if songs.length}
        <PlayButton size={48} playing={playingHere && $playerPlaying} on:click={onPlay} />
      {/if}
    </svelte:fragment>
    {songs.length ? beforeName : ''}<a href={paths.artist(artist.id)} use:link>{artist.name}</a>{songs.length ? afterName : ''}
  </StickyBar>

  <div class="page-pad sections">
    {#if songs.length}
      <section>
        <TrackList rows={shownSongs} queue={songs} showCover showAlbum showAdded {context} on:changed={() => load(id)} />
        {#if songs.length > FIRST_SONGS}
          <button class="text-btn more" on:click={() => toggleList('songs')}>
            {expanded.songs ? $t('disc.showLess') : $t('artist.seeMore')}
          </button>
        {/if}
      </section>
    {/if}

    {#if releases.length}
      <section>
        <SectionHead title={$t('artist.likedReleases')}>
          {#if releases.length > perRow}
            <button class="text-btn" on:click={() => toggleList('releases')}>
              {expanded.releases ? $t('disc.showLess') : $t('disc.showAll')}
            </button>
          {/if}
        </SectionHead>
        <CardGrid bind:perRow>
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
        </CardGrid>
      </section>
    {/if}

    {#if !songs.length && !releases.length}
      <EmptyState title={$t('liked.empty')} text={$t('liked.emptyHint')} />
    {/if}
  </div>
{:else}
  <div class="page-pad"><Skeleton kind="bar" /></div>
{/if}

<style>
  .more {
    margin-top: 8px;
    padding: 8px 12px;
  }
  @media (max-width: 768px) {
    .more {
      padding: 8px;
    }
  }
</style>
