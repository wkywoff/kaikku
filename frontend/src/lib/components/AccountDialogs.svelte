<script>
  import { accountDialog } from '../stores/accountDialog.js';
  import { me } from '../stores/me.js';

  const close = () => accountDialog.set(null);
</script>

{#if $accountDialog === 'users'}
  {#await import('./UsersDialog.svelte') then m}
    <svelte:component this={m.default} me={$me} on:close={close} />
  {/await}
{:else if $accountDialog === 'invite'}
  {#await import('./InviteDialog.svelte') then m}
    <svelte:component this={m.default} on:close={close} />
  {/await}
{/if}
