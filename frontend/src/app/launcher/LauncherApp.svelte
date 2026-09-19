<script lang="ts">
  import LauncherTitlebar from './LauncherTitlebar.svelte';
  import { onMount } from 'svelte';
  import { getGameWindowState, onGameWindowState, type GameWindowState } from '../../platform/desktop.js';
  import { fileTypes, openFiles } from './file-open.js';
  import { mapActions } from './map-actions.js';
  import MapLibrary from '../../features/map-library/MapLibrary.svelte';
  import { initLibrary, setActiveMap, showLibraryNotice } from '../../features/map-library/library.svelte.js';
  import { onLauncherPageRequested } from './events.js';
  import MapSyncLayout from './MapSyncLayout.svelte';
  import PluginsPage from './PluginsPage.svelte';
  import GameSettings from '../../features/settings/GameSettings.svelte';
  import DisplaySettings from '../../features/settings/DisplaySettings.svelte';
  import ControlsSettings from '../../features/settings/ControlsSettings.svelte';
  import { initSettingsSync, settings } from '../../features/settings/store.svelte.js';
  import UpdatePanel from '../../features/updates/UpdatePanel.svelte';

  type Page = 'maps' | 'plugins' | 'game' | 'display' | 'controls';

  const pages: { id: Page; label: string }[] = [
    { id: 'maps', label: 'Maps' },
    { id: 'game', label: 'Game' },
    { id: 'display', label: 'Display' },
    { id: 'controls', label: 'Controls' },
    { id: 'plugins', label: 'Plugins' }
  ];
  const roundPresets = ['unlimited', '5', '10'];
  const timerPresets = ['unlimited', '120', 'countup'];
  let page = $state<Page>('maps');
  let gameWindow = $state<GameWindowState>({ open: false, fullscreen: false });
  let pluginMessage = $state('');
  let roundsDraft = $state(roundPresets.includes(settings.rounds) ? '7' : settings.rounds);
  let timerDraft = $state(timerPresets.includes(settings.timer) ? '3' : String(Number(settings.timer) / 60));

  function receiveGameState(next: GameWindowState) {
    gameWindow = next;
    setActiveMap(next.mapId || '');
  }

  onMount(() => {
    const stopSettings = initSettingsSync();
    const stopGameState = onGameWindowState(receiveGameState);
    const stopLauncherRequests = onLauncherPageRequested((request) => {
      if (request.page === 'plugins') pluginMessage = request.message || '';
      else if (request.message) showLibraryNotice(request.message, true);
      page = request.page;
    });
    void getGameWindowState().then(receiveGameState);
    void initLibrary();
    return () => {
      stopSettings();
      stopGameState();
      stopLauncherRequests();
    };
  });
</script>

<svelte:body class:launcher-body={true} />

<div class="launcher-shell launcher-app" data-theme={settings.theme}>
  <LauncherTitlebar />

  <aside class="launcher-sidebar">
    <div class="launcher-brand">
      <img src="/images/ohneguessr-logo.svg" alt="" />
      <span>OhneGuessr</span>
    </div>
    <nav aria-label="Launcher sections">
      {#each pages as item}
        <button
          type="button"
          class:active={page === item.id}
          aria-current={page === item.id ? 'page' : undefined}
          aria-label={item.label}
          title={item.label}
          onclick={() => {
            page = item.id;
          }}
        >
          <img class="nav-icon" src={`/icons/${item.id === 'plugins' ? 'plugin' : item.id}.svg`} alt="" />
          <span class="nav-label">{item.label}</span>
        </button>
      {/each}
    </nav>
    <div class="launcher-sidebar-footer">
      <a
        class="launcher-repo-link"
        href="https://github.com/ohne-b/OhneGuessr"
        target="_blank"
        rel="noopener noreferrer"
        aria-label="Open OhneGuessr on GitHub"
        title="GitHub"
      >
        <img class="nav-icon" src="/icons/github.svg" alt="" />
      </a>
      <UpdatePanel />
    </div>
  </aside>

  <main class="launcher-main">
    {#if page === 'maps'}
      <MapSyncLayout>
        <MapLibrary actions={mapActions} {fileTypes} onFiles={openFiles} />
      </MapSyncLayout>
    {:else if page === 'plugins'}
      <PluginsPage message={pluginMessage} />
    {:else if page === 'game'}
      <GameSettings bind:roundsDraft bind:timerDraft />
    {:else if page === 'display'}
      <DisplaySettings bind:gameWindow />
    {:else}
      <ControlsSettings />
    {/if}
  </main>
</div>
