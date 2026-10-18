<script>
  // Круглая кнопка Play/Pause с залитыми глифами (как у Spotify). Цвет —
  // динамический акцент страницы (variant="accent") или белый (плеер).
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import { t } from '../i18n.js';

  export let playing = false;
  export let loading = false;
  export let size = 56; // px
  export let variant = 'accent'; // 'accent' | 'white'
  export let label = '';
  export let disabled = false;
</script>

<button
  class="play-button {variant}"
  style="--size: {size}px"
  aria-label={label || $t(playing ? 'player.pause' : 'player.play')}
  title={label || $t(playing ? 'player.pause' : 'player.play')}
  {disabled}
  on:click
>
  {#if loading}
    <LoaderCircle class="spin" size={size * 0.42} />
  {:else if playing}
    <svg viewBox="0 0 24 24" width={size * 0.42} height={size * 0.42} aria-hidden="true">
      <rect x="5" y="3.5" width="4.6" height="17" rx="1.2" fill="currentColor" />
      <rect x="14.4" y="3.5" width="4.6" height="17" rx="1.2" fill="currentColor" />
    </svg>
  {:else}
    <svg viewBox="0 0 24 24" width={size * 0.42} height={size * 0.42} aria-hidden="true">
      <path d="M7 3.9v16.2a1 1 0 0 0 1.52.85l13.06-8.1a1 1 0 0 0 0-1.7L8.52 3.05A1 1 0 0 0 7 3.9z" fill="currentColor" />
    </svg>
  {/if}
</button>

<style>
  .play-button {
    width: var(--size);
    height: var(--size);
    flex-shrink: 0;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition:
      transform 0.12s,
      filter 0.15s;
  }
  .play-button.accent {
    background: var(--page-accent, var(--accent));
    color: var(--page-accent-text, var(--accent-text));
    box-shadow: 0 8px 18px rgba(0, 0, 0, 0.35);
  }
  .play-button.white {
    background: #fff;
    color: #000;
  }
  @media (hover: hover) {
    .play-button:hover:not(:disabled) {
      transform: scale(1.05);
      filter: brightness(1.08);
    }
  }
  .play-button:active:not(:disabled) {
    transform: scale(0.96);
  }
  /* Треугольник и так стоит центром тяжести посередине (вершины 7, 7 и 22
     по x — центр 12 из 24): это и есть оптический центр. Прежний сдвиг на
     1 вправо поверх него уводил значок от центра. */
</style>
