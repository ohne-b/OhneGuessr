import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { OpenSvViewer } from '@/rendering/panorama/panorama.js';

vi.mock('@/rendering/panorama/car-mask.js', () => ({ setCarHidden: vi.fn(), installCarMask: vi.fn() }));

class Host extends EventTarget {
  style = {};
  appendChild() {}
}

const spawn = { lat: 1, lng: 2, panoid: 'start', heading: 70, pitch: 8, zoom: 2 };
const positions = {
  start: { lat: 1, lng: 2 },
  target: { lat: 1.001, lng: 2 },
  checkpoint: { lat: 1.002, lng: 2 }
};
const latLng = (point: { lat: number; lng: number }) => ({ lat: () => point.lat, lng: () => point.lng });
const response = (id: keyof typeof positions) => ({ data: { location: { pano: id, latLng: latLng(positions[id]) } } });
const getPanorama = vi.fn();
const computeOffset = vi.fn(() => latLng(positions.target));
let resolveLookup: (data: ReturnType<typeof response>) => void;
let viewer: OpenSvViewer;
let pano: ReturnType<typeof mockPanorama>;

function mockPanorama() {
  const listeners = new Map<string, Set<() => void>>();
  const emit = (event: string) => { for (const listener of listeners.get(event) ?? []) listener(); };
  let id: keyof typeof positions = 'start';
  let position = positions.start;
  let pov = { heading: 70, pitch: 8 };
  let zoom = 2;
  let status = 'OK';
  return {
    addListener: (event: string, listener: () => void) => {
      if (!listeners.has(event)) listeners.set(event, new Set());
      listeners.get(event)!.add(listener);
      return { remove: () => listeners.get(event)!.delete(listener) };
    },
    getPano: () => id,
    getPosition: () => latLng(position),
    getPov: () => pov,
    getZoom: () => zoom,
    getStatus: () => status,
    setPano: vi.fn((next: keyof typeof positions) => {
      id = next;
      status = 'LOADING';
      emit('pano_changed');
      position = positions[next];
      pov = { heading: 0, pitch: 0 };
      zoom = 1;
      status = 'OK';
      emit('position_changed');
      emit('status_changed');
    }),
    setPov: (next: typeof pov) => { pov = next; },
    setZoom: (next: number) => { zoom = next; },
    setOptions: vi.fn(), setVisible: vi.fn(), focus: vi.fn()
  };
}

beforeEach(async () => {
  vi.useFakeTimers();
  getPanorama.mockReset();
  computeOffset.mockClear();
  pano = mockPanorama();
  vi.stubGlobal('document', { createElement: () => new Host() });
  vi.stubGlobal('window', { google: { maps: {
    StreetViewPanorama: function () { return pano; },
    StreetViewService: function () { return { getPanorama }; },
    StreetViewPreference: { NEAREST: 'nearest' },
    StreetViewSource: { GOOGLE: 'google' },
    geometry: { spherical: { computeOffset } }
  } } });
  viewer = new OpenSvViewer(new Host() as unknown as HTMLElement);
  getPanorama.mockResolvedValueOnce(response('start'));
  await viewer.showLocation({ ...spawn });
  viewer.beginRound(spawn);
  getPanorama.mockClear().mockImplementation(() => new Promise((resolve) => { resolveLookup = resolve; }));
  pano.setPano.mockClear();
});

afterEach(() => {
  viewer.cancelJump();
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('distance jumps', () => {
  it.each([1, -1] as const)('jumps in direction %s once, preserving the latest view, spawn, checkpoint and trail', async (direction) => {
    pano.setPano('checkpoint');
    viewer.toggleCheckpoint();
    viewer.resetView();
    const jumping = viewer.jump(direction);
    expect(await viewer.jump(direction)).toBe(false);
    expect(getPanorama).toHaveBeenCalledOnce();
    expect(computeOffset).toHaveBeenCalledWith(positions.start, 100, direction === 1 ? 70 : 250);
    expect(getPanorama).toHaveBeenCalledWith({
      location: expect.anything(), radius: 100, preference: 'nearest', sources: ['google']
    });
    pano.setPov({ heading: 90, pitch: 12 });
    pano.setZoom(3);
    resolveLookup(response('target'));
    expect(await jumping).toBe(true);
    expect(pano.getPano()).toBe('target');
    expect(pano.getPov()).toEqual({ heading: 90, pitch: 12 });
    expect(pano.getZoom()).toBe(3);
    expect(viewer.getTrail().flat().at(-1)).toEqual(positions.target);
    viewer.toggleCheckpoint();
    await Promise.resolve();
    expect(pano.getPano()).toBe('checkpoint');
    viewer.resetView();
    expect(pano.getPano()).toBe('start');
  });

  it.each(['nm', 'nmpz', 'peek', 'lookBehind', 'loading'] as const)('does not jump during %s', async (state) => {
    if (state === 'nm' || state === 'nmpz') viewer.setMode(state);
    if (state === 'peek') { viewer.toggleCheckpoint(); viewer.startCheckpointPeek(); }
    if (state === 'lookBehind') viewer.startLookBehind();
    if (state === 'loading') void viewer.showLocation({ ...spawn });
    getPanorama.mockClear();
    expect(await viewer.jump(1)).toBe(false);
    expect(getPanorama).not.toHaveBeenCalled();
  });

  it.each(['reset', 'round', 'mode', 'walk', 'checkpoint', 'lookBehind', 'cancel', 'timeout'] as const)(
    'discards a lookup after %s, allowing another jump without a backlog', async (action) => {
      const jumping = viewer.jump(1);
      const oldLookup = resolveLookup;
      if (action === 'reset') viewer.resetView();
      if (action === 'round') viewer.beginRound(spawn);
      if (action === 'mode') { viewer.setMode('nm'); viewer.setMode('moving'); }
      if (action === 'walk') { pano.setPano('checkpoint'); pano.setPano('start'); }
      if (action === 'checkpoint') viewer.toggleCheckpoint();
      if (action === 'lookBehind') { viewer.startLookBehind(); viewer.endLookBehind(); }
      if (action === 'cancel') viewer.cancelJump();
      if (action === 'timeout') await vi.advanceTimersByTimeAsync(12000);
      expect(await jumping).toBe(false);
      const next = viewer.jump(-1);
      oldLookup(response('target'));
      await Promise.resolve();
      expect(pano.getPano()).toBe('start');
      resolveLookup(response('target'));
      expect(await next).toBe(true);
    }
  );

  it('leaves the view alone on missing coverage or the same panorama, and recovers', async () => {
    getPanorama.mockRejectedValueOnce(new Error('ZERO_RESULTS'));
    expect(await viewer.jump(1)).toBe(false);
    getPanorama.mockResolvedValueOnce(response('start'));
    expect(await viewer.jump(-1)).toBe(false);
    expect(pano.setPano).not.toHaveBeenCalled();
    const jumping = viewer.jump(1);
    resolveLookup(response('target'));
    expect(await jumping).toBe(true);
  });
});
