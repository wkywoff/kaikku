<script>
  // «Любимые треки» — всё, что пользователь отметил плюсом, новые сверху.
  // Снятый лайк сразу убирает строку (likedIds — общий стор).
  import { api } from '../api.js';
  import { player, isPlaying as playerPlaying, shuffleOn } from '../stores/player.js';
  import { paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { refresh, playRows, playingContext, isContext } from '../library.js';
  import { likedIds } from '../stores/collection.js';
  import { me } from '../stores/me.js';
  import { rowFromCollectionTrack, filterRows } from '../rows.js';
  import FilterBox from '../components/FilterBox.svelte';
  import SortButton from '../components/SortButton.svelte';
  import { sortRows } from '../sort.js';
  import { sortPrefs } from '../stores/sort.js';
  import { tracksCount, longDuration } from '../format.js';
  import Hero from '../components/Hero.svelte';
  import Artwork from '../components/Artwork.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';
  import Shuffle from '@lucide/svelte/icons/shuffle';
  import Search from '@lucide/svelte/icons/search';

  const LIKED = { kind: 'liked', id: '' };
  let all = [];
  let loading = true;
  let error = '';

  pageTint().color({ r: 255, g: 77, b: 46 }); // как обложка «Любимых» (#ff4d2e, Artwork.svelte)

  async function load() {
    try {
      const data = await api.likedTracks();
      all = data.tracks.map(rowFromCollectionTrack);
      refresh(all.map((r) => r.deezerId));
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }
  load();

  $: rows = all.filter((r) => $likedIds.has(r.deezerId));
  let filter = '';
  $: sort = $sortPrefs.liked;
  // Сервер отдаёт новые сверху — это и есть «дата добавления» по убыванию.
  $: shown = sort.key === 'added' && sort.desc ? filterRows(rows, filter) : sortRows(filterRows(rows, filter), sort.key, sort.desc, $lang);
  $: sortOptions = [
    { key: 'added', label: $t('sort.added'), desc: true },
    { key: 'title', label: $t('sort.title') },
    { key: 'artist', label: $t('sort.artist') },
    { key: 'album', label: $t('sort.album') },
    { key: 'duration', label: $t('sort.duration') },
  ];
  $: playingHere = isContext($playingContext, 'liked');
  $: totalSec = rows.reduce((s, r) => s + (r.durationMs || 0) / 1000, 0);

  function onPlay() {
    if (playingHere) return player.toggle();
    if (rows.length) playRows(rows, null, LIKED);
  }
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else}
  <div class="page-pad">
    <Hero kind={$t('playlist.kind')} title={$t('liked.title')}>
      <Artwork slot="art" kind="liked" />
      <svelte:fragment slot="meta">
        <span class="owner">{$me?.username || ''}</span>
        {#if rows.length}<span class="dot">•</span><span class="muted-meta">{tracksCount(rows.length, $lang)}, {longDuration(totalSec, $lang)}</span>{/if}
      </svelte:fragment>
    </Hero>
  </div>

  {#if rows.length}
    <div class="actions page-pad">
      <PlayButton size={56} playing={playingHere && $playerPlaying} label={playingHere && $playerPlaying ? $t('player.pause') : $t('player.play')} on:click={onPlay} />
      <button class="icon-btn big" class:on={$shuffleOn} title={$t('player.shuffle')} aria-pressed={$shuffleOn} on:click={() => player.toggleShuffle()}>
        <Shuffle size={28} />
      </button>
      <div class="filter-slot">
        <FilterBox bind:value={filter} placeholder={$t('liked.search')} />
        <SortButton options={sortOptions} value={sort} on:change={(e) => sortPrefs.update((p) => ({ ...p, liked: e.detail }))} />
      </div>
    </div>
    <div class="page-pad">
      {#if shown.length}
        <TrackList rows={shown} showCover showAlbum context={LIKED} />
      {:else}
        <p class="muted no-match">{$t('list.noMatch', { q: filter })}</p>
      {/if}
    </div>
  {:else if !loading}
    <div class="page-pad empty">
      <h2>{$t('liked.empty')}</h2>
      <p class="muted">{$t('liked.emptyHint')}</p>
      <a class="pill" href={paths.search()} use:link><Search size={16} /> {$t('playlist.find')}</a>
    </div>
  {/if}
{/if}

<style>
  .owner {
    font-weight: 700;
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
  }
  .icon-btn.big {
    width: 44px;
    height: 44px;
  }
  @media (max-width: 768px) {
    .actions {
      gap: 16px;
    }
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
    padding-top: 32px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
  .empty h2 {
    font-size: 22px;
  }
</style>
