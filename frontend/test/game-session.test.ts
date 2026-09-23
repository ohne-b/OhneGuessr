import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { settings, state } from '@/features/game/state.svelte.js';
import { gameMode, type GameMode } from '@/features/game/game-mode.svelte.js';
import { gmap, viewer, resultMap, summaryMap } from '@/features/game/runtime.js';
import { ui } from '@/features/game/ui.svelte.js';
import {
  activateRequestedGame, completeModeRound, finishRound, nextRound,
  selectFinalRound, startModeGame, type SessionEffects
} from '@/features/game/session.js';
import { cancelRoundPreload } from '@/features/game/round-preparation.js';
import type { MapItem } from '@/features/map-library/types.js';
import type { RevealResult } from '@/rendering/map/types.js';

vi.mock('@/features/settings/store.svelte.js', () => ({ settings: {} }));
vi.mock('@/features/map-library/api.js', () => ({ loadLibrary: vi.fn(), sampleMap: vi.fn() }));
vi.mock('@/features/game/runtime.js', () => ({
  viewer: {
    setMode: vi.fn(), setStartZoomedOut: vi.fn(), showLocation: vi.fn(),
    beginRound: vi.fn(), cancelJump: vi.fn(), getTrail: vi.fn(() => [])
  },
  gmap: { guess: null, reset: vi.fn(), resize: vi.fn() },
  resultMap: { show: vi.fn(), showMany: vi.fn() },
  summaryMap: { show: vi.fn() },
  guessPanel: { setFullscreen: vi.fn(), setPinned: vi.fn() }
}));

const map = {
  id: 'world', name: 'World', file: 'world.json', folder: '',
  count: 2, source: { type: 'learnable-meta' }, managed: true
} satisfies MapItem;
const locations = [{ lat: 1, lng: 2, panoid: 'first' }, { lat: 3, lng: 4, panoid: 'second' }];
const effects: SessionEffects = {
  beforeStart: vi.fn(), reset: vi.fn(), selectMap: vi.fn(), startRound: vi.fn(),
  showResult: vi.fn(), selectFinalRound: vi.fn()
};

function activate(mode: GameMode | null = null) {
  gameMode.current = mode;
  return activateRequestedGame({
    mode, map,
    sample: mode ? null : {
      locations: locations.map((location, sourceIndex) => ({ ...location, sourceIndex })),
      locationCount: 2, mapDiagonalKm: 100
    }
  }, effects);
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => setTimeout(callback, 16));
  vi.stubGlobal('cancelAnimationFrame', clearTimeout);
  Object.assign(settings, { rounds: '2', timer: 'unlimited', scoring: 'world', movement: 'moving' });
  Object.assign(state, { phase: 'booting', results: [], round: 0, total: 0, current: null });
  Object.assign(gameMode, { current: null, busy: false, error: '' });
  gmap.guess = null;
  vi.mocked(viewer.showLocation).mockResolvedValue(true);
});

afterEach(() => {
  cancelRoundPreload();
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('game session', () => {
  it('scores guesses, advances rounds, and preserves integration data and final selection', async () => {
    await activate();
    expect(state.phase).toBe('guessing');
    expect(effects.selectMap).toHaveBeenCalledWith(map);
    expect(vi.mocked(effects.selectMap).mock.calls[0][0]).not.toBe(map);
    expect(effects.startRound).toHaveBeenCalledWith(map, locations[0]);
    gmap.guess = { lat: 1, lng: 2 };
    vi.mocked(viewer.cancelJump).mockClear();
    await finishRound();
    await finishRound(); // A repeated submission cannot append another result.
    expect(viewer.cancelJump).toHaveBeenCalledOnce();
    expect(state.results).toHaveLength(1);
    expect(state.total).toBe(5000);
    expect(resultMap.show).toHaveBeenCalledWith(state.results[0], []);
    expect(effects.showResult).toHaveBeenCalledWith(map, locations[0], 0);
    await nextRound();
    expect(state.round).toBe(1);
    expect(effects.startRound).toHaveBeenLastCalledWith(map, locations[1]);
    gmap.guess = null;
    await finishRound();
    await nextRound();
    expect(state.phase).toBe('final');
    expect(state.results[1]).toMatchObject({ guess: null, points: 0 });
    expect(summaryMap.show).toHaveBeenLastCalledWith(state.results);
    selectFinalRound(0);
    expect(effects.selectFinalRound).toHaveBeenLastCalledWith(map, locations[0], 0);
    selectFinalRound(0);
    expect(ui.selectedFinalRound).toBeNull();
    expect(effects.selectFinalRound).toHaveBeenLastCalledWith(map, null, null);
  });

  it('starts the countdown only after the panorama is ready and forfeits on timeout', async () => {
    settings.timer = '1';
    let loaded!: (ready: boolean) => void;
    vi.mocked(viewer.showLocation).mockReturnValueOnce(new Promise((resolve) => { loaded = resolve; }));
    const starting = activate();
    await vi.advanceTimersByTimeAsync(5000);
    expect(state.phase).toBe('loading');
    expect(state.results).toEqual([]);
    loaded(true);
    await starting;
    expect(ui.timerRemaining).toBe(1);
    vi.mocked(viewer.cancelJump).mockClear();
    await vi.advanceTimersByTimeAsync(1000);
    expect(viewer.cancelJump).toHaveBeenCalledOnce();
    expect(state.phase).toBe('result');
    expect(state.results[0]).toMatchObject({ guess: null, points: 0 });
  });

  it('waits in a mode lobby and coalesces hosted reveals without invoking normal result effects', async () => {
    let reveal!: (results: RevealResult[]) => void;
    const mode: GameMode = {
      id: 'party', movement: 'nm', allowsGuess: false, components: {},
      deck: () => locations, initialize: vi.fn(async () => {}),
      start: (start) => start(), rematch: (start) => start(),
      completeRound: vi.fn(() => new Promise<RevealResult[]>((resolve) => { reveal = resolve; })),
      initialFinalRound: () => null, finalResults: () => []
    };
    await activate(mode);
    expect(state.phase).toBe('empty');
    expect(viewer.showLocation).not.toHaveBeenCalled();
    await startModeGame();
    vi.mocked(viewer.cancelJump).mockClear();
    const first = completeModeRound();
    const repeated = completeModeRound();
    expect(viewer.cancelJump).toHaveBeenCalledOnce();
    expect(mode.completeRound).toHaveBeenCalledTimes(1);
    const results = [{ actual: locations[0], guess: { lat: 1, lng: 2 } }];
    reveal(results);
    await Promise.all([first, repeated]);
    expect(state.phase).toBe('result');
    expect(state.results).toHaveLength(1);
    expect(gameMode.busy).toBe(false);
    expect(resultMap.showMany).toHaveBeenCalledWith(results);
    expect(effects.showResult).not.toHaveBeenCalled();
  });
});
