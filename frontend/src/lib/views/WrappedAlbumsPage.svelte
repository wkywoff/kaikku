<script>
  // Все альбомы, прослушанные за год (/wrapped/<год>/albums) — «Показать все»
  // из итогов года, как вся дискография у артиста: сетка обложек, под каждой —
  // сколько раз слушали. Порядок — от самого слушаемого, его считает сервер.
  import { api } from '../api.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { navStore, paths, link } from '../stores/nav.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { playingContext, isContext, playAlbum } from '../library.js';
  import { albumMenuItems } from '../cardMenu.js';
  import { count } from '../wrappedText.js';
  import MediaCard from '../components/MediaCard.svelte';

  let data = null;
  let error = '';

  pageTint().none();

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

  // Какой альбом сейчас играет (если включён отсюда или с его страницы).
  $: playingCtx = $playerPlaying ? $playingContext : null;

  function onPlay(albumId) {
    playAlbum(albumId).catch((e) => (error = e.message));
  }
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if data}
  <div class="bar page-pad">
    <a class="name" href={paths.wrapped(data.year)} use:link>{$t('wrapped.heading', { year: data.year })}</a>
    <span class="total">{count(data.albums.length, 'albums', $lang)}</span>
  </div>

  <div class="page-pad">
    {#if data.albums.length}
      <div class="grid">
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
      </div>
    {:else}
      <p class="empty">{$t('wrapped.albumsEmpty')}</p>
    {/if}
  </div>
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
    justify-content: space-between;
    gap: 16px;
    padding-top: 20px;
    padding-bottom: 16px;
    background: var(--bg);
    /* Содержимое — в колонке страницы (App.svelte), а фон полосы — во всю
       ширину: тень без размытия продолжает его в стороны, clip-path
       оставляет её только по высоте полосы. */
    box-shadow: 0 0 0 100vmax var(--bg);
    clip-path: inset(0 -100vmax);
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 24px;
    font-weight: 700;
  }
  @media (hover: hover) {
    .name:hover {
      text-decoration: underline;
    }
  }
  .total {
    flex-shrink: 0;
    font-size: 14px;
    font-weight: 500;
    color: var(--text-dim);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  .empty {
    margin-top: 8px;
    color: var(--text-dim);
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
    .name {
      font-size: 20px;
    }
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
  }
</style>
