<script context="module">
  import { MAX_PER_ROW } from '../layout.js';
  import { LRU } from '../lru.js';
  const cache = new LRU(30); // deezer id артиста -> Promise<albums>
</script>

<script>
  // «Ещё альбомы: <артист>» под треклистом альбома, как в Spotify: только
  // релизы типа «альбом» (без синглов и EP), новые первыми, одна строка
  // карточек и «Показать все» — страница дискографии с фильтром «Альбомы».
  import { api } from '../api.js';
  import { paths, link } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import { recordCategory, year } from '../format.js';
  import { albumMenuItems } from '../cardMenu.js';
  import MediaCard from './MediaCard.svelte';

  export let artist; // { id, name } — Deezer
  export let excludeId = null; // текущий альбом

  let albums = [];
  let gridWidth = 0;

  $: load(artist.id);

  async function load(id) {
    if (!cache.has(id)) cache.set(id, api.discoverArtistAlbums(id).then((d) => d.albums).catch(() => (cache.delete(id), [])));
    const all = await cache.get(id);
    if (id !== artist.id) return;
    albums = all
      .filter((a) => recordCategory(a.record_type) === 'album' && a.id !== excludeId)
      .sort((x, y) => (y.release_date || '').localeCompare(x.release_date || ''));
  }

  // Сколько карточек в строке (min 176 px; на телефоне — 2), как на странице артиста.
  $: perRow = gridWidth < 560 ? 2 : Math.min(MAX_PER_ROW, Math.max(2, Math.floor(gridWidth / 176)));
</script>

{#if albums.length}
  <section>
    <div class="head">
      <h2>{$t('album.moreBy', { name: artist.name })}</h2>
      {#if albums.length > perRow}
        <a class="show-all" href={paths.discography(artist.id, 'album')} use:link>{$t('disc.showAll')}</a>
      {/if}
    </div>
    <div class="grid" bind:clientWidth={gridWidth}>
      {#each albums.slice(0, perRow) as a (a.id)}
        <MediaCard href={paths.album(a.id)} menu={() => albumMenuItems(a)} image={a.cover_url} hidden={a.hidden} title={a.title} subtitle={year(a.release_date)} />
      {/each}
    </div>
  </section>
{/if}

<style>
  section {
    margin-top: 40px;
  }
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 8px;
  }
  h2 {
    font-size: 24px;
  }
  .show-all {
    flex-shrink: 0;
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
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  @media (max-width: 768px) {
    h2 {
      font-size: 20px;
    }
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
  }
</style>
