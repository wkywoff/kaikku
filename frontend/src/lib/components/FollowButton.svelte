<script>
  export let following = false;
  export let label;
  export let disabled = false;
</script>

<button type="button" class="pill outline follow" aria-pressed={following} {disabled} on:click><span class="label">{label}</span></button>

<style>
  .follow {
    min-width: 0;
    max-width: 100%;
    padding: 0 16px;
    font-weight: 700;
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
