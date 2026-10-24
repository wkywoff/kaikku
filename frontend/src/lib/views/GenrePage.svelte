<script>
  // Жанр (/genre/<slug>, теги Last.fm): твои треки этого жанра, другие
  // артисты жанра, которых слушают на сервисе, и их хиты. Если у тебя есть
  // микс этого жанра — ссылка на него.
  import { api } from '../api.js';
  import { isPlaying as playerPlaying } from '../stores/player.js';
  import { navStore, paths, link } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import { pageTint } from '../stores/theme.js';
  import { setRouteTitle } from '../stores/title.js';
  import { refresh, playRows, playArtistTop, playingContext, isContext } from '../library.js';
  import { rowFromDeezerTrack } from '../rows.js';
  import { tagTitle, mixHue, mixColor } from '../mixes.js';
  import { artistMenuItems } from '../cardMenu.js';
  import Hero from '../components/Hero.svelte';
  import Artwork from '../components/Artwork.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';
  import MediaCard from '../components/MediaCard.svelte';
  import Shelf from '../components/Shelf.svelte';
  import Sparkles from '@lucide/svelte/icons/sparkles';

  const slug = $navStore.id;
  let page = null;
  let missing = false;
  let error = '';

  pageTint().color(mixColor(`tag-${slug}`));

  api
    .genre(slug)
    .then((p) => {
      page = p;
      refresh([...p.own, ...p.tracks].map((tr) => tr.id));
    })
    .catch((e) => (e.status === 404 ? (missing = true) : (error = e.message)));

  const asRow = (tr) => ({ ...rowFromDeezerTrack(tr), trackNo: null, discNo: null });
  $: title = page ? tagTitle(page.genre.name) : '';
  $: if (page) setRouteTitle(`genre:${slug}`, title);
  $: own = (page?.own || []).map(asRow);
  $: more = (page?.tracks || []).map(asRow);
  $: all = [...own, ...more];
  $: covers = [...new Set(all.map((r) => r.coverUrl).filter(Boolean))].slice(0, 4);
  $: playingCtx = $playerPlaying ? $playingContext : null;
  // Своего контекста у жанра нет: в истории это будут альбомы треков.
  const play = () => all.length && playRows(all, null, null, { chosen: false });
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if missing}
  <div class="page-pad empty">
    <h2>{$t('genre.missing')}</h2>
    <a class="pill" href={paths.search()} use:link>{$t('nav.search')}</a>
  </div>
{:else if page}
  <div class="page-pad">
    <Hero kind={$t('genre.kind')} {title}>
      <Artwork slot="art" kind="mix" images={covers} label={title} hue={mixHue(`tag-${slug}`)} sizes="(max-width: 768px) min(62vw, 240px), 232px" />
      <svelte:fragment slot="meta">
        <div class="description">{$t('genre.description')}</div>
      </svelte:fragment>
    </Hero>
  </div>

  <div class="actions page-pad">
    {#if all.length}
      <PlayButton size={56} label={$t('player.play')} on:click={play} />
    {/if}
    {#if page.mix_id}
      <a class="pill" href={paths.mix(page.mix_id)} use:link><Sparkles size={16} /> {$t('genre.mix', { name: title })}</a>
    {/if}
  </div>

  {#if own.length}
    <section class="page-pad">
      <h2>{$t('genre.yours')}</h2>
      <TrackList rows={own} showCover showAlbum header={false} />
    </section>
  {/if}

  {#if page.artists.length}
    <div class="page-pad">
      <Shelf title={$t('genre.artists')} items={page.artists} round let:item>
        <MediaCard
          href={paths.artist(item.id)}
          menu={() => artistMenuItems(item)}
          image={item.picture_url}
          round
          title={item.name}
          subtitle={$t('artist.kind')}
          onPlay={() => playArtistTop(item.id)}
          playing={isContext(playingCtx, 'artist', item.id)}
        />
      </Shelf>
    </div>
  {/if}

  {#if more.length}
    <section class="page-pad">
      <h2>{$t('genre.more')}</h2>
      <TrackList rows={more} showCover showAlbum header={false} />
    </section>
  {/if}

  {#if !own.length && !page.artists.length && !more.length}
    <p class="page-pad muted">{$t('genre.empty')}</p>
  {/if}
{/if}

<style>
  .description {
    width: 100%;
    color: rgba(255, 255, 255, 0.7);
    font-weight: 400;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 24px;
    padding-top: 20px;
    padding-bottom: 24px;
  }
  section {
    margin-bottom: 32px;
  }
  section h2 {
    font-size: 24px;
    margin-bottom: 8px;
  }
  .empty {
    padding-top: 48px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
  @media (max-width: 768px) {
    section h2 {
      font-size: 20px;
    }
  }
</style>
