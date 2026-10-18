<script>
  // Админ: одноразовая ссылка на новый пароль для пользователя (почты у нас
  // нет). Человек откроет её и сам задаст пароль — админ его не видит.
  // Ссылка живёт 24 часа, новая отменяет старую (handlers_password.go).
  import { onMount, createEventDispatcher } from 'svelte';
  import { fade, scale } from 'svelte/transition';
  import { api } from '../api.js';
  import { t } from '../i18n.js';
  import X from '@lucide/svelte/icons/x';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import { focusTrap } from '../focusTrap.js';

  export let me = null;

  const dispatch = createEventDispatcher();
  let users = [];
  let userId = '';
  let link = null; // { url, username }
  let copied = false;
  let busy = false;
  let error = '';

  onMount(() => {
    api
      .adminUsers()
      .then((d) => {
        users = d.users.filter((u) => u.id !== me?.id);
        userId = users[0]?.id || '';
      })
      .catch((e) => (error = e.message));
    const onKey = (e) => e.key === 'Escape' && dispatch('close');
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  async function create() {
    busy = true;
    error = '';
    copied = false;
    try {
      const r = await api.createPasswordReset(userId);
      link = { url: `${location.origin}/?reset=${r.code}`, username: r.username };
      copy();
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(link.url);
      copied = true;
    } catch {
      copied = false; // ссылку видно — её можно выделить и скопировать руками
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="backdrop" use:focusTrap transition:fade={{ duration: 120 }} on:click|self={() => dispatch('close')} role="dialog" tabindex="-1" aria-modal="true" aria-label={$t('reset.title')}>
  <div class="dialog" transition:scale={{ start: 0.96, duration: 150 }}>
    <div class="head">
      <h2>{$t('reset.title')}</h2>
      <button type="button" class="icon-btn" on:click={() => dispatch('close')} aria-label={$t('album.close')}><X size={20} /></button>
    </div>
    <p class="hint">{$t('reset.hint')}</p>

    <div class="row">
      <select bind:value={userId} aria-label={$t('reset.user')} on:change={() => (link = null)}>
        {#each users as u (u.id)}<option value={u.id}>{u.username}</option>{/each}
      </select>
      <button type="button" class="primary" on:click={create} disabled={busy || !userId}>{$t('reset.create')}</button>
    </div>

    {#if link}
      <div class="link">
        <span class="hint">{$t('reset.linkFor', { name: link.username })}</span>
        <div class="row">
          <input readonly value={link.url} on:focus={(e) => e.currentTarget.select()} />
          <button class="icon-btn" on:click={copy} title={$t('invite.copy')} aria-label={$t('invite.copy')}>
            {#if copied}<Check size={18} />{:else}<Copy size={18} />{/if}
          </button>
        </div>
        {#if copied}<span class="hint">{$t('reset.copied')}</span>{/if}
      </div>
    {/if}
    {#if error}<p class="error-text">{error}</p>{/if}
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
    padding: 16px;
    background: rgba(0, 0, 0, 0.7);
  }
  .dialog {
    width: min(480px, 100%);
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 24px;
    border-radius: 8px;
    background: #282828;
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  h2 {
    font-size: 22px;
  }
  .hint {
    margin: 0;
    font-size: 14px;
    color: var(--text-dim);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  select,
  input {
    flex: 1;
    min-width: 0;
    height: 40px;
    padding: 0 10px;
    border-radius: 4px;
    border: 1px solid transparent;
    background: #3e3e3e;
    color: var(--text);
    font: inherit;
    font-size: 14px;
  }
  .primary {
    height: 40px;
    padding: 0 20px;
    border-radius: 999px;
    background: #fff;
    color: #000;
    font-weight: 700;
    white-space: nowrap;
  }
  .primary:disabled {
    opacity: 0.6;
  }
  .link {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
</style>
