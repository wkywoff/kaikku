<script context="module">
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
  import HeroActions from '../components/HeroActions.svelte';
  import SectionHead from '../components/SectionHead.svelte';
  import CardGrid from '../components/CardGrid.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import FollowButton from '../components/FollowButton.svelte';
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
  let perRow = 2;
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

  <HeroActions class="fit">
    <PlayButton size={56} playing={playingHere && $playerPlaying} label={$t('artist.playPopular')} on:click={onPlay} />
    <button class="icon-btn big" class:on={$shuffleOn} title={$t('player.shuffle')} aria-pressed={$shuffleOn} on:click={() => player.toggleShuffle()}>
      <Shuffle size={28} />
    </button>
    <FollowButton {following} label={following ? $t('artist.following') : $t('artist.follow')} on:click={() => toggleFollow(artist.id)} />
    <button class="icon-btn" title={$t('artist.radio')} aria-label={$t('artist.radio')} on:click={onRadio}>
      <Radio size={24} />
    </button>
    {#if isLocalId(artist.id) ? editable : $canEdit}
      <button class="icon-btn" title={$t('edit.artistTitle')} aria-label={$t('edit.artistTitle')} on:click={() => (editing = true)}>
        <Pencil size={22} />
      </button>
    {/if}
    <button class="icon-btn" title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: artist.name })} on:click={artistMenu}>
      <Ellipsis size={24} />
    </button>
  </HeroActions>

  {#if editing}
    {#await import('../components/EditDialog.svelte') then m}
      <svelte:component this={m.default} kind="artist" id={artist.library_id || null} deezerId={artist.id} on:close={() => (editing = false)} on:saved={() => ((editing = false), reload())} />
    {/await}
  {/if}

  <div class="page-pad sections">
    {#if top.length || latest || likedLabel}
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
            <SectionHead title={$t('artist.popular')}>
              {#if top.length > 5}
                <button class="text-btn" on:click={() => (topExpanded = !topExpanded)}>{topExpanded ? $t('disc.showLess') : $t('disc.showAll')}</button>
              {/if}
            </SectionHead>
            <TrackList rows={topExpanded ? top : top.slice(0, 5)} queue={top} showCover header={false} context={{ kind: 'artist', id: artist.id, title: artist.name }} on:changed={reload} />
          </section>
        {/if}
      </div>
    {/if}

    {#if albums.length}
      <section>
        <SectionHead title={$t('artist.discography')}>
          {#if shown.length > perRow}
            <a class="text-btn" href={paths.discography(artist.id, category)} use:link>{$t('disc.showAll')}</a>
          {/if}
        </SectionHead>
        <div class="chips" role="tablist">
          {#each ['all', 'album', 'ep', 'single'] as c}
            {#if c === 'all' || groups[c].length}
              <button class="pill" class:active={category === c} role="tab" aria-selected={category === c} on:click={() => pickCategory(c)}>
                {$t(`disc.${c}`)}
              </button>
            {/if}
          {/each}
        </div>
        <CardGrid bind:perRow>
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
        </CardGrid>
      </section>
    {/if}
    <ArtistAbout artistId={artist.id} />
    {#if related.length}
      <section>
        <SectionHead title={$t('artist.related')}>
          {#if related.length > perRow}
            <button class="text-btn" on:click={() => (relatedExpanded = !relatedExpanded)}>{relatedExpanded ? $t('disc.showLess') : $t('disc.showAll')}</button>
          {/if}
        </SectionHead>
        <CardGrid bind:perRow>
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
        </CardGrid>
      </section>
    {/if}
  </div>
{:else if loading}
  <div class="page-pad"><Skeleton kind="hero" round /></div>
{/if}

<style>
  @media (max-width: 768px) {
    :global(.page-pad.hero-actions.fit) {
      gap: 0;
    }
    :global(.page-pad.hero-actions.fit > .play-button) {
      margin-right: 6px;
    }
    :global(.page-pad.hero-actions.fit > .icon-btn) {
      flex: 0 1000000 48px;
      min-width: 30px;
      border-radius: 999px;
    }
    :global(.page-pad.hero-actions.fit > .icon-btn.big) {
      flex-basis: 56px;
      min-width: 36px;
    }
    :global(.page-pad.hero-actions.fit > .follow) {
      flex: 0 1 auto;
      margin: 0 6px;
      padding: 0 12px;
      font-size: 13px;
    }
  }
  .lead {
    display: flex;
    flex-direction: column;
    gap: 32px;
  }
  .highlights {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 280px), 1fr));
    gap: 24px;
    max-width: 900px;
  }
  .lead.beside {
    flex-direction: row;
    align-items: flex-start;
    gap: 48px;
  }
  .popular {
    min-width: 0;
  }
  .beside .popular {
    flex: 1;
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
  .chips {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
    flex-wrap: wrap;
  }
  @media (max-width: 768px) {
    .highlights {
      gap: 16px;
    }
  }
</style>
