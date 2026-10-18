<script>
  // Уведомление внизу по центру (stores/toast.js), над плеером.
  import { fly } from 'svelte/transition';
  import { toasts } from '../stores/toast.js';
</script>

{#if $toasts}
  {#key $toasts.id}
    <div class="toast" class:error={$toasts.kind === 'error'} role="status" transition:fly={{ y: 12, duration: 180 }}>{$toasts.text}</div>
  {/key}
{/if}

<style>
  .toast {
    position: fixed;
    left: 50%;
    bottom: calc(var(--player-h) + 16px);
    transform: translateX(-50%);
    z-index: 95;
    max-width: min(480px, calc(100vw - 32px));
    padding: 10px 16px;
    border-radius: 6px;
    background: #3d91f4;
    color: #fff;
    font-size: 14px;
    font-weight: 500;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
    pointer-events: none;
  }
  .toast.error {
    background: #c9283a;
  }
  @media (max-width: 768px) {
    .toast {
      bottom: calc(var(--tabbar-h) + 84px + env(safe-area-inset-bottom)); /* над мини-плеером и нижней панелью */
    }
  }
</style>
