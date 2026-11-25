<script>
  import { createEventDispatcher } from 'svelte';
  import { t } from '../../i18n.js';
  import { placeholderBackground } from '../../placeholder.js';
  import ImagePlus from '@lucide/svelte/icons/image-plus';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';
  import Trash2 from '@lucide/svelte/icons/trash-2';

  export let coverUrl = '';
  export let busy = false;
  export let placeholderFor = null;
  export let optional = false;
  export let hint = '';
  export let kindLine = '';
  export let title = '';
  export let artists = '';
  export let meta = '';
  export let removeLabel = '';

  const dispatch = createEventDispatcher();
  let input;
  let over = false;

  function pick(file) {
    if (file && file.type.startsWith('image/')) dispatch('file', file);
  }
</script>

<span class="label">{$t('upload.cover')}{#if optional}<span class="optional">{$t('upload.optional')}</span>{/if}</span>
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="cover"
  class:over
  class:filled={!!coverUrl}
  on:dragover|preventDefault={() => (over = true)}
  on:dragleave={() => (over = false)}
  on:drop|preventDefault={(e) => ((over = false), pick(e.dataTransfer?.files?.[0]))}
>
  <button type="button" class="cover-btn" aria-label={$t(coverUrl ? 'upload.changeCover' : 'upload.cover')} on:click={() => input.click()}>
    {#if coverUrl}
      <img src={coverUrl} alt="" />
      <span class="cover-overlay"><ImagePlus size={28} />{$t('upload.changeCover')}</span>
    {:else}
      {#if placeholderFor}<span class="cover-ph" style:background={placeholderBackground(placeholderFor)}></span>{/if}
      <span class="cover-empty"><ImagePlus size={36} /><span>{$t('upload.coverDrop')}</span></span>
    {/if}
    {#if busy}<span class="cover-busy"><LoaderCircle class="spin" size={28} /></span>{/if}
  </button>
</div>
{#if removeLabel && coverUrl && !busy}
  <button type="button" class="remove" on:click={() => dispatch('remove')}><Trash2 size={14} /> {removeLabel}</button>
{/if}
<input bind:this={input} type="file" accept="image/*" hidden on:change={(e) => (pick(e.currentTarget.files[0]), (e.currentTarget.value = ''))} />
<div class="summary">
  {#if hint}<small>{hint}</small>{/if}
  {#if title || artists}
    <div class="summary-card">
      <span class="kind">{kindLine}</span>
      <strong class="sum-title">{title}</strong>
      <span class="sum-artists">{artists || '—'}</span>
      {#if meta}<span class="sum-meta">{meta}</span>{/if}
    </div>
  {/if}
</div>

<style>
  .cover {
    width: 100%;
    aspect-ratio: 1;
    border-radius: 8px;
    border: 2px dashed var(--line);
    overflow: hidden;
    transition: border-color 0.15s;
  }
  .cover.filled {
    border-style: solid;
    border-color: transparent;
    box-shadow: 0 8px 28px rgba(0, 0, 0, 0.45);
  }
  .cover.over {
    border-color: var(--text);
  }
  .cover-btn {
    position: relative;
    display: block;
    width: 100%;
    height: 100%;
  }
  .cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .cover-ph {
    position: absolute;
    inset: 0;
    opacity: 0.6;
    transition: opacity 0.15s;
  }
  .cover-empty,
  .cover-overlay,
  .cover-busy {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 16px;
    text-align: center;
    font-size: 13px;
    color: var(--text);
    text-shadow: 0 1px 6px rgba(0, 0, 0, 0.6);
  }
  .cover-overlay {
    background: rgba(0, 0, 0, 0.6);
    opacity: 0;
    transition: opacity 0.15s;
  }
  .cover-btn:focus-visible .cover-overlay {
    opacity: 1;
  }
  @media (hover: hover) {
    .cover-btn:hover .cover-overlay {
      opacity: 1;
    }
    .cover-btn:hover .cover-ph {
      opacity: 0.35;
    }
    .cover:not(.filled):hover {
      border-color: var(--field-line);
    }
  }
  .cover-busy {
    background: rgba(0, 0, 0, 0.6);
  }
  .remove {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    width: fit-content;
    padding: 4px 0;
    font-size: 13px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .remove:hover {
      color: var(--text);
    }
  }
  .summary {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }
  .summary-card {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    padding-top: 14px;
    border-top: 1px solid var(--border);
  }
  .kind {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-dim);
  }
  .sum-title {
    font: 700 17px/1.3 var(--font-display);
    overflow-wrap: anywhere;
  }
  .sum-artists,
  .sum-meta {
    font-size: 13px;
    color: var(--text-dim);
    overflow-wrap: anywhere;
  }
  .sum-meta {
    color: var(--text-faint);
  }
  @media (max-width: 900px) {
    .summary-card {
      padding-top: 0;
      border-top: 0;
      order: -1;
    }
  }
</style>
