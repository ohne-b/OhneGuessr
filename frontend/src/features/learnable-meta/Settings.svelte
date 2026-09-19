<script lang="ts">
  import InfoLink from '../../components/InfoLink.svelte';
  import IconButton from '../../components/IconButton.svelte';
  import SyncControls from '../map-sync/SyncControls.svelte';
  import { runSyncAction, type SyncActions } from '../map-sync/sync-actions.js';
  import { reloadLibrary } from '../map-library/library.svelte.js';
  import {
    addMap,
    forgetKey,
    getStatus,
    runSync,
    saveKey,
    type LearnableMetaStatus
  } from './api.js';
  import './learnable-meta.css';
  import { learnableMetaPlugin, publishLearnableMetaStatus } from './status.svelte.js';

  const status = $derived(learnableMetaPlugin.status);
  let mapName = $state('');
  let mapId = $state('');
  const actions = $state<SyncActions>({ busy: false, message: null });

  const available = $derived(status?.available !== false);
  const enabled = $derived(Boolean(status?.enabled));
  const hasKey = $derived(Boolean(status?.hasKey));
  const running = $derived(Boolean(status?.running));

  const statusMessage = $derived.by(() => {
    if (actions.message) return actions.message;
    if (status?.error) return { text: status.error, error: true };
    if (!available) {
      return {
        text: 'Start the OhneGuessr app to use Learnable Meta sync.',
        error: true
      };
    }
    if (running) {
      return {
        text: status?.phase === 'cancelling'
          ? 'Cancelling synchronization…'
          : status?.total
            ? `Synchronizing ${status.completed || 0} / ${status.total}…`
            : 'Starting synchronization…',
        error: false
      };
    }
    if (status?.phase === 'cancelled') {
      return { text: 'Synchronization cancelled.', error: false };
    }
    if (status?.lastResult) {
      const result = status.lastResult;
      const parts = [`${result.updated} updated`, `${result.unchanged} unchanged`];
      if (result.failed) parts.push(`${result.failed} failed`);
      const firstFailure = result.failures?.[0]?.error;
      return {
        text: parts.join(' · ') + (firstFailure ? ` — ${firstFailure}` : ''),
        error: Boolean(result.failed)
      };
    }
    if (hasKey && !status?.maps?.length) {
      return { text: 'API key saved. Add a map to verify it.', error: false };
    }
    if (status?.lastSyncAt) {
      const date = new Date(status.lastSyncAt);
      if (!Number.isNaN(date.getTime())) {
        return { text: `Last sync ${date.toLocaleString('en-GB', { dateStyle: 'short', timeStyle: 'short', hourCycle: 'h23' })}`, error: false };
      }
    }
    return {
      text: hasKey ? 'Ready to synchronize.' : 'Add an API key to connect.',
      error: false
    };
  });

  async function accept(next: LearnableMetaStatus, reloadAfter = false) {
    const completed = status?.running && !next.running &&
      (next.phase === 'complete' || next.phase === 'cancelled');
    actions.message = null;
    publishLearnableMetaStatus(next);
    if (reloadAfter || completed) await reloadLibrary();
  }

  async function refreshStatus() {
    try {
      await accept(await getStatus());
    } catch {
      actions.message = {
        text: 'Start the OhneGuessr app to use Learnable Meta sync.',
        error: true
      };
      publishLearnableMetaStatus({
        available: false,
        enabled: false,
        hasKey: false,
        running: false,
        maps: []
      });
    }
  }

  async function addLearnableMap() {
    const name = mapName.trim();
    const id = mapId.trim();
    if (!name || !id) {
      actions.message = { text: 'Enter both a local name and map ID.', error: true };
      return;
    }
    if (await runSyncAction(actions, () => addMap(id, name).then((next) => accept(next, true)),
      'Checking and downloading the Learnable Meta map…', 'Could not add that map.')) {
      mapName = '';
      mapId = '';
    }
  }

  $effect(() => {
    if (status) actions.message = null;
  });
</script>

<section class="sync-section">
  <h2>Learnable Meta</h2>
  <div class="sync-details" class:hidden={!enabled || !available}>
    <SyncControls
      provider="Learnable Meta"
      {status}
      account={hasKey ? 'API key saved locally' : ''}
      {actions}
      refresh={refreshStatus}
      saveKey={(key) => saveKey(key).then(accept)}
      forgetKey={() => forgetKey().then(accept)}
      sync={() => runSync().then(accept)}
    />
    <form
      class="sync-key-form lm-map-form"
      class:hidden={!hasKey}
      onsubmit={(event) => {
        event.preventDefault();
        void addLearnableMap();
      }}
    >
      <input
        bind:value={mapName}
        type="text"
        maxlength="120"
        autocomplete="off"
        placeholder="Local map name"
        aria-label="Local map name"
        disabled={actions.busy || running}
      />
      <input
        bind:value={mapId}
        type="text"
        maxlength="200"
        autocomplete="off"
        spellcheck="false"
        placeholder="GeoGuessr ID"
        aria-label="Learnable Meta GeoGuessr ID"
        disabled={actions.busy || running}
      />
      <IconButton
        icon="plus-icon"
        type="submit"
        class="lm-map-add"
        disabled={actions.busy || running}
        aria-label="Add map"
        title="Add map"
      />
    </form>
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
    href="https://github.com/ohne-b/OhneGuessr#learnable-meta-sync"
    label="Open the Learnable Meta sync guide on GitHub"
  />
</div>
