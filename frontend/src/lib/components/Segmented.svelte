<script>
  import { createEventDispatcher } from 'svelte';

  export let options = [];
  export let value;
  export let label;

  const dispatch = createEventDispatcher();
</script>

<div class="segmented" role="radiogroup" aria-label={label}>
  {#each options as [key, name] (key)}
    <button type="button" role="radio" aria-checked={value === key} class:active={value === key} on:click={() => dispatch('change', key)}>{name}</button>
  {/each}
</div>

<style>
  .segmented {
    flex-shrink: 0;
    display: inline-flex;
    flex-wrap: wrap;
    gap: 4px;
    width: fit-content;
    max-width: 100%;
    padding: 4px;
    border-radius: 999px;
    background: var(--bg-card);
  }
  button {
    padding: 6px 14px;
    border-radius: 999px;
    font-size: 14px;
    color: var(--text-dim);
  }
  button.active {
    background: var(--text);
    color: var(--bg);
  }
  @media (max-width: 720px) {
    button {
      padding: 6px 10px;
      font-size: 13px;
    }
  }
</style>
