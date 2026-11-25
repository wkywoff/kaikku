<script>
  import { paths } from '../stores/nav.js';
  import { signOut } from '../signout.js';
  import { openMenu } from '../stores/contextMenu.js';
  import { accountDialog } from '../stores/accountDialog.js';
  import { installPrompt, install } from '../stores/install.js';
  import { me, adminMode } from '../stores/me.js';
  import { t } from '../i18n.js';
  import UserPlus from '@lucide/svelte/icons/user-plus';
  import UserRound from '@lucide/svelte/icons/user-round';
  import SearchX from '@lucide/svelte/icons/search-x';
  import LogOut from '@lucide/svelte/icons/log-out';
  import Settings from '@lucide/svelte/icons/settings';
  import Sparkles from '@lucide/svelte/icons/sparkles';
  import UsersRound from '@lucide/svelte/icons/users-round';
  import ShieldCheck from '@lucide/svelte/icons/shield-check';
  import MonitorDown from '@lucide/svelte/icons/monitor-down';

  const PHONE = '(max-width: 768px)';

  function menuItems() {
    const name = $me?.username;
    const title = [name, $me?.is_admin && $t('account.admin')].filter(Boolean).join(' • ');
    return [
      ...(matchMedia(PHONE).matches || !title ? [] : [{ heading: title }]),
      ...(name ? [{ label: $t('account.profile'), icon: UserRound, href: paths.user(name) }] : []),
      { label: $t('account.settings'), icon: Settings, href: paths.settings() },
      { label: $t('wrapped.title'), icon: Sparkles, href: paths.wrapped() },
      ...($installPrompt ? [{ label: $t('account.install'), icon: MonitorDown, onClick: () => install($installPrompt) }] : []),
      ...($me?.is_admin
        ? [
            { separator: true },
            { label: $t('account.adminMode'), icon: ShieldCheck, checked: $adminMode, onClick: () => adminMode.update((on) => !on) },
            { label: $t('missing.title'), icon: SearchX, href: paths.missing() },
            { label: $t('account.invite'), icon: UserPlus, onClick: () => accountDialog.set('invite') },
            { label: $t('account.users'), icon: UsersRound, onClick: () => accountDialog.set('users') },
          ]
        : []),
      { separator: true },
      { label: $t('account.logout'), icon: LogOut, onClick: signOut },
    ];
  }

  function show(e) {
    const name = $me?.username || '';
    openMenu(e, menuItems(), { title: name, subtitle: $me?.is_admin ? $t('account.admin') : '' });
  }
</script>

<button class="avatar" on:click={show} aria-label={$t('account.menu')} aria-haspopup="menu" title={$me?.username || ''}>
  {($me?.username || '?').slice(0, 1).toUpperCase()}
</button>

<style>
  .avatar {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    background: var(--player-accent, var(--switch-off));
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
</style>
