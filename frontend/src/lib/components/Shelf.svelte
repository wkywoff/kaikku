<script>
  import { t } from '../i18n.js';
  import SectionHead from './SectionHead.svelte';
  import CardGrid from './CardGrid.svelte';
  import GhostCard from './GhostCard.svelte';

  export let title;
  export let subtitle = '';
  export let items = [];
  export let key = (item) => item.id;
  export let loading = false;
  export let round = false;
  export let priority = false;

  let expanded = false;
  let perRow = 2;

  $: visible = expanded ? items : items.slice(0, perRow);
</script>

{#if loading}
  <section aria-busy="true">
    <SectionHead {title} />
    <CardGrid bind:perRow>
      {#each Array(perRow) as _}<GhostCard {round} />{/each}
    </CardGrid>
  </section>
{:else if items.length}
  <section>
    <SectionHead {title} {subtitle}>
      {#if items.length > perRow}
        <button class="text-btn" on:click={() => (expanded = !expanded)}>{expanded ? $t('disc.showLess') : $t('disc.showAll')}</button>
      {/if}
    </SectionHead>
    <CardGrid bind:perRow>
      {#each visible as item, i (key(item))}
        <slot {item} eager={priority && i < perRow} first={priority && i === 0} />
      {/each}
    </CardGrid>
  </section>
{/if}
