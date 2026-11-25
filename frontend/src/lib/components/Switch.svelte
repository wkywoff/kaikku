<script>
  export let checked = false;
  export let label;
  export let disabled = false;
</script>

<button type="button" class="switch" role="switch" aria-checked={checked} aria-label={label} {disabled} on:click><span class="knob"></span></button>

<style>
  .switch {
    position: relative;
    flex-shrink: 0;
    width: 40px;
    height: 24px;
    border-radius: 999px;
    background: var(--switch-off);
    transition: background-color 0.15s;
  }
  .switch[aria-checked='true'] {
    background: var(--ok);
  }
  .knob {
    position: absolute;
    top: 3px;
    left: 3px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #fff;
    transition: transform 0.15s;
  }
  .switch[aria-checked='true'] .knob {
    transform: translateX(16px);
  }
</style>
