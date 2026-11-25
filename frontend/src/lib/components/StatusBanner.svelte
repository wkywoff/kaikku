<script>
  import { online, updateReady } from '../stores/network.js';
  import { t } from '../i18n.js';
  import WifiOff from '@lucide/svelte/icons/wifi-off';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
</script>

{#if !$online}
  <div class="banner offline" role="status"><WifiOff size={16} /> {$t('status.offline')}</div>
{:else if $updateReady}
  <div class="banner update" role="status">
    <RefreshCw size={16} />
    <span>{$t('status.update')}</span>
    <button class="pill primary" on:click={() => location.reload()}>{$t('status.reload')}</button>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0 8px 8px;
    padding: 8px 14px;
    border-radius: var(--radius);
    font-size: 14px;
    font-weight: 500;
  }
  .offline {
    background: var(--bg-card);
    color: var(--text-dim);
  }
  .update {
    background: var(--info-fill);
    color: #fff;
  }
  .update span {
    flex: 1;
  }
</style>
