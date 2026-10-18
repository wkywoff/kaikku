<script>
  // Ползунок прогресса/громкости в стиле Spotify: тонкая полоса, заливка до
  // текущего значения, при наведении — акцентный цвет и кружок. Пока
  // человек тянет, показывается его значение, а не обновления плеера.
  import { createEventDispatcher } from 'svelte';

  export let value = 0;
  export let max = 1;
  export let step = 0.01;
  export let label = '';

  const dispatch = createEventDispatcher();
  let dragging = false;
  let dragValue = 0;

  $: shown = dragging ? dragValue : value;
  $: pct = max > 0 ? Math.min(100, Math.max(0, (shown / max) * 100)) : 0;

  function onInput(e) {
    dragging = true;
    dragValue = Number(e.target.value);
    dispatch('input', dragValue);
  }
  // Значение для change — то, что выбрал человек (dragValue), а не
  // e.target.value: к моменту change Svelte мог уже вернуть в <input>
  // текущую позицию плеера, и перемотка «отпрыгивала» назад.
  function onChange(e) {
    dispatch('change', dragging ? dragValue : Number(e.target.value));
    dragging = false;
    if (matchMedia('(hover: none)').matches) e.target.blur(); // палец отпустил — фокус ползунку не нужен
  }
</script>

<input
  class="slider"
  type="range"
  min="0"
  {max}
  {step}
  value={shown}
  style="--pct: {pct}%"
  aria-label={label}
  on:input={onInput}
  on:change={onChange}
  on:pointercancel={() => (dragging = false)}
/>

<style>
  .slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 12px;
    margin: 0;
    background: transparent;
    cursor: pointer;
    --fill: #fff;
  }
  /* Акцентный цвет — под курсором и с клавиатуры. На телефоне полоса
     всегда белая: после касания фокус оставался на ней, и она так и
     оставалась цветной. */
  @media (hover: hover) {
    .slider:hover,
    .slider:focus-visible {
      --fill: var(--player-accent, var(--accent));
    }
  }
  .slider::-webkit-slider-runnable-track {
    height: 4px;
    border-radius: 2px;
    background: linear-gradient(to right, var(--fill) var(--pct), rgba(255, 255, 255, 0.3) var(--pct));
  }
  .slider::-moz-range-track {
    height: 4px;
    border-radius: 2px;
    background: rgba(255, 255, 255, 0.3);
  }
  .slider::-moz-range-progress {
    height: 4px;
    border-radius: 2px;
    background: var(--fill);
  }
  .slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    width: 12px;
    height: 12px;
    margin-top: -4px;
    border-radius: 50%;
    background: #fff;
    border: none;
    opacity: 0;
    box-shadow: 0 2px 4px rgba(0, 0, 0, 0.5);
  }
  .slider::-moz-range-thumb {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: #fff;
    border: none;
    opacity: 0;
  }
  .slider:focus-visible::-webkit-slider-thumb,
  .slider:active::-webkit-slider-thumb {
    opacity: 1;
  }
  @media (hover: hover) {
    .slider:hover::-webkit-slider-thumb {
      opacity: 1;
    }
  }
  .slider:active::-moz-range-thumb {
    opacity: 1;
  }
  @media (hover: hover) {
    .slider:hover::-moz-range-thumb {
      opacity: 1;
    }
  }
  .slider:focus-visible {
    outline: none;
  }
  @media (hover: none) {
    .slider::-webkit-slider-thumb {
      opacity: 1;
    }
  }
</style>
