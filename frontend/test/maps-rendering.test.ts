import { afterEach, describe, expect, it, vi } from 'vitest';
import Point from '@mapbox/point-geometry';
import type { DragPanOptions, MapOptions } from 'maplibre-gl';
import { createRevealMaps, GuessMap } from '@/rendering/map/map.js';

const { setResults, fitBounds, createMap, easeTo } = vi.hoisted(() => ({
  setResults: vi.fn(), fitBounds: vi.fn(), createMap: vi.fn<(options: MapOptions) => void>(),
  easeTo: vi.fn()
}));
vi.mock('@/rendering/map/result-layers.js', () => ({ ResultLayers: class { setResults = setResults; } }));
vi.mock('maplibre-gl', async (importOriginal) => ({
  ...await importOriginal<typeof import('maplibre-gl')>(),
  Map: class {
    constructor(private options: MapOptions) { createMap(options); }
    scrollZoom = { setWheelZoomRate: vi.fn(), setZoomRate: vi.fn() };
    touchZoomRotate = { disableRotation: vi.fn() };
    on = vi.fn();
    resize = vi.fn();
    stop = vi.fn();
    easeTo = easeTo;
    fitBounds = fitBounds;
    getContainer() { return this.options.container; }
    getMaxZoom() { return 20; }
  }
}));
afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks(); });

it('shares one lazy reveal map across rounds and summaries, ignoring empty summaries', () => {
  const element = () => ({
    classList: { add: vi.fn() }, clientWidth: 400, clientHeight: 300,
    parentElement: null as unknown,
    appendChild(child: { parentElement: unknown }) { child.parentElement = this; }
  });
  const slots = { result: element(), final: element() };
  const elements: ReturnType<typeof element>[] = [];
  vi.stubGlobal('document', {
    documentElement: {}, querySelector: () => null,
    getElementById: (id: keyof typeof slots) => slots[id] || null,
    createElement: () => { const node = element(); elements.push(node); return node; }
  });
  vi.stubGlobal('window', { devicePixelRatio: 1 });
  vi.stubGlobal('getComputedStyle', () => ({ getPropertyValue: () => '#22c55e' }));
  vi.stubGlobal('requestAnimationFrame', (callback: () => void) => { callback(); return 1; });
  const { resultMap, summaryMap } = createRevealMaps('result', 'final');
  const result = { actual: { lat: 1, lng: 2 }, guess: null };

  summaryMap.show([]);
  expect(createMap).not.toHaveBeenCalled();
  resultMap.show(result);
  expect(elements[0].parentElement).toBe(slots.result);
  expect(setResults).toHaveBeenLastCalledWith([result], null);
  expect(fitBounds).toHaveBeenLastCalledWith(expect.anything(), expect.objectContaining({ maxZoom: 4 }));
  summaryMap.show([result]);
  expect(elements[0].parentElement).toBe(slots.final);
  resultMap.showMany([result, result]);
  expect(setResults).toHaveBeenLastCalledWith([result, result], null);
  summaryMap.show([]);
  expect(elements[0].parentElement).toBe(slots.result);
  expect(createMap).toHaveBeenCalledOnce();
});

it('matches the measured GeoGuessr pan timing and curve without retiming other animations', () => {
  const element = { classList: { add: vi.fn() }, appendChild: vi.fn() };
  vi.stubGlobal('document', {
    documentElement: {}, getElementById: () => element, createElement: () => element
  });
  vi.stubGlobal('window', { devicePixelRatio: 1 });
  vi.stubGlobal('getComputedStyle', () => ({ getPropertyValue: () => '#22c55e' }));
  const { map } = new GuessMap('map', vi.fn());
  const { easing, linearity, maxSpeed, deceleration } = createMap.mock.calls[0][0].dragPan as Required<DragPanOptions>;

  // Reference flings measured in GeoGuessr, confirmed against its Maps 3.64 math.
  for (const [speed, expectedDuration, expectedDistance] of [
    [250, 156.25, 19.53125],
    [500, 220.97087, 55.24272],
    [1000, 312.5, 156.25],
    [2000, 441.94174, 441.94174],
    [4000, 625, 1250],
    [8000, 883.88348, 3535.53391]
  ]) {
    const scaledSpeed = Math.min(speed * linearity, maxSpeed);
    const duration = scaledSpeed / (deceleration * linearity) * 1000;
    const distance = scaledSpeed * duration / 2000;
    const eventData = { originalEvent: { type: 'mouseup' } };
    const options = { duration, offset: new Point(-distance, 0), easing, noMoveStart: true };
    map.easeTo(options, eventData);
    const [adjusted, data] = easeTo.mock.lastCall!;
    expect(adjusted.duration).toBeCloseTo(expectedDuration, 4);
    expect(adjusted.offset[0]).toBeCloseTo(-expectedDistance, 4);
    expect(adjusted.offset[1]).toBe(0);
    expect(adjusted.noMoveStart).toBe(true);
    expect(data).toBe(eventData);
    expect(options.duration).toBe(duration);
  }
  map.easeTo({ duration: 1000, offset: [300, 400], easing });
  expect(easeTo.mock.lastCall![0].offset).toEqual([93.75, 125]);

  expect(easing(0)).toBe(0);
  expect(easing(0.5)).toBe(0.875);
  expect(easing(1)).toBe(1);
  for (const options of [
    { duration: 500, offset: [100, 0] as [number, number], easing: (t: number) => t },
    { duration: 0, offset: [0, 0] as [number, number], easing }
  ]) {
    map.easeTo(options);
    expect(easeTo.mock.lastCall![0]).toBe(options);
  }
});

describe('GuessMap', () => {
  it('places a guess at the visible map center', () => {
    const guessMap = Object.assign(Object.create(GuessMap.prototype), {
      guess: null,
      map: { getCenter: () => ({ lat: 12.5, lng: -45.25 }) }
    }) as GuessMap;
    const syncGuess = vi.spyOn(guessMap, 'syncGuess').mockImplementation(() => {});

    expect(guessMap.placeGuessAtCenter()).toEqual({ lat: 12.5, lng: -45.25 });
    expect(guessMap.guess).toEqual({ lat: 12.5, lng: -45.25 });
    expect(syncGuess).toHaveBeenCalledOnce();
  });
});
