// MapLibre maps:
//   GuessMap     - permanent in-game map for dropping a guess
//   RevealEngine - one lazy map shared by the round and final result screens
import * as maplibregl from 'maplibre-gl';
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url';
import type { FeatureCollection, Point as GeoJsonPoint } from 'geojson';
import type {
  FitBoundsOptions,
  GeoJSONSource,
  Map as MapLibreMap,
  MapOptions,
  StyleSpecification
} from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import {
  DEFAULT_MAP_STYLE_KEY,
  DEFAULT_MAP_ZOOM_SPEED,
  isDarkMapStyle,
  normalizeMapZoomSpeed
} from './config.js';
import { buildMapStyle } from './map-style.js';
import { ResultLayers } from './result-layers.js';
import type { Location, Point, Trail } from '../../shared/geo.js';
import type { RevealResult } from './types.js';

maplibregl.setWorkerUrl(workerUrl);

const INITIAL_CENTER: [number, number] = [0, 20];
const INITIAL_ZOOM = 1;
const MIN_ZOOM = 1;
const MAPLIBRE_TILE_SIZE = 512;
const WORLD_FILL_OVERSCAN = 12;
const BASE_WHEEL_ZOOM_RATE = 1 / 360;
const BASE_TRACKPAD_ZOOM_RATE = 1 / 85;
const REVEAL_EDGE_PADDING = 64;
const SINGLE_POINT_EPSILON = 1e-7;
const panEasing = (t: number) => 1 - (1 - t) ** 3;
const mapViewports = new WeakMap<MapLibreMap, HTMLElement>();
const worldFillMaps = new WeakSet<MapLibreMap>();

interface Rect {
  top: number;
  right: number;
  bottom: number;
  left: number;
}

interface StyledMapOwner {
  map: MapLibreMap;
  styleKey: string;
}

function accentColor() {
  return getComputedStyle(document.documentElement)
    .getPropertyValue('--accent').trim() || '#22c55e';
}

function applyMapZoomSpeed(map: MapLibreMap, value: unknown) {
  const speed = normalizeMapZoomSpeed(value);
  map.scrollZoom.setWheelZoomRate(BASE_WHEEL_ZOOM_RATE * speed);
  map.scrollZoom.setZoomRate(BASE_TRACKPAD_ZOOM_RATE * speed);
  return speed;
}

function mapViewport(map: MapLibreMap) {
  return mapViewports.get(map) || map.getContainer();
}

function createRenderContainer(viewport: HTMLElement) {
  const container = document.createElement('div');
  container.className = 'map-overscan';
  viewport.classList.add('map-viewport');
  viewport.appendChild(container);
  return container;
}

// Horizontal world copies cover either side of the map. Vertically, keep the
// single Mercator world just larger than the viewport so its edges never show.
function keepWorldFilled(map: MapLibreMap) {
  const height = mapViewport(map).clientHeight;
  if (!height) return;
  const fillZoom = Math.log2(
    (height + WORLD_FILL_OVERSCAN) / MAPLIBRE_TILE_SIZE
  );
  const minZoom = Math.max(
    MIN_ZOOM,
    Math.min(map.getMaxZoom(), fillZoom)
  );
  if (map.getMinZoom() !== minZoom) map.setMinZoom(minZoom);
  if (map.getZoom() < minZoom) map.jumpTo({ zoom: minZoom });
}

function resizeMap(map: MapLibreMap) {
  map.resize();
  if (worldFillMaps.has(map)) keepWorldFilled(map);
}

function createMap(
  container: HTMLElement,
  styleKey: string,
  options: Partial<MapOptions> = {},
  { fillWorld = true }: { fillWorld?: boolean } = {}
) {
  const definition = buildMapStyle(styleKey);
  const renderContainer = createRenderContainer(container);
  const map = new maplibregl.Map({
    container: renderContainer,
    style: definition.style as StyleSpecification,
    center: INITIAL_CENTER,
    zoom: INITIAL_ZOOM,
    minZoom: 0,
    maxZoom: definition.maxZoom,
    renderWorldCopies: true,
    dragPan: {
      // These units make MapLibre's duration in ms equal to release speed in px/s.
      linearity: 1,
      maxSpeed: Infinity,
      deceleration: 1000,
      easing: panEasing
    },
    dragRotate: false,
    pitchWithRotate: false,
    touchPitch: false,
    maxPitch: 0,
    keyboard: false,
    reduceMotion: false,
    trackResize: false,
    pixelRatio: Math.min(window.devicePixelRatio || 1, 2),
    cancelPendingTileRequestsWhileZooming: false,
    ...options
  });
  const easeTo = map.easeTo.bind(map);
  map.easeTo = (options, eventData) => {
    // GeoGuessr's Google Maps 3.64 uses cubic easing and sqrt(speed) timing:
    // 1000 px/s coasts 156.25 px over 312.5 ms. Keep MapLibre's gesture handling.
    // Reference: https://maps.googleapis.com/maps-api-v3/api/js/64/14a/map.js
    if (options.easing === panEasing && options.duration && options.offset) {
      const duration = 1000 * Math.sqrt(options.duration / 1000) / 3.2;
      const scale = duration / options.duration;
      const offset = Array.isArray(options.offset)
        ? options.offset : [options.offset.x, options.offset.y];
      options = {
        ...options, duration,
        offset: [offset[0] * scale, offset[1] * scale]
      };
    }
    return easeTo(options, eventData);
  };
  mapViewports.set(map, container);
  if (fillWorld) worldFillMaps.add(map);
  applyMapZoomSpeed(map, DEFAULT_MAP_ZOOM_SPEED);
  map.touchZoomRotate.disableRotation();
  if (fillWorld) keepWorldFilled(map);
  return { map, styleKey: definition.key };
}

function area(rect: Rect) {
  return Math.max(0, rect.right - rect.left) *
    Math.max(0, rect.bottom - rect.top);
}

// Use the largest unobstructed rectangle around an overlay. This naturally
// puts round results above their bottom panel and final results beside the
// desktop card (or above its responsive bottom-sheet layout).
function clearViewportArea(viewport: HTMLElement, obstruction: Element | null): Rect {
  const width = viewport.clientWidth;
  const height = viewport.clientHeight;
  const full = { top: 0, right: width, bottom: height, left: 0 };
  if (!obstruction) return full;

  const viewportRect = viewport.getBoundingClientRect();
  const obstructionRect = obstruction.getBoundingClientRect();
  const blocked = {
    top: Math.max(0, Math.min(height, obstructionRect.top - viewportRect.top)),
    right: Math.max(0, Math.min(width, obstructionRect.right - viewportRect.left)),
    bottom: Math.max(0, Math.min(height, obstructionRect.bottom - viewportRect.top)),
    left: Math.max(0, Math.min(width, obstructionRect.left - viewportRect.left))
  };
  if (blocked.right <= blocked.left || blocked.bottom <= blocked.top) return full;

  return [
    { top: 0, right: width, bottom: blocked.top, left: 0 },
    { top: blocked.bottom, right: width, bottom: height, left: 0 },
    { top: 0, right: blocked.left, bottom: height, left: 0 },
    { top: 0, right: width, bottom: height, left: blocked.right }
  ].reduce((best, candidate) => area(candidate) > area(best) ? candidate : best);
}

function fitPadding(map: MapLibreMap, obstruction: Element | null) {
  const viewport = mapViewport(map);
  const renderContainer = map.getContainer();
  const horizontalOverscan = Math.max(
    0,
    (renderContainer.clientWidth - viewport.clientWidth) / 2
  );
  const verticalOverscan = Math.max(
    0,
    (renderContainer.clientHeight - viewport.clientHeight) / 2
  );
  const clear = clearViewportArea(viewport, obstruction);
  const clearWidth = clear.right - clear.left;
  const clearHeight = clear.bottom - clear.top;
  const margin = Math.min(
    REVEAL_EDGE_PADDING,
    Math.max(0, clearWidth / 4),
    Math.max(0, clearHeight / 4)
  );
  return {
    top: Math.round(verticalOverscan + clear.top + margin),
    right: Math.round(horizontalOverscan + viewport.clientWidth - clear.right + margin),
    bottom: Math.round(verticalOverscan + viewport.clientHeight - clear.bottom + margin),
    left: Math.round(horizontalOverscan + clear.left + margin)
  };
}

function setMapStyle(owner: StyledMapOwner, key: string, beforeChange: (() => void) | null = null) {
  const definition = buildMapStyle(key);
  if (definition.key === owner.styleKey) return;
  beforeChange?.();
  owner.styleKey = definition.key;
  owner.map.setMaxZoom(definition.maxZoom);
  if (worldFillMaps.has(owner.map)) keepWorldFilled(owner.map);
  owner.map.setStyle(definition.style as StyleSpecification);
}

function observeMapSize(
  map: MapLibreMap,
  container: HTMLElement,
  onResize: (() => void) | null = null
) {
  if (typeof ResizeObserver === 'undefined') return null;
  const observer = new ResizeObserver(([entry]) => {
    if (!entry || entry.contentRect.width <= 0 || entry.contentRect.height <= 0) return;
    resizeMap(map);
    onResize?.();
  });
  observer.observe(container);
  return observer;
}

function isPoint(value: unknown): value is Point {
  if (!value || typeof value !== 'object') return false;
  const point = value as Partial<Point>;
  return typeof point.lat === 'number' && Number.isFinite(point.lat) &&
    typeof point.lng === 'number' && Number.isFinite(point.lng);
}

function pointCoordinates(point: Point): [number, number] {
  return [point.lng, point.lat];
}

export class GuessMap {
  readonly container: HTMLElement;
  readonly map: MapLibreMap;
  styleKey: string;
  guess: Point | null = null;
  private accent: string;
  private readonly resizeObserver: ResizeObserver | null;

  constructor(
    elId: string,
    onPlace: (point: Point, options: { submit: boolean }) => void,
    styleKey = DEFAULT_MAP_STYLE_KEY
  ) {
    const container = document.getElementById(elId);
    if (!container) throw new Error(`Missing #${elId}`);
    this.container = container;
    const created = createMap(this.container, styleKey, {
      attributionControl: false,
      doubleClickZoom: false
    });
    this.map = created.map;
    this.styleKey = created.styleKey;
    this.accent = accentColor();

    this.map.on('style.load', () => this.installGuessLayer());
    this.map.on('click', (event) => {
      this.setGuess(event.lngLat);
      onPlace(this.guess!, { submit: event.originalEvent?.shiftKey === true });
    });
    this.map.on('dragstart', () => {
      this.map.getCanvas().style.cursor = 'grabbing';
    });
    this.map.on('dragend', () => {
      this.map.getCanvas().style.cursor = 'crosshair';
    });
    this.resizeObserver = observeMapSize(this.map, this.container);
  }

  installGuessLayer() {
    if (!this.map.getSource('guess-point')) {
      this.map.addSource('guess-point', {
        type: 'geojson',
        data: this.guessData()
      });
    }
    if (!this.map.getLayer('guess-point')) {
      this.map.addLayer({
        id: 'guess-point',
        type: 'circle',
        source: 'guess-point',
        paint: {
          'circle-radius': 8,
          'circle-color': this.accent,
          'circle-stroke-color': '#ffffff',
          'circle-stroke-width': 2
        }
      });
    }
    this.syncGuess();
  }

  guessData(): FeatureCollection<GeoJsonPoint> {
    return {
      type: 'FeatureCollection',
      features: this.guess ? [{
        type: 'Feature',
        properties: {},
        geometry: { type: 'Point', coordinates: pointCoordinates(this.guess) }
      }] : []
    };
  }

  syncGuess() {
    (this.map.getSource('guess-point') as GeoJSONSource | undefined)?.setData(this.guessData());
  }

  resize() {
    resizeMap(this.map);
  }

  setStyle(key: string) {
    setMapStyle(this, key);
  }

  setAccent(accent: string) {
    this.accent = accent;
    if (this.map.getLayer('guess-point')) {
      this.map.setPaintProperty('guess-point', 'circle-color', accent);
    }
  }

  setZoomSpeed(value: unknown) {
    return applyMapZoomSpeed(this.map, value);
  }

  setGuess(point: Point) {
    this.guess = { lat: point.lat, lng: point.lng };
    this.syncGuess();
  }

  placeGuessAtCenter() {
    this.setGuess(this.map.getCenter());
    return this.guess!;
  }

  reset() {
    this.guess = null;
    this.syncGuess();
    this.map.stop();
    this.map.jumpTo({ center: INITIAL_CENTER, zoom: INITIAL_ZOOM });
  }
}

function streetViewUrl(actual: Location) {
  const params = new URLSearchParams({
    api: '1',
    map_action: 'pano',
    viewpoint: `${actual.lat},${actual.lng}`
  });
  if (actual.panoid) params.set('pano', actual.panoid);
  return `https://www.google.com/maps/@?${params}`;
}

export function openStreetView(actual: Location) {
  window.open(streetViewUrl(actual), '_blank', 'noopener,noreferrer');
}

function resultPoints(results: readonly RevealResult[]) {
  const points: Point[] = [];
  for (const { guess, actual } of results) {
    if (isPoint(guess)) points.push(guess);
    if (isPoint(actual)) points.push(actual);
  }
  return points;
}

const normalizedLongitude = (lng: number) => ((lng % 360) + 360) % 360;

// LngLatBounds only treats a bounds object as dateline-crossing when west is
// greater than east. Extending it point-by-point cannot produce that shape, so
// unwrap all points into their smallest circular longitude interval first.
function unwrapPoints(points: readonly Point[]): Point[] {
  if (points.length < 2) return [...points];
  const longitudes = points
    .map((point) => normalizedLongitude(point.lng))
    .sort((a, b) => a - b);
  let largestGap = -1;
  let startIndex = 0;
  for (let index = 0; index < longitudes.length; index++) {
    const next = index + 1 < longitudes.length
      ? longitudes[index + 1]
      : longitudes[0] + 360;
    const gap = next - longitudes[index];
    if (gap > largestGap) {
      largestGap = gap;
      startIndex = (index + 1) % longitudes.length;
    }
  }

  const start = longitudes[startIndex];
  const unwrapped = points.map((point) => {
    let lng = normalizedLongitude(point.lng);
    if (lng < start) lng += 360;
    return { lat: point.lat, lng };
  });
  let west = Infinity;
  let east = -Infinity;
  for (const point of unwrapped) {
    west = Math.min(west, point.lng);
    east = Math.max(east, point.lng);
  }
  const worldOffset = Math.floor(((west + east) / 2 + 180) / 360) * 360;
  return unwrapped.map((point) => ({
    lat: point.lat,
    lng: point.lng - worldOffset
  }));
}

function equalPointBounds(bounds: maplibregl.LngLatBounds) {
  const southWest = bounds.getSouthWest();
  const northEast = bounds.getNorthEast();
  return southWest.lat === northEast.lat && southWest.lng === northEast.lng;
}

class RevealEngine {
  map!: MapLibreMap;
  styleKey: string;
  private accent: string;
  private zoomSpeed = DEFAULT_MAP_ZOOM_SPEED;
  private readonly container: HTMLDivElement;
  private layers!: ResultLayers;
  private resizeObserver: ResizeObserver | null = null;
  private cameraRevision = 0;
  private fitRequest: FitRequest | null = null;

  constructor(styleKey: string) {
    this.styleKey = buildMapStyle(styleKey).key;
    this.accent = accentColor();
    this.container = document.createElement('div');
    this.container.className = 'reveal-map';
  }

  mount(slotId: string) {
    const slot = document.getElementById(slotId);
    if (!slot) throw new Error(`Missing #${slotId}`);
    if (this.container.parentElement !== slot) slot.appendChild(this.container);
    if (!this.map) this.create();
    else resizeMap(this.map);
  }

  create() {
    const created = createMap(this.container, this.styleKey, {
      attributionControl: false,
      zoom: 1,
      minZoom: -2
    }, { fillWorld: false });
    this.map = created.map;
    this.styleKey = created.styleKey;
    applyMapZoomSpeed(this.map, this.zoomSpeed);
    this.layers = new ResultLayers(
      this.map,
      openStreetView,
      this.accent,
      isDarkMapStyle(this.styleKey)
    );
    this.map.on('style.load', () => this.layers.install());
    this.resizeObserver = observeMapSize(
      this.map,
      this.container,
      () => this.scheduleFit()
    );
  }

  show(
    slotId: string,
    results: RevealResult[],
    trail: Trail | null,
    obstruction: Element | null
  ) {
    this.mount(slotId);
    this.layers.setResults(results, trail);
    this.fitRequest = {
      points: resultPoints(results),
      obstruction
    };
    this.scheduleFit();
  }

  scheduleFit() {
    const request = this.fitRequest;
    if (!request) return;
    const revision = ++this.cameraRevision;
    // Camera math is available before tiles finish loading. Waiting on
    // style.load here can miss the event and leave the previous result view.
    requestAnimationFrame(() => {
      if (revision !== this.cameraRevision || request !== this.fitRequest) return;
      this.fit(request);
    });
  }

  fit({ points, obstruction }: FitRequest) {
    if (!points.length) return;
    resizeMap(this.map);
    this.map.stop();
    const fittedPoints = unwrapPoints(points);
    const bounds = new maplibregl.LngLatBounds();
    for (const point of fittedPoints) bounds.extend(pointCoordinates(point));

    const onePoint = fittedPoints.length === 1 || equalPointBounds(bounds);
    if (onePoint) {
      const point = fittedPoints[0];
      bounds.extend([point.lng - SINGLE_POINT_EPSILON, point.lat - SINGLE_POINT_EPSILON]);
      bounds.extend([point.lng + SINGLE_POINT_EPSILON, point.lat + SINGLE_POINT_EPSILON]);
    }

    const options: FitBoundsOptions = {
      padding: fitPadding(this.map, obstruction),
      duration: 0
    };
    if (onePoint) {
      options.maxZoom = Math.min(4, this.map.getMaxZoom());
    }
    this.map.fitBounds(bounds, options);
  }

  setStyle(key: string) {
    if (!this.map) {
      this.styleKey = buildMapStyle(key).key;
      return;
    }
    setMapStyle(this, key, () => this.layers.invalidate());
    this.layers.setDark(isDarkMapStyle(this.styleKey));
  }

  setAccent(accent: string) {
    this.accent = accent;
    this.layers?.setAccent(accent);
  }

  setZoomSpeed(value: unknown) {
    this.zoomSpeed = normalizeMapZoomSpeed(value);
    if (this.map) applyMapZoomSpeed(this.map, this.zoomSpeed);
    return this.zoomSpeed;
  }
}

interface FitRequest {
  points: Point[];
  obstruction: Element | null;
}

export function createRevealMaps(
  resultElId: string,
  finalElId: string,
  styleKey = DEFAULT_MAP_STYLE_KEY
) {
  const engine = new RevealEngine(styleKey);
  const resultPanel = document.getElementById('resultPanel');
  const finalCard = document.querySelector('#final .final-card');
  const showMany = (results: RevealResult[], trail: Trail | null = null) =>
    engine.show(resultElId, results, trail, resultPanel);
  return {
    resultMap: {
      show: (result: RevealResult, trail: Trail | null = null) => showMany([result], trail),
      showMany,
      setStyle: engine.setStyle.bind(engine),
      setAccent: engine.setAccent.bind(engine),
      setZoomSpeed: engine.setZoomSpeed.bind(engine)
    },
    summaryMap: {
      show(results: RevealResult[]) {
        if (results.length) engine.show(finalElId, results, null, finalCard);
      }
    }
  };
}
