<script context="module">
  import { MAX_PER_ROW } from '../layout.js';
  import { LRU } from '../lru.js';
  const relatedCache = new LRU(30);
</script>

<script>
  import { api } from '../api.js';
  import { artistPages as cache } from '../artistCache.js';
  import { player, isPlaying as playerPlaying, shuffleOn } from '../stores/player.js';
  import { navStore, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { libraryState, refresh, playRows, trackIdOf, playArtistRadio, playingContext, isContext, playAlbum, playArtistTop } from '../library.js';
  import { likedIds, savedAlbumIds, followedIds, collectionLoaded, toggleFollow } from '../stores/collection.js';
  import { toast } from '../stores/toast.js';
  import Radio from '@lucide/svelte/icons/radio';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import { openMenu } from '../stores/contextMenu.js';
  import { longpress } from '../longpress.js';
  import { rowFromDeezerTrack } from '../rows.js';
  import { dayMonth, fansLabel, recordCategory, recordTypeLabel, releasesCount, tracksCount, year } from '../format.js';
  import { countLiked, latestRelease } from '../artistHighlights.js';
  import Hero from '../components/Hero.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';
  import { albumMenuItems, artistMenuItems } from '../cardMenu.js';
  import MediaCard from '../components/MediaCard.svelte';
  import ArtistHighlight from '../components/ArtistHighlight.svelte';
  import ArtistAbout from '../components/ArtistAbout.svelte';
  import { canEdit } from '../stores/me.js';
  import { isLocalId } from '../links.js';
  import Pencil from '@lucide/svelte/icons/pencil';
  import { setRouteTitle } from '../stores/title.js';
  import Shuffle from '@lucide/svelte/icons/shuffle';

  const BESIDE_POPULAR_MIN = 1000;
  const SIDE_BY_SIDE_MIN = 1500;
  const NOTHING_LIKED = { tracks: [], albums: [] };

  let artist = null;
  const tint = pageTint();
  let top = [];
  let topExpanded = false;
  let albums = [];
  let liked = NOTHING_LIKED;
  let leadWidth = 0;
  let loading = true;
  let error = '';
  let category = $navStore.disc || 'all';
  let related = [];
  let relatedExpanded = false;
  let gridWidth = 0;
  let editing = false;
  let editable = false;

  function reload() {
    cache.delete($navStore.id);
    load($navStore.id);
  }

  $: load($navStore.id);
  $: loadRelated($navStore.id);

  async function loadRelated(id) {
    relatedExpanded = false;
    related = relatedCache.get(id) || [];
    try {
      const { artists } = await api.relatedArtists(id);
      relatedCache.set(id, artists);
      if (id === $navStore.id) related = artists || [];
    } catch {
    }
  }

  let seq = 0;
  async function load(id) {
    const my = ++seq;
    error = '';
    const cached = cache.get(id);
    if (cached) apply(cached);
    else {
      artist = null;
      top = [];
      albums = [];
      liked = NOTHING_LIKED;
    }
    loading = !cached;
    try {
      const data = await api.discoverArtist(id);
      if (my !== seq) return;
      cache.set(id, data);
      apply(data);
    } catch (e) {
      if (my === seq) error = e.message;
    } finally {
      if (my === seq) loading = false;
    }
  }

  function apply(data) {
    artist = data.artist;
    editable = !!data.editable;
    albums = data.albums;
    liked = data.liked || NOTHING_LIKED;
    setRouteTitle(`artist:${$navStore.id}`, artist.name);
    top = (data.top || []).map((tr) => ({ ...rowFromDeezerTrack(tr), trackNo: null, discNo: null }));
    tint.from(artist.picture_url);
    refresh(top.map((r) => r.deezerId));
  }

  const byDate = (x, y) => (y.release_date || '').localeCompare(x.release_date || '');
  $: groups = {
    all: [...albums].sort(byDate),
    album: albums.filter((a) => recordCategory(a.record_type) === 'album').sort(byDate),
    ep: albums.filter((a) => recordCategory(a.record_type) === 'ep').sort(byDate),
    single: albums.filter((a) => recordCategory(a.record_type) === 'single').sort(byDate),
  };
  $: shown = groups[category] || [];
  $: perRow = gridWidth < 560 ? 2 : Math.min(MAX_PER_ROW, Math.max(2, Math.floor(gridWidth / 176)));
  $: visible = shown.slice(0, perRow);
  $: relatedVisible = relatedExpanded ? related : related.slice(0, perRow);
  $: latest = latestRelease(albums);
  $: likedSongs = $collectionLoaded ? countLiked($likedIds, liked.tracks, top.map((r) => r.deezerId)) : liked.tracks.length;
  $: likedReleases = $collectionLoaded ? countLiked($savedAlbumIds, liked.albums, albums.map((a) => a.id)) : liked.albums.length;
  $: likedLabel = [likedSongs && tracksCount(likedSongs, $lang), likedReleases && releasesCount(likedReleases, $lang)].filter(Boolean).join(' • ');
  $: latestLabel = latest ? [recordTypeLabel(latest.record_type, $lang), dayMonth(latest.release_date, $lang)].join(' • ') : '';
  $: following = !!artist && $followedIds.has(artist.id);

  function artistMenu(e) {
    openMenu(e, artistMenuItems(artist), { title: artist.name, image: artist.picture_url, round: true });
  }

  function onRadio() {
    playArtistRadio(artist.id).catch((e) => toast(e.message, 'error'));
  }

  function pickCategory(c) {
    category = c;
  }

  $: playingHere = !!artist && isContext($playingContext, 'artist', artist.id);

  $: playingCtx = $playerPlaying ? $playingContext : null;
  const playCard = (start) => start().catch((e) => toast(e.message, 'error'));

  function onPlay() {
    if (playingHere) return player.toggle();
    const first = top.findIndex((r) => trackIdOf(r, $libraryState));
    playRows(top, $shuffleOn ? null : first >= 0 ? first : null, { kind: 'artist', id: artist.id, title: artist.name });
  }
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if artist}
  <div class="page-pad" role="group" on:contextmenu={artistMenu} use:longpress={artistMenu}>
    <Hero image={artist.picture_url} round kind={$t('artist.kind')} title={artist.name}>
      <svelte:fragment slot="meta">{fansLabel(artist.fans, $lang)}</svelte:fragment>
    </Hero>
  </div>

  <div class="actions page-pad">
    <PlayButton size={56} playing={playingHere && $playerPlaying} label={$t('artist.playPopular')} on:click={onPlay} />
    <button class="icon-btn big" class:on={$shuffleOn} title={$t('player.shuffle')} aria-pressed={$shuffleOn} on:click={() => player.toggleShuffle()}>
      <Shuffle size={28} />
    </button>
    <button class="pill outline follow" aria-pressed={following} on:click={() => toggleFollow(artist.id)}>
      <span class="label">{following ? $t('artist.following') : $t('artist.follow')}</span>
    </button>
    <button class="icon-btn" title={$t('artist.radio')} aria-label={$t('artist.radio')} on:click={onRadio}>
      <Radio size={24} />
    </button>
    <button class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: artist.name })} on:click={artistMenu}>
      <Ellipsis size={24} />
    </button>
    {#if isLocalId(artist.id) ? editable : $canEdit}
      <button class="icon-btn" title={$t('edit.artistTitle')} aria-label={$t('edit.artistTitle')} on:click={() => (editing = true)}>
        <Pencil size={22} />
      </button>
    {/if}
  </div>

  {#if editing}
    {#await import('../components/EditDialog.svelte') then m}
      <svelte:component this={m.default} kind="artist" id={artist.library_id || null} deezerId={artist.id} on:close={() => (editing = false)} on:saved={() => ((editing = false), reload())} />
    {/await}
  {/if}

  {#if top.length || latest || likedLabel}
    <div class="page-pad">
      <div class="lead" class:beside={top.length && leadWidth >= BESIDE_POPULAR_MIN} class:side-by-side={top.length && leadWidth >= SIDE_BY_SIDE_MIN} bind:clientWidth={leadWidth}>
        {#if latest || likedLabel}
          <div class="highlights">
            {#if likedLabel}
              <ArtistHighlight
                href={paths.artistLiked(artist.id)}
                label={$t('artist.youLiked')}
                title={likedLabel}
                subtitle={$t('artist.likedBy', { name: artist.name })}
                image={artist.picture_url}
                liked
              />
            {/if}
            {#if latest}
              <ArtistHighlight
                href={paths.album(latest.id)}
                label={$t('artist.latestRelease')}
                title={latest.title}
                subtitle={latestLabel}
                image={latest.cover_url}
                menu={() => albumMenuItems(latest)}
              />
            {/if}
          </div>
        {/if}
        {#if top.length}
          <section class="popular">
            <div class="section-head">
              <h2>{$t('artist.popular')}</h2>
              {#if top.length > 5}
                <button class="show-all" on:click={() => (topExpanded = !topExpanded)}>
                  {topExpanded ? $t('disc.showLess') : $t('disc.showAll')}
                </button>
              {/if}
            </div>
            <TrackList rows={topExpanded ? top : top.slice(0, 5)} queue={top} showCover header={false} context={{ kind: 'artist', id: artist.id, title: artist.name }} on:changed={reload} />
          </section>
        {/if}
      </div>
    </div>
  {/if}

  {#if albums.length}
    <section class="page-pad">
      <div class="section-head">
        <h2>{$t('artist.discography')}</h2>
        {#if shown.length > perRow}
          <a class="show-all" href={paths.discography(artist.id, category)} use:link>{$t('disc.showAll')}</a>
        {/if}
      </div>
      <div class="chips" role="tablist">
        {#each ['all', 'album', 'ep', 'single'] as c}
          {#if c === 'all' || groups[c].length}
            <button class="pill" class:active={category === c} role="tab" aria-selected={category === c} on:click={() => pickCategory(c)}>
              {$t(`disc.${c}`)}
            </button>
          {/if}
        {/each}
      </div>
      <div class="grid" bind:clientWidth={gridWidth}>
        {#each visible as a (a.id)}
          <MediaCard
            href={paths.album(a.id)}
            menu={() => albumMenuItems(a)}
            image={a.cover_url}
            hidden={a.hidden}
            title={a.title}
            subtitle={[year(a.release_date), recordTypeLabel(a.record_type, $lang)].filter(Boolean).join(' • ')}
            onPlay={() => playCard(() => playAlbum(a.id))}
            playing={isContext(playingCtx, 'album', a.id)}
          />
        {/each}
      </div>
    </section>
  {/if}
  <ArtistAbout artistId={artist.id} />
  {#if related.length}
    <section class="page-pad">
      <div class="section-head">
        <h2>{$t('artist.related')}</h2>
        {#if related.length > perRow}
          <button class="show-all" on:click={() => (relatedExpanded = !relatedExpanded)}>
            {relatedExpanded ? $t('disc.showLess') : $t('disc.showAll')}
          </button>
        {/if}
      </div>
      <div class="grid">
        {#each relatedVisible as a (a.id)}
          <MediaCard
            href={paths.artist(a.id)}
            menu={() => artistMenuItems(a)}
            image={a.picture_url}
            round
            title={a.name}
            subtitle={$t('artist.kind')}
            onPlay={() => playCard(() => playArtistTop(a.id))}
            playing={isContext(playingCtx, 'artist', a.id)}
          />
        {/each}
      </div>
    </section>
  {/if}
{:else if loading}
  <div class="page-pad skeleton">
    <div class="sk-art"></div>
    <div class="sk-line"></div>
  </div>
{/if}

<style>
  .actions {
    display: flex;
    align-items: center;
    gap: 24px;
    padding-top: 20px;
    padding-bottom: 24px;
  }
  .icon-btn.big {
    width: 44px;
    height: 44px;
  }
  .follow {
    padding: 0 16px;
    font-weight: 700;
  }
  @media (max-width: 768px) {
    .actions {
      gap: 0;
    }
    .actions > :global(.play-button) {
      margin-right: 6px;
    }
    .actions > .icon-btn {
      flex: 0 1000000 48px;
      min-width: 30px;
      border-radius: 999px;
    }
    .actions > .icon-btn.big {
      flex-basis: 56px;
      min-width: 36px;
    }
    .follow {
      flex: 0 1 auto;
      min-width: 0;
      margin: 0 6px;
      padding: 0 12px;
      font-size: 13px;
    }
    .follow .label {
      overflow: hidden;
      text-overflow: ellipsis;
    }
  }
  section,
  .highlights {
    margin-bottom: 40px;
  }
  .highlights {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr));
    gap: 24px;
    max-width: 900px;
  }
  .lead.beside {
    display: flex;
    align-items: flex-start;
    gap: 48px;
  }
  .beside .popular {
    flex: 1;
    min-width: 0;
  }
  .beside .highlights {
    order: 1;
    flex-shrink: 0;
    grid-template-columns: 300px;
    gap: 40px;
  }
  .side-by-side .highlights {
    grid-template-columns: none;
    grid-auto-flow: column;
    grid-auto-columns: 300px;
    gap: 24px;
  }
  h2 {
    font-size: 24px;
    margin-bottom: 16px;
  }
  .section-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 16px;
  }
  .show-all {
    font-size: 14px;
    font-weight: 700;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .show-all:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
  .chips {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
    flex-wrap: wrap;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
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
    border-radius: 50%;
    background: var(--bg-card);
  }
  .sk-line {
    height: 72px;
    width: 420px;
    max-width: 50vw;
    border-radius: 8px;
    background: var(--bg-card);
  }
  @media (max-width: 768px) {
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
    .highlights {
      gap: 16px;
      margin-bottom: 32px;
    }
    h2 {
      font-size: 20px;
    }
    .skeleton {
      flex-direction: column;
      align-items: center;
    }
  }
</style>
