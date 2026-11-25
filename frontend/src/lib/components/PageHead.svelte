<script>
  export let title;
  export let intro = '';
</script>

<header class="page-head">
  <h1>{title}</h1>
  {#if $$slots.corner}<div class="corner"><slot name="corner" /></div>{/if}
  {#if $$slots.default}<div class="actions"><slot /></div>{/if}
  {#if intro}<p class="muted intro">{intro}</p>{/if}
</header>

<style>
  .page-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px 16px;
    padding-top: 24px;
    margin-bottom: 24px;
  }
  h1 {
    min-width: 0;
    font-size: 32px;
    overflow-wrap: anywhere;
  }
  .corner {
    display: none;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .intro {
    grid-column: 1 / -1;
    max-width: 680px;
    margin: 0;
    font-size: 14px;
  }
  @media (max-width: 768px) {
    .page-head {
      padding-top: 16px;
    }
    h1 {
      font-size: 26px;
    }
    .corner {
      display: block;
    }
    .actions {
      grid-column: 1 / -1;
    }
  }
</style>
