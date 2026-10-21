<script context="module">
  import { LRU } from '../lru.js';
  const cache = new LRU(20); // запрос -> результаты, чтобы «назад» не ждал Deezer
</script>

<script>
  // Результаты поиска (запрос — в адресе, поле ввода — в верхней панели):
  // лучший результат, треки, исполнители, альбомы — как у Spotify.
  import { onDestroy } from 'svelte';
  import SearchField from '../components/SearchField.svelte';
  import { sized } from '../imageproxy.js';
  import { api } from '../api.js';
  import { navStore, paths, link, lastSearch } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { refresh } from '../library.js';
  import { recordTypeLabel, year } from '../format.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { rowFromDeezerTrack } from '../rows.js';
  import TrackList from '../components/TrackList.svelte';
  import { albumMenuItems, artistMenuItems } from '../cardMenu.js';
  import MediaCard from '../components/MediaCard.svelte';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import History from '@lucide/svelte/icons/history';
  import { recentSearches, rememberSearch, removeRecent } from '../stores/recentSearches.js';
  import { tagTitle, mixHue } from '../mixes.js';

  // Плитки жанров — на пустой странице поиска, один раз за открытие.
  let genres = [];
  api
    .genres()
    .then((d) => (genres = d.genres))
    .catch(() => {});

  let results = null; // { tracks, artists, albums }
  let loading = false;
  let error = '';

  $: run($navStore.q || '');

  const tint = pageTint();
  let seq = 0;
  // Запрос, который ещё идёт: человек печатает дальше — он отменяется, и
  // сервер не тратит на брошенную выдачу общий лимит Deezer.
  let inflight = null;
  onDestroy(() => inflight?.abort());
  async function run(q) {
    const my = ++seq;
    inflight?.abort();
    inflight = null;
    lastSearch.set(q);
    error = '';
    tint.from(null);
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

  $: rows = (results?.tracks || []).map((tr) => rowFromDeezerTrack(tr));
  $: topArtist = results?.artists?.[0];
  $: people = results?.users || [];
  $: empty = results && !rows.length && !results.artists?.length && !results.albums?.length && !people.length;
</script>

<div class="page-pad search">
  <!-- На телефоне верхней панели нет — поле поиска здесь. -->
  <div class="phone-search"><SearchField /></div>
  {#if !$navStore.q}
    {#if $recentSearches.length}
      <section class="recent">
        <div class="recent-head">
          <h2>{$t('search.recent')}</h2>
          <button class="clear-recent" on:click={() => recentSearches.set([])}>{$t('search.clearRecent')}</button>
        </div>
        <ul>
          {#each $recentSearches as q (q)}
            <li>
              <a href={paths.search(q)} use:link><History size={18} /> <span>{q}</span></a>
              <button class="icon-btn" aria-label={$t('search.removeRecent', { q })} title={$t('search.removeRecent', { q })} on:click={() => recentSearches.update((l) => removeRecent(l, q))}><X size={16} /></button>
            </li>
          {/each}
        </ul>
      </section>
    {:else}
      <div class="hint">
        <Search size={48} />
        <h2>{$t('search.hintTitle')}</h2>
        <p class="muted">{$t('search.hintText')}</p>
      </div>
    {/if}
    {#if genres.length}
      <section class="genres">
        <h2>{$t('search.genres')}</h2>
        <div class="tiles">
          {#each genres as g (g.slug)}
            <a class="tile" href={paths.genre(g.slug)} use:link style:--hue={mixHue(`tag-${g.slug}`)}>{tagTitle(g.name)}</a>
          {/each}
        </div>
      </section>
    {/if}
  {:else if error}
    <p class="error-text">{error}</p>
  {:else if empty}
    <div class="hint">
      <h2>{$t('search.none', { q: $navStore.q })}</h2>
      <p class="muted">{$t('search.noneText')}</p>
    </div>
  {:else if results}
    <!-- Что-то из выдачи открыли или включили — запрос запоминается. -->
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <div class="results" on:click|capture={(e) => e.target instanceof Element && e.target.closest('a, button, .row') && rememberSearch($navStore.q)}>
    <div class="top">
      {#if topArtist}
        <section class="best">
          <h2>{$t('search.best')}</h2>
          <a class="best-card" href={paths.artist(topArtist.id)} use:link>
            {#if topArtist.picture_url}<img src={sized(topArtist.picture_url, 320)} alt="" />{/if}
            <div class="best-name">{topArtist.name}</div>
            <span class="best-kind">{$t('artist.kind')}</span>
          </a>
        </section>
      {/if}
      {#if rows.length}
        <section class="songs">
          <h2>{$t('search.tracks')}</h2>
          <TrackList rows={rows.slice(0, 4)} showCover header={false} numbered={false} playSimilar on:changed={() => (cache.delete($navStore.q), run($navStore.q))} />
        </section>
      {/if}
    </div>

    <!-- Человек — только по нику целиком (internal/api/profiles.go). -->
    {#if people.length}
      <section>
        <h2>{$t('search.people')}</h2>
        <div class="row-grid">
          {#each people as p (p.id)}
            <MediaCard href={paths.user(p.username)} round title={p.username} subtitle={$t('people.kind')}>
              <div slot="art" class="initial" style="--hue: {mixHue(`user-${p.username.toLowerCase()}`)}">{p.username.slice(0, 1).toUpperCase()}</div>
            </MediaCard>
          {/each}
        </div>
      </section>
    {/if}

    {#if results.artists.length}
      <section>
        <h2>{$t('search.artists')}</h2>
        <div class="row-grid">
          {#each results.artists.slice(0, 6) as a (a.id)}
            <MediaCard href={paths.artist(a.id)} menu={() => artistMenuItems(a)} image={a.picture_url} title={a.name} subtitle={$t('artist.kind')} round />
          {/each}
        </div>
      </section>
    {/if}

    {#if results.albums.length}
      <section>
        <h2>{$t('search.albums')}</h2>
        <div class="row-grid">
          {#each results.albums.slice(0, 6) as a (a.id)}
            <MediaCard
              href={paths.album(a.id)}
              menu={() => albumMenuItems(a)}
              image={a.cover_url}
              hidden={a.hidden}
              title={a.title}
              subtitle={[year(a.release_date), recordTypeLabel(a.record_type, $lang), a.artist_name].filter(Boolean).join(' • ')}
            />
          {/each}
        </div>
      </section>
    {/if}

    {#if rows.length > 4}
      <section>
        <h2>{$t('search.more')}</h2>
        <TrackList rows={rows.slice(4)} showCover showAlbum header={false} numbered={false} playSimilar on:changed={() => (cache.delete($navStore.q), run($navStore.q))} />
      </section>
    {/if}
    </div>
  {:else if loading}
    <p class="muted">{$t('search.searching')}</p>
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
  @media (max-width: 768px) {
    .phone-search {
      display: flex;
      padding: 12px 0 8px;
    }
  }
  .search {
    padding-top: 24px;
  }
  section {
    margin-bottom: 32px;
    min-width: 0;
  }
  h2 {
    font-size: 24px;
    margin-bottom: 12px;
  }
  .top {
    display: grid;
    grid-template-columns: minmax(280px, 2fr) 3fr;
    gap: 24px;
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
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }
  .best-name {
    font-size: 32px;
    font-weight: 800;
    letter-spacing: -0.03em;
    line-height: 1.1;
  }
  .best-kind {
    align-self: flex-start;
    padding: 4px 12px;
    border-radius: 999px;
    background: rgba(0, 0, 0, 0.35);
    font-size: 13px;
    font-weight: 600;
  }
  .row-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    grid-auto-rows: min-content;
    margin: 0 -12px;
  }
  .recent {
    max-width: 640px;
    padding-top: 8px;
  }
  .recent-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 8px;
  }
  .recent h2 {
    font-size: 22px;
  }
  .clear-recent {
    font-size: 14px;
    font-weight: 700;
    color: var(--text-dim);
  }
  .recent ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .recent li {
    display: flex;
    align-items: center;
    border-radius: 6px;
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
    .clear-recent:hover {
      color: var(--text);
    }
  }
  .genres {
    margin-top: 32px;
  }
  .genres h2 {
    font-size: 22px;
    margin-bottom: 12px;
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
    border-radius: 8px;
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
  @media (max-width: 768px) {
    .tiles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .tile {
      font-size: 16px;
    }
  }
  .hint {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 12px;
    padding: 80px 0;
    color: var(--text-dim);
  }
  .hint h2 {
    color: var(--text);
  }
  @media (max-width: 1000px) {
    .top {
      grid-template-columns: 1fr;
    }
  }
  @media (max-width: 768px) {
    .search {
      padding-top: 16px;
    }
    h2 {
      font-size: 20px;
    }
    .row-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
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
      font-size: 22px;
      flex: 1;
    }
  }
</style>
