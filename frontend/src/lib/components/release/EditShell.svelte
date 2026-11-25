<script>
  import { link } from '../../stores/nav.js';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';
  import LoaderCircle from '@lucide/svelte/icons/loader-circle';

  export let backHref;
  export let backLabel;
  export let title = '';
  export let legend = '';
  export let loading = false;
  export let error = '';
</script>

<div class="page-pad release-edit">
  <a class="back" href={backHref} use:link><ArrowLeft size={18} /> {backLabel}</a>

  {#if error}
    <p class="error-text">{error}</p>
  {:else if loading}
    <div class="loading"><LoaderCircle class="spin" size={24} /></div>
  {:else}
    <header class="head">
      <div class="head-text">
        <h1>{title}</h1>
        {#if legend}<p class="legend">{legend}</p>{/if}
      </div>
      <div class="chips"><slot name="chips" /></div>
    </header>

    <div class="layout">
      <aside class="side"><slot name="side" /></aside>
      <div class="sections"><slot /></div>
    </div>
  {/if}
</div>

{#if !loading && !error}
  <div class="savebar">
    <div class="savebar-in page-pad"><slot name="bar" /></div>
  </div>
{/if}

<style>
  .release-edit {
    --edit-w: 1120px;
    padding-top: 16px;
    padding-bottom: 32px;
    max-width: calc(var(--edit-w) + 64px);
    margin: 0 auto;
  }
  .back {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    color: var(--text-dim);
  }
  @media (hover: hover) {
    .back:hover {
      color: var(--text);
    }
  }
  .loading {
    padding: 48px 0;
    color: var(--text-dim);
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px 24px;
    margin: 16px 0 28px;
  }
  .head-text {
    min-width: 0;
  }
  h1 {
    font-size: 32px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .legend {
    margin: 8px 0 0;
    max-width: 640px;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-faint);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
    flex-shrink: 0;
    padding-top: 6px;
  }
  .chips :global(.state) {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 12px;
    border-radius: 999px;
    background: var(--bg-press);
    font-size: 13px;
    color: var(--text-dim);
    white-space: nowrap;
  }
  .chips :global(.state.live) {
    background: rgba(30, 215, 96, 0.15);
    color: var(--ok);
  }
  .chips :global(.state.warn) {
    background: rgba(255, 170, 60, 0.14);
    color: #ffb95c;
  }

  .layout {
    display: grid;
    grid-template-columns: 260px minmax(0, 1fr);
    gap: 32px;
    align-items: start;
  }
  .side {
    position: sticky;
    top: 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .sections {
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-width: 0;
  }

  .release-edit :global(b.req) {
    color: var(--danger);
    font-weight: 700;
  }
  .release-edit :global(.card) {
    display: flex;
    flex-direction: column;
    gap: 22px;
    padding: 24px;
    border-radius: 12px;
    background: rgba(255, 255, 255, 0.035);
    border: 1px solid rgba(255, 255, 255, 0.06);
  }
  .release-edit :global(.card h2) {
    font-size: 18px;
    line-height: 1.3;
  }
  .release-edit :global(.count) {
    margin-left: 6px;
    font-size: 14px;
    font-weight: 500;
    color: var(--text-dim);
  }
  .release-edit :global(.optional) {
    margin-left: 4px;
    font: 500 13px var(--font);
    color: var(--text-faint);
  }
  .release-edit :global(.field) {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .release-edit :global(.label) {
    font-size: 14px;
    font-weight: 600;
  }
  .release-edit :global(.label.sub) {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--text-dim);
  }
  .release-edit :global(.field small),
  .release-edit :global(.side small) {
    font-size: 12px;
    line-height: 1.45;
    color: var(--text-faint);
  }
  .release-edit :global(.row2) {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(160px, 220px);
    gap: 20px;
    align-items: start;
  }
  .release-edit :global(.field input[type='text']),
  .release-edit :global(.field input[type='date']) {
    height: 42px;
    padding: 0 12px;
    border-radius: 6px;
    border: 1px solid transparent;
    background: #3a3a3a;
    color: var(--text);
    font-size: 15px;
    font-family: inherit;
    color-scheme: dark;
  }
  .release-edit :global(.field input:focus) {
    outline: none;
    border-color: #6a6a6a;
    background: #333;
  }
  .release-edit :global(.field input.invalid) {
    border-color: var(--danger);
  }
  .release-edit :global(.link-row) {
    display: flex;
    gap: 8px;
  }
  .release-edit :global(.link-row input) {
    flex: 1;
    min-width: 0;
  }
  .release-edit :global(.link-row .pill) {
    height: 42px;
    padding: 0 18px;
    font-weight: 700;
  }
  .release-edit :global(.pill:disabled) {
    opacity: 0.5;
  }

  .savebar {
    position: sticky;
    bottom: 0;
    z-index: 4;
    margin-top: 8px;
    background: rgba(18, 18, 18, 0.94);
    backdrop-filter: blur(8px);
    box-shadow: 0 0 0 100vmax rgba(18, 18, 18, 0.94);
    clip-path: inset(-1px -100vmax 0);
  }
  .savebar::before {
    content: '';
    position: absolute;
    top: -1px;
    left: 0;
    right: 0;
    height: 1px;
    background: var(--border);
    box-shadow: 0 0 0 100vmax var(--border);
    clip-path: inset(0 -100vmax);
  }
  .savebar-in {
    display: flex;
    align-items: center;
    gap: 16px;
    padding-top: 12px;
    padding-bottom: 12px;
    max-width: calc(1120px + 64px);
    margin: 0 auto;
  }
  .savebar-in :global(.status) {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: 14px;
    color: var(--text-dim);
  }
  .savebar-in :global(.status.warn) {
    color: var(--danger);
  }
  .savebar-in :global(.buttons) {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .savebar-in :global(.buttons .pill) {
    height: 40px;
    padding: 0 20px;
    font-weight: 700;
  }
  .savebar-in :global(.pill.primary) {
    background: var(--text);
    color: var(--bg);
  }
  @media (hover: hover) {
    .savebar-in :global(.pill.primary:hover:not(:disabled)) {
      background: var(--text);
      transform: scale(1.04);
    }
  }
  .savebar-in :global(.pill:disabled) {
    opacity: 0.5;
  }

  @media (max-width: 768px) {
    .savebar {
      bottom: calc(var(--tabbar-h) + env(safe-area-inset-bottom));
    }
    :global(.shell.with-player) .savebar {
      bottom: calc(var(--tabbar-h) + 76px + env(safe-area-inset-bottom));
    }
  }

  @media (max-width: 900px) {
    .layout {
      grid-template-columns: minmax(0, 1fr);
      gap: 20px;
    }
    .side {
      position: static;
      display: grid;
      grid-template-columns: 160px minmax(0, 1fr);
      gap: 8px 16px;
      align-items: start;
    }
    .side > :global(.label) {
      grid-column: 1 / -1;
    }
  }
  @media (max-width: 720px) {
    .head {
      flex-direction: column;
      gap: 12px;
      margin-bottom: 20px;
    }
    .chips {
      justify-content: flex-start;
      padding-top: 0;
    }
    h1 {
      font-size: 24px;
    }
    .release-edit :global(.card) {
      padding: 16px;
      gap: 18px;
    }
    .release-edit :global(.row2) {
      grid-template-columns: minmax(0, 1fr);
    }
    .side {
      grid-template-columns: 120px minmax(0, 1fr);
    }
    .savebar-in {
      flex-wrap: wrap;
      gap: 8px;
    }
    .savebar-in :global(.status) {
      flex-basis: 100%;
    }
    .savebar-in :global(.buttons) {
      width: 100%;
      justify-content: flex-end;
    }
  }
</style>
