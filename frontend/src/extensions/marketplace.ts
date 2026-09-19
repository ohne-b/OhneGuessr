import type {
  PluginInfo,
  PluginManifest,
  PluginSetting
} from '../../bindings/github.com/ohne-b/OhneGuessr/internal/pluginmanager/index.js';

export interface PluginCardEntry {
  id: string;
  name: string;
  description: string;
  icon: string;
  version: string;
  latestVersion: string;
  experimental: boolean;
  installed: boolean;
  enabled: boolean;
  updatable: boolean;
  available: boolean;
  settings: PluginSetting[];
  configured: string[];
}

export function mergePluginEntries(
  catalog: readonly PluginManifest[],
  installed: readonly PluginInfo[]
): PluginCardEntry[] {
  const catalogByID = new Map(catalog.map((plugin) => [plugin.id, plugin]));
  const installedByID = new Map(installed.map((plugin) => [plugin.id, plugin]));
  const ids = new Set([...catalogByID.keys(), ...installedByID.keys()]);
  return [...ids].map((id) => {
    const latest = catalogByID.get(id);
    const current = installedByID.get(id);
    const display = latest || current!;
    return {
      id,
      name: display.name,
      description: display.description,
      icon: display.icon,
      version: current?.version || latest?.version || '',
      latestVersion: latest?.version || '',
      experimental: Boolean(latest ? latest.experimental : current?.experimental),
      installed: Boolean(current),
      enabled: Boolean(current?.enabled),
      updatable: Boolean(current && latest && current.version !== latest.version),
      available: Boolean(latest),
      settings: current?.settings || latest?.settings || [],
      configured: current?.configured || []
    };
  }).sort((left, right) => left.name.localeCompare(right.name));
}
