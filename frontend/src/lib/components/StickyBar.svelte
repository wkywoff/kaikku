<div class="sticky-bar page-pad">
  <slot name="lead" />
  <h1><slot /></h1>
  {#if $$slots.aside}<div class="aside"><slot name="aside" /></div>{/if}
</div>

<style>
  .sticky-bar {
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 16px;
    padding-top: 20px;
    padding-bottom: 16px;
    background: var(--bg);
    box-shadow: 0 0 0 100vmax var(--bg);
    clip-path: inset(0 -100vmax);
  }
  h1 {
    flex: 1;
    min-width: 0;
    font-size: 24px;
    line-height: 1.2;
    overflow-wrap: anywhere;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  @media (hover: hover) {
    h1 :global(a:hover) {
      text-decoration: underline;
    }
  }
  .aside {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 20px;
    font-size: 14px;
    font-weight: 500;
    color: var(--text-dim);
  }
  @media (max-width: 768px) {
    .sticky-bar {
      padding-top: 12px;
    }
    h1 {
      font-size: 20px;
    }
    .aside {
      gap: 14px;
    }
  }
</style>
