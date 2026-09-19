<script lang="ts">
  import InfoLink from '../../components/InfoLink.svelte';
  import SyncControls from '../map-sync/SyncControls.svelte';
  import { type SyncActions } from '../map-sync/sync-actions.js';
  import { reloadLibrary } from '../map-library/library.svelte.js';
  import {
    forgetKey,
    getStatus,
    runSync,
    saveKey,
    type MapMakingAppStatus
  } from './api.js';
  import { mapMakingAppPlugin, publishMapMakingAppStatus } from './status.svelte.js';

  const status = $derived(mapMakingAppPlugin.status);
  const actions = $state<SyncActions>({ busy: false, message: null });

  const available = $derived(status?.available !== false);
  const enabled = $derived(Boolean(status?.enabled));
  const hasKey = $derived(Boolean(status?.hasKey));

  const statusMessage = $derived.by(() => {
    if (actions.message) return actions.message;
    if (status?.error) return { text: status.error, error: true };
    if (!available) {
      return { text: 'Start the OhneGuessr app to use sync.', error: true };
    }
    if (status?.running) {
      const phases: Record<string, string> = {
        catalog: 'Loading map catalog…',
        publishing: 'Saving synchronized maps…'
      };
      return {
        text: phases[status?.phase || ''] || (status?.total
          ? `Downloading ${status.completed || 0} / ${status.total}…`
          : 'Starting sync…'),
        error: false
      };
    }
    if (status?.phase === 'cancelled') return { text: 'Sync cancelled.', error: false };
    if (status?.lastResult) {
      const result = status.lastResult;
      const parts = [`${result.updated} updated`, `${result.unchanged} unchanged`];
      if (result.removed) parts.push(`${result.removed} removed`);
      if (result.failed) parts.push(`${result.failed} failed`);
      return { text: parts.join(' · '), error: Boolean(result.failed) };
    }
    if (status?.lastSyncAt) {
      const date = new Date(status.lastSyncAt);
      if (!Number.isNaN(date.getTime())) {
        return { text: `Last sync ${date.toLocaleString('en-GB', { dateStyle: 'short', timeStyle: 'short', hourCycle: 'h23' })}`, error: false };
      }
    }
    return {
      text: hasKey ? 'Ready to sync.' : 'Add an API key to connect.',
      error: false
    };
  });

  async function accept(next: MapMakingAppStatus) {
    const completed = status?.running && !next.running && next.phase === 'complete';
    publishMapMakingAppStatus(next);
    if (completed) await reloadLibrary();
  }

  async function refreshStatus() {
    try {
      await accept(await getStatus());
    } catch {
      publishMapMakingAppStatus({ available: false, enabled: false, hasKey: false, running: false });
    }
  }
</script>

<section class="sync-section">
  <h2>Map Making App Sync</h2>
  <div class="sync-details" class:hidden={!enabled || !available}>
    <SyncControls
      provider="Map Making App"
      {status}
      account={status?.user?.username ? `Connected as ${status.user.username}` : ''}
      {actions}
      refresh={refreshStatus}
      saveKey={(key) => saveKey(key).then(accept)}
      forgetKey={() => forgetKey().then(accept)}
      sync={() => runSync().then(accept)}
      keyProgress="Checking API key…"
      syncProgress="Starting sync…"
    />
  </div>
</section>
<div class="sync-footer">
  <div
    class="settings-note sync-status"
    class:error={statusMessage.error}
    class:hidden={!enabled && available && !actions.message}
  >
    {statusMessage.text}
  </div>
  <InfoLink
    class="sync-info-link"
    href="https://github.com/ohne-b/OhneGuessr#map-making-app-sync"
    label="Open the Map Making App sync guide on GitHub"
  />
</div>
