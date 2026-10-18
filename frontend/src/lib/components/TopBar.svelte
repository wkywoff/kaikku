<script>
  // Верхняя панель (компьютер; на телефоне вместо неё нижняя — BottomNav):
  // логотип, «Главная» и «Медиатека» по бокам поля поиска — так поиск ровно
  // по центру экрана, как у Spotify, — справа меню аккаунта.
  import { onMount } from 'svelte';
  import { navStore, paths, link } from '../stores/nav.js';
  import House from '@lucide/svelte/icons/house';
  import Library from '@lucide/svelte/icons/library';
  import SearchField from './SearchField.svelte';
  import AccountMenu from './AccountMenu.svelte';
  import { t } from '../i18n.js';

  let searchField;

  onMount(() => {
    // «/» или Ctrl/Cmd+K — к поиску, как в Spotify и браузерах.
    const onGlobalKey = (e) => {
      const typing = /^(INPUT|TEXTAREA)$/.test(document.activeElement?.tagName);
      if ((e.key === '/' && !typing) || (e.key.toLowerCase() === 'k' && (e.ctrlKey || e.metaKey))) {
        e.preventDefault();
        searchField.focus();
      }
    };
    window.addEventListener('keydown', onGlobalKey);
    return () => window.removeEventListener('keydown', onGlobalKey);
  });

  $: libraryActive = ['library', 'liked', 'uploads', 'upload'].includes($navStore.view);
</script>

<header class="topbar">
  <a class="brand" href={paths.home()} use:link aria-label="kaikku">
    <svg class="mark" viewBox="0 0 100 100" width="32" height="32" aria-hidden="true">
      <path d="M30 0 H70 C88 0 100 12 100 30 V70 C100 88 88 100 70 100 H30 C12 100 0 88 0 70 V30 C0 12 12 0 30 0 Z" fill="#FF4D2E" />
      <path d="M30 22 V78 M34 50 C45.3 51.9 51.9 47.2 54 36 C56.1 24.8 62.7 20.1 74 22 M34 50 C45.3 48.1 51.9 52.8 54 64 C56.1 75.2 62.7 79.9 74 78" fill="none" stroke="#141414" stroke-width="11" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
    <span class="brand-name">kaikku</span>
  </a>

  <a class="round-btn" class:active={$navStore.view === 'home'} href={paths.home()} use:link aria-label={$t('nav.home')} title={$t('nav.home')}>
    <House size={24} />
  </a>
  <SearchField bind:this={searchField} />
  <a class="round-btn" class:active={libraryActive} href={paths.library()} use:link aria-label={$t('nav.library')} title={$t('nav.library')}>
    <Library size={24} />
  </a>

  <div class="right">
    <AccountMenu />
  </div>
</header>

<style>
  /* Колонки: логотип | Главная | поиск | Медиатека | аккаунт. Крайние — 1fr,
     кнопки по бокам поиска одинаковые, поэтому поиск ровно по центру. */
  .topbar {
    height: var(--topbar-h);
    display: grid;
    grid-template-columns: 1fr auto minmax(0, 480px) auto 1fr;
    align-items: center;
    gap: 8px;
    padding: 0 16px 0 20px;
    background: #000;
    position: relative;
    z-index: 20;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    justify-self: start;
    margin-right: 8px;
  }
  /* Логотип — Unbounded (main.js), как в макетах названия. */
  .brand-name {
    font-family: var(--font-display);
    font-weight: 800;
    font-size: 19px;
    letter-spacing: -0.03em;
  }
  .round-btn {
    width: 48px;
    height: 48px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--bg-card);
    color: var(--text-dim);
    display: flex;
    align-items: center;
    justify-content: center;
    transition:
      color 0.15s,
      transform 0.1s,
      background-color 0.15s;
  }
  @media (hover: hover) {
    .round-btn:hover {
      color: var(--text);
      background: var(--bg-hover);
      transform: scale(1.04);
    }
  }
  .round-btn.active {
    color: var(--text);
  }
  .right {
    justify-self: end;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  @media (max-width: 768px) {
    .topbar {
      display: none;
    }
  }
</style>
