<script context="module">
  import { LRU } from '../lru.js';
  const cache = new LRU(20);
</script>

<script>
  import { onDestroy } from 'svelte';
  import SearchField from '../components/SearchField.svelte';
  import { sized } from '../imageproxy.js';
  import { api } from '../api.js';
  import { navStore, paths, link, lastSearch } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { refresh, playingContext, isContext, playAlbum, playArtistTop } from '../library.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { toast } from '../stores/toast.js';
  import { recordTypeLabel, year } from '../format.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { rowFromDeezerTrack } from '../rows.js';
  import TrackList from '../components/TrackList.svelte';
  import { albumMenuItems, artistMenuItems } from '../cardMenu.js';
  import MediaCard from '../components/MediaCard.svelte';
  import CardGrid from '../components/CardGrid.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import SectionHead from '../components/SectionHead.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import History from '@lucide/svelte/icons/history';
  import { recentSearches, rememberSearch, removeRecent } from '../stores/recentSearches.js';
  import { tagTitle, mixHue } from '../mixes.js';

  let genres = [];
  api
    .genres()
    .then((d) => (genres = d.genres))
    .catch(() => {});

  let results = null;
  let loading = false;
  let error = '';

  $: run($navStore.q || '');

  const tint = pageTint();
  let seq = 0;
  let inflight = null;
  onDestroy(() => inflight?.abort());
  async function run(q) {
    const my = ++seq;
    inflight?.abort();
    inflight = null;
    lastSearch.set(q);
    error = '';
    tint.none();
    if (!q) {
      results = null;
      loading = false;
      return;
    }
    const cached = cache.get(q);
    results = cached || null;
    loading = !cached;
    if (cached) refresh(cached.tracks.map((t) => t.id));
    const controller = new AbortController();
    inflight = controller;
    try {
      const fresh = await api.search(q, { signal: controller.signal });
      if (my !== seq) return;
      cache.set(q, fresh);
      results = fresh;
      refresh(fresh.tracks.map((t) => t.id));
    } catch (e) {
      if (my === seq && e.name !== 'AbortError') error = e.message;
    } finally {
      if (my === seq) {
        loading = false;
        inflight = null;
      }
    }
  }

  $: rows = (results?.tracks || []).map((tr) => ({ ...rowFromDeezerTrack(tr), trackNo: null, discNo: null }));
  $: topArtist = results?.artists?.[0];
  $: people = results?.users || [];
  $: empty = results && !rows.length && !results.artists?.length && !results.albums?.length && !people.length;

  $: playingCtx = $playerPlaying ? $playingContext : null;
  const playCard = (start) => start().catch((e) => toast(e.message, 'error'));
</script>

<div class="page-pad search">
  <div class="phone-search"><SearchField /></div>
  {#if !$navStore.q}
    <div class="sections">
      {#if $recentSearches.length}
        <section>
          <SectionHead title={$t('search.recent')}>
            <button class="text-btn" on:click={() => recentSearches.set([])}>{$t('search.clearRecent')}</button>
          </SectionHead>
          <ul class="recent">
            {#each $recentSearches as q (q)}
              <li>
                <a href={paths.search(q)} use:link><History size={18} /> <span>{q}</span></a>
                <button class="icon-btn" aria-label={$t('search.removeRecent', { q })} title={$t('search.removeRecent', { q })} on:click={() => recentSearches.update((l) => removeRecent(l, q))}><X size={16} /></button>
              </li>
            {/each}
          </ul>
        </section>
      {:else}
        <EmptyState icon={Search} title={$t('search.hintTitle')} text={$t('search.hintText')} />
      {/if}
      {#if genres.length}
        <section>
          <SectionHead title={$t('search.genres')} />
          <div class="tiles">
            {#each genres as g (g.slug)}
              <a class="tile" href={paths.genre(g.slug)} use:link style:--hue={mixHue(`tag-${g.slug}`)}>{tagTitle(g.name)}</a>
            {/each}
          </div>
        </section>
      {/if}
    </div>
  {:else if error}
    <p class="error-text">{error}</p>
  {:else if empty}
    <EmptyState title={$t('search.none', { q: $navStore.q })} text={$t('search.noneText')} />
  {:else if results}
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="sections" on:click|capture={(e) => e.target instanceof Element && e.target.closest('a, button, .row') && rememberSearch($navStore.q)}>
      <div class="top">
        {#if topArtist}
          <section class="best">
            <SectionHead title={$t('search.best')} />
            <a class="best-card" href={paths.artist(topArtist.id)} use:link>
              {#if topArtist.picture_url}<img src={sized(topArtist.picture_url, 320)} alt="" />{/if}
              <div class="best-name">{topArtist.name}</div>
              <span class="best-kind">{$t('artist.kind')}</span>
            </a>
          </section>
        {/if}
        {#if rows.length}
          <section class="songs">
            <SectionHead title={$t('search.tracks')} />
            <TrackList rows={rows.slice(0, 4)} showCover header={false} numbered={false} playSimilar on:changed={() => (cache.delete($navStore.q), run($navStore.q))} />
          </section>
        {/if}
      </div>

      {#if people.length}
        <section>
          <SectionHead title={$t('search.people')} />
          <CardGrid>
            {#each people as p (p.id)}
              <MediaCard href={paths.user(p.username)} round title={p.username} subtitle={$t('people.kind')}>
                <div slot="art" class="initial" style="--hue: {mixHue(`user-${p.username.toLowerCase()}`)}">{p.username.slice(0, 1).toUpperCase()}</div>
              </MediaCard>
            {/each}
          </CardGrid>
        </section>
      {/if}

      {#if results.artists.length}
        <section>
          <SectionHead title={$t('search.artists')} />
          <CardGrid>
            {#each results.artists.slice(0, 6) as a (a.id)}
              <MediaCard
                href={paths.artist(a.id)}
                menu={() => artistMenuItems(a)}
                image={a.picture_url}
                title={a.name}
                subtitle={$t('artist.kind')}
                round
                onPlay={() => playCard(() => playArtistTop(a.id))}
                playing={isContext(playingCtx, 'artist', a.id)}
              />
            {/each}
          </CardGrid>
        </section>
      {/if}

      {#if results.albums.length}
        <section>
          <SectionHead title={$t('search.albums')} />
          <CardGrid>
            {#each results.albums.slice(0, 6) as a (a.id)}
              <MediaCard
                href={paths.album(a.id)}
                menu={() => albumMenuItems(a)}
                image={a.cover_url}
                hidden={a.hidden}
                title={a.title}
                subtitle={[year(a.release_date), recordTypeLabel(a.record_type, $lang), a.artist_name].filter(Boolean).join(' • ')}
                onPlay={() => playCard(() => playAlbum(a.id))}
                playing={isContext(playingCtx, 'album', a.id)}
              />
            {/each}
          </CardGrid>
        </section>
      {/if}

      {#if rows.length > 4}
        <section>
          <SectionHead title={$t('search.more')} />
          <TrackList rows={rows.slice(4)} showCover showAlbum header={false} numbered={false} playSimilar on:changed={() => (cache.delete($navStore.q), run($navStore.q))} />
        </section>
      {/if}
    </div>
  {:else if loading}
    <Skeleton kind="rows" />
  {/if}
</div>

<style>
  .phone-search {
    display: none;
  }
  .initial {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    background: hsl(var(--hue) 45% 38%);
    color: #fff;
    font-size: 56px;
    font-weight: 800;
  }
  .search {
    padding-top: 24px;
  }
  .top {
    display: grid;
    grid-template-columns: minmax(280px, 2fr) 3fr;
    gap: 32px 24px;
  }
  .top > section {
    min-width: 0;
  }
  .best-card {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 20px;
    border-radius: var(--radius);
    background: var(--bg-elevated);
    transition: background-color 0.2s;
    min-height: 240px;
  }
  @media (hover: hover) {
    .best-card:hover {
      background: var(--bg-hover);
    }
  }
  .best-card img {
    width: 96px;
    height: 96px;
    border-radius: 50%;
    object-fit: cover;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
  }
  .best-name {
    font-family: var(--font-display);
    font-size: 32px;
    font-weight: 700;
    letter-spacing: -0.03em;
    line-height: 1.1;
    overflow-wrap: anywhere;
  }
  .best-kind {
    align-self: flex-start;
    padding: 4px 12px;
    border-radius: 999px;
    background: rgba(0, 0, 0, 0.35);
    font-size: 13px;
    font-weight: 600;
  }
  .recent {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    column-gap: 16px;
  }
  .recent li {
    display: flex;
    align-items: center;
    border-radius: var(--radius-md);
  }
  .recent li a {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 8px;
    color: var(--text);
  }
  .recent li a span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .recent li a :global(svg) {
    color: var(--text-dim);
    flex-shrink: 0;
  }
  @media (hover: hover) {
    .recent li:hover {
      background: var(--bg-hover);
    }
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 16px;
  }
  .tile {
    display: flex;
    align-items: flex-start;
    aspect-ratio: 16 / 9;
    padding: 14px 16px;
    border-radius: var(--radius);
    background: hsl(var(--hue) 55% 42%);
    color: #fff;
    font-family: var(--font-display);
    font-weight: 700;
    font-size: 18px;
    line-height: 1.2;
    overflow: hidden;
    overflow-wrap: anywhere;
  }
  @media (hover: hover) {
    .tile:hover {
      filter: brightness(1.1);
    }
  }
  @media (max-width: 1000px) {
    .top {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 768px) {
    .phone-search {
      display: flex;
      padding: 0 0 8px;
    }
    .search {
      padding-top: 16px;
    }
    .tiles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .tile {
      font-size: 16px;
    }
    .best-card {
      min-height: 0;
      flex-direction: row;
      align-items: center;
    }
    .best-card img {
      width: 64px;
      height: 64px;
    }
    .best-name {
      flex: 1;
      font-size: 22px;
    }
  }
</style>
