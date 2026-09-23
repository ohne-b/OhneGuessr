export type MovementMode = 'moving' | 'nm' | 'nmpz';

// Panorama viewer backed by Google's real Street View (the vendored, key-less Maps
// JS API in vendor/opensv/opensv.js). Movement mode is set via setMode():
//   moving — walk (click the road / arrows), pan, and zoom
//   nm     — no moving; pan and zoom allowed
//   nmpz   — no move, pan, or zoom (locked to the spawn view)

import { publicAsset } from '../../platform/assets.js';
import type { Location, Point, Trail } from '../../shared/geo.js';
import { capturePanoViewport, type PanoramaCaptureOptions } from './panorama-capture.js';
import { installCarMask, setCarHidden } from './car-mask.js';
import { fetchPanoramaDetails, type PanoramaDetails } from './panorama-details.js';

const OPENSV_SRC = publicAsset('vendor/opensv/opensv.js');
const DEFAULT_ZOOM = 1;
const FULLY_ZOOMED_OUT = -3; // bottom of OpenSV's panorama zoom range
const ZOOM_IN = 3;     // google SV zoom level for "zoomed in"
const TWEEN_MS = 160;
const POSITION_EPSILON = 1e-5; // ~1 m; enough to bind a viewer event to its lookup
const JUMP_METRES = 100;
// Keys Street View uses to walk; blocked outside moving mode.
const MOVE_KEYS = new Set(['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'KeyW', 'KeyA', 'KeyS', 'KeyD']);

export const resolveStartZoom = (locationZoom: unknown, forceZoomedOut = false) =>
  forceZoomedOut
    ? FULLY_ZOOMED_OUT
    : (typeof locationZoom === 'number' && Number.isFinite(locationZoom) ? locationZoom : DEFAULT_ZOOM);

let loadPromise: Promise<typeof google> | null = null;
const streetViewReady = () =>
  Boolean(window.google?.maps?.StreetViewPanorama && window.google?.maps?.StreetViewService);

type Position = Point | google.maps.LatLng | null | undefined;

export interface PanoramaView {
  position: Point;
  heading: number;
  pitch: number;
  zoom: number;
  width: number;
  height: number;
}

export interface PanoramaMetadata extends PanoramaView {
  panoId: string;
  description: string;
  shortDescription: string;
  photographer: { heading: number | null; pitch: number | null };
}

function samePosition(a: Position, b: Position) {
  if (!a || !b) return false;
  const latA = typeof a.lat === 'function' ? a.lat() : a.lat;
  const lngA = typeof a.lng === 'function' ? a.lng() : a.lng;
  const latB = typeof b.lat === 'function' ? b.lat() : b.lat;
  const lngB = typeof b.lng === 'function' ? b.lng() : b.lng;
  const lngDelta = Math.abs(((lngA - lngB + 540) % 360) - 180);
  return Math.abs(latA - latB) <= POSITION_EPSILON && lngDelta <= POSITION_EPSILON;
}

// Inject the vendored Maps JS API once. No tile-host rewrite => tiles hit Google
// directly, which works in a plain browser (verified by the throwaway prototype).
export function loadOpenSV(): Promise<typeof google> {
  installCarMask();
  if (streetViewReady()) return Promise.resolve(window.google!);
  if (loadPromise) return loadPromise;
  loadPromise = new Promise<typeof google>((resolve, reject) => {
    const s = document.createElement('script');
    s.src = OPENSV_SRC;
    s.onload = async () => {
      const t0 = Date.now();
      while (!streetViewReady()) {
        if (Date.now() - t0 > 10000) return reject(new Error('opensv loaded but Street View missing'));
        await new Promise((r) => setTimeout(r, 50));
      }
      resolve(window.google!);
    };
    s.onerror = () => reject(new Error('failed to load opensv.js'));
    document.head.appendChild(s);
  });
  return loadPromise;
}

interface SavedView {
  panoid: string;
  position: Point;
  pov: { heading: number; pitch: number };
  zoom: number;
}

interface CheckpointPeek {
  source: SavedView;
  token: number;
  ready: boolean;
  released: boolean;
  returning: boolean;
}

interface LookBehindView {
  token: number;
  pov: { heading: number; pitch: number };
  zoom: number;
}

export class OpenSvViewer {
  public onChange: ((heading: number) => void) | null = null;
  private readonly _host: HTMLDivElement;
  private readonly _lock: HTMLDivElement;
  private readonly _roundListeners = new Set<() => void>();
  private readonly streetView: google.maps.StreetViewService;
  private readonly pano: google.maps.StreetViewPanorama;
  private _details: PanoramaDetails | null = null;
  private defaultHeading = 0;
  private defaultPitch = 0;
  private _locationZoom = DEFAULT_ZOOM;
  private _roundStartZoom = DEFAULT_ZOOM;
  private _forceStartZoomedOut = false;
  private _tweenId = 0;
  private _startPanoId: string | null = null;
  private _trail: Trail = [];
  private _trailActive = false;
  private _checkpoint: SavedView | null = null;
  private _checkpointBusy = false;
  private _checkpointPeek: CheckpointPeek | null = null;
  private _cancelCheckpointJump: (() => void) | null = null;
  private _cancelDistanceJump: (() => void) | null = null;
  private _lookBehind: LookBehindView | null = null;
  private _roundToken = 0;
  private mode: MovementMode = 'moving';

  constructor(container: HTMLElement, hideCar = false) {
    setCarHidden(hideCar);

    // Street View mutates the inline style of the element it's given (position, etc.),
    // which would collapse our fixed/inset #pano to 0 height. Hand it a dedicated
    // 100%×100% inner div instead so #pano keeps its size.
    const host = document.createElement('div');
    host.style.width = '100%';
    host.style.height = '100%';
    container.appendChild(host);
    this._host = host;
    // Transparent overlay to block drag-look in NMPZ (Street View has no pan-disable
    // option). pointer-events toggles per mode; the HUD/guess map sit above it.
    const lock = document.createElement('div');
    Object.assign(lock.style, { position: 'absolute', inset: '0', zIndex: '1', pointerEvents: 'none' });
    container.appendChild(lock);
    this._lock = lock;

    const g = window.google;
    this.streetView = new g.maps.StreetViewService();
    this.pano = new g.maps.StreetViewPanorama(host, {
      disableDefaultUI: true,
      motionTracking: false,
      clickToGo: true,
      linksControl: true,
      showRoadLabels: false,
      scrollwheel: true,
      visible: false
    });
    // Live heading for the compass.
    this.pano.addListener('pov_changed', () => {
      if (this.onChange) this.onChange(this.pano.getPov().heading);
    });
    // Record each step so the result map can show where the player walked.
    this.pano.addListener('position_changed', () => {
      if (!this._trailActive || this._checkpointBusy) return;
      const p = this.pano.getPosition?.();
      if (!p) return;
      const point = { lat: p.lat(), lng: p.lng() };
      let segment = this._trail[this._trail.length - 1];
      if (!segment) { segment = []; this._trail.push(segment); }
      const last = segment[segment.length - 1];
      if (last && last.lat === point.lat && last.lng === point.lng) return;
      segment.push(point);
    });
    // Keyboard walking only in moving mode.
    host.addEventListener('keydown', (e) => {
      if (this.mode !== 'moving' && MOVE_KEYS.has(e.code)) { e.stopPropagation(); e.preventDefault(); }
    }, true);
  }

  // moving / nm / nmpz. clickToGo+links gate walking; scrollwheel gates zoom; the
  // overlay gates pan (look-around) for nmpz only.
  setMode(mode: MovementMode) {
    this.cancelJump();
    this.mode = mode === 'nm' || mode === 'nmpz' ? mode : 'moving';
    const moving = this.mode === 'moving';
    const nmpz = this.mode === 'nmpz';
    if (nmpz) this.endLookBehind();
    if (!moving) {
      this._clearCheckpoint();
      this._trailActive = false;
    } else if (this._trail.length) {
      this._trailActive = true;
    }
    this.pano.setOptions({ clickToGo: moving, linksControl: moving, scrollwheel: !nmpz });
    this._lock.style.pointerEvents = nmpz ? 'auto' : 'none';
  }

  setCarHidden(hidden: boolean) { setCarHidden(hidden); }

  // Starting view for this location (and where R returns to).
  setDefaultView(heading = 0, pitch = 0) {
    this.defaultHeading = heading;
    this.defaultPitch = pitch;
  }

  setStartZoomedOut(enabled: boolean) {
    this._forceStartZoomedOut = Boolean(enabled);
    this._roundStartZoom = resolveStartZoom(this._locationZoom, this._forceStartZoomedOut);
    this.pano.setZoom(this._roundStartZoom);
  }

  resetView() {
    this.cancelJump();
    this._cancelTween();
    // In moving mode, R also returns to where the round started.
    if (this._startPanoId && this.pano.getPano() !== this._startPanoId) {
      this.pano.setPano(this._startPanoId);
    }
    this.pano.setPov({ heading: this.defaultHeading, pitch: this.defaultPitch });
    this.pano.setZoom(this._roundStartZoom);
  }

  // Approximate jump in the viewing direction, snapped to official coverage.
  jump(direction: 1 | -1): Promise<boolean> {
    if (this.mode !== 'moving' || !this._trailActive || this._cancelDistanceJump ||
        this._checkpointBusy || this._lookBehind) return Promise.resolve(false);
    const source = this._captureView();
    if (!source) return Promise.resolve(false);

    let target: SavedView | null = null;
    const wait = this._settlePanorama(() => {
      const expected = target ?? source;
      if (this.pano.getPano() !== expected.panoid) { wait.cancel(); return false; }
      if (!target) {
        if (!samePosition(this.pano.getPosition(), source.position)) wait.cancel();
        return false;
      }
      if (this.pano.getStatus() !== 'OK' ||
          !samePosition(this.pano.getPosition(), target.position)) return false;
      this.pano.setPov(target.pov);
      this.pano.setZoom(target.zoom);
      return true;
    });
    this._cancelDistanceJump = wait.cancel;
    wait.start();

    try {
      const g = window.google.maps;
      const location = g.geometry.spherical.computeOffset(
        source.position, JUMP_METRES, source.pov.heading + (direction < 0 ? 180 : 0)
      );
      this.streetView.getPanorama({
        location, radius: JUMP_METRES,
        preference: g.StreetViewPreference.NEAREST,
        sources: [g.StreetViewSource.GOOGLE]
      }).then(({ data }) => {
        if (!wait.active()) return;
        const location = data.location;
        const current = this._captureView();
        if (!location?.pano || !location.latLng || location.pano === source.panoid ||
            !current || current.panoid !== source.panoid ||
            !samePosition(current.position, source.position)) { wait.cancel(); return; }
        target = {
          ...current,
          panoid: location.pano,
          position: { lat: location.latLng.lat(), lng: location.latLng.lng() }
        };
        this._cancelTween();
        this.pano.setPano(target.panoid);
        wait.check();
      }).catch(wait.cancel);
    } catch {
      wait.cancel();
    }
    return wait.promise.finally(() => {
      if (this._cancelDistanceJump === wait.cancel) this._cancelDistanceJump = null;
    });
  }

  cancelJump() {
    const cancel = this._cancelDistanceJump;
    this._cancelDistanceJump = null;
    cancel?.();
  }

  getHeading() { return this.pano.getPov().heading; }
  get lat() { return this.pano.getPov().pitch; }

  getView(): PanoramaView | null {
    const position = this.pano.getPosition?.();
    const pov = this.pano.getPov?.();
    const bounds = this._host.getBoundingClientRect();
    if (!position || !pov || bounds.width <= 0 || bounds.height <= 0) return null;
    return {
      position: { lat: position.lat(), lng: position.lng() },
      heading: pov.heading ?? 0,
      pitch: pov.pitch ?? 0,
      zoom: this.pano.getZoom?.() ?? DEFAULT_ZOOM,
      width: bounds.width,
      height: bounds.height
    };
  }

  getMetadata(): PanoramaMetadata | null {
    const view = this.getView();
    const panoId = this.pano.getPano?.();
    if (!view || !panoId) return null;
    const location = this.pano.getLocation?.();
    const photographer = this.pano.getPhotographerPov?.();
    const heading = photographer?.heading;
    const pitch = photographer?.pitch;
    return {
      ...view,
      panoId,
      description: typeof location?.description === 'string' ? location.description : '',
      shortDescription: typeof location?.shortDescription === 'string' ? location.shortDescription : '',
      photographer: {
        heading: typeof heading === 'number' && Number.isFinite(heading) ? heading : null,
        pitch: typeof pitch === 'number' && Number.isFinite(pitch) ? pitch : null
      }
    };
  }

  async getDetails(): Promise<PanoramaDetails | null> {
    const panoId = this.pano.getPano?.();
    if (!panoId) return null;
    if (this._details?.panoId === panoId) {
      return { ...this._details, coverageDates: [...this._details.coverageDates] };
    }
    try {
      const details = await fetchPanoramaDetails(panoId);
      if (!details) return null;
      if (this.pano.getPano?.() === panoId) this._details = details;
      return { ...details, coverageDates: [...details.coverageDates] };
    } catch {
      return null;
    }
  }

  captureViewport(options?: PanoramaCaptureOptions) {
    const bounds = this._host.getBoundingClientRect();
    return capturePanoViewport(this.pano, this._host, bounds.width, bounds.height, options);
  }

  onRoundStart(listener: () => void) {
    this._roundListeners.add(listener);
    return () => { this._roundListeners.delete(listener); };
  }

  // Separate walked paths; a checkpoint return starts a fresh segment.
  getTrail(): Trail { return this._trail.map((segment) => segment.map((point) => ({ ...point }))); }

  // Turn a prepared panorama into the active round. Preparation may happen while
  // the result screen covers the viewer, so reset the trail and focus only now.
  beginRound(loc: Location) {
    this._clearCheckpoint();
    this._clearLookBehind();
    this._cancelTween();
    const heading = loc.heading ?? 0;
    const pitch = loc.pitch ?? 0;
    this._locationZoom = resolveStartZoom(loc.zoom);
    this._roundStartZoom = resolveStartZoom(this._locationZoom, this._forceStartZoomedOut);
    this.setDefaultView(heading, pitch);
    this.pano.setPov({ heading, pitch });
    this.pano.setZoom(this._roundStartZoom);
    this._trail = [];
    const p = this.pano.getPosition?.();
    if (p) this._trail.push([{ lat: p.lat(), lng: p.lng() }]);
    this._trailActive = true;
    if (this.mode !== 'nmpz') this.pano.focus?.();
    for (const listener of this._roundListeners) listener();
  }

  // C alternates between saving the exact current view and returning to it once.
  toggleCheckpoint() {
    if (this.mode !== 'moving' || !this._trailActive ||
        this._checkpointBusy || this._lookBehind) return;
    this.cancelJump();

    if (!this._checkpoint) {
      this._checkpoint = this._captureView();
      return;
    }

    const checkpoint = this._checkpoint;
    const token = this._roundToken;
    this._checkpointBusy = true;
    this._jumpToView(checkpoint).then((ok) => {
      if (token !== this._roundToken) return;
      this._cancelCheckpointJump = null;
      this._checkpointBusy = false;
      const p = this.pano.getPosition?.();
      const point = p
        ? { lat: p.lat(), lng: p.lng() }
        : { ...checkpoint.position };
      this._trail.push([point]);
      if (!ok) return; // keep the checkpoint so C can retry

      this._checkpoint = null;
      this.pano.focus?.();
    });
  }

  // V temporarily visits an armed checkpoint; releasing it restores this view.
  startCheckpointPeek() {
    if (this.mode !== 'moving' || !this._trailActive ||
        !this._checkpoint || this._lookBehind) return false;
    if (this._checkpointPeek) {
      this._checkpointPeek.released = false;
      return true;
    }
    if (this._checkpointBusy) return false;
    this.cancelJump();
    const source = this._captureView();
    if (!source) return false;

    const peek = {
      source,
      token: this._roundToken,
      ready: false,
      released: false,
      returning: false
    };
    this._checkpointPeek = peek;
    this._checkpointBusy = true;
    this._jumpToView(this._checkpoint).then((ok) => {
      if (this._checkpointPeek !== peek || peek.token !== this._roundToken) return;
      this._cancelCheckpointJump = null;
      peek.ready = ok;
      if (!ok || peek.released) this._restoreCheckpointPeek(peek);
    });
    return true;
  }

  endCheckpointPeek() {
    const peek = this._checkpointPeek;
    if (!peek) return;
    peek.released = true;
    if (peek.ready) this._restoreCheckpointPeek(peek);
  }

  startLookBehind() {
    if (this.mode === 'nmpz' || this._checkpointBusy || this._lookBehind) return false;
    this.cancelJump();
    const pov = this.pano.getPov?.();
    if (!pov) return false;
    this._cancelTween();
    this._lookBehind = {
      token: this._roundToken,
      pov: { heading: pov.heading ?? 0, pitch: pov.pitch ?? 0 },
      zoom: this.pano.getZoom?.() ?? DEFAULT_ZOOM
    };
    this.pano.setPov({
      heading: (this._lookBehind.pov.heading + 180) % 360,
      pitch: this._lookBehind.pov.pitch
    });
    return true;
  }

  endLookBehind() {
    const view = this._lookBehind;
    if (!view) return;
    this._lookBehind = null;
    if (view.token !== this._roundToken) return;
    this._cancelTween();
    this.pano.setPov(view.pov);
    this.pano.setZoom(view.zoom);
    if (this.mode !== 'nmpz') this.pano.focus?.();
  }

  // faceNorth/zoom pan or zoom the view, so they no-op in nmpz (the locked mode).
  faceNorth() { if (this.mode !== 'nmpz') this._tweenPov(0, 0); }
  faceNorthDown() { if (this.mode !== 'nmpz') this._tweenPov(0, -90); }

  zoomFull(direction: number) {
    if (!direction || this.mode === 'nmpz') return;
    this.pano.setZoom(direction > 0 ? ZOOM_IN : FULLY_ZOOMED_OUT);
  }

  private _settlePanorama(ready: () => boolean, signal?: AbortSignal) {
    let done = false;
    let poll = 0;
    let timer = 0;
    let listeners: google.maps.MapsEventListener[] = [];
    let resolve!: (ok: boolean) => void;
    const promise = new Promise<boolean>((next) => { resolve = next; });
    const cleanup = () => {
      clearInterval(poll);
      clearTimeout(timer);
      for (const listener of listeners) listener?.remove?.();
      signal?.removeEventListener('abort', cancel);
    };
    const finish = (ok: boolean) => {
      if (done) return;
      done = true;
      cleanup();
      resolve(ok);
    };
    const cancel = () => finish(false);
    const check = () => {
      if (!done && ready()) finish(true);
    };
    const start = () => {
      if (done || listeners.length) return;
      listeners = [
        this.pano.addListener('pano_changed', check),
        this.pano.addListener('position_changed', check),
        this.pano.addListener('status_changed', check)
      ];
      // Some builds coalesce unchanged status events, so keep a cheap fallback.
      poll = setInterval(check, 150);
    };

    timer = setTimeout(cancel, 12000);
    signal?.addEventListener('abort', cancel, { once: true });
    if (signal?.aborted) cancel();
    return { promise, cancel, check, start, active: () => !done };
  }

  // Resolve one exact pano before touching the shared viewer. A failed lookup may
  // finish late, but its promise can no longer move a newer replacement location.
  showLocation(loc: Location, signal?: AbortSignal): Promise<boolean> {
    this._clearCheckpoint();
    this._clearLookBehind();
    this._trailActive = false;
    this._trail = [];

    let targetPano: string | null = null;
    let targetPosition: google.maps.LatLng | null = null;
    const wait = this._settlePanorama(() => {
      if (!targetPano ||
          this.pano.getPano?.() !== targetPano ||
          (targetPosition && !samePosition(this.pano.getPosition?.(), targetPosition)) ||
          this.pano.getStatus?.() !== 'OK') return false;
      this._startPanoId = targetPano;
      return true;
    }, signal);
    if (!wait.active()) return wait.promise;

    const request = loc.panoid
      ? { pano: loc.panoid }
      : { location: { lat: loc.lat, lng: loc.lng } };

    try {
      this.streetView.getPanorama(request).then(({ data }) => {
        if (!wait.active()) return;
        const location = data?.location;
        if (!location?.pano) { wait.cancel(); return; }

        targetPano = location.pano;
        loc.panoid = targetPano;
        targetPosition = location.latLng || null;
        wait.start();
        loc.heading ??= 0;
        loc.pitch ??= 0;
        loc.zoom = resolveStartZoom(loc.zoom, this._forceStartZoomedOut);
        this.pano.setPov({ heading: loc.heading, pitch: loc.pitch });
        this.pano.setZoom(loc.zoom);
        this.pano.setPano(targetPano);
        this.pano.setVisible(true);
        wait.check();
      }).catch(wait.cancel);
    } catch {
      wait.cancel();
    }
    return wait.promise;
  }

  _clearCheckpoint() {
    this.cancelJump();
    this._roundToken += 1;
    const cancel = this._cancelCheckpointJump;
    this._cancelCheckpointJump = null;
    this._checkpoint = null;
    this._checkpointBusy = false;
    this._checkpointPeek = null;
    cancel?.();
  }

  _clearLookBehind() {
    this._lookBehind = null;
  }

  _captureView(): SavedView | null {
    const panoid = this.pano.getPano?.();
    const position = this.pano.getPosition?.();
    const pov = this.pano.getPov?.();
    if (!panoid || !position || !pov) return null;
    return {
      panoid,
      position: { lat: position.lat(), lng: position.lng() },
      pov: { heading: pov.heading ?? 0, pitch: pov.pitch ?? 0 },
      zoom: this.pano.getZoom?.() ?? DEFAULT_ZOOM
    };
  }

  _restoreCheckpointPeek(peek: CheckpointPeek) {
    if (this._checkpointPeek !== peek || peek.returning) return;
    peek.returning = true;
    this._jumpToView(peek.source).then((ok) => {
      if (this._checkpointPeek !== peek || peek.token !== this._roundToken) return;
      this._cancelCheckpointJump = null;
      this._checkpointPeek = null;
      this._checkpointBusy = false;
      if (!ok) {
        const p = this.pano.getPosition?.();
        if (p) this._trail.push([{ lat: p.lat(), lng: p.lng() }]);
      }
      this.pano.focus?.();
      if (ok && peek.ready && !peek.released) this.startCheckpointPeek();
    });
  }

  _jumpToView(view: SavedView): Promise<boolean> {
    this._cancelTween();
    const wait = this._settlePanorama(() => {
      if (this.pano.getStatus?.() !== 'OK' ||
          this.pano.getPano?.() !== view.panoid ||
          !samePosition(this.pano.getPosition?.(), view.position)) return false;
      this.pano.setPov(view.pov);
      this.pano.setZoom(view.zoom);
      return true;
    });
    wait.start();
    this._cancelCheckpointJump = wait.cancel;
    if (this.pano.getPano?.() !== view.panoid) this.pano.setPano(view.panoid);
    wait.check();
    return wait.promise;
  }

  // Eased POV move (quadratic ease-out, shortest-angle).
  _tweenPov(heading: number, pitch: number) {
    this._cancelTween();
    const from = this.pano.getPov();
    let dh = from.heading - heading;
    if (dh > 180) dh -= 360;
    if (dh < -180) dh += 360;
    const dp = (from.pitch ?? 0) - pitch;
    const start = performance.now();
    const tick = (now: number) => {
      const t = Math.min((now - start) / TWEEN_MS, 1);
      const e = t * (2 - t);
      this.pano.setPov({ heading: heading + dh * (1 - e), pitch: pitch + dp * (1 - e) });
      if (t < 1) this._tweenId = requestAnimationFrame(tick);
      else this._tweenId = 0;
    };
    this._tweenId = requestAnimationFrame(tick);
  }

  _cancelTween() {
    if (this._tweenId) cancelAnimationFrame(this._tweenId);
    this._tweenId = 0;
  }

}
