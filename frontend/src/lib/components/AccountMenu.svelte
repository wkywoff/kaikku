<script>
  // Аватар и меню аккаунта: профиль, настройки, итоги года, установка, для админа —
  // режим админа, приглашение, сброс пароля; выход. На компьютере — в
  // верхней панели, на телефоне — в шапке медиатеки (как у Spotify).
  import { navigateAfterPaint, paths } from '../stores/nav.js';
  import { signOut } from '../signout.js';
  import { api } from '../api.js';
  import UserPlus from '@lucide/svelte/icons/user-plus';
  import UserRound from '@lucide/svelte/icons/user-round';
  import SearchX from '@lucide/svelte/icons/search-x';
  import LogOut from '@lucide/svelte/icons/log-out';
  import Copy from '@lucide/svelte/icons/copy';
  import Check from '@lucide/svelte/icons/check';
  import Settings from '@lucide/svelte/icons/settings';
  import Sparkles from '@lucide/svelte/icons/sparkles';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import ShieldCheck from '@lucide/svelte/icons/shield-check';
  import MonitorDown from '@lucide/svelte/icons/monitor-down';
  import { installPrompt, install } from '../stores/install.js';
  import { me, adminMode } from '../stores/me.js';
  import { t } from '../i18n.js';

  let root;
  let menuOpen = false;
  let resetOpen = false; // админ: окно «Сбросить пароль»
  let invite = null; // { link } | { error }
  let copied = false;

  async function createInvite() {
    invite = null;
    copied = false;
    try {
      const { code } = await api.createInvite();
      invite = { link: `${location.origin}/?invite=${code}` };
      copy();
    } catch (e) {
      invite = { error: e.message };
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(invite.link);
      copied = true;
    } catch {
      copied = false; // нет доступа к буферу — ссылку видно и её можно выделить
    }
  }

  function closeMenus(e) {
    if (root && !root.contains(e.target)) {
      menuOpen = false;
      invite = null;
    }
  }

  function go(path) {
    menuOpen = false;
    invite = null;
    navigateAfterPaint(path);
  }
</script>

<svelte:window on:click={closeMenus} />

<div class="account" bind:this={root}>
  <button class="avatar" on:click={() => ((menuOpen = !menuOpen), (invite = null))} aria-label={$t('account.menu')} aria-expanded={menuOpen} title={$me?.username || ''}>
    {($me?.username || '?').slice(0, 1).toUpperCase()}
  </button>
  {#if menuOpen}
    <div class="popover menu" role="menu">
      <div class="popover-title">{$me?.username || ''}{#if $me?.is_admin}<span class="muted"> · {$t('account.admin')}</span>{/if}</div>
      {#if $me?.username}
        <button class="menu-item" role="menuitem" on:click={() => go(paths.user($me.username))}><UserRound size={16} /> {$t('account.profile')}</button>
      {/if}
      <button class="menu-item" role="menuitem" on:click={() => go(paths.settings())}><Settings size={16} /> {$t('account.settings')}</button>
      <button class="menu-item" role="menuitem" on:click={() => go(paths.wrapped())}><Sparkles size={16} /> {$t('wrapped.title')}</button>
      {#if $installPrompt}
        <button class="menu-item" role="menuitem" on:click={() => ((menuOpen = false), install($installPrompt))}><MonitorDown size={16} /> {$t('account.install')}</button>
      {/if}
      {#if $me?.is_admin}
        <button class="menu-item" role="menuitemcheckbox" aria-checked={$adminMode} on:click={() => adminMode.update((on) => !on)}>
          <ShieldCheck size={16} /> {$t('account.adminMode')}
          <span class="mini-switch" class:on={$adminMode} aria-hidden="true"></span>
        </button>
        <button class="menu-item" role="menuitem" on:click={() => go(paths.missing())}><SearchX size={16} /> {$t('missing.title')}</button>
        <button class="menu-item" role="menuitem" on:click={createInvite}><UserPlus size={16} /> {$t('account.invite')}</button>
        <button class="menu-item" role="menuitem" on:click={() => ((menuOpen = false), (invite = null), (resetOpen = true))}><KeyRound size={16} /> {$t('account.resetPassword')}</button>
        {#if invite}
          <div class="invite-box">
            {#if invite.error}
              <span class="error-text">{invite.error}</span>
            {:else}
              <div class="muted small">{$t('invite.title')}</div>
              <div class="invite-row">
                <input readonly value={invite.link} on:focus={(e) => e.currentTarget.select()} />
                <button class="icon-btn" on:click={copy} title={$t('invite.copy')} aria-label={$t('invite.copy')}>
                  {#if copied}<Check size={18} />{:else}<Copy size={18} />{/if}
                </button>
              </div>
              {#if copied}<div class="muted small">{$t('invite.copied')}</div>{/if}
            {/if}
          </div>
        {/if}
      {/if}
      <div class="sep"></div>
      <button class="menu-item" role="menuitem" on:click={signOut}><LogOut size={16} /> {$t('account.logout')}</button>
    </div>
  {/if}
</div>

{#if resetOpen}
  {#await import('./ResetPasswordDialog.svelte') then m}
    <svelte:component this={m.default} me={$me} on:close={() => (resetOpen = false)} />
  {/await}
{/if}

<style>
  .account {
    position: relative;
  }
  .avatar {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    background: var(--player-accent, #535353);
    color: #121212;
    font-weight: 700;
    border: 4px solid var(--bg-card);
    box-sizing: content-box;
    transition: transform 0.1s;
  }
  @media (hover: hover) {
    .avatar:hover {
      transform: scale(1.06);
    }
  }
  .popover {
    position: absolute;
    right: 0;
    top: calc(100% + 8px);
    z-index: 30;
    min-width: 220px;
    padding: 8px;
    border-radius: 8px;
    background: #282828;
    box-shadow: 0 16px 32px rgba(0, 0, 0, 0.6);
  }
  .popover-title {
    padding: 6px 8px 10px;
    font-size: 13px;
    font-weight: 600;
  }
  .menu {
    width: 300px;
  }
  .invite-box {
    padding: 4px 8px 8px;
  }
  .sep {
    height: 1px;
    margin: 4px 0;
    background: rgba(255, 255, 255, 0.1);
  }
  .invite-row {
    display: flex;
    gap: 4px;
    align-items: center;
  }
  .invite-row input {
    flex: 1;
    min-width: 0;
    padding: 8px 10px;
    border-radius: 6px;
    border: 1px solid var(--border);
    background: var(--bg-input);
    font-size: 13px;
  }
  .small {
    font-size: 12px;
    padding: 4px 0;
  }
  .menu-item {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 8px;
    border-radius: 4px;
    font-size: 14px;
  }
  @media (hover: hover) {
    .menu-item:hover {
      background: rgba(255, 255, 255, 0.1);
    }
  }
  .mini-switch {
    position: relative;
    margin-left: auto;
    width: 30px;
    height: 18px;
    border-radius: 999px;
    background: #535353;
    transition: background 0.15s;
    flex-shrink: 0;
  }
  .mini-switch::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: #fff;
    transition: transform 0.15s;
  }
  .mini-switch.on {
    background: var(--ok);
  }
  .mini-switch.on::after {
    transform: translateX(12px);
  }

  @media (max-width: 768px) {
    .menu {
      width: min(300px, calc(100vw - 24px));
    }
  }
</style>
