<script>
  // Очередь — шторка справа (на телефоне — во весь экран): сейчас играет,
  // далее и то, что подобрало автовоспроизведение; клик по треку
  // переключает на него, стрелки переставляют. Внизу — переключатель
  // автовоспроизведения (то же, что в настройках). На телефоне закрывается
  // и жестом «назад» (там она во весь экран, как отдельный экран).
  import { sized } from '../imageproxy.js';
  import { createEventDispatcher, onMount } from 'svelte';
  import { backCloses } from '../stores/nav.js';
  import { fly } from 'svelte/transition';
  import { player, formatTime } from '../stores/player.js';
  import { t } from '../i18n.js';
  import { settings } from '../stores/settings.js';
  import ArtistLinks from './ArtistLinks.svelte';
  import X from '@lucide/svelte/icons/x';
  import ChevronUp from '@lucide/svelte/icons/chevron-up';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import Trash2 from '@lucide/svelte/icons/trash-2';

  const dispatch = createEventDispatcher();

  onMount(() => {
    if (matchMedia('(max-width: 768px)').matches) return backCloses(() => dispatch('close'));
  });

  $: s = $player;
  $: current = s.queue[s.index];
  $: after = s.queue.map((item, i) => ({ item, i })).filter(({ i }) => i > s.index);
  $: upcoming = after.filter(({ item }) => !item.autoplay);
  $: autoplayed = after.filter(({ item }) => item.autoplay);
</script>

<aside class="queue" transition:fly={{ x: 40, duration: 200 }} aria-label={$t('queue.title')}>
  <div class="head">
    <h3>{$t('queue.title')}</h3>
    <button class="icon-btn" on:click={() => dispatch('close')} aria-label={$t('album.close')}><X size={20} /></button>
  </div>

  {#if current}
    <div class="label">{$t('player.nowPlaying')}</div>
    <div class="item current">
      {#if current.coverUrl}<img src={sized(current.coverUrl, 64)} alt="" />{:else}<span class="ph"></span>{/if}
      <div class="meta"><span class="title">{current.title}</span><span class="artist"><ArtistLinks artists={current.artists || [{ name: current.artist }]} /></span></div>
      <span class="num">{formatTime((current.durationMs || 0) / 1000)}</span>
    </div>
  {/if}

  {#each [['queue.next', upcoming], ['queue.fromAutoplay', autoplayed]] as [label, list]}
    {#if list.length}
      <div class="label">{$t(label)}</div>
      {#each list as { item, i } (i + ':' + (item.id || item.deezerId))}
        <div class="item">
          <button class="pick" on:click={() => player.jumpTo(i)} aria-label={`${$t('player.play')}: ${item.title}`}>
            {#if item.coverUrl}<img src={sized(item.coverUrl, 64)} alt="" loading="lazy" />{:else}<span class="ph"></span>{/if}
            <div class="meta"><span class="title">{item.title}</span><span class="artist">{item.artist}</span></div>
          </button>
          <div class="tools">
            <button class="icon-btn" on:click={() => player.moveInQueue(i, -1)} disabled={i <= s.index + 1} aria-label={$t('queue.up')}><ChevronUp size={16} /></button>
            <button class="icon-btn" on:click={() => player.moveInQueue(i, 1)} disabled={i === s.queue.length - 1} aria-label={$t('queue.down')}><ChevronDown size={16} /></button>
            <button class="icon-btn" on:click={() => player.removeFromQueue(i)} aria-label={$t('queue.remove')}><Trash2 size={16} /></button>
          </div>
        </div>
      {/each}
    {/if}
  {/each}
  {#if !after.length}
    <p class="muted empty">{$t('queue.empty')}</p>
  {/if}

  <div class="autoplay">
    <div class="ap-label">
      <span>{$t('queue.autoplay')}</span>
      <small>{$t('queue.autoplayHint')}</small>
    </div>
    <button class="switch" role="switch" aria-checked={$settings.autoplay} aria-label={$t('queue.autoplay')} on:click={() => settings.set('autoplay', !$settings.autoplay)}>
      <span class="knob"></span>
    </button>
  </div>
</aside>

<style>
  .queue {
    position: fixed;
    top: calc(var(--topbar-h) + 8px);
    right: 8px;
    bottom: calc(var(--player-h) + 8px);
    width: 360px;
    z-index: 30;
    padding: 16px 12px;
    border-radius: var(--radius);
    background: var(--bg-elevated);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.6);
    overflow-y: auto;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 4px 8px;
  }
  h3 {
    font-size: 18px;
  }
  .label {
    padding: 12px 8px 6px;
    font-size: 14px;
    font-weight: 700;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 6px;
  }
  @media (hover: hover) {
    .item:hover {
      background: rgba(255, 255, 255, 0.07);
    }
  }
  .pick {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    text-align: left;
  }
  .item img,
  .ph {
    width: 44px;
    height: 44px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
    background: var(--bg-press);
  }
  .current {
    gap: 12px;
  }
  .meta {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .title,
  .artist {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .current .title {
    color: var(--player-accent);
  }
  .artist {
    font-size: 13px;
    color: var(--text-dim);
  }
  .tools {
    display: flex;
    opacity: 0;
  }
  .tools:focus-within {
    opacity: 1;
  }
  @media (hover: hover) {
    .item:hover .tools {
      opacity: 1;
    }
  }
  .tools .icon-btn {
    width: 28px;
    height: 28px;
  }
  .empty {
    padding: 8px;
    font-size: 14px;
  }
  .autoplay {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 16px 8px 0;
    padding-top: 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.1);
  }
  .ap-label {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 14px;
    font-weight: 700;
  }
  .ap-label small {
    font-size: 12px;
    font-weight: 400;
    color: var(--text-dim);
  }
  .switch {
    position: relative;
    width: 40px;
    height: 24px;
    flex-shrink: 0;
    border-radius: 999px;
    background: #535353;
    transition: background 0.15s;
  }
  .switch[aria-checked='true'] {
    background: var(--ok);
  }
  .knob {
    position: absolute;
    top: 3px;
    left: 3px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #fff;
    transition: transform 0.15s;
  }
  .switch[aria-checked='true'] .knob {
    transform: translateX(16px);
  }
  @media (max-width: 768px) {
    .queue {
      inset: 0;
      width: auto;
      border-radius: 0;
      z-index: 60;
      padding-top: max(16px, env(safe-area-inset-top));
      padding-bottom: max(16px, env(safe-area-inset-bottom));
    }
    .tools {
      opacity: 1;
    }
  }
</style>
