<script>
  // «Поделиться итогами»: картинка с топами года (lib/shareCard.js) — её
  // можно скачать, а на телефоне отправить через системное «Поделиться».
  // Картинка собирается здесь, в браузере, и на сервер не попадает.
  import { modal } from '../modal.js';
  import { onMount, onDestroy, createEventDispatcher } from 'svelte';
  import { fade, scale } from 'svelte/transition';
  import { t } from '../i18n.js';
  import { lang } from '../stores/settings.js';
  import { me } from '../stores/me.js';
  import { shareCardModel, shareCardBlob } from '../shareCard.js';
  import X from '@lucide/svelte/icons/x';
  import Download from '@lucide/svelte/icons/download';
  import Share2 from '@lucide/svelte/icons/share-2';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let data; // ответ /api/me/wrapped

  const dispatch = createEventDispatcher();
  const close = () => dispatch('close');
  let file = null;
  let url = '';
  let error = '';

  // Язык сменили при открытом окне — карточка собирается заново.
  $: build(data, $lang, $me?.username);
  let seq = 0;
  async function build(data, lang, username) {
    const my = ++seq;
    error = '';
    try {
      const blob = await shareCardBlob(shareCardModel(data, { lang, username }));
      if (my !== seq) return;
      if (url) URL.revokeObjectURL(url);
      file = new File([blob], `kaikku-${data.year}.png`, { type: 'image/png' });
      url = URL.createObjectURL(file);
    } catch {
      if (my === seq) error = $t('wrapped.shareFailed');
    }
  }
  onDestroy(() => url && URL.revokeObjectURL(url));

  // Системное «Поделиться» с файлом есть на телефонах; на компьютере — только «Скачать».
  $: canShare = !!file && typeof navigator !== 'undefined' && !!navigator.canShare?.({ files: [file] });

  function download() {
    const a = document.createElement('a');
    a.href = url;
    a.download = file.name;
    a.click();
  }

  async function share() {
    try {
      await navigator.share({ files: [file] });
    } catch (e) {
      if (e?.name !== 'AbortError') download(); // не вышло — хотя бы сохранить
    }
  }

  onMount(() => {
    const onKey = (e) => e.key === 'Escape' && close();
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="backdrop" use:modal={close} transition:fade={{ duration: 120 }} on:click|self={close} role="dialog" tabindex="-1" aria-modal="true" aria-label={$t('wrapped.shareTitle')}>
  <div class="dialog" transition:scale={{ start: 0.96, duration: 150 }}>
    <div class="head">
      <h2>{$t('wrapped.shareTitle')}</h2>
      <button type="button" class="icon-btn" on:click={close} aria-label={$t('album.close')}><X size={20} /></button>
    </div>

    <div class="preview" aria-busy={!url && !error}>
      {#if url}
        <img src={url} alt={$t('wrapped.shareTitle')} />
      {:else if error}
        <p class="error-text">{error}</p>
      {:else}
        <span class="spin"><LoaderCircle size={28} /></span>
      {/if}
    </div>

    <div class="actions">
      {#if canShare}
        <button type="button" class="pill" on:click={download} disabled={!url}><Download size={16} /> {$t('wrapped.download')}</button>
        <button type="button" class="pill active" on:click={share}><Share2 size={16} /> {$t('share.share')}</button>
      {:else}
        <button type="button" class="pill active" on:click={download} disabled={!url}><Download size={16} /> {$t('wrapped.download')}</button>
      {/if}
    </div>
    <p class="note">{$t('wrapped.shareNote')}</p>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 80;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 16px 16px max(16px, env(safe-area-inset-bottom));
    background: rgba(0, 0, 0, 0.7);
  }
  .dialog {
    width: min(400px, 100%);
    max-height: 100%; /* по видимой высоте окна, как у остальных окон */
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 20px 24px 18px;
    border-radius: 8px;
    background: #282828;
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  h2 {
    font-size: 20px;
  }
  /* Картинка 9:16 — по высоте окна: кнопки под ней всегда видны. */
  .preview {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 9 / 16;
    /* …и по ширине: на узком телефоне — в поля окна (100vw − отступы подложки и окна). */
    height: min(560px, 100dvh - 220px, (100vw - 80px) * 16 / 9);
    flex: 0 0 auto;
    align-self: center;
    border-radius: 14px;
    overflow: hidden;
    background: #121212;
  }
  .preview img {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }
  .preview .spin {
    display: flex;
    color: var(--text-dim);
  }
  .actions {
    display: flex;
    justify-content: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .actions .pill {
    height: 40px;
    padding: 0 20px;
    font-weight: 700;
  }
  .note {
    margin: 0;
    text-align: center;
    font-size: 12px;
    color: var(--text-dim);
  }
</style>
