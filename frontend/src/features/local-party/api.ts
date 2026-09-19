import { LocalParty } from '../../../bindings/github.com/ohne-b/OhneGuessr/internal/local-party/index.js';
import { desktopRuntimeAvailable, onDesktopEvent } from '../../platform/desktop.js';
import type { LauncherTheme } from '../../styles/theme.js';
import type { PartyRoundReveal } from './types.js';

export function launchParty(mapID: string, theme: LauncherTheme, accentColor: string) {
  if (!desktopRuntimeAvailable()) {
    return Promise.reject(new Error('Local Party requires the desktop app.'));
  }
  return LocalParty.LaunchParty(mapID, theme, accentColor);
}

export function getPartyHostState(id: string) {
  return LocalParty.GetPartyHostState(id);
}

export function lockPartyRoster(id: string) {
  return LocalParty.LockPartyRoster(id);
}

export function beginPartyRound(
  id: string,
  round: number,
  rounds: number,
  deadline: number,
  mapStyle: string
) {
  return LocalParty.BeginPartyRound(id, round, rounds, deadline, mapStyle);
}

export async function closePartyRound(id: string, round: number) {
  return (await LocalParty.ClosePartyRound(id, round)) ?? [];
}

export function publishPartyReveal(id: string, reveal: PartyRoundReveal) {
  return LocalParty.PublishPartyReveal(id, reveal);
}

export function finishParty(id: string) {
  return LocalParty.FinishParty(id);
}

export function resetParty(id: string) {
  return LocalParty.ResetParty(id);
}

export function onPartyChanged(listener: (id: string) => void) {
  return onDesktopEvent('party:changed', (data) => listener(String(data || '')));
}
