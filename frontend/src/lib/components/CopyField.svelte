<script>
  import { onMount } from 'svelte';
  import { t } from '../i18n.js';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';

  export let value;
  export let copiedText;

  let copied = false;

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      copied = true;
    } catch {
      copied = false;
    }
  }

  onMount(copy);
</script>

<div class="row">
  <input class="input" readonly {value} on:focus={(e) => e.currentTarget.select()} />
  <button type="button" class="icon-btn" on:click={copy} title={$t('invite.copy')} aria-label={$t('invite.copy')}>
    {#if copied}<Check size={18} />{:else}<Copy size={18} />{/if}
  </button>
</div>
{#if copied}<span class="hint">{copiedText}</span>{/if}

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .hint {
    font-size: 14px;
    color: var(--text-dim);
  }
</style>
