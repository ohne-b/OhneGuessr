// Owns round progression and scoring; app setup supplies the feature effects.
import { CONFIG } from './config.js';
import { setLoading, ui } from './ui.svelte.js';
import { GAME_PHASE, state, settings } from './state.svelte.js';
import { haversineKm, scoreFor } from './scoring.js';
import { RoundTimer } from './timer.js';
import { gameMode } from './game-mode.svelte.js';
import { viewer, gmap, resultMap, summaryMap, guessPanel } from './runtime.js';
import { loadLibrary, sampleMap } from '../map-library/api.js';
import {
  createSampledDeck, ensureDeckIndex, hasNextRound, hasSampledLocations,
  PANORAMA_RETRIES, resizeSampledDeck, selectSampledMap, UNLIMITED_BATCH_ROUNDS, useFixedDeck
} from './deck.js';
import {
  cancelRoundPreload, preparationMatches, prepareRound, scheduleNextRoundPreload,
  takeRoundPreload, type RoundPreparation
} from './round-preparation.js';
import type { GamePhase, RoundResult } from './types.js';
import type { Location, Point, Trail } from '../../shared/geo.js';
import type { MapItem } from '../map-library/types.js';
import type { Settings } from '../settings/types.js';

export interface SessionEffects {
  beforeStart(): void;
  reset(): void;
  selectMap(map: MapItem | null): void;
  startRound(map: MapItem | null, location: Location): void;
  showResult(map: MapItem | null, location: Location, round: number): void;
  selectFinalRound(map: MapItem | null, location: Location | null, round: number | null): void;
}

let effects: SessionEffects;

// World: fixed scale. Country: the loaded map's bbox diagonal.
const effectiveScaleKm = () =>
  gameMode.current?.scoreScaleKm?.() ?? (
    settings.scoring === 'country' && state.mapDiagonalKm > 0
    ? state.mapDiagonalKm
    : CONFIG.WORLD_SCALE_KM
  );
// 'unlimited' -> Infinity (the game never ends on its own).
const roundsPerGame = () =>
  settings.rounds === 'unlimited' ? Infinity : (parseInt(settings.rounds, 10) || CONFIG.ROUNDS);
const movementForGame = () => gameMode.current?.movement ?? settings.movement;
const activeTimerSeconds = () => gameMode.current?.timerSeconds?.() ?? (
  settings.timer === 'unlimited' ? 0 : (parseInt(settings.timer, 10) || 0)
);
const timerCountsUp = () => !gameMode.current?.timerSeconds && settings.timer === 'countup';
const ACTIVE_GAME_PHASES = new Set<GamePhase>([
  GAME_PHASE.LOADING,
  GAME_PHASE.GUESSING,
  GAME_PHASE.RESULT
]);

let modeRoundPending = false;

const currentMapItem = (): MapItem | null => {
  const map = state.map;
  return map ? { ...map, source: map.source ? { ...map.source } : null } : null;
};

// Timer policy for the current round; RoundTimer handles the ticking.
const roundTimer = new RoundTimer({
  getSeconds: activeTimerSeconds,
  isCountUp: timerCountsUp,
  isActive: () => state.phase === GAME_PHASE.GUESSING,
  onExpire: () => { void finishRound(); }, // forfeit or hosted reveal
  onTick: ({ visible, remaining, low }) => {
    ui.timerVisible = visible;
    ui.timerRemaining = remaining;
    ui.timerLow = low;
  }
});

function updateResultActions() {
  ui.nextLabel = hasNextRound() ? 'Next' : 'See results';
  ui.endGameVisible = state.unlimited;
}

export async function startGame() {
  viewer.cancelJump();
  effects.beforeStart();
  gameMode.current?.reset?.();
  cancelRoundPreload();
  roundTimer.stop();
  effects.reset();
  state.phase = GAME_PHASE.LOADING;
  ui.resultVisible = false;
  ui.finalVisible = false;
  const modeDeck = gameMode.current?.deck?.();
  if (modeDeck) {
    useFixedDeck(modeDeck);
  } else {
    const n = roundsPerGame();
    state.unlimited = !Number.isFinite(n);
    await createSampledDeck(state.unlimited ? UNLIMITED_BATCH_ROUNDS : n);
    if (!state.deck.length) throw new Error(`"${state.map?.name || 'Map'}" has no playable locations`);
    state.rounds = state.unlimited ? Infinity : state.deck.length;
  }
  state.round = 0;
  state.total = 0;
  state.results = [];
  viewer.setMode(movementForGame());
  viewer.setStartZoomedOut(gameMode.current?.startZoomedOut ?? settings.streetViewZoomedOut);
  await loadRound();
}

const modeError = (error: unknown, fallback: string) =>
  error instanceof Error && error.message ? error.message : fallback;

export async function startModeGame() {
  const mode = gameMode.current;
  if (!mode || gameMode.busy) return;
  gameMode.busy = true;
  gameMode.error = '';
  try {
    await mode.start(startGame);
  } catch (error) {
    gameMode.error = modeError(error, 'Could not start this game mode.');
  } finally {
    gameMode.busy = false;
  }
}

export async function rematchModeGame() {
  const mode = gameMode.current;
  if (!mode || gameMode.busy) return;
  gameMode.busy = true;
  gameMode.error = '';
  try {
    await mode.rematch(startGame, () => {
      state.phase = GAME_PHASE.EMPTY;
      ui.finalVisible = false;
    });
  } catch (error) {
    gameMode.error = modeError(error, 'Could not reset this game mode.');
  } finally {
    gameMode.busy = false;
  }
}

export function endModeGame() {
  gameMode.current?.close?.();
}

// Apply a rounds-per-game change. Outside a game it restarts; mid-game it grows or
// trims the upcoming deck in place, keeping the played and current rounds.
async function applyRoundLimitChange() {
  if (!hasSampledLocations()) return;
  const inGame = ACTIVE_GAME_PHASES.has(state.phase);
  if (!inGame) { await startGame(); return; }

  cancelRoundPreload();
  await resizeSampledDeck(roundsPerGame(), () => {
    // Result screen open: its available actions may have changed.
    if (state.phase === GAME_PHASE.RESULT) {
      updateResultActions();
      scheduleNextRoundPreload(viewer);
    }
  });
}

async function loadRound(preparation: RoundPreparation | null = null) {
  state.phase = GAME_PHASE.LOADING;
  guessPanel.setFullscreen(false);
  guessPanel.setPinned(false);
  await ensureDeckIndex(state.round);
  ui.resultVisible = false;
  ui.hasGuess = false;
  gmap.reset();
  gmap.resize();

  let prepared = preparation;
  if (!prepared || !preparationMatches(prepared, state.round)) prepared = prepareRound(state.round, viewer);
  if (prepared.status === 'loading') setLoading(true, 'Loading panorama…');
  prepared = await prepared.promise!;
  if (!preparationMatches(prepared, state.round)) return;
  if (prepared.status !== 'ready') {
    state.phase = GAME_PHASE.ERROR;
    setLoading(true, 'Could not find Street View coverage for this round.');
    return;
  }

  if (!prepared.location) return;
  state.current = prepared.location;
  viewer.beginRound(prepared.location);
  const mode = gameMode.current;
  let completeImmediately = false;
  if (mode?.beginRound) {
    try {
      const seconds = activeTimerSeconds();
      completeImmediately = await mode.beginRound({
        round: state.round,
        rounds: state.unlimited ? 0 : state.rounds,
        deadline: seconds ? Date.now() + seconds * 1000 : 0,
        mapStyle: settings.mapStyle
      });
      state.phase = GAME_PHASE.GUESSING;
    } catch (error) {
      state.phase = GAME_PHASE.ERROR;
      gameMode.error = modeError(error, 'Could not start the hosted round.');
      setLoading(true, gameMode.error);
      return;
    }
  } else {
    state.phase = GAME_PHASE.GUESSING;
  }
  setLoading(false);
  roundTimer.start(); // start after load so loading time isn't counted
  effects.startRound(currentMapItem(), { ...state.current });
  if (completeImmediately) void completeModeRound();
}

export function onPlaceGuess(_guess: Point, { submit = false }: { submit?: boolean } = {}) {
  if (state.phase !== GAME_PHASE.GUESSING) return;
  ui.hasGuess = true;
  if (submit) submitGuess();
}

export function submitGuess() {
  if (state.phase === GAME_PHASE.RESULT) { nextRound(); return; }
  if (state.phase !== GAME_PHASE.GUESSING) return;
  if (!gmap.guess) return;
  void finishRound();
}

function scoreGuess(actual: Location, guess: Point) {
  const distanceKm = haversineKm(guess, actual);
  return { distanceKm, points: scoreFor(distanceKm, effectiveScaleKm()) };
}

function recordModeResult(round: number, result: RoundResult) {
  gameMode.current?.recordResult?.({
    round,
    actual: result.actual,
    result,
    score: (guess) => scoreGuess(result.actual, guess)
  });
}

// Score and reveal the round. A null guess (timeout) is a forfeit, 0 points.
export async function finishRound() {
  if (state.phase !== GAME_PHASE.GUESSING) return;
  if (gameMode.current?.completeRound) {
    await completeModeRound();
    return;
  }
  state.phase = GAME_PHASE.RESULT;
  viewer.cancelJump();
  guessPanel.setFullscreen(false);
  guessPanel.setPinned(false);
  roundTimer.stop();
  const trail = viewer.getTrail();

  const current = state.current;
  if (!current) return;
  const guess = gmap.guess;
  const distKm = guess ? haversineKm(guess, current) : null;
  const points = distKm == null ? 0 : scoreFor(distKm, effectiveScaleKm());
  state.total += points;
  const result: RoundResult = {
    guess: guess ? { lat: guess.lat, lng: guess.lng } : null,
    actual: {
      lat: current.lat,
      lng: current.lng,
      panoid: current.panoid || null
    },
    distKm, points
  };
  recordModeResult(state.round, result);
  state.results.push(result);
  showRoundResult(result, trail);
}

export async function completeModeRound() {
  const mode = gameMode.current;
  if (!mode?.completeRound || modeRoundPending || state.phase !== GAME_PHASE.GUESSING) return;
  viewer.cancelJump();
  modeRoundPending = true;
  gameMode.busy = true;
  gameMode.error = '';
  roundTimer.stop();
  try {
    const current = state.current;
    if (!current) throw new Error('The current location is unavailable.');
    const reveals = await mode.completeRound({
      round: state.round,
      actual: { ...current },
      score: (guess) => scoreGuess(current, guess)
    });
    const result: RoundResult = {
      guess: null,
      actual: { lat: current.lat, lng: current.lng, panoid: current.panoid || null },
      distKm: null,
      points: 0
    };
    state.results.push(result);
    state.phase = GAME_PHASE.RESULT;
    updateResultActions();
    setLoading(false);
    ui.resultVisible = true;
    resultMap.showMany(reveals);
    scheduleNextRoundPreload(viewer);
  } catch (error) {
    state.phase = GAME_PHASE.ERROR;
    gameMode.error = modeError(error, 'Could not complete the hosted round.');
    setLoading(true, gameMode.error);
  } finally {
    gameMode.busy = false;
    modeRoundPending = false;
  }
}

function showRoundResult(result: RoundResult, trail: Trail | null = null) {
  const { actual } = result;
  updateResultActions();

  setLoading(false);
  ui.resultVisible = true;
  const modeResults = gameMode.current?.roundResults?.(state.round, result);
  if (modeResults?.length) resultMap.showMany(modeResults, trail);
  else resultMap.show(result, trail);
  effects.showResult(currentMapItem(), { ...actual }, state.round);
  scheduleNextRoundPreload(viewer);
}

export async function nextRound() {
  if (state.phase !== GAME_PHASE.RESULT || (gameMode.current && gameMode.busy)) return;
  if (!hasNextRound()) {
    if (gameMode.current && !await finishModeSession()) return;
    showFinal();
    return;
  }

  const nextIndex = state.round + 1;
  const preload = takeRoundPreload(nextIndex);
  state.round = nextIndex;
  await loadRound(preload);
}

export async function endUnlimitedGame() {
  if (state.phase !== GAME_PHASE.RESULT || !state.unlimited || (gameMode.current && gameMode.busy)) return;
  if (gameMode.current && !await finishModeSession()) return;
  showFinal();
}

async function finishModeSession() {
  const mode = gameMode.current;
  if (!mode?.finish) return true;
  gameMode.busy = true;
  gameMode.error = '';
  try {
    await mode.finish();
    return true;
  } catch (error) {
    gameMode.error = modeError(error, 'Could not finish this game mode.');
    return false;
  } finally {
    gameMode.busy = false;
  }
}

function applyFinalRoundSelection() {
  const mode = gameMode.current;
  if (mode) {
    summaryMap.show(mode.finalResults(ui.selectedFinalRound));
    return;
  }
  const results = ui.selectedFinalRound == null
    ? state.results
    : [state.results[ui.selectedFinalRound]];
  summaryMap.show(results);
  const selectedResult = ui.selectedFinalRound == null
    ? null
    : state.results[ui.selectedFinalRound];
  effects.selectFinalRound(
    currentMapItem(),
    selectedResult?.actual ? { ...selectedResult.actual } : null,
    ui.selectedFinalRound
  );
}

export function selectFinalRound(index: number) {
  const mode = gameMode.current;
  ui.selectedFinalRound = mode?.selectFinalRound
    ? mode.selectFinalRound(ui.selectedFinalRound, index)
    : (mode ? index : (ui.selectedFinalRound === index ? null : index));
  applyFinalRoundSelection();
}

function showFinal() {
  cancelRoundPreload();
  roundTimer.stop();
  state.phase = GAME_PHASE.FINAL;
  ui.selectedFinalRound = gameMode.current?.initialFinalRound() ?? null;
  setLoading(false);
  ui.resultVisible = false;
  ui.finalVisible = true;
  // Mode-specific final UI is inserted reactively, so fit after that DOM update.
  if (gameMode.current) requestAnimationFrame(applyFinalRoundSelection);
  else applyFinalRoundSelection();
}

export function applySessionSettings(next: Settings, previous: Settings) {
  if (!gameMode.current && next.rounds !== previous.rounds) void applyRoundLimitChange();
  if (!gameMode.current && next.timer !== previous.timer) {
    if (state.phase === GAME_PHASE.GUESSING) roundTimer.start();
    else roundTimer.stop();
  }
}

export async function loadRequestedGameData() {
  const mode = gameMode.current;
  const loaded = await mode?.load?.();
  if (loaded) {
    return { mode, map: loaded.map, sample: null };
  }
  const mapID = new URLSearchParams(location.search).get('map')?.trim();
  if (!mapID) throw new Error('No map was selected');
  const { maps } = await loadLibrary();
  const selected = maps.find((item) => item.id === mapID);
  if (!selected) throw new Error('That map no longer exists');
  const rounds = roundsPerGame();
  const sample = await sampleMap(
    selected,
    (Number.isFinite(rounds) ? rounds : UNLIMITED_BATCH_ROUNDS) + PANORAMA_RETRIES
  );
  return {
    mode,
    map: { ...selected, count: sample.locationCount },
    sample
  };
}

export async function activateRequestedGame({
  mode,
  map,
  sample
}: Awaited<ReturnType<typeof loadRequestedGameData>>, sessionEffects: SessionEffects) {
  effects = sessionEffects;
  state.map = map;
  effects.selectMap(currentMapItem());
  setLoading(true, `Loading ${map.name}…`);
  selectSampledMap(map, sample);
  if (sample && !sample.locationCount) throw new Error(`"${map.name}" has no playable locations`);
  if (mode) {
    await mode.initialize(map);
    viewer.setMode(movementForGame());
    viewer.setStartZoomedOut(mode.startZoomedOut ?? settings.streetViewZoomedOut);
    if (mode.autoStart) {
      await startGame();
      return;
    }
    state.phase = GAME_PHASE.EMPTY;
    setLoading(false);
    return;
  }
  await startGame();
}

export async function refreshGameMode() {
  const mode = gameMode.current;
  if (!mode?.refresh) return;
  try {
    if (await mode.refresh() && state.phase === GAME_PHASE.GUESSING) await completeModeRound();
  } catch (error) {
    gameMode.error = modeError(error, 'The hosted game connection was lost.');
  }
}

