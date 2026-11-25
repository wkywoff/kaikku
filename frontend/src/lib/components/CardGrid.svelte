<script>
  import { cardsPerRow } from '../layout.js';

  export let perRow = 2;

  let grid;
  let width = 0;
  $: if (width && grid) perRow = cardsPerRow(grid);
</script>

<div class="card-grid" bind:this={grid} bind:clientWidth={width}><slot /></div>

<style>
  .card-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(max(176px, 10%), 1fr));
    margin: 0 -12px;
  }
  @media (max-width: 768px) {
    .card-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 0 -8px;
    }
  }
</style>
