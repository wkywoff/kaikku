<script>
  import { onDestroy } from 'svelte';
  import { t } from '../i18n.js';
  import { lang } from '../stores/settings.js';
  import { me } from '../stores/me.js';
  import { shareCardModel, shareCardBlob } from '../shareCard.js';
  import Dialog from './Dialog.svelte';
  import Download from '@lucide/svelte/icons/download';
  import Share2 from '@lucide/svelte/icons/share-2';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let data;

  let file = null;
  let url = '';
  let error = '';

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
      if (e?.name !== 'AbortError') download();
    }
  }
</script>

<Dialog title={$t('wrapped.shareTitle')} width={400} on:close>
  <div class="preview" aria-busy={!url && !error}>
    {#if url}
      <img src={url} alt={$t('wrapped.shareTitle')} />
    {:else if error}
      <p class="error-text">{error}</p>
    {:else}
      <span class="spin"><LoaderCircle size={28} /></span>
    {/if}
  </div>

  <svelte:fragment slot="footer">
    {#if canShare}
      <button type="button" class="pill lg" on:click={download} disabled={!url}><Download size={16} /> {$t('wrapped.download')}</button>
      <button type="button" class="pill primary lg" on:click={share}><Share2 size={16} /> {$t('share.share')}</button>
    {:else}
      <button type="button" class="pill primary lg" on:click={download} disabled={!url}><Download size={16} /> {$t('wrapped.download')}</button>
    {/if}
  </svelte:fragment>
</Dialog>

<style>
  .preview {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 9 / 16;
    height: min(560px, 100dvh - 190px, (100vw - 80px) * 16 / 9);
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
</style>
