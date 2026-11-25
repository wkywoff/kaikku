<script>
  export let title;
  export let subtitle = '';
</script>

<div class="section-head">
  <div class="text">
    <div class="line">
      <h2>{title}</h2>
      <slot name="extra" />
    </div>
    {#if subtitle}<p class="sub">{subtitle}</p>{/if}
  </div>
  <slot />
</div>

<style>
  .section-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: 8px;
  }
  .text {
    min-width: 0;
  }
  .line {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  h2 {
    font-size: 24px;
  }
  .sub {
    margin: 2px 0 0;
    font-size: 14px;
    color: var(--text-dim);
  }
  @media (max-width: 768px) {
    h2 {
      font-size: 20px;
    }
  }
</style>
