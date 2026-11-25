<script>
  import { api } from '../api.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { navStore, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { playingContext, isContext, playAlbum } from '../library.js';
  import { albumMenuItems } from '../cardMenu.js';
  import { count, WRAPPED_TINT } from '../wrappedText.js';
  import MediaCard from '../components/MediaCard.svelte';
  import CardGrid from '../components/CardGrid.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import StickyBar from '../components/StickyBar.svelte';

  let data = null;
  let error = '';

  pageTint().none(WRAPPED_TINT);

  $: load($navStore.year);
  let seq = 0;
  async function load(year) {
    const my = ++seq;
    error = '';
    try {
      const d = await api.wrappedAlbums(year);
      if (my === seq) data = d;
    } catch (e) {
      if (my === seq) error = e.message;
    }
  }

  $: playingCtx = $playerPlaying ? $playingContext : null;

  function onPlay(albumId) {
    playAlbum(albumId).catch((e) => (error = e.message));
  }
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if data}
  <StickyBar>
    <a href={paths.wrapped(data.year)} use:link>{$t('wrapped.heading', { year: data.year })}</a>
    <svelte:fragment slot="aside">{count(data.albums.length, 'albums', $lang)}</svelte:fragment>
  </StickyBar>

  <div class="page-pad">
    {#if data.albums.length}
      <CardGrid>
        {#each data.albums as a (a.id)}
          <MediaCard
            href={paths.album(a.id)}
            menu={() => albumMenuItems(a)}
            image={a.cover_url}
            title={a.title}
            subtitle={[count(a.plays, 'plays', $lang), a.artist].filter(Boolean).join(' • ')}
            onPlay={() => onPlay(a.id)}
            playing={isContext(playingCtx, 'album', a.id)}
          />
        {/each}
      </CardGrid>
    {:else}
      <EmptyState title={$t('wrapped.albumsEmpty')} />
    {/if}
  </div>
{:else}
  <div class="page-pad"><Skeleton kind="bar" /></div>
{/if}
