// Gameplay shortcuts and compass input use the same phase guards.
import { desktopRuntimeAvailable, getGameWindowState, setGameFullscreen } from '../../platform/desktop.js';
import { openStreetView } from '../../rendering/map/map.js';
import { Keybindings } from '../settings/keybindings.js';
import { GAME_PHASE, state } from './state.svelte.js';
import { gameMode } from './game-mode.svelte.js';
import {
  viewer, gmap, guessPanel, compassCanvas, classicCompass, setGuessMapSize
} from './runtime.js';
import { finishRound, nextRound, onPlaceGuess, rematchModeGame, startGame, submitGuess } from './session.js';
import type { GuessMapSize } from './view-options.js';

const canInteractWithGuess = () =>
  state.phase === GAME_PHASE.GUESSING && (gameMode.current?.allowsGuess ?? true);

function setGuessMapSizeFromShortcut(size: GuessMapSize, event: KeyboardEvent) {
  if (event.repeat || !canInteractWithGuess()) return;
  setGuessMapSize(size);
}

function jumpFromShortcut(direction: 1 | -1, event: KeyboardEvent) {
  if (event.defaultPrevented || !canInteractWithGuess() || gameMode.busy) return;
  event.preventDefault();
  event.stopPropagation();
  if (!event.repeat) void viewer.jump(direction);
}

// What each shortcut does; names match keybindings.js.
const KEY_ACTIONS: Record<string, (event: KeyboardEvent) => void> = {
  submitOrNext: (event) => {
    if (event.repeat) return;
    if (state.phase === GAME_PHASE.FINAL) {
      if (gameMode.current) void rematchModeGame();
      else void startGame();
    }
    else if (state.phase === GAME_PHASE.RESULT) nextRound();
    else if (state.phase === GAME_PHASE.GUESSING) {
      if (gameMode.current?.completeRound) void finishRound();
      else if (gmap.guess) submitGuess();
    }
  },
  placeGuessAtCenter: (event) => {
    if (!event.repeat && canInteractWithGuess()) onPlaceGuess(gmap.placeGuessAtCenter());
  },
  zoomIn: () => { if (canInteractWithGuess()) viewer.zoomFull(1); },
  zoomOut: () => { if (canInteractWithGuess()) viewer.zoomFull(-1); },
  resetView: () => { if (canInteractWithGuess()) viewer.resetView(); },
  jumpForward: (event) => jumpFromShortcut(1, event),
  jumpBackward: (event) => jumpFromShortcut(-1, event),
  checkpoint: (event) => {
    if (!event.repeat && canInteractWithGuess()) viewer.toggleCheckpoint();
  },
  checkpointPeek: (event) => {
    if (!event.repeat && canInteractWithGuess()) viewer.startCheckpointPeek();
  },
  lookBehind: (event) => {
    if (!event.repeat && canInteractWithGuess()) viewer.startLookBehind();
  },
  faceNorth: () => {
    if (!canInteractWithGuess()) return;
    // Press once to face north; again while north to look straight down.
    const h = viewer.getHeading();
    const atNorth = Math.min(h, 360 - h) < 1.5;
    if (atNorth && Math.abs(viewer.lat) < 2) viewer.faceNorthDown();
    else viewer.faceNorth();
  },
  toggleMapPinned: (event) => {
    if (!event.repeat && canInteractWithGuess()) guessPanel.setPinned(!guessPanel.isPinned());
  },
  toggleMapFullscreen: () => {
    if (canInteractWithGuess()) guessPanel.setFullscreen(!guessPanel.isFullscreen());
  },
  toggleDesktopFullscreen: (event) => {
    if (event.repeat || !desktopRuntimeAvailable()) return;
    event.preventDefault();
    void getGameWindowState().then(({ fullscreen }) => setGameFullscreen(!fullscreen));
  },
  openStreetView: (event) => {
    if (!event.repeat && state.current &&
        (state.phase === GAME_PHASE.GUESSING || state.phase === GAME_PHASE.RESULT)) {
      openStreetView(state.current);
    }
  },
  mapSizeDefault: (event) => setGuessMapSizeFromShortcut('default', event),
  mapSizeLarge: (event) => setGuessMapSizeFromShortcut('large', event),
  mapSizeXl: (event) => setGuessMapSizeFromShortcut('xl', event),
  mapSizeXxl: (event) => setGuessMapSizeFromShortcut('xxl', event),
  mapSizeMax: (event) => setGuessMapSizeFromShortcut('max', event),
  hideHud: () => {
    if (state.phase === GAME_PHASE.GUESSING) document.body.classList.toggle('ui-hidden');
  }
};

const KEY_RELEASES: Record<string, (event: KeyboardEvent) => void> = {
  checkpointPeek: () => viewer.endCheckpointPeek(),
  lookBehind: () => viewer.endLookBehind()
};

export const keybindings = new Keybindings({
  actions: KEY_ACTIONS,
  releases: KEY_RELEASES
});

export function bindCompassInput() {
  const faceNorth = () => {
    if (canInteractWithGuess()) viewer.faceNorth();
  };
  compassCanvas.addEventListener('click', faceNorth);
  classicCompass.addEventListener('click', faceNorth);
  classicCompass.addEventListener('keydown', (event) => {
    if (event.code === 'Space' || event.code === 'Enter') event.stopPropagation();
  });
}

export function bindKeyboardInput() {
  // Consume jump bindings before Street View can interpret a rebound arrow/WASD key.
  window.addEventListener('keydown', (event) => {
    const action = keybindings.map[event.code];
    if (action === 'jumpForward' || action === 'jumpBackward') keybindings.onKeyDown(event);
  }, true);
  window.addEventListener('keydown', keybindings.onKeyDown);
  window.addEventListener('keyup', keybindings.onKeyUp);
  window.addEventListener('blur', () => {
    viewer.endCheckpointPeek();
    viewer.endLookBehind();
  });
}
