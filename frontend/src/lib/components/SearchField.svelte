<script>
  // Поле поиска-пилюля: в верхней панели на компьютере и вверху страницы
  // поиска на телефоне (там верхней панели нет).
  import { navStore, navigate, paths, lastSearch } from '../stores/nav.js';
  import Search from '@lucide/svelte/icons/search';
  import X from '@lucide/svelte/icons/x';
  import { t } from '../i18n.js';
  import { rememberSearch } from '../stores/recentSearches.js';

  let input;
  let query = $navStore.view === 'search' ? $navStore.q || '' : '';
  let debounce;

  /** Фокус в поле (горячие клавиши «/» и Ctrl+K в TopBar). */
  export function focus() {
    input.focus();
    input.select();
  }

  // Поле показывает запрос текущей страницы поиска (в том числе после «назад»).
  // Вне поиска поле пустое; фокус в нём возвращает последний запрос.
  $: if (document.activeElement !== input) query = $navStore.view === 'search' ? $navStore.q || '' : '';

  function onInput() {
    clearTimeout(debounce);
    debounce = setTimeout(() => {
      const q = query.trim();
      // Первый символ открывает страницу поиска (новая запись истории),
      // дальше печать её только уточняет (replace).
      navigate(paths.search(q), { replace: $navStore.view === 'search' });
    }, 250);
  }

  function clear() {
    query = '';
    clearTimeout(debounce);
    navigate(paths.search(''), { replace: $navStore.view === 'search' });
    input.focus();
  }

  function onFocus() {
    if ($navStore.view !== 'search') navigate(paths.search($lastSearch));
  }

  function onKeydown(e) {
    if (e.key === 'Enter' && query.trim()) rememberSearch(query);
    if (e.key === 'Escape') {
      if (query) clear();
      else input.blur();
    }
  }
</script>

<label class="search" class:active={$navStore.view === 'search'}>
  <Search size={22} />
  <input
    bind:this={input}
    bind:value={query}
    on:input={onInput}
    on:focus={onFocus}
    on:keydown={onKeydown}
    type="search"
    placeholder={$t('search.placeholder')}
    aria-label={$t('search.label')}
    autocomplete="off"
    spellcheck="false"
  />
  {#if query}
    <button class="clear" type="button" on:click|preventDefault={clear} aria-label={$t('search.clear')}><X size={20} /></button>
  {/if}
</label>

<style>
  .search {
    flex: 1;
    min-width: 0;
    height: 48px;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 12px 0 14px;
    border-radius: 999px;
    background: var(--bg-card);
    color: var(--text-dim);
    border: 1px solid transparent;
    transition:
      background-color 0.15s,
      border-color 0.15s;
    cursor: text;
  }
  @media (hover: hover) {
    .search:hover {
      background: var(--bg-hover);
      border-color: var(--border);
    }
  }
  .search:focus-within {
    border-color: var(--text);
    color: var(--text);
  }
  .search input {
    flex: 1;
    min-width: 0;
    height: 100%;
    background: none;
    border: none;
    outline: none;
    font-size: 16px; /* меньше 16 px — iOS зумит страницу при фокусе */
  }
  .search input::placeholder {
    color: var(--text-dim);
  }
  .search input::-webkit-search-cancel-button {
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
