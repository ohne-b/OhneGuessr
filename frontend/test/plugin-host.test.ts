import { afterEach, describe, expect, it, vi } from 'vitest';
import type { PluginManifest } from '../bindings/github.com/ohne-b/OhneGuessr/internal/pluginmanager/index.js';
import {
  createPluginHost,
  pluginHudButtons,
  type PanoramaPluginHost
} from '@/extensions/host.svelte.js';
import { createPluginWindow, type PluginWindowHandle } from '@/components/plugin-window.js';

vi.mock('@/components/plugin-window.js', () => ({ createPluginWindow: vi.fn() }));

const hosts: ReturnType<typeof createPluginHost>[] = [];

afterEach(() => {
  for (const host of hosts.splice(0)) host.dispose();
  vi.clearAllMocks();
});

const manifest = {
  id: 'runtime-test', name: 'Runtime Test', description: '', icon: 'M1 1',
  version: '1.0.0', apiVersion: 1, main: 'index.js'
} as PluginManifest;

function panoramaHost(): PanoramaPluginHost {
  return {
    getMetadata: () => null,
    getDetails: vi.fn(async () => ({
      panoId: 'test-pano', imageDate: '2024-05', elevation: 123.4,
      cameraType: 'gen4' as const, panoType: 'official' as const,
      uploader: null, drivingDirection: 90, coverageDates: ['2024-05'], copyright: '© Google'
    })),
    captureViewport: vi.fn(),
    onRoundStart: vi.fn(() => vi.fn())
  };
}

describe('additional plugin host', () => {
  it('owns shared HUD buttons and calls plugins directly', async () => {
    const host = createPluginHost(manifest, panoramaHost());
    hosts.push(host);
    const onClick = vi.fn();
    const button = host.api.hud.addButton({ label: 'Test action', pressed: true, onClick });

    expect(pluginHudButtons[0]).toMatchObject({
      id: 'runtime-test:1', icon: 'M1 1', label: 'Test action', pressed: true
    });
    pluginHudButtons[0].onClick();
    await vi.waitFor(() => expect(onClick).toHaveBeenCalledOnce());
    await expect(host.api.panorama.getDetails()).resolves.toEqual({
      panoId: 'test-pano', imageDate: '2024-05', elevation: 123.4,
      cameraType: 'gen4', panoType: 'official',
      uploader: null, drivingDirection: 90, coverageDates: ['2024-05'], copyright: '© Google'
    });
    button.setPressed(false);
    expect(pluginHudButtons[0].pressed).toBe(false);

    host.dispose();
    expect(pluginHudButtons).toHaveLength(0);
  });

  it('creates one host-styled window and removes it on disposal', () => {
    const remove = vi.fn();
    vi.mocked(createPluginWindow).mockReturnValue({
      content: {} as HTMLDivElement,
      headerActions: {} as HTMLDivElement,
      configure: vi.fn(), show: vi.fn(), hide: vi.fn(), resetLayout: vi.fn(), remove
    } as PluginWindowHandle);
    const host = createPluginHost(manifest, panoramaHost());
    hosts.push(host);

    host.api.ui.createWindow();
    expect(createPluginWindow).toHaveBeenCalledWith({
      title: 'Runtime Test', ariaLabel: undefined, closeLabel: undefined, onClose: undefined,
      layoutKey: 'ohneguessr.plugin.runtime-test.window.layout'
    });
    expect(() => host.api.ui.createWindow()).toThrow('one shared window');

    host.dispose();
    expect(remove).toHaveBeenCalledOnce();
  });
});
