<script>
  import { onMount } from 'svelte';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import Dialog from './Dialog.svelte';
  import CopyField from './CopyField.svelte';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  let link = '';
  let error = '';

  onMount(() => {
    api
      .createInvite()
      .then(({ code }) => (link = `${location.origin}/?invite=${code}`))
      .catch((e) => (error = e.message));
  });
</script>

<Dialog title={$t('account.invite')} width={480} on:close>
  {#if error}
    <p class="error-text">{error}</p>
  {:else if link}
    <p class="hint">{$t('invite.title')}</p>
    <CopyField value={link} copiedText={$t('invite.copied')} />
  {:else}
    <span class="wait"><LoaderCircle class="spin" size={24} /></span>
  {/if}
</Dialog>

<style>
  .hint {
    margin: 0;
    font-size: 14px;
    color: var(--text-dim);
  }
  .wait {
    display: flex;
    color: var(--text-dim);
  }
</style>
