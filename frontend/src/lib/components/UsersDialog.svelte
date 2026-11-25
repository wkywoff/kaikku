<script>
  import { onMount } from 'svelte';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import Dialog from './Dialog.svelte';
  import CopyField from './CopyField.svelte';

  export let me = null;

  let users = [];
  let userId = '';
  let link = null;
  let busy = false;
  let error = '';

  $: selected = users.find((u) => u.id === userId) || null;

  onMount(() => {
    api
      .adminUsers()
      .then((d) => {
        users = d.users.filter((u) => u.id !== me?.id);
        userId = users[0]?.id || '';
      })
      .catch((e) => (error = e.message));
  });

  async function create() {
    busy = true;
    error = '';
    link = null;
    try {
      const r = await api.createPasswordReset(userId);
      link = { url: `${location.origin}/?reset=${r.code}`, username: r.username };
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  async function setAdmin(user, admin) {
    if (!confirm($t(admin ? 'users.grantConfirm' : 'users.revokeConfirm', { name: user.username }))) return;
    busy = true;
    error = '';
    try {
      const r = await api.setUserAdmin(user.id, admin);
      users = users.map((u) => (u.id === r.id ? { ...u, is_admin: r.is_admin } : u));
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }
</script>

<Dialog title={$t('account.users')} width={480} locked={busy} on:close>
  <select class="input" bind:value={userId} aria-label={$t('reset.user')} on:change={() => ((link = null), (error = ''))}>
    {#each users as u (u.id)}<option value={u.id}>{u.username}{u.is_admin ? ` • ${$t('account.admin')}` : ''}</option>{/each}
  </select>

  <div class="section">
    <h3>{$t('reset.title')}</h3>
    <p class="hint">{$t('reset.hint')}</p>
    <button type="button" class="pill primary lg" on:click={create} disabled={busy || !selected}>{$t('reset.create')}</button>
    {#if link}
      <div class="link">
        <span class="hint">{$t('reset.linkFor', { name: link.username })}</span>
        {#key link.url}<CopyField value={link.url} copiedText={$t('reset.copied')} />{/key}
      </div>
    {/if}
  </div>

  <div class="section">
    <h3>{$t('users.adminTitle')}</h3>
    <p class="hint">{$t('users.adminHint')}</p>
    <button type="button" class="pill outline lg" on:click={() => setAdmin(selected, !selected.is_admin)} disabled={busy || !selected}>
      {selected?.is_admin ? $t('users.revoke') : $t('users.grant')}
    </button>
  </div>

  {#if error}<p class="error-text">{error}</p>{/if}
</Dialog>

<style>
  h3 {
    font-size: 15px;
  }
  .section {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .hint {
    margin: 0;
    font-size: 14px;
    color: var(--text-dim);
  }
  .link {
    align-self: stretch;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
</style>
