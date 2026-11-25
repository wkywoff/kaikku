<script>
  export let title;
  export let text = '';
  export let icon = null;
</script>

<div class="empty-state">
  {#if icon}<span class="icon"><svelte:component this={icon} size={40} /></span>{/if}
  <h2>{title}</h2>
  {#if text}<p class="muted">{text}</p>{/if}
  <slot />
</div>

<style>
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
    max-width: 480px;
    padding: 32px 0;
  }
  .icon {
    display: flex;
    color: var(--text-dim);
  }
  h2 {
    font-size: 22px;
  }
  p {
    margin: 0;
  }
</style>
