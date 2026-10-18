<script>
  // Нижняя панель на телефоне, как у Spotify: Главная, Поиск, Медиатека.
  // Верхней панели на телефоне нет; меню аккаунта — в шапке медиатеки.
  import { navStore, paths, link, lastSearch } from '../stores/nav.js';
  import House from '@lucide/svelte/icons/house';
  import Search from '@lucide/svelte/icons/search';
  import Library from '@lucide/svelte/icons/library';
  import { t } from '../i18n.js';

  $: view = $navStore.view;
  $: tabs = [
    { href: paths.home(), label: 'nav.home', icon: House, active: view === 'home' },
    { href: paths.search($lastSearch), label: 'nav.search', icon: Search, active: view === 'search' },
    { href: paths.library(), label: 'nav.library', icon: Library, active: ['library', 'liked', 'settings', 'wrapped', 'uploads', 'upload'].includes(view) },
  ];
</script>

<nav class="bottom-nav" aria-label={$t('nav.main')}>
  {#each tabs as tab (tab.label)}
    <a href={tab.href} use:link class:active={tab.active} aria-current={tab.active ? 'page' : undefined} aria-label={$t(tab.label)}>
      <svelte:component this={tab.icon} size={24} strokeWidth={tab.active ? 2.4 : 1.8} />
      <span>{$t(tab.label)}</span>
    </a>
  {/each}
</nav>

<style>
  .bottom-nav {
    display: none;
  }
  @media (max-width: 768px) {
    /* Затемнение низа страницы, как у Spotify: начинается выше мини-плеера
       (::before) и густеет к низу по плавной кривой, без видимого края —
       контент под панелью уходит в темноту, подписи читаются. */
    .bottom-nav {
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      z-index: 35;
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      height: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
      padding-bottom: env(safe-area-inset-bottom);
      background: linear-gradient(180deg, rgba(0, 0, 0, 0.88) 0%, #000 35%);
    }
    .bottom-nav::before {
      content: '';
      position: absolute;
      left: 0;
      right: 0;
      bottom: 100%;
      height: 140px; /* мини-плеер (64 + отступ) и ещё столько же над ним */
      pointer-events: none;
      background: linear-gradient(
        180deg,
        rgba(0, 0, 0, 0) 0%,
        rgba(0, 0, 0, 0.025) 10%,
        rgba(0, 0, 0, 0.092) 20%,
        rgba(0, 0, 0, 0.19) 30%,
        rgba(0, 0, 0, 0.31) 40%,
        rgba(0, 0, 0, 0.44) 50%,
        rgba(0, 0, 0, 0.57) 60%,
        rgba(0, 0, 0, 0.69) 70%,
        rgba(0, 0, 0, 0.788) 80%,
        rgba(0, 0, 0, 0.855) 90%,
        rgba(0, 0, 0, 0.88) 100%
      );
    }
    a {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: flex-end;
      gap: 4px;
      padding-bottom: 8px;
      color: var(--text-dim);
      font-size: 11px;
      font-weight: 500;
      -webkit-tap-highlight-color: transparent;
    }
    a.active {
      color: var(--text);
      font-weight: 700;
    }
  }
</style>
