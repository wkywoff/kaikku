<script>
  // Нижний плеер как у Spotify: слева обложка и название (ссылки на альбом и
  // артиста), по центру shuffle / prev / play / next / repeat и прогресс,
  // справа очередь, громкость, полноэкранный режим. Таймер сна — в меню
  // трека (правый клик, долгое нажатие). Обложка с названием
  // лежит на скруглённой плашке цвета обложки (тот же цвет, что у шапки
  // страницы альбома), уходящей в чёрный. На телефоне — компактная полоса, нажатие
  // открывает полноэкранный плеер.
  import { sized } from '../imageproxy.js';
  import { longpress } from '../longpress.js';
  import { swipeTrack } from '../swipe.js';
  import { createEventDispatcher } from 'svelte';
  import { player, formatTime } from '../stores/player.js';
  import { settings } from '../stores/settings.js';
  import { link } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import PlayButton from './PlayButton.svelte';
  import ArtistLinks from './ArtistLinks.svelte';
  import Slider from './Slider.svelte';
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
  import LikeButton from './LikeButton.svelte';
  import { openMenu } from '../stores/contextMenu.js';
  import { trackMenuItems } from '../trackMenu.js';
  import { rowFromEntry } from '../library.js';
  import { sleepMenuItems, sleepLabel } from '../sleepMenu.js';
  import { wheelVolume } from '../wheelVolume.js';

  export let queueOpen = false;

  const dispatch = createEventDispatcher();

  $: s = $player;
  $: track = s.queue[s.index];

  // Меню трека — как в списках, плюс таймер сна (как в «…» полноэкранного).
  function openTrackMenu(e) {
    openMenu(e, [
      ...trackMenuItems(rowFromEntry(track)),
      { separator: true },
      { label: sleepLabel(s.sleep, $t), icon: Moon, submenu: sleepMenuItems().filter((i) => !i.heading) },
    ]);
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
</script>

{#if track}
  <footer class="player">
    <!-- Правый клик по треку — то же меню, что в списках. -->
    <div
      class="now"
      role="group"
      on:contextmenu={openTrackMenu}
      use:longpress={openTrackMenu}
      use:swipeTrack={{ onNext: () => player.next(), onPrev: () => player.prev({ always: true }) }}
    >
      {#if track.coverUrl}
        <img class="cover" src={sized(track.coverUrl, 64)} alt="" />
      {:else}
        <div class="cover placeholder"><Disc3 size={24} /></div>
      {/if}
      <div class="meta">
        {#if track.albumHref}
          <a class="title" href={track.albumHref} use:link title={track.title}>{track.title}</a>
        {:else}
          <span class="title">{track.title}</span>
        {/if}
        <span class="artist"><ArtistLinks menu artists={track.artists || [{ name: track.artist, href: track.artistHref }]} /></span>
      </div>
      <span class="like-slot"><LikeButton deezerId={track.deezerId} /></span>
      <!-- На телефоне вся левая часть открывает полноэкранный плеер. -->
      <button class="open-full" on:click={() => dispatch('expand')} aria-label={$t('player.open')}></button>
    </div>

    <div class="center">
      <div class="controls">
        <button class="icon-btn hide-mobile" class:on={s.shuffle} on:click={() => player.toggleShuffle()} title={$t('player.shuffle')} aria-pressed={s.shuffle}>
          <Shuffle size={18} />
        </button>
        <button class="icon-btn hide-mobile" on:click={() => player.prev()} title={$t('player.prev')} aria-label={$t('player.prev')}>
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path d="M6 4.5v15M19 5.1v13.8a.8.8 0 0 1-1.25.66L8.6 12.66a.8.8 0 0 1 0-1.32l9.15-6.9A.8.8 0 0 1 19 5.1z" fill="currentColor" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
        </button>
        <PlayButton variant="white" size={36} playing={s.isPlaying} loading={s.isLoading && !s.isPlaying} label={s.isPlaying ? $t('player.pause') : $t('player.play')} on:click={() => player.toggle()} />
        <button class="icon-btn" on:click={() => player.next()} disabled={!player.canGoNext(s, $settings.autoplay)} title={$t('player.next')} aria-label={$t('player.next')}>
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true"><path d="M18 4.5v15M5 5.1v13.8a.8.8 0 0 0 1.25.66l9.15-6.9a.8.8 0 0 0 0-1.32l-9.15-6.9A.8.8 0 0 0 5 5.1z" fill="currentColor" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
        </button>
        <button class="icon-btn hide-mobile" class:on={s.repeat !== 'off'} on:click={() => player.cycleRepeat()}
          title={$t(s.repeat === 'one' ? 'player.repeatOne' : s.repeat === 'all' ? 'player.repeatAll' : 'player.repeat')} aria-pressed={s.repeat !== 'off'}>
          {#if s.repeat === 'one'}<Repeat1 size={18} />{:else}<Repeat size={18} />{/if}
        </button>
      </div>
      <div class="progress">
        <span class="num time">{formatTime(s.currentTime)}</span>
        <Slider value={s.currentTime} max={s.duration || 0} step={0.1} label={$t('player.position')} on:change={(e) => player.seek(e.detail)} />
        <span class="num time">{formatTime(s.duration)}</span>
      </div>
    </div>

    <div class="extra">
      <button class="icon-btn" on:click={() => dispatch('lyrics')} title={$t('lyrics.show')} aria-label={$t('lyrics.show')}>
        <MicVocal size={18} />
      </button>
      <button class="icon-btn" class:on={queueOpen} on:click={() => dispatch('queue')} title={$t('player.queue')} aria-pressed={queueOpen}>
        <ListMusic size={18} />
      </button>
      <!-- Колёсико мыши над кнопкой или ползунком меняет громкость. -->
      <div class="volume-group" use:wheelVolume>
        <button class="icon-btn" on:click={toggleMute} title={s.volume === 0 ? $t('player.unmute') : $t('player.mute')}>
          {#if s.volume === 0}<VolumeX size={18} />{:else if s.volume < 0.34}<Volume size={18} />{:else if s.volume < 0.67}<Volume1 size={18} />{:else}<Volume2 size={18} />{/if}
        </button>
        <div class="volume"><Slider value={s.volume} max={1} label={$t('player.volume')} on:input={(e) => player.setVolume(e.detail)} on:change={(e) => player.setVolume(e.detail)} /></div>
      </div>
      <button class="icon-btn" on:click={() => dispatch('fullscreen')} title={$t('player.fullscreen')}>
        <Maximize2 size={18} />
      </button>
    </div>

    <!-- Тонкая полоса прогресса над мини-плеером на телефоне. -->
    <div class="mobile-progress" style="--pct: {s.duration ? (s.currentTime / s.duration) * 100 : 0}%"></div>
  </footer>
{/if}

<style>
  /* 30% / 40% / 30%, как у Spotify: слева трек — не уже 240 px, справа
     кнопки — не меньше, чем им нужно; сжимается середина. В узком окне
     правая часть проще (ниже). */
  .player {
    height: var(--player-h);
    display: grid;
    grid-template-columns: minmax(240px, 3fr) minmax(0, 4fr) minmax(min-content, 3fr);
    align-items: center;
    gap: 16px;
    padding: 0 16px 0 8px;
    background: #000;
    position: relative;
  }
  .now {
    display: flex;
    align-items: center;
    gap: 14px;
    -webkit-touch-callout: none; /* долгое нажатие — меню трека (longpress.js) */
    width: fit-content;
    max-width: 100%;
    min-width: 0;
    padding: 8px 64px 8px 8px;
    position: relative;
    isolation: isolate;
  }
  /* Плашка: сплошной цвет под маской-градиентом. background-color
     анимируется при смене трека, а сам градиент — нет. */
  .now::before {
    content: '';
    position: absolute;
    inset: 0;
    z-index: -1;
    border-radius: 8px;
    background-color: var(--player-base);
    -webkit-mask-image: linear-gradient(90deg, #000 0%, rgba(0, 0, 0, 0.55) 45%, transparent 100%);
    mask-image: linear-gradient(90deg, #000 0%, rgba(0, 0, 0, 0.55) 45%, transparent 100%);
    transition: background-color 0.6s;
  }
  .cover {
    width: 56px;
    height: 56px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
  }
  .placeholder {
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-press);
    color: var(--text-faint);
  }
  .meta {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .title,
  .artist {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .title {
    font-size: 14px;
    font-weight: 500;
  }
  .artist {
    font-size: 12px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    a.title:hover {
      text-decoration: underline;
      color: var(--text);
    }
  }
  .open-full {
    display: none;
  }
  .like-slot {
    display: flex;
    flex-shrink: 0;
    position: relative;
    z-index: 1; /* над .open-full на телефоне */
  }
  .center {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    max-width: 720px;
    width: 100%;
    justify-self: center;
  }
  .controls {
    display: flex;
    align-items: center;
    gap: 12px;
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
  .progress {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
  }
  .time {
    font-size: 12px;
    min-width: 40px;
    text-align: center;
  }
  .extra {
    justify-self: end;
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .volume-group {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .volume {
    width: 100px;
    display: flex;
  }
  .mobile-progress {
    display: none;
  }

  @media (max-width: 1100px) and (min-width: 769px) {
    .now {
      padding-right: 24px;
    }
    .volume {
      width: 72px;
    }
  }
  /* Ещё уже — без ползунка громкости (кнопка звука и колёсико остаются). */
  @media (max-width: 940px) and (min-width: 769px) {
    .volume {
      display: none;
    }
    .now {
      gap: 10px;
    }
    .cover {
      width: 48px;
      height: 48px;
    }
  }

  @media (max-width: 768px) {
    /* Мини-плеер парит над страницей и нижней панелью (как в Spotify):
       вокруг него видно контент, а не чёрную рамку. Отступ под него — в
       App.svelte. Отступы одинаковые с обеих сторон: полосы прокрутки на
       телефонной раскладке нет (App.svelte). */
    .player {
      position: fixed;
      left: 8px;
      right: 8px;
      bottom: calc(var(--tabbar-h) + 4px + env(safe-area-inset-bottom)); /* над нижней панелью */
      z-index: 40;
      height: 64px;
      grid-template-columns: minmax(0, 1fr) auto;
      padding: 0 8px;
      box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
      gap: 8px;
      border-radius: 8px;
      background: color-mix(in srgb, var(--player-base) 65%, #000);
      overflow: hidden; /* свайп трека (swipe.js) уводит название за край плашки — не за плеер */
    }
    .now {
      width: auto;
      padding: 0;
      gap: 10px;
    }
    /* Плюс — справа, у кнопки Play, а не сразу за коротким названием. */
    .like-slot {
      margin-left: auto;
    }
    .now::before {
      display: none;
    }
    .cover {
      width: 44px;
      height: 44px;
    }
    .meta :global(a) {
      pointer-events: none; /* на телефоне нажатие открывает плеер, а не ссылку */
    }
    .open-full {
      display: block;
      position: absolute;
      inset: 0;
    }
    .center {
      width: auto;
    }
    .controls {
      gap: 4px;
    }
    .progress,
    .extra,
    .hide-mobile {
      display: none;
    }
    .mobile-progress {
      display: block;
      position: absolute;
      left: 8px;
      right: 8px;
      bottom: 0;
      height: 2px;
      border-radius: 1px;
      background: linear-gradient(to right, #fff var(--pct), rgba(255, 255, 255, 0.25) var(--pct));
    }
  }
</style>
