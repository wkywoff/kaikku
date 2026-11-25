<script>
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
  import HeroActions from '../components/HeroActions.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import SectionHead from '../components/SectionHead.svelte';
  import Skeleton from '../components/Skeleton.svelte';
  import Artwork from '../components/Artwork.svelte';
  import PlayButton from '../components/PlayButton.svelte';
  import TrackList from '../components/TrackList.svelte';
  import MediaCard from '../components/MediaCard.svelte';
  import Shelf from '../components/Shelf.svelte';
  import Search from '@lucide/svelte/icons/search';
  import Sparkles from '@lucide/svelte/icons/sparkles';

  const slug = $navStore.id;
  let page = null;
  let missing = false;
  let error = '';

  const tint = pageTint();
  tint.color(mixColor(`tag-${slug}`));

  api
    .genre(slug)
    .then((p) => {
      page = p;
      refresh([...p.own, ...p.tracks].map((tr) => tr.id));
    })
    .catch((e) => {
      if (e.status === 404) {
        missing = true;
        tint.none();
      } else error = e.message;
    });

  const asRow = (tr) => ({ ...rowFromDeezerTrack(tr), trackNo: null, discNo: null });
  $: title = page ? tagTitle(page.genre.name) : '';
  $: if (page) setRouteTitle(`genre:${slug}`, title);
  $: own = (page?.own || []).map(asRow);
  $: more = (page?.tracks || []).map(asRow);
  $: all = [...own, ...more];
  $: covers = [...new Set(all.map((r) => r.coverUrl).filter(Boolean))].slice(0, 4);
  $: playingCtx = $playerPlaying ? $playingContext : null;
  const play = () => all.length && playRows(all, null, null, { chosen: false });
</script>

{#if error}
  <p class="error-text page-pad">{error}</p>
{:else if missing}
  <div class="page-pad">
    <EmptyState title={$t('genre.missing')}>
      <a class="pill" href={paths.search()} use:link><Search size={16} /> {$t('nav.search')}</a>
    </EmptyState>
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

  <HeroActions>
    {#if all.length}
      <PlayButton size={56} label={$t('player.play')} on:click={play} />
    {/if}
    {#if page.mix_id}
      <a class="pill mix" href={paths.mix(page.mix_id)} use:link><Sparkles size={16} /> <span class="label">{$t('genre.mix', { name: title })}</span></a>
    {/if}
  </HeroActions>

  <div class="page-pad sections">
    {#if own.length}
      <section>
        <SectionHead title={$t('genre.yours')} />
        <TrackList rows={own} showCover showAlbum header={false} />
      </section>
    {/if}

    {#if page.artists.length}
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
    {/if}

    {#if more.length}
      <section>
        <SectionHead title={$t('genre.more')} />
        <TrackList rows={more} showCover showAlbum header={false} />
      </section>
    {/if}

    {#if !own.length && !page.artists.length && !more.length}
      <p class="muted">{$t('genre.empty')}</p>
    {/if}
  </div>
{:else}
  <div class="page-pad"><Skeleton kind="hero" /></div>
{/if}

<style>
  .description {
    width: 100%;
    color: rgba(255, 255, 255, 0.7);
    font-weight: 400;
  }
  .mix {
    min-width: 0;
  }
  .mix :global(svg) {
    flex-shrink: 0;
  }
  .mix .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
