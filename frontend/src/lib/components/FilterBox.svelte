<script>
  // Поиск внутри списка, как в Spotify: лупа, по нажатию — поле ввода.
  // Esc очищает и сворачивает; пустое поле сворачивается, когда уходит фокус.
  import { tick } from 'svelte';
  import { t } from '../i18n.js';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';

  export let value = '';
  export let placeholder = '';

  let open = !!value;
  let input;

  async function expand() {
    open = true;
    await tick();
    input?.focus();
  }

  function onKey(e) {
    if (e.key === 'Escape') {
      value = '';
      open = false;
    }
  }

  function onBlur() {
    if (!value) open = false;
  }
</script>

<div class="filter" class:open>
  {#if open}
    <Search size={16} />
    <input bind:this={input} bind:value type="search" {placeholder} aria-label={placeholder} on:keydown={onKey} on:blur={onBlur} />
    {#if value}
      <button class="clear" type="button" aria-label={$t('search.clear')} on:mousedown|preventDefault on:click={() => ((value = ''), input?.focus())}><X size={14} /></button>
    {/if}
  {:else}
    <button class="icon-btn" type="button" title={placeholder} aria-label={placeholder} on:click={expand}><Search size={20} /></button>
  {/if}
</div>

<style>
  .filter {
    display: flex;
    align-items: center;
    color: var(--text-dim);
  }
  .filter.open {
    gap: 8px;
    height: 36px;
    width: min(240px, 56vw);
    padding: 0 10px;
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.1);
  }
  input {
    flex: 1;
    min-width: 0;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 14px;
  }
  input::-webkit-search-cancel-button {
    display: none;
  }
  .clear {
    display: flex;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .clear:hover {
      color: var(--text);
    }
  }
</style>
