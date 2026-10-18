<script context="module">
  // Последние ответы — чтобы «назад» на главную показывал её сразу; null —
  // ещё не загружалось.
  let cached = { recent: null, mixes: null, releases: null, suggestions: null };
</script>

<script>
  // Главная. Сверху «Недавно прослушанное» и миксы (собираются сами из
  // истории и лайков, lib/mixes.js), ниже — открытия: новые релизы
  // подписок, «Тебе может понравиться» (похожие на тех, кого слушаешь) и
  // публичные плейлисты друзей. Play на карточке сразу играет.
  // Своя музыка — в медиатеке (LibraryPage); итоги года — в меню аккаунта,
  // а здесь только в сезон (wrappedSeasonYear).
  import { api } from '../api.js';
  import { nowPlaying, isPlaying as playerPlaying } from '../stores/player.js';
  import { paths, link } from '../stores/nav.js';
  import { pageTint } from '../stores/theme.js';
  import { playingContext, isContext, playAlbum, playArtistTop, playPlaylist, playLiked, playMix } from '../library.js';
  import { mixTitle, mixSubtitle, mixHue } from '../mixes.js';
  import { likedIds, savedAlbumIds, followedIds, playlists, collectionLoaded } from '../stores/collection.js';
  import { recordTypeLabel, wrappedSeasonYear } from '../format.js';
  import { lang } from '../stores/settings.js';
  import { t } from '../i18n.js';
  import { albumMenuItems, artistMenuItems, mixMenuItems, playlistMenuItems } from '../cardMenu.js';
  import MediaCard from '../components/MediaCard.svelte';
  import Artwork from '../components/Artwork.svelte';
  import Shelf from '../components/Shelf.svelte';
  import Search from '@lucide/svelte/icons/search';
  import Sparkles from '@lucide/svelte/icons/sparkles';

  let error = '';

  let recent = cached.recent;
  let mixes = cached.mixes;
  let releases = cached.releases;
  let suggestions = cached.suggestions; // { artists, based_on }

  // Части грузятся все сразу и друг другу не мешают: не ответила (Deezer
  // занят) — её просто не будет.
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

  // Показываются части сверху вниз: часть видна, когда готовы и все выше
  // неё. Иначе медленные «Новые релизы» (дискографии из Deezer) приходили
  // последними и раздвигали уже показанное (CLS в Lighthouse). На месте
  // первой неготовой — заглушка, и она всегда последняя на странице: её
  // замена или исчезновение ничего выше не двигает.
  //   0 недавнее (и медиатека — от неё подсказка нового аккаунта),
  //   1 миксы, 2 новые релизы (без подписок их не будет — не ждём),
  //   3 «Тебе может понравиться», 4 плейлисты друзей.
  $: ready = [
    recent !== null && $collectionLoaded,
    mixes !== null,
    releases !== null || ($collectionLoaded && !$followedIds.size),
    suggestions !== null,
  ];
  $: shown = ready.includes(false) ? ready.indexOf(false) : ready.length;

  $: others = $playlists.others;
  // Совсем новый аккаунт: ни истории, ни своей музыки — подсказка, с чего начать.
  $: empty =
    $collectionLoaded && !recent?.length && !$likedIds.size && !$playlists.mine.length && !$savedAlbumIds.size && !$followedIds.size;
  const wrappedYear = wrappedSeasonYear();

  // Цвет главной — обложка того, что сейчас играет (или стоит на паузе).
  const tint = pageTint();
  $: nowCover = $nowPlaying?.coverUrl || null; // строка: tint.from — только при смене обложки
  $: tint.from(nowCover);

  const greetingKey = (() => {
    const h = new Date().getHours();
    if (h < 5) return 'home.night';
    if (h < 12) return 'home.morning';
    if (h < 18) return 'home.day';
    return 'home.evening';
  })();

  // Какая карточка сейчас играет — по тому, откуда включена музыка (library.js).
  $: playingCtx = $playerPlaying ? $playingContext : null;
  $: isPlaying = (kind, id = '') => isContext(playingCtx, kind, id);

  // Play на карточке; ошибка загрузки — в строку ошибки страницы.
  const run = (start) => start().catch((e) => (error = e.message));

  const recentKey = (it) => `${it.kind}:${it.album?.id || it.artist?.id || it.playlist?.id || it.mix?.id || ''}`;
</script>

<div class="page-pad home">
  <div class="top">
    <h1>{$t(greetingKey)}</h1>
    {#if wrappedYear && recent?.length}
      <a class="pill create wrapped" href={paths.wrapped(String(wrappedYear))} use:link><Sparkles size={16} /> {$t('home.wrapped', { year: wrappedYear })}</a>
    {/if}
  </div>

  {#if error}<p class="error-text">{error}</p>{/if}

  <!-- Первая полка видна сразу: её первый ряд — без отложенной загрузки,
       первая картинка — главная на экране (LCP). -->
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
    <div class="empty">
      <h2>{$t('home.emptyTitle')}</h2>
      <p class="muted">{$t('home.emptyText')}</p>
      <a class="pill" href={paths.search()} use:link><Search size={16} /> {$t('home.emptyCta')}</a>
    </div>
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
        subtitle={[recordTypeLabel(item.record_type, $lang), item.artist_name].filter(Boolean).join(' • ')}
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

<style>
  .home {
    padding-top: 24px;
    padding-bottom: 24px;
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
    margin-bottom: 4px;
  }
  h1 {
    font-size: 32px;
  }
  .create {
    flex-shrink: 0;
  }
  .wrapped {
    background: linear-gradient(135deg, #ff5f6d, #6a11cb);
    color: #fff;
    border-color: transparent;
  }
  .empty {
    padding: 48px 0 16px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
    max-width: 480px;
  }
  @media (max-width: 768px) {
    .home {
      padding-top: 16px;
    }
    h1 {
      font-size: 26px;
    }
  }
</style>
