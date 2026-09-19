import type { LauncherTheme } from '../../styles/theme.js';
import type { Point } from '../../shared/geo.js';

export type {
  PartyHostPlayer,
  PartyHostState,
  PartyPlayerRound,
  PartyRoundReveal
} from '../../../bindings/github.com/ohne-b/OhneGuessr/internal/local-party/models.js';

export type PartyPhase = 'lobby' | 'guessing' | 'scoring' | 'result' | 'final' | 'closed';

export interface PartyColorOption {
  value: string;
  available: boolean;
}

export interface PartyGuestResult {
  actual: Point;
  guess?: Point;
  distanceKm?: number;
  points: number;
}

export interface PartyGuestState {
  phase: PartyPhase;
  theme: LauncherTheme;
  accentColor: string;
  joined: boolean;
  capacity: number;
  playerCount: number;
  colors?: PartyColorOption[];
  color?: string;
  round: number;
  rounds: number;
  deadline: number;
  mapStyle?: string;
  locked: boolean;
  guess?: Point;
  result?: PartyGuestResult;
  total: number;
  place?: number;
  message?: string;
}
