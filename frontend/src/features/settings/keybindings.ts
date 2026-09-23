import { DEFAULT_MAP_ZOOM_SPEED } from '../../rendering/map/config.js';
import { settings, updateSettings } from './store.svelte.js';
import type { Settings } from './types.js';

// KeyboardEvent.code values per action. Several codes = several keys; [] disables.
export const KEYBINDINGS: Record<string, string[]> = {
  submitOrNext: ['Space'],
  placeGuessAtCenter: [],
  zoomIn: ['KeyE'],
  zoomOut: ['KeyQ'],
  resetView: ['KeyR'],
  jumpForward: ['ArrowUp'],
  jumpBackward: ['ArrowDown'],
  checkpoint: ['KeyC'],
  checkpointPeek: ['KeyV'],
  lookBehind: ['KeyB'],
  faceNorth: ['KeyN'],
  toggleMapPinned: ['KeyM'],
  toggleMapFullscreen: ['KeyF'],
  toggleDesktopFullscreen: ['F11'],
  openStreetView: [],
  mapSizeDefault: ['Digit1'],
  mapSizeLarge: ['Digit2'],
  mapSizeXl: ['Digit3'],
  mapSizeXxl: ['Digit4'],
  mapSizeMax: ['Digit5'],
  hideHud: ['KeyH']
};

export interface ControlItem {
  action: string;
  label: string;
}

export interface ControlRow extends Partial<ControlItem> {
  label: string;
  items?: ControlItem[];
}

export const CONTROL_ROWS: ControlRow[] = [
  { action: 'submitOrNext', label: 'Submit / Next' },
  { action: 'placeGuessAtCenter', label: 'Place guess at map center' },
  { action: 'zoomIn', label: 'Zoom in' },
  { action: 'zoomOut', label: 'Zoom out' },
  { action: 'resetView', label: 'Reset view' },
  { action: 'jumpForward', label: 'Jump forward ~100 m' },
  { action: 'jumpBackward', label: 'Jump backward ~100 m' },
  { action: 'checkpoint', label: 'Set / return checkpoint' },
  { action: 'checkpointPeek', label: 'Peek checkpoint' },
  { action: 'lookBehind', label: 'Look behind' },
  { action: 'faceNorth', label: 'Face north' },
  { action: 'toggleMapPinned', label: 'Toggle pinned map' },
  { action: 'toggleMapFullscreen', label: 'Toggle map fullscreen' },
  { action: 'toggleDesktopFullscreen', label: 'Toggle desktop fullscreen' },
  { action: 'openStreetView', label: 'Open actual Street View' },
  {
    label: 'Map size presets',
    items: [
      { action: 'mapSizeDefault', label: 'Default map size' },
      { action: 'mapSizeLarge', label: 'Large map size' },
      { action: 'mapSizeXl', label: 'XL map size' },
      { action: 'mapSizeXxl', label: 'XXL map size' },
      { action: 'mapSizeMax', label: 'Max map size' }
    ]
  },
  { action: 'hideHud', label: 'Hide HUD' }
];

export function codeLabel(code: string | null) {
  if (!code) return 'Unbound';
  const named: Record<string, string> = {
    Space: 'Space', Escape: 'Esc', Enter: 'Enter', Tab: 'Tab',
    ArrowUp: '↑', ArrowDown: '↓', ArrowLeft: '←', ArrowRight: '→',
    Backquote: '`', Minus: '-', Equal: '=', Slash: '/', Backslash: '\\',
    BracketLeft: '[', BracketRight: ']', Semicolon: ';', Quote: "'",
    Comma: ',', Period: '.'
  };
  if (named[code]) return named[code];
  const match = code.match(/^Key([A-Z])$/) || code.match(/^Digit(\d)$/);
  if (match) return match[1];
  const numpad = code.match(/^Numpad(\d)$/);
  return numpad ? `Num ${numpad[1]}` : code;
}

export function compactCodeLabel(code: string | null) {
  if (!code) return '—';
  const named: Record<string, string> = {
    Space: 'Spc', Enter: 'Ent', Backspace: 'Bksp', Delete: 'Del',
    PageUp: 'PgUp', PageDown: 'PgDn', NumpadAdd: 'N+',
    NumpadSubtract: 'N−', NumpadMultiply: 'N×', NumpadDivide: 'N÷',
    NumpadDecimal: 'N.'
  };
  if (named[code]) return named[code];
  const numpad = code.match(/^Numpad(\d)$/);
  if (numpad) return `N${numpad[1]}`;
  const label = codeLabel(code);
  return label.length <= 4 ? label : `${label.slice(0, 3)}…`;
}

export function currentBindings(source: Settings = settings) {
  const overrides = source.keybindings || {};
  const bindings = { ...KEYBINDINGS, ...overrides };
  const claimed = new Set(Object.values(overrides).flat());
  for (const action of Object.keys(KEYBINDINGS)) {
    if (Object.hasOwn(overrides, action)) continue;
    bindings[action] = (bindings[action] || []).filter((code) => !claimed.has(code));
  }
  return bindings;
}

export function setBinding(action: string, code: string | null) {
  const bindings = currentBindings();
  const next: Record<string, string[]> = {};
  for (const name of Object.keys(bindings)) {
    next[name] = (bindings[name] || []).filter((value) => value !== code);
  }
  next[action] = code ? [code] : [];
  updateSettings({ keybindings: next });
}

export function resetControls() {
  updateSettings({ keybindings: {}, mapZoomSpeed: DEFAULT_MAP_ZOOM_SPEED });
}

export class Keybindings {
  actions: Record<string, (event: KeyboardEvent) => void>;
  releases: Record<string, (event: KeyboardEvent) => void>;
  map: Record<string, string> = {};

  constructor({
    actions,
    releases = {}
  }: {
    actions: Record<string, (event: KeyboardEvent) => void>;
    releases?: Record<string, (event: KeyboardEvent) => void>;
  }) {
    this.actions = actions;
    this.releases = releases;
    this.rebuild();
    this.onKeyDown = this.onKeyDown.bind(this);
    this.onKeyUp = this.onKeyUp.bind(this);
  }

  rebuild() {
    this.map = {};
    for (const [action, codes] of Object.entries(currentBindings())) {
      for (const code of codes) this.map[code] = action;
    }
  }

  onKeyDown(event: KeyboardEvent) {
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    const target = event.target;
    if (target instanceof HTMLElement &&
        (target.isContentEditable || target.closest('input, textarea, select'))) return;
    const action = this.map[event.code];
    if (!action || !this.actions[action]) return;
    if (event.code === 'Space') event.preventDefault();
    this.actions[action](event);
  }

  onKeyUp(event: KeyboardEvent) {
    const action = this.map[event.code];
    if (action && this.releases[action]) this.releases[action](event);
  }
}
