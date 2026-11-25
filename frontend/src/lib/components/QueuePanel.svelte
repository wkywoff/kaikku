<script>
  import { createEventDispatcher, onMount } from 'svelte';
  import { fly } from 'svelte/transition';
  import { backCloses } from '../stores/nav.js';
  import { t } from '../i18n.js';
  import QueueView from './QueueView.svelte';
  import X from '@lucide/svelte/icons/x';

  const dispatch = createEventDispatcher();

  onMount(() => {
    if (matchMedia('(max-width: 768px)').matches) return backCloses(() => dispatch('close'));
  });
</script>

<aside class="queue" transition:fly={{ x: 40, duration: 200 }} aria-label={$t('queue.title')}>
  <div class="head">
    <h3>{$t('queue.title')}</h3>
    <button class="icon-btn" on:click={() => dispatch('close')} aria-label={$t('album.close')}><X size={20} /></button>
  </div>
  <QueueView compact />
</aside>

<style>
  .queue {
    position: fixed;
    top: calc(var(--topbar-h) + 8px);
    right: 8px;
    bottom: calc(var(--player-h) + var(--player-pad, 0px) + 8px);
    width: 360px;
    z-index: 30;
    padding: 16px;
    border-radius: var(--radius);
    background: var(--bg-elevated);
    box-shadow: 0 16px 48px rgba(0, 0, 0, 0.6);
    overflow-y: auto;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }
  h3 {
    font-size: 18px;
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
  }
</style>
