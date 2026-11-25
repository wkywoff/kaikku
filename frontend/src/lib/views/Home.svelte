<script context="module">
  let cached = { recent: null, mixes: null, releases: null, suggestions: null };
</script>

<script>
  import { api } from '../api.js';
  import { nowPlaying, isPlaying as playerPlaying } from '../stores/player.js';
  import { paths, link } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { playingContext, isContext, playAlbum, playArtistTop, playPlaylist, playLiked, playMix } from '../library.js';
  import { mixTitle, mixSubtitle, mixHue } from '../mixes.js';
  import { likedIds, savedAlbumIds, followedIds, playlists, collectionLoaded } from '../stores/collection.js';
  import { albumArtists, recordTypeLabel, wrappedSeasonYear } from '../format.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { albumMenuItems, artistMenuItems, mixMenuItems, playlistMenuItems } from '../cardMenu.js';
  import MediaCard from '../components/MediaCard.svelte';
  import Artwork from '../components/Artwork.svelte';
  import Shelf from '../components/Shelf.svelte';
  import PageHead from '../components/PageHead.svelte';
  import EmptyState from '../components/EmptyState.svelte';
  import Search from '@lucide/svelte/icons/search';
  import Sparkles from '@lucide/svelte/icons/sparkles';

  let error = '';

  let recent = cached.recent;
  let mixes = cached.mixes;
  let releases = cached.releases;
  let suggestions = cached.suggestions;

  async function loadPart(fetch, apply, giveUp) {
    try {
      apply(await fetch());
    } catch {
      giveUp();
    }
  }
  loadPart(api.recent, (d) => (recent = cached.recent = d.items), () => (recent = recent ?? []));
  loadPart(api.mixes, (d) => (mixes = cached.mixes = d.mixes), () => (mixes = mixes ?? []));
  loadPart(api.newReleases, (d) => (releases = cached.releases = d.albums), () => (releases = releases ?? []));
  loadPart(api.suggestions, (d) => (suggestions = cached.suggestions = d), () => (suggestions = suggestions ?? { artists: [], based_on: [] }));

  $: ready = [
    recent !== null && $collectionLoaded,
    mixes !== null,
    releases !== null || ($collectionLoaded && !$followedIds.size),
    suggestions !== null,
  ];
  $: shown = ready.includes(false) ? ready.indexOf(false) : ready.length;

  $: others = $playlists.others;
  $: empty =
    $collectionLoaded && !recent?.length && !$likedIds.size && !$playlists.mine.length && !$savedAlbumIds.size && !$followedIds.size;
  const wrappedYear = wrappedSeasonYear();

  const tint = pageTint();
  $: nowCover = $nowPlaying?.coverUrl || null;
  $: tint.from(nowCover);

  const greetingKey = (() => {
    const h = new Date().getHours();
    if (h < 5) return 'home.night';
    if (h < 12) return 'home.morning';
    if (h < 18) return 'home.day';
    return 'home.evening';
  })();

  $: playingCtx = $playerPlaying ? $playingContext : null;
  $: isPlaying = (kind, id = '') => isContext(playingCtx, kind, id);

  const run = (start) => start().catch((e) => (error = e.message));

  const recentKey = (it) => `${it.kind}:${it.album?.id || it.artist?.id || it.playlist?.id || it.mix?.id || ''}`;
</script>

<div class="page-pad home">
  <PageHead title={$t(greetingKey)}>
    {#if wrappedYear && recent?.length}
      <a class="pill wrapped" href={paths.wrapped(String(wrappedYear))} use:link><Sparkles size={16} /> {$t('home.wrapped', { year: wrappedYear })}</a>
    {/if}
  </PageHead>

  {#if error}<p class="error-text">{error}</p>{/if}

  <div class="sections">
    <Shelf title={$t('home.recent')} items={recent || []} loading={shown === 0} key={recentKey} priority let:item let:eager let:first>
      {#if item.kind === 'liked'}
        <MediaCard href={paths.liked()} title={$t('liked.title')} subtitle={$t('playlist.kind')} onPlay={() => run(playLiked)} playing={isPlaying('liked')}>
          <Artwork slot="art" kind="liked" />
        </MediaCard>
      {:else if item.kind === 'playlist'}
        <MediaCard
          href={paths.playlist(item.playlist.id)}
          menu={() => playlistMenuItems(item.playlist)}
          title={item.playlist.title}
          subtitle={$t('home.by', { name: item.playlist.owner.username })}
          onPlay={item.playlist.track_count ? () => run(() => playPlaylist(item.playlist.id)) : null}
          playing={isPlaying('playlist', item.playlist.id)}
        >
          <Artwork slot="art" images={item.playlist.covers} iconSize={48} {eager} />
        </MediaCard>
      {:else if item.kind === 'mix'}
        <MediaCard href={paths.mix(item.mix.id)} menu={() => mixMenuItems(item.mix)} title={mixTitle(item.mix, $t)} subtitle={mixSubtitle(item.mix, $t)} onPlay={() => run(() => playMix(item.mix.id))} playing={isPlaying('mix', item.mix.id)}>
          <Artwork slot="art" kind="mix" images={item.mix.covers} label={mixTitle(item.mix, $t)} hue={mixHue(item.mix.id)} {eager} />
        </MediaCard>
      {:else if item.kind === 'artist'}
        <MediaCard
          href={paths.artist(item.artist.id)}
          menu={() => artistMenuItems(item.artist)}
          image={item.artist.picture_url}
          {eager}
          {first}
          round
          title={item.artist.name}
          subtitle={$t('artist.kind')}
          onPlay={() => run(() => playArtistTop(item.artist.id))}
          playing={isPlaying('artist', item.artist.id)}
        />
      {:else}
        <MediaCard
          href={paths.album(item.album.id)}
          menu={() => albumMenuItems(item.album)}
          image={item.album.cover_url}
          {eager}
          {first}
          hidden={item.album.hidden}
          title={item.album.title}
          subtitle={item.album.artist_name}
          onPlay={() => run(() => playAlbum(item.album.id))}
          playing={isPlaying('album', item.album.id)}
        />
      {/if}
    </Shelf>

    {#if shown > 0 && empty}
      <EmptyState title={$t('home.emptyTitle')} text={$t('home.emptyText')}>
        <a class="pill" href={paths.search()} use:link><Search size={16} /> {$t('home.emptyCta')}</a>
      </EmptyState>
    {/if}

    {#if shown >= 1}
      <Shelf title={$t('home.mixes')} items={mixes || []} loading={shown === 1} let:item>
        <MediaCard href={paths.mix(item.id)} menu={() => mixMenuItems(item)} title={mixTitle(item, $t)} subtitle={mixSubtitle(item, $t)} onPlay={() => run(() => playMix(item.id))} playing={isPlaying('mix', item.id)}>
          <Artwork slot="art" kind="mix" images={item.covers} label={mixTitle(item, $t)} hue={mixHue(item.id)} />
        </MediaCard>
      </Shelf>
    {/if}

    {#if shown >= 2}
      <Shelf title={$t('home.releases')} items={releases || []} loading={shown === 2} let:item>
        <MediaCard
          href={paths.album(item.id)}
          menu={() => albumMenuItems(item)}
          image={item.cover_url}
          hidden={item.hidden}
          title={item.title}
          subtitle={[recordTypeLabel(item.record_type, $lang), albumArtists(item)].filter(Boolean).join(' • ')}
          onPlay={() => run(() => playAlbum(item.id))}
          playing={isPlaying('album', item.id)}
        />
      </Shelf>
    {/if}

    {#if shown >= 3}
      <Shelf
        title={$t('home.suggestions')}
        subtitle={suggestions?.based_on?.length ? $t('home.suggestionsBased', { names: suggestions.based_on.join(', ') }) : ''}
        items={suggestions?.artists || []}
        loading={shown === 3}
        round
        let:item
      >
        <MediaCard
          href={paths.artist(item.id)}
          menu={() => artistMenuItems(item)}
          image={item.picture_url}
          round
          title={item.name}
          subtitle={$t('artist.kind')}
          onPlay={() => run(() => playArtistTop(item.id))}
          playing={isPlaying('artist', item.id)}
        />
      </Shelf>
    {/if}

    {#if shown > 3 && others.length}
      <Shelf title={$t('home.friends')} items={others} let:item>
        <MediaCard
          href={paths.playlist(item.id)}
          menu={() => playlistMenuItems(item)}
          title={item.title}
          subtitle={$t('home.by', { name: item.owner.username })}
          onPlay={item.track_count ? () => run(() => playPlaylist(item.id)) : null}
          playing={isPlaying('playlist', item.id)}
        >
          <Artwork slot="art" images={item.covers} iconSize={48} />
        </MediaCard>
      </Shelf>
    {/if}
  </div>
</div>

<style>
  .home {
    padding-bottom: 24px;
  }
  .wrapped {
    background: linear-gradient(135deg, #ff5f6d, #6a11cb);
    color: #fff;
    border-color: transparent;
  }
</style>
