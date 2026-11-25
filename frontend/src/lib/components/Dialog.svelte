<script>
  import { createEventDispatcher, onMount } from 'svelte';
  import { fade, scale } from 'svelte/transition';
  import { modal } from '../modal.js';
  import { t } from '../i18n.js';
  import X from '@lucide/svelte/icons/x';

  export let title;
  export let width = 560;
  export let locked = false;
  export let keepOnBackdrop = false;
  export let onSubmit = () => {};

  const dispatch = createEventDispatcher();
  const close = () => !locked && dispatch('close');
  const dismiss = () => !keepOnBackdrop && close();

  onMount(() => {
    const onKey = (e) => e.key === 'Escape' && dismiss();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="backdrop" use:modal={dismiss} transition:fade={{ duration: 120 }} on:click|self={dismiss} role="dialog" tabindex="-1" aria-modal="true" aria-label={title}>
  <form class="dialog" style:width="min({width}px, 100%)" transition:scale={{ start: 0.96, duration: 150 }} on:submit|preventDefault={onSubmit}>
    <div class="head">
      <h2>{title}</h2>
      <button type="button" class="icon-btn" on:click={close} aria-label={$t('album.close')} disabled={locked}><X size={20} /></button>
    </div>
    <slot />
    {#if $$slots.footer}
      <div class="footer"><slot name="footer" /></div>
    {/if}
  </form>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 88;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px 16px max(16px, env(safe-area-inset-bottom));
    background: rgba(0, 0, 0, 0.7);
  }
  .dialog {
    max-height: 100%;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 24px;
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  h2 {
    min-width: 0;
    font-size: 22px;
  }
  .dialog > :global(.error-text) {
    margin: 0;
  }
  .footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 12px;
    margin-top: 8px;
  }
  .footer :global(.pill:last-child) {
    margin-left: auto;
  }
</style>
