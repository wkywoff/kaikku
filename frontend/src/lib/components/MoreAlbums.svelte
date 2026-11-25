<script context="module">
  import { LRU } from '../lru.js';
  const cache = new LRU(30);
</script>

<script>
  import { api } from '../api.js';
  import { paths, link } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import { recordCategory, year } from '../format.js';
  import { albumMenuItems } from '../cardMenu.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { playingContext, isContext, playAlbum } from '../library.js';
  import { toast } from '../stores/toast.js';
  import MediaCard from './MediaCard.svelte';
  import SectionHead from './SectionHead.svelte';
  import CardGrid from './CardGrid.svelte';

  export let artist;
  export let excludeId = null;

  let albums = [];
  let perRow = 2;

  $: load(artist.id);

  async function load(id) {
    if (!cache.has(id)) cache.set(id, api.discoverArtistAlbums(id).then((d) => d.albums).catch(() => (cache.delete(id), [])));
    const all = await cache.get(id);
    if (id !== artist.id) return;
    albums = all
      .filter((a) => recordCategory(a.record_type) === 'album' && a.id !== excludeId)
      .sort((x, y) => (y.release_date || '').localeCompare(x.release_date || ''));
  }

  $: playingCtx = $playerPlaying ? $playingContext : null;
  const play = (id) => playAlbum(id).catch((e) => toast(e.message, 'error'));
</script>

{#if albums.length}
  <section>
    <SectionHead title={$t('album.moreBy', { name: artist.name })}>
      {#if albums.length > perRow}
        <a class="text-btn" href={paths.discography(artist.id, 'album')} use:link>{$t('disc.showAll')}</a>
      {/if}
    </SectionHead>
    <CardGrid bind:perRow>
      {#each albums.slice(0, perRow) as a (a.id)}
        <MediaCard
          href={paths.album(a.id)}
          menu={() => albumMenuItems(a)}
          image={a.cover_url}
          hidden={a.hidden}
          title={a.title}
          subtitle={year(a.release_date)}
          onPlay={() => play(a.id)}
          playing={isContext(playingCtx, 'album', a.id)}
        />
      {/each}
    </CardGrid>
  </section>
{/if}
