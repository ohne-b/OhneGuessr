<script lang="ts">
  import PluginSummary from '../components/PluginSummary.svelte';
  import ToggleSwitch from '../components/ToggleSwitch.svelte';
  import { onMount, type Snippet } from 'svelte';
  import {
    PluginService,
    type PluginInfo,
    type PluginManifest
  } from '../../bindings/github.com/ohne-b/OhneGuessr/internal/pluginmanager/index.js';
  import { desktopRuntimeAvailable } from '../platform/desktop.js';
  import { mergePluginEntries } from './marketplace.js';

  let { error = $bindable(''), children }: {
    error?: string;
    children: Snippet<[Snippet]>;
  } = $props();
  let catalogError = $state('');
  let loading = $state(false);
  let busy = $state('');
  let catalog = $state<PluginManifest[]>([]);
  let installed = $state<PluginInfo[]>([]);
  let settingValues = $state<Record<string, string>>({});
  const additional = $derived(mergePluginEntries(catalog, installed));

  const errorText = (value: unknown) => value instanceof Error ? value.message : String(value);

  async function refreshInstalled() {
    installed = await PluginService.Installed() || [];
  }

  async function refresh() {
    if (!desktopRuntimeAvailable()) return;
    loading = true;
    error = '';
    catalogError = '';
    const [catalogResult, installedResult] = await Promise.allSettled([
      PluginService.Catalog(),
      PluginService.Installed()
    ]);
    if (catalogResult.status === 'fulfilled') catalog = catalogResult.value || [];
    else catalogError = errorText(catalogResult.reason);
    if (installedResult.status === 'fulfilled') installed = installedResult.value || [];
    else error = errorText(installedResult.reason);
    loading = false;
  }

  async function run(operation: string, action: () => Promise<unknown>) {
    if (busy) return false;
    busy = operation;
    error = '';
    try {
      await action();
      await refreshInstalled();
      return true;
    } catch (next) {
      error = errorText(next);
      return false;
    } finally {
      busy = '';
    }
  }

  async function saveSetting(pluginID: string, settingKey: string) {
    const field = `${pluginID}:${settingKey}`;
    const value = settingValues[field]?.trim();
    if (!value) return;
    if (await run(`${field}:save`, () => PluginService.SetSetting(pluginID, settingKey, value))) {
      settingValues[field] = '';
    }
  }

  onMount(refresh);
</script>

{#snippet content()}
  {#if !desktopRuntimeAvailable()}
    <div role="tabpanel"><p class="plugin-empty">Additional plugins require the desktop app.</p></div>
  {:else}
    <div class="plugin-list" role="tabpanel" aria-busy={loading}>
      {#if loading && !additional.length}
        {#each Array(4) as _}
          <div class="plugin-row plugin-skeleton" aria-hidden="true"></div>
        {/each}
      {:else}
        {#each additional as plugin (plugin.id)}
          <article class="plugin-row external-plugin" class:enabled={plugin.enabled}>
            <PluginSummary icon={plugin.icon} name={plugin.name} description={plugin.description} svg>
              <span class="plugin-title"
                ><b>{plugin.name}</b>
                {#if plugin.experimental}<span class="plugin-badge">Experimental</span>{/if}
              </span>
            </PluginSummary>
            {#if plugin.installed}
              <ToggleSwitch
                class="plugin-switch"
                dimmed={false}
                label={`${plugin.enabled ? 'Disable' : 'Enable'} ${plugin.name}`}
                checked={plugin.enabled}
                disabled={Boolean(busy)}
                onchange={(event) =>
                  run(`${plugin.id}:toggle`, () =>
                    PluginService.SetEnabled(plugin.id, event.currentTarget.checked)
                  )}
              >
                {#snippet after()}{#if busy === `${plugin.id}:toggle`}<small
                      >{plugin.enabled ? 'Disabling…' : 'Enabling…'}</small
                    >{/if}{/snippet}
              </ToggleSwitch>
            {/if}
            <span class="plugin-actions">
              <small class="plugin-version"
                >v{plugin.version}{plugin.updatable ? ` → v${plugin.latestVersion}` : ''}</small
              >
              {#if plugin.installed}
                {#if plugin.updatable}
                  <button
                    type="button"
                    class="plugin-primary"
                    disabled={Boolean(busy)}
                    onclick={() => run(`${plugin.id}:update`, () => PluginService.Install(plugin.id))}
                  >
                    {busy === `${plugin.id}:update` ? 'Updating…' : 'Update'}
                  </button>
                {/if}
                <button
                  type="button"
                  class="plugin-remove"
                  disabled={Boolean(busy)}
                  onclick={() => run(`${plugin.id}:remove`, () => PluginService.Uninstall(plugin.id))}
                >
                  {busy === `${plugin.id}:remove` ? 'Removing…' : 'Remove'}
                </button>
              {:else}
                <button
                  type="button"
                  class="plugin-primary"
                  disabled={Boolean(busy) || !plugin.available}
                  onclick={() => run(`${plugin.id}:install`, () => PluginService.Install(plugin.id))}
                >
                  {busy === `${plugin.id}:install` ? 'Installing…' : 'Install'}
                </button>
              {/if}
            </span>
            {#if plugin.installed && plugin.settings.length}
              <div class="plugin-settings">
                {#each plugin.settings as setting (setting.key)}
                  {@const field = `${plugin.id}:${setting.key}`}
                  {@const configured = plugin.configured.includes(setting.key)}
                  <div class="plugin-setting">
                    <label for={field}>{setting.label}</label>
                    <div class="plugin-setting-controls">
                      <input
                        id={field}
                        type="password"
                        autocomplete="off"
                        spellcheck="false"
                        placeholder={configured ? 'API key saved' : 'Enter API key'}
                        value={settingValues[field] || ''}
                        oninput={(event) => {
                          settingValues[field] = event.currentTarget.value;
                        }}
                      />
                      <button
                        type="button"
                        disabled={Boolean(busy) || !settingValues[field]?.trim()}
                        onclick={() => saveSetting(plugin.id, setting.key)}
                      >
                        {busy === `${field}:save` ? 'Saving…' : 'Save'}
                      </button>
                      {#if configured}
                        <button
                          type="button"
                          class="plugin-remove"
                          disabled={Boolean(busy)}
                          onclick={() =>
                            run(`${field}:forget`, () =>
                              PluginService.SetSetting(plugin.id, setting.key, '')
                            )}
                        >
                          {busy === `${field}:forget` ? 'Forgetting…' : 'Forget'}
                        </button>
                      {/if}
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </article>
        {/each}
      {/if}
    </div>
    {#if catalogError}
      <p class="settings-note plugin-error" role="alert">
        Could not load the plugin catalog. <button type="button" onclick={refresh}>Retry</button>
      </p>
    {/if}
  {/if}
{/snippet}

<!-- Keep catalog state and refresh timing alive while the page displays its Core tab. -->
{@render children(content)}
