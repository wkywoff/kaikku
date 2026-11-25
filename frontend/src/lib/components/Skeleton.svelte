<script>
  export let kind = 'hero';
  export let round = false;
  export let count = 8;
</script>

{#if kind === 'hero'}
  <div class="hero" aria-hidden="true">
    <div class="art" class:round></div>
    <div class="lines"><div></div><div></div></div>
  </div>
{:else if kind === 'bar'}
  <div class="bar" aria-hidden="true"></div>
{:else}
  <div class="rows" aria-hidden="true">
    {#each Array(count) as _}
      <div class="row"><div class="cover"></div><div class="text"><div></div><div></div></div></div>
    {/each}
  </div>
{/if}

<style>
  div[aria-hidden='true'] {
    animation: pulse 1.6s ease-in-out infinite;
  }
  @keyframes pulse {
    50% {
      opacity: 0.55;
    }
  }
  .hero {
    display: flex;
    align-items: flex-end;
    gap: 24px;
    min-height: 280px;
    padding: 24px 0;
  }
  .art {
    width: 232px;
    height: 232px;
    flex-shrink: 0;
    border-radius: var(--radius-md);
    background: var(--bg-card);
  }
  .art.round {
    border-radius: 50%;
  }
  .lines {
    flex: 1;
    min-width: 0;
  }
  .lines div {
    width: 360px;
    max-width: 100%;
    height: 48px;
    margin-top: 12px;
    border-radius: var(--radius);
    background: var(--bg-card);
  }
  .bar {
    width: 240px;
    height: 32px;
    margin: 24px 0 16px;
    border-radius: var(--radius);
    background: var(--bg-card);
  }
  .rows {
    display: flex;
    flex-direction: column;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 56px;
    padding: 0 12px;
  }
  .cover {
    width: 40px;
    height: 40px;
    flex-shrink: 0;
    border-radius: var(--radius-sm);
    background: var(--bg-card);
  }
  .text {
    flex: 1;
    min-width: 0;
  }
  .text div {
    width: 40%;
    height: 12px;
    margin: 5px 0;
    border-radius: var(--radius-md);
    background: var(--bg-card);
  }
  .text div:last-child {
    width: 25%;
  }
  @media (max-width: 768px) {
    .hero {
      flex-direction: column;
      align-items: center;
      min-height: 0;
      gap: 16px;
      padding-top: 12px;
    }
    .art {
      width: min(62vw, 240px);
      height: min(62vw, 240px);
    }
    .lines {
      flex: none;
      width: 100%;
    }
    .lines div {
      margin: 12px auto 0;
    }
  }
</style>
