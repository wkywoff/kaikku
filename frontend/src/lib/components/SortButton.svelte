<script>
  import { createEventDispatcher } from 'svelte';
  import { openMenu } from '../stores/contextMenu.js';
  import { t } from '../i18n.js';
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import ArrowDownUp from '@lucide/svelte/icons/arrow-down-up';

  export let options = [];
  export let value = { key: '', desc: false };
  export let directional = true;

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

<button class="drop-btn" on:click={show} aria-haspopup="menu" aria-label={$t('disc.sortBy')} title={current?.label || ''}>
  <span class="label">{current?.label || ''}</span>
  <span class="chevron"><ChevronDown size={16} /></span>
  <span class="icon"><ArrowDownUp size={20} /></span>
</button>

<style>
  .icon {
    display: none;
  }
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
