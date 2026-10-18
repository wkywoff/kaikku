<script>
  // Обложка во весь экран (как в Spotify по клику на артворк альбома).
  // Закрывается кнопкой, Esc или кликом мимо картинки.
  import { modal } from '../modal.js';
  import { sized } from '../imageproxy.js';
  import { onMount, createEventDispatcher } from 'svelte';
  import { fade, scale } from 'svelte/transition';
  import { t } from '../i18n.js';

  export let src;
  export let alt = '';

  const dispatch = createEventDispatcher();
  let closeButton;

  onMount(() => {
    closeButton?.focus();
    const onKey = (e) => e.key === 'Escape' && dispatch('close');
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="backdrop" use:modal={() => dispatch('close')} transition:fade={{ duration: 150 }} on:click|self={() => dispatch('close')} role="dialog" tabindex="-1" aria-modal="true" aria-label={alt}>
  <img src={sized(src, 640)} {alt} transition:scale={{ start: 0.92, duration: 180 }} />
  <button bind:this={closeButton} class="close" on:click={() => dispatch('close')}>{$t('album.close')}</button>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 70;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 24px;
    padding: 24px;
    background: rgba(0, 0, 0, 0.75);
  }
  img {
    width: min(640px, 86vw, calc(100vh - 140px));
    aspect-ratio: 1;
    object-fit: cover;
    box-shadow: 0 24px 80px rgba(0, 0, 0, 0.7);
  }
  .close {
    font-weight: 700;
    font-size: 15px;
    padding: 8px 16px;
  }
  @media (hover: hover) {
    .close:hover {
      transform: scale(1.04);
    }
  }
</style>
