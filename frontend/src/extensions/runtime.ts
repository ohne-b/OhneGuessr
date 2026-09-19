import {
  PluginService,
  type PluginModule
} from '../../bindings/github.com/ohne-b/OhneGuessr/internal/pluginmanager/index.js';
import { desktopRuntimeAvailable } from '../platform/desktop.js';
import {
  createPluginHost,
  type ExternalPlugin,
  type PanoramaPluginHost
} from './host.svelte.js';

interface ActivePlugin {
  deactivate(): void;
}

let modules: PluginModule[] = [];
const active = new Map<string, ActivePlugin>();

export async function loadExternalPlugins() {
  if (!desktopRuntimeAvailable()) return;
  try {
    modules = await PluginService.EnabledModules() || [];
  } catch (error) {
    modules = [];
    console.error('[plugin] failed to read installed plugins:', error);
  }
}

export async function activateExternalPlugins(host: PanoramaPluginHost) {
  for (const module of [...modules].sort((a, b) =>
    a.manifest.name.localeCompare(b.manifest.name))) {
    if (active.has(module.manifest.id)) continue;
    const pluginHost = createPluginHost(module.manifest, host);
    let moduleURL = '';
    try {
      moduleURL = URL.createObjectURL(new Blob([
        module.source,
        `\n//# sourceURL=ohneguessr-plugin/${module.manifest.id}/index.js`
      ], { type: 'application/javascript' }));
      const loaded = await import(/* @vite-ignore */ moduleURL) as { default?: ExternalPlugin };
      if (!loaded.default || typeof loaded.default.activate !== 'function') {
        throw new Error('plugin module must default-export an activate function');
      }
      const cleanup = await loaded.default.activate(pluginHost.api);
      if (cleanup !== undefined && typeof cleanup !== 'function') {
        throw new Error('plugin activate must return a cleanup function or nothing');
      }
      active.set(module.manifest.id, {
        deactivate() {
          try { cleanup?.(); }
          catch (error) { console.error(`[plugin] cleanup failed for "${module.manifest.id}":`, error); }
          pluginHost.dispose();
        }
      });
    } catch (error) {
      pluginHost.dispose();
      console.error(`[plugin] failed to load "${module.manifest.id}":`, error);
    } finally {
      if (moduleURL) URL.revokeObjectURL(moduleURL);
    }
  }
}

export function deactivateExternalPlugins() {
  for (const bridge of [...active.values()].reverse()) bridge.deactivate();
  active.clear();
}
