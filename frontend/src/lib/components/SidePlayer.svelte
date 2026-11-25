<script>
  import { createEventDispatcher, onDestroy } from 'svelte';
  import { api } from '../api.js';
  import { artistInfos, artistPages, artistBios } from '../artistCache.js';
  import { sized } from '../imageproxy.js';
  import { marquee } from '../marquee.js';
  import { player, nowPlaying, queueState, formatTime } from '../stores/player.js';
  import { settings, lang } from '../stores/settings.js';
  import { link, paths } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import { fansLabel } from '../format.js';
  import { followedIds, toggleFollow } from '../stores/collection.js';
  import { mainArtistId, rowFromEntry } from '../library.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { trackMenuItems, trackMenuHeader } from '../trackMenu.js';
  import { sleepMenuItems, sleepLabel } from '../sleepMenu.js';
  import { wheelVolume } from '../wheelVolume.js';
  import { sidePlayer, playingFrom, clampWidth, maxWidthFor, SIDE_MIN } from '../stores/sidePlayer.js';
  import PlayButton from './PlayButton.svelte';
  import ArtistLinks from './ArtistLinks.svelte';
  import Slider from './Slider.svelte';
  import LikeButton from './LikeButton.svelte';
  import QueueView from './QueueView.svelte';
  import FollowButton from './FollowButton.svelte';
  import Shuffle from '@lucide/svelte/icons/shuffle';
  import Repeat from '@lucide/svelte/icons/repeat';
  import Repeat1 from '@lucide/svelte/icons/repeat-1';
  import MicVocal from '@lucide/svelte/icons/mic-vocal';
  import ListMusic from '@lucide/svelte/icons/list-music';
  import Volume from '@lucide/svelte/icons/volume';
  import Volume1 from '@lucide/svelte/icons/volume-1';
  import Volume2 from '@lucide/svelte/icons/volume-2';
  import VolumeX from '@lucide/svelte/icons/volume-x';
  import Maximize2 from '@lucide/svelte/icons/maximize-2';
  import Disc3 from '@lucide/svelte/icons/disc-3';
  import Moon from '@lucide/svelte/icons/moon';
  import Ellipsis from '@lucide/svelte/icons/ellipsis';
  import PanelRightClose from '@lucide/svelte/icons/panel-right-close';

  const dispatch = createEventDispatcher();
  const smoothTime = player.smoothTime;
  const RESIZE_STEP = 16;

  let innerWidth = window.innerWidth;
  $: width = clampWidth($sidePlayer.width, innerWidth);

  $: s = $player;
  $: q = $queueState;
  $: track = $nowPlaying;
  $: from = playingFrom(track, $t);
  $: nextIndex = q.index + 1 < q.queue.length ? q.index + 1 : q.repeat !== 'off' && q.queue.length > 1 ? 0 : -1;
  $: nextEntry = nextIndex >= 0 ? q.queue[nextIndex] : null;

  let showQueue = false;

  function openTrackMenu(e) {
    const row = rowFromEntry(track);
    openMenu(
      e,
      [
        ...trackMenuItems(row),
        { separator: true },
        { label: sleepLabel(s.sleep, $t), icon: Moon, submenu: sleepMenuItems().filter((i) => !i.heading) },
      ],
      trackMenuHeader(row),
    );
  }

  let lastVolume = 1;
  function toggleMute() {
    if (s.volume > 0) {
      lastVolume = s.volume;
      player.setVolume(0);
    } else {
      player.setVolume(lastVolume || 1);
    }
  }

  let resizing = null;
  let resizeFrame = 0;
  let resizeTo = 0;
  function startResize(e) {
    if (e.button !== 0) return;
    e.preventDefault();
    resizing = { x: e.clientX, width, id: e.pointerId };
    e.currentTarget.setPointerCapture(e.pointerId);
  }
  function applyResize() {
    resizeFrame = 0;
    sidePlayer.resize(resizeTo);
  }
  function moveResize(e) {
    if (!resizing || e.pointerId !== resizing.id) return;
    resizeTo = clampWidth(resizing.width + resizing.x - e.clientX, innerWidth);
    if (!resizeFrame) resizeFrame = requestAnimationFrame(applyResize);
  }
  function endResize(e) {
    if (!resizing || e.pointerId !== resizing.id) return;
    if (resizeFrame) {
      cancelAnimationFrame(resizeFrame);
      applyResize();
    }
    resizing = null;
    sidePlayer.commitWidth();
  }
  function keyResize(e) {
    const dir = e.key === 'ArrowLeft' ? 1 : e.key === 'ArrowRight' ? -1 : 0;
    if (!dir) return;
    e.preventDefault();
    sidePlayer.resize(clampWidth(width + dir * RESIZE_STEP, innerWidth));
    sidePlayer.commitWidth();
  }
  $: document.body.classList.toggle('side-resizing', !!resizing);
  onDestroy(() => {
    cancelAnimationFrame(resizeFrame);
    document.body.classList.remove('side-resizing');
  });

  let artistInView = false;
  let artist = null;
  let artistFailed = null;
  let artistSeq = 0;
  let bio = null;
  let bioSeq = 0;
  $: artistId = mainArtistId(track);
  $: if (artistInView) loadArtist(artistId);
  $: shownArtist = artist && artist.id === artistId && artist.picture_url ? artist : null;
  $: if (artistInView && shownArtist) loadBio(shownArtist.id, $lang);
  $: bioText = bio && bio.key === `${artistId}:${$lang}` ? bio.text : '';

  async function loadArtist(id) {
    if (!id || artist?.id === id || artistFailed === id) return;
    const known = artistInfos.get(id) || artistPages.get(id)?.artist;
    if (known) {
      artist = { ...known, id };
      return;
    }
    const my = ++artistSeq;
    try {
      const info = await api.artistInfo(id);
      const found = { ...info, id };
      artistInfos.set(id, found);
      if (my === artistSeq) artist = found;
    } catch {
      if (my === artistSeq) artistFailed = id;
    }
  }

  async function loadBio(id, language) {
    const key = `${id}:${language}`;
    if (bio?.key === key) return;
    const known = artistBios.get(key);
    if (known) {
      bio = { key, text: oneParagraph(known.text) };
      return;
    }
    const my = ++bioSeq;
    try {
      const found = await api.artistBio(id, language);
      artistBios.set(key, found);
      if (my === bioSeq) bio = { key, text: oneParagraph(found.text) };
    } catch {
      if (my === bioSeq) bio = { key, text: '' };
    }
  }

  const oneParagraph = (text) => (text || '').replace(/\s+/g, ' ').trim();

  function inView(node) {
    const io = new IntersectionObserver(([entry]) => (artistInView = entry.isIntersecting));
    io.observe(node);
    return {
      destroy() {
        io.disconnect();
        artistInView = false;
      },
    };
  }
</script>

<svelte:window bind:innerWidth />

<!-- svelte-ignore a11y_no_noninteractive_tabindex a11y_no_noninteractive_element_interactions -->
<div
  class="resizer"
  class:active={!!resizing}
  role="separator"
  aria-orientation="vertical"
  aria-label={$t('side.resize')}
  aria-valuemin={SIDE_MIN}
  aria-valuemax={maxWidthFor(innerWidth)}
  aria-valuenow={width}
  tabindex="0"
  on:pointerdown={startResize}
  on:pointermove={moveResize}
  on:pointerup={endResize}
  on:pointercancel={endResize}
  on:keydown={keyResize}
></div>

<aside class="side-player" class:tint-full={$settings.playerTint === 'full'} style="width: {width}px" aria-label={$t('player.nowPlaying')}>
  <div class="head">
    {#if from?.href}
      <a class="from" href={from.href} use:link title={from.title}>{from.title}</a>
    {:else}
      <span class="from">{from?.title || $t('player.nowPlaying')}</span>
    {/if}
    {#if track}
      <button class="icon-btn" class:sleeping={!!s.sleep} title={$t('menu.more')} aria-label={$t('menu.moreFor', { title: track.title })} on:click={openTrackMenu}><Ellipsis size={20} /></button>
    {/if}
    <button class="icon-btn" title={$t('side.hide')} aria-label={$t('side.hide')} on:click={() => sidePlayer.close()}><PanelRightClose size={20} /></button>
  </div>

  {#if track}
    <div class="body" class:queue-mode={showQueue}>
      {#if !showQueue}
        <div class="cover">
          {#if track.coverUrl}<img src={sized(track.coverUrl, 640)} alt="" decoding="async" />{:else}<div class="placeholder"><Disc3 size={72} /></div>{/if}
        </div>
      {/if}

      <div class="title-row">
        {#if showQueue}
          {#if track.coverUrl}<img class="mini-cover" src={sized(track.coverUrl, 64)} alt="" />{:else}<div class="mini-cover placeholder"><Disc3 size={24} /></div>{/if}
        {/if}
        <div class="meta">
          {#if track.albumHref}
            <a class="title" href={track.albumHref} use:link use:marquee={track.title} title={track.title}><span>{track.title}</span></a>
          {:else}
            <span class="title" use:marquee={track.title} title={track.title}><span>{track.title}</span></span>
          {/if}
          <span class="artist"><ArtistLinks menu artists={track.artists || [{ name: track.artist, href: track.artistHref }]} /></span>
        </div>
        <LikeButton deezerId={track.deezerId} size={24} />
      </div>

      <div class="progress">
        <Slider value={$smoothTime} max={s.duration || 0} step={0.1} label={$t('player.position')} on:change={(e) => player.seek(e.detail)} />
        <div class="times num"><span>{formatTime(s.currentTime)}</span><span>{formatTime(s.duration)}</span></div>
      </div>

      <div class="controls">
        <button class="icon-btn dup" class:on={s.shuffle} on:click={() => player.toggleShuffle()} title={$t('player.shuffle')} aria-pressed={s.shuffle}><Shuffle size={20} /></button>
        <button class="icon-btn skip" on:click={() => player.prev()} title={$t('player.prev')} aria-label={$t('player.prev')}>
          <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d="M6 4.5v15M19 5.1v13.8a.8.8 0 0 1-1.25.66L8.6 12.66a.8.8 0 0 1 0-1.32l9.15-6.9A.8.8 0 0 1 19 5.1z" fill="currentColor" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
        </button>
        <PlayButton variant="white" size={56} playing={s.isPlaying} loading={s.isLoading && !s.isPlaying} label={s.isPlaying ? $t('player.pause') : $t('player.play')} on:click={() => player.toggle()} />
        <button class="icon-btn skip" on:click={() => player.next()} disabled={!player.canGoNext(s, $settings.autoplay)} title={$t('player.next')} aria-label={$t('player.next')}>
          <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d="M18 4.5v15M5 5.1v13.8a.8.8 0 0 0 1.25.66l9.15-6.9a.8.8 0 0 0 0-1.32l-9.15-6.9A.8.8 0 0 0 5 5.1z" fill="currentColor" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
        </button>
        <button class="icon-btn dup" class:on={s.repeat !== 'off'} on:click={() => player.cycleRepeat()} aria-pressed={s.repeat !== 'off'} title={$t(s.repeat === 'one' ? 'player.repeatOne' : s.repeat === 'all' ? 'player.repeatAll' : 'player.repeat')}>
          {#if s.repeat === 'one'}<Repeat1 size={20} />{:else}<Repeat size={20} />{/if}
        </button>
      </div>

      <div class="extras">
        <button class="icon-btn" on:click={() => dispatch('lyrics')} title={$t('lyrics.show')} aria-label={$t('lyrics.show')}><MicVocal size={18} /></button>
        <button class="icon-btn" class:on={showQueue} on:click={() => (showQueue = !showQueue)} title={$t('player.queue')} aria-pressed={showQueue}><ListMusic size={18} /></button>
        <div class="volume-group" use:wheelVolume>
          <button class="icon-btn" on:click={toggleMute} title={s.volume === 0 ? $t('player.unmute') : $t('player.mute')}>
            {#if s.volume === 0}<VolumeX size={18} />{:else if s.volume < 0.34}<Volume size={18} />{:else if s.volume < 0.67}<Volume1 size={18} />{:else}<Volume2 size={18} />{/if}
          </button>
          <div class="volume"><Slider value={s.volume} max={1} label={$t('player.volume')} on:input={(e) => player.setVolume(e.detail)} on:change={(e) => player.setVolume(e.detail)} /></div>
        </div>
        <button class="icon-btn" on:click={() => dispatch('fullscreen')} title={$t('player.fullscreen')} aria-label={$t('player.fullscreen')}><Maximize2 size={18} /></button>
      </div>

      {#if showQueue}
        <div class="queue"><QueueView compact /></div>
      {:else}
        {#if nextEntry}
          <section class="card next">
            <div class="card-head">
              <h3>{$t('queue.next')}</h3>
              <button class="text-link" on:click={() => (showQueue = true)}>{$t('side.openQueue')}</button>
            </div>
            <button class="next-track" on:click={() => player.jumpTo(nextIndex)} aria-label={`${$t('player.play')}: ${nextEntry.title}`}>
              {#if nextEntry.coverUrl}<img src={sized(nextEntry.coverUrl, 64)} alt="" loading="lazy" />{:else}<span class="ph"></span>{/if}
              <span class="next-meta"><span class="next-title">{nextEntry.title}</span><span class="next-artist">{nextEntry.artist}</span></span>
            </button>
          </section>
        {/if}

        {#if artistId}
          <div class="artist-slot" use:inView>
            {#if shownArtist}
              <section class="card artist-card">
                <a class="artist-photo" href={paths.artist(shownArtist.id)} use:link tabindex="-1" aria-hidden="true">
                  <img src={sized(shownArtist.picture_url, 640)} alt="" loading="lazy" />
                </a>
                <h3 class="over-photo">{$t('side.aboutArtist')}</h3>
                <div class="artist-body">
                  <div class="artist-head" class:no-fans={!shownArtist.fans}>
                    <a class="artist-name" href={paths.artist(shownArtist.id)} use:link>{shownArtist.name}</a>
                    {#if shownArtist.fans}<span class="fans">{fansLabel(shownArtist.fans, $lang)}</span>{/if}
                    <FollowButton following={$followedIds.has(shownArtist.id)} label={$followedIds.has(shownArtist.id) ? $t('artist.following') : $t('artist.follow')} on:click={() => toggleFollow(shownArtist.id)} />
                  </div>
                  {#if bioText}
                    <a class="bio" href={paths.artist(shownArtist.id)} use:link>{bioText}</a>
                  {/if}
                </div>
              </section>
            {/if}
          </div>
        {/if}
      {/if}
    </div>
  {/if}
</aside>

<style>
  :global(:root:has(.side-player)) {
    --player-h: 0px;
  }
  :global(body.side-resizing) {
    cursor: col-resize;
    user-select: none;
  }
  .resizer {
    width: 8px;
    flex-shrink: 0;
    display: flex;
    justify-content: center;
    cursor: col-resize;
    touch-action: none;
    outline: none;
  }
  .resizer::after {
    content: '';
    width: 2px;
    height: 100%;
    border-radius: 1px;
    background: rgba(255, 255, 255, 0.35);
    opacity: 0;
    transition: opacity 0.15s;
  }
  .resizer.active::after,
  .resizer:focus-visible::after {
    opacity: 1;
  }
  @media (hover: hover) {
    .resizer:hover::after {
      opacity: 1;
    }
  }
  .side-player {
    --side-bg: var(--bg);
    flex-shrink: 0;
    margin-right: 8px;
    border-radius: var(--radius);
    background: var(--side-bg);
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: none;
    transition: background-color 0.6s;
  }
  .side-player::-webkit-scrollbar {
    display: none;
  }
  .side-player::after {
    content: '';
    display: block;
    position: sticky;
    bottom: 0;
    height: 16px;
    margin-top: -16px;
    background: linear-gradient(to bottom, transparent, var(--side-bg));
    pointer-events: none;
  }
  .side-player.tint-full {
    --side-bg: var(--player-flat);
    --bg-card: rgba(255, 255, 255, 0.07);
  }
  .head {
    position: sticky;
    top: 0;
    z-index: 1;
    display: flex;
    align-items: center;
    gap: 2px;
    height: 56px;
    padding: 0 8px 0 16px;
    background: inherit;
  }
  .from {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: 15px;
    font-weight: 700;
  }
  @media (hover: hover) {
    a.from:hover {
      text-decoration: underline;
    }
  }
  .head .icon-btn {
    flex-shrink: 0;
  }
  .icon-btn.sleeping {
    color: var(--player-accent);
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 0 16px 16px;
  }
  .cover {
    width: 100%;
    aspect-ratio: 1;
    border-radius: var(--radius);
    overflow: hidden;
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
    background: var(--bg-press);
  }
  .cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .placeholder {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-press);
    color: var(--text-faint);
  }
  .title-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .mini-cover {
    width: 56px;
    height: 56px;
    flex-shrink: 0;
    border-radius: 4px;
    object-fit: cover;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
  }
  .meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .title {
    display: block;
    overflow: hidden;
    white-space: nowrap;
    padding: 0.08em 0;
    margin-top: -0.08em;
    font-size: 22px;
    font-weight: 700;
    letter-spacing: -0.02em;
    line-height: 1.2;
  }
  .title > span {
    display: inline-block;
  }
  .title:global(.marquee-on) {
    -webkit-mask-image: linear-gradient(90deg, transparent 0, #000 var(--marquee-fade), #000 calc(100% - 16px), transparent);
    mask-image: linear-gradient(90deg, transparent 0, #000 var(--marquee-fade), #000 calc(100% - 16px), transparent);
  }
  @media (hover: hover) {
    a.title:hover > span {
      text-decoration: underline;
    }
  }
  .artist {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    font-size: 14px;
    color: var(--text-dim);
  }
  .queue-mode .title {
    font-size: 17px;
  }
  .progress {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .times {
    display: flex;
    justify-content: space-between;
    font-size: 12px;
  }
  .controls {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: -4px;
  }
  .controls .skip {
    color: var(--text);
  }
  .queue-mode .controls .dup {
    visibility: hidden;
  }
  .icon-btn.on {
    color: var(--player-accent);
    position: relative;
  }
  .icon-btn.on::after {
    content: '';
    position: absolute;
    bottom: 2px;
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: currentColor;
  }
  .extras {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: -6px;
  }
  .extras .icon-btn {
    flex-shrink: 0;
  }
  .volume-group {
    flex: 0 1 auto;
    min-width: 0;
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .volume {
    flex: 0 1 112px;
    min-width: 48px;
    display: flex;
  }
  .card {
    border-radius: var(--radius);
    background: var(--bg-card);
  }
  .card h3 {
    font-size: 15px;
    font-weight: 700;
    letter-spacing: 0;
  }
  .next {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px;
  }
  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .text-link {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-dim);
    white-space: nowrap;
  }
  @media (hover: hover) {
    .text-link:hover {
      color: var(--text);
      text-decoration: underline;
    }
  }
  .next-track {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 0 -8px;
    padding: 6px 8px;
    border-radius: 6px;
    text-align: left;
    color: inherit;
    min-width: 0;
  }
  @media (hover: hover) {
    .next-track:hover {
      background: rgba(255, 255, 255, 0.07);
    }
  }
  .next-track img,
  .next-track .ph {
    width: 48px;
    height: 48px;
    flex-shrink: 0;
    border-radius: 4px;
    object-fit: cover;
    background: var(--bg-press);
  }
  .next-meta {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .next-title,
  .next-artist {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .next-title {
    font-weight: 500;
  }
  .next-artist {
    font-size: 13px;
    color: var(--text-dim);
  }
  .artist-slot {
    min-height: 1px;
    margin-top: -16px;
  }
  .artist-card {
    position: relative;
    margin-top: 16px;
  }
  .artist-photo {
    display: block;
    aspect-ratio: 16 / 10;
    border-radius: var(--radius) var(--radius) 0 0;
    background: var(--bg-press);
  }
  .artist-photo img {
    width: 100%;
    height: 100%;
    border-radius: inherit;
    object-fit: cover;
    object-position: center 25%;
    display: block;
  }
  .over-photo {
    position: absolute;
    top: 14px;
    left: 16px;
    pointer-events: none;
    text-shadow: 0 1px 8px rgba(0, 0, 0, 0.7);
  }
  .artist-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
  }
  .artist-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 4px 12px;
  }
  .artist-name {
    grid-column: 1 / -1;
    font-size: 17px;
    font-weight: 700;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .no-fans .artist-name {
    grid-column: 1;
  }
  .fans {
    font-size: 14px;
    color: var(--text-dim);
  }
  .artist-head :global(.follow) {
    grid-column: 2;
  }
  .bio {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 4;
    line-clamp: 4;
    overflow: hidden;
    font-size: 14px;
    line-height: 1.5;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .artist-name:hover {
      text-decoration: underline;
    }
    .bio:hover {
      color: var(--text);
    }
  }
</style>
