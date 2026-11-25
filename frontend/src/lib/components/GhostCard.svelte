<script>
  export let round = false;
</script>

<div class="ghost" aria-hidden="true">
  <div class="art" class:round></div>
  <div class="line"></div>
  <div class="line short"></div>
</div>

<style>
  .ghost {
    padding: 12px;
    min-width: 0;
    animation: ghost 1.6s ease-in-out infinite;
  }
  .art {
    aspect-ratio: 1;
    margin-bottom: 10px;
    border-radius: var(--radius-md);
    background: var(--bg-card);
  }
  .art.round {
    border-radius: 50%;
  }
  .line {
    width: 70%;
    height: 12px;
    margin: 5px 0;
    border-radius: var(--radius-md);
    background: var(--bg-card);
  }
  .line.short {
    width: 45%;
  }
  @keyframes ghost {
    50% {
      opacity: 0.55;
    }
  }
</style>
