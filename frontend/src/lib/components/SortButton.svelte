<script>
  // Кнопка сортировки списка («Дата добавления ˅»): меню вариантов, как у
  // Spotify; повторный выбор того же варианта меняет направление.
  //   options — [{ key, label, desc? }] (desc — направление по умолчанию;
  //   без направления — key 'custom');
  //   value — { key, desc }; on:change — новое значение.
  import { createEventDispatcher } from 'svelte';
  import { openMenu } from '../stores/contextMenu.js';
  import { t } from '../i18n.js';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import ArrowDownUp from '@lucide/svelte/icons/arrow-down-up';

  export let options = [];
  export let value = { key: '', desc: false };
  export let directional = true; // false — у вариантов нет направления (медиатека)

  const dispatch = createEventDispatcher();
  $: current = options.find((o) => o.key === value.key) || options[0];

  function show(e) {
    openMenu(e, [
      { heading: $t('disc.sortBy') },
      ...options.map((o) => ({
        label: o.label,
        checked: o.key === current?.key,
        icon: directional && o.key === current?.key && o.key !== 'custom' ? (value.desc ? ArrowDown : ArrowUp) : null,
        onClick: () => dispatch('change', { key: o.key, desc: o.key === current?.key ? !value.desc : !!o.desc }),
      })),
    ]);
  }
</script>

<button class="sort" on:click={show} aria-haspopup="menu" aria-label={$t('disc.sortBy')} title={current?.label || ''}>
  <span class="label">{current?.label || ''}</span>
  <span class="chevron"><ChevronDown size={16} /></span>
  <span class="icon"><ArrowDownUp size={20} /></span>
</button>

<style>
  .sort {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    font-weight: 500;
    color: var(--text-dim);
    white-space: nowrap;
  }
  @media (hover: hover) {
    .sort:hover {
      color: var(--text);
    }
  }
  .icon {
    display: none;
  }
  /* На телефоне подпись («Дата добавления») не влезает в строку кнопок —
     значок, как в Spotify; что выбрано — видно в меню. */
  @media (max-width: 768px) {
    .label,
    .chevron {
      display: none;
    }
    .icon {
      display: inline-flex;
    }
  }
</style>
