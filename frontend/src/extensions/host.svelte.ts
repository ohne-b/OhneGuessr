import {
  PluginService,
  type PluginManifest
} from '../../bindings/github.com/ohne-b/OhneGuessr/internal/pluginmanager/index.js';
import { Browser } from '@wailsio/runtime';
import type { PanoramaCapture, PanoramaCaptureOptions } from '../rendering/panorama/panorama-capture.js';
import type { PanoramaMetadata } from '../rendering/panorama/panorama.js';
import type { PanoramaDetails } from '../rendering/panorama/panorama-details.js';
import { reverseLocation } from './location.js';
import { createPluginWindow, type PluginWindowHandle } from '../components/plugin-window.js';

export interface PanoramaPluginHost {
  getMetadata(): PanoramaMetadata | null;
  getDetails(): Promise<PanoramaDetails | null>;
  captureViewport(options?: PanoramaCaptureOptions): Promise<PanoramaCapture>;
  onRoundStart(listener: () => void): () => void;
}

export interface PluginHudButton {
  id: string;
  icon: string;
  label: string;
  pressed: boolean;
  onClick(): void;
}

export type ExternalPluginAPI = OhneGuessrPluginAPI;
export type ExternalPlugin = OhneGuessrPlugin;

export const pluginHudButtons = $state<PluginHudButton[]>([]);

function reportCallbackError(pluginID: string, error: unknown) {
  console.error(`[plugin] callback failed for "${pluginID}":`, error);
}

export function createPluginHost(manifest: PluginManifest, host: PanoramaPluginHost) {
  const disposables: (() => void)[] = [];
  let buttonID = 0;
  let pluginWindow: PluginWindowHandle | null = null;
  let disposed = false;

  const invoke = (callback: () => void | Promise<void>) => {
    try {
      void Promise.resolve(callback()).catch((error) => reportCallbackError(manifest.id, error));
    } catch (error) {
      reportCallbackError(manifest.id, error);
    }
  };

  const api: ExternalPluginAPI = {
    panorama: {
      getMetadata: () => host.getMetadata(),
      getDetails: () => host.getDetails(),
      captureViewport: (options) => host.captureViewport(options),
      onRoundStart(listener) {
        const remove = host.onRoundStart(() => invoke(listener));
        disposables.push(remove);
        return remove;
      }
    },
    location: { reverse: reverseLocation },
    hud: {
      addButton(options) {
        const id = `${manifest.id}:${++buttonID}`;
        let removed = false;
        const button = $state<PluginHudButton>({
          id,
          icon: options.icon || manifest.icon,
          label: options.label,
          pressed: Boolean(options.pressed),
          onClick: () => invoke(options.onClick)
        });
        pluginHudButtons.push(button);
        const handle: ReturnType<OhneGuessrPluginAPI['hud']['addButton']> = {
          setPressed(pressed) {
            if (!removed) button.pressed = Boolean(pressed);
          },
          remove() {
            if (removed) return;
            removed = true;
            const index = pluginHudButtons.findIndex((item) => item.id === id);
            if (index >= 0) pluginHudButtons.splice(index, 1);
          }
        };
        disposables.push(handle.remove);
        return handle;
      }
    },
    ui: {
      createWindow(options = {}) {
        if (pluginWindow) throw new Error('additional plugins may create one shared window');
        const onClose = options.onClose;
        pluginWindow = createPluginWindow({
          title: options.title || manifest.name,
          ariaLabel: options.ariaLabel,
          closeLabel: options.closeLabel,
          onClose: onClose ? () => invoke(onClose) : undefined,
          layoutKey: `ohneguessr.plugin.${manifest.id}.window.layout`
        });
        disposables.push(pluginWindow.remove);
        return pluginWindow;
      },
      openExternal: Browser.OpenURL
    },
    settings: {
      get: (key) => PluginService.Setting(manifest.id, key)
    }
  };

  return {
    api,
    dispose() {
      if (disposed) return;
      disposed = true;
      for (let index = disposables.length - 1; index >= 0; index--) {
        try { disposables[index](); }
        catch (error) { reportCallbackError(manifest.id, error); }
      }
      disposables.length = 0;
      pluginWindow = null;
    }
  };
}
