const form = document.querySelector("#lookup-form");
const stationInput = document.querySelector("#station");
const airportDirectoryButton = document.querySelector("#save-airport-directory");
const airportDirectoryStatus = document.querySelector("#airport-directory-status");
const airportDirectoryDBName = "fp-offline-airports";
const airportSearchResults = document.querySelector("#airport-search-results");

const lookupButton = document.querySelector("#lookup");
const message = document.querySelector("#message");
const briefing = document.querySelector("#briefing");
const emptyState = document.querySelector("#empty-state");
const connection = document.querySelector("#connection");
const localPrefix = "fp-briefing:";

function setConnection() {
  const online = navigator.onLine;
  connection.textContent = online ? "Online" : "Offline";
  connection.classList.toggle("offline", !online);
}

function firstText(record, keys) {
  for (const key of keys) {
    const value = record?.[key];
    if (value !== undefined && value !== null && String(value).trim() !== "") return String(value);
  }
  return "";
}

function formatReportTime(value) {
  if (!value) return "";
  const parsed = typeof value === "number" ? new Date(value < 1e12 ? value * 1000 : value) : new Date(value);
  return Number.isNaN(parsed.valueOf()) ? String(value) : parsed.toLocaleString();
}

function formatCoordinate(value, positiveHemisphere, negativeHemisphere, limit) {
  if (value === null || value === undefined || String(value).trim() === "") return "";
  const coordinate = Number(value);
  if (!Number.isFinite(coordinate) || Math.abs(coordinate) > limit) return "";
  const totalSeconds = Math.round(Math.abs(coordinate) * 3600);
  const degrees = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const hemisphere = coordinate < 0 ? negativeHemisphere : positiveHemisphere;
  return `${degrees}°${String(minutes).padStart(2, "0")}′${String(seconds).padStart(2, "0")}″${hemisphere}`;
}

function renderMETARDetails(metar) {
  const details = document.querySelector("#metar-details");
  details.replaceChildren();
  const values = [];
  const category = firstText(metar, ["fltCat"]).toUpperCase();
  if (["VFR", "MVFR", "IFR", "LIFR"].includes(category)) values.push({ label: category, category: true });
  const direction = firstText(metar, ["wdir"]);
  const speed = firstText(metar, ["wspd"]);
  const gust = firstText(metar, ["wgst"]);
  if (speed) {
    const windDirection = direction ? (direction === "VRB" ? "variable" : `${direction}°`) : "";
    values.push({ label: `Wind ${windDirection ? `${windDirection} at ` : ""}${speed} kt${gust ? ` gusting ${gust} kt` : ""}` });
  }
  const visibility = firstText(metar, ["visib"]);
  if (visibility) values.push({ label: `Visibility ${visibility} SM` });
  const weather = firstText(metar, ["wxString"]);
  if (weather) values.push({ label: `Weather ${weather}` });
  const clouds = Array.isArray(metar.clouds) ? metar.clouds : [];
  const cloudNames = { FEW: "Few", SCT: "Scattered", BKN: "Broken", OVC: "Overcast", VV: "Vertical visibility" };
  const layers = clouds.map((layer) => {
    const cover = String(layer?.cover || "").toUpperCase();
    if (!cover) return "";
    const base = layer.base ?? (cover === "VV" ? layer.vertVis : null);
    const height = base === null || base === undefined || !Number.isFinite(Number(base)) ? "" : ` ${Math.round(Number(base)).toLocaleString()} ft`;
    return `${cloudNames[cover] || cover}${height}`;
  }).filter(Boolean);
  if (layers.length) values.push({ label: `Clouds ${layers.join(" · ")}` });
  const ceiling = clouds
    .filter((layer) => ["BKN", "OVC", "VV"].includes(String(layer?.cover || "").toUpperCase()))
    .map((layer) => {
      const base = layer.base ?? layer.vertVis;
      return base === null || base === undefined ? NaN : Number(base);
    })
    .filter((base) => Number.isFinite(base) && base >= 0)
    .sort((left, right) => left - right)[0];
  if (ceiling !== undefined) values.push({ label: `Ceiling ${Math.round(ceiling).toLocaleString()} ft AGL` });
  const temperature = firstText(metar, ["temp"]);
  const dewpoint = firstText(metar, ["dewp"]);
  if (temperature) values.push({ label: `Temp ${temperature}°C${dewpoint ? ` / dew ${dewpoint}°C` : ""}` });
  const altimeter = Number(metar.altim);
  if (Number.isFinite(altimeter) && altimeter > 0) {
    values.push({ label: `Altimeter ${(altimeter / 33.8638866667).toFixed(2)} inHg / ${altimeter.toFixed(1)} hPa` });
  }
  for (const value of values) {
    const chip = document.createElement("span");
    chip.className = value.category ? `weather-chip flight-${category.toLowerCase()}` : "weather-chip";
    chip.textContent = value.label;
    details.append(chip);
  }
}
const tafTimeFormatter = new Intl.DateTimeFormat("en-GB", {
  day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit",
  hourCycle: "h23", timeZone: "UTC"
});

function formatTAFTime(value) {
  const seconds = Number(value);
  if (!Number.isFinite(seconds)) return "";
  const date = new Date(seconds * 1000);
  if (Number.isNaN(date.valueOf())) return "";
  const parts = Object.fromEntries(tafTimeFormatter.formatToParts(date).map((part) => [part.type, part.value]));
  return `${parts.day} ${parts.month} ${parts.year} ${parts.hour}:${parts.minute}Z`;
}

function renderTAFDetails(taf) {
  const list = document.querySelector("#taf-periods");
  const status = document.querySelector("#taf-period-status");
  list.replaceChildren();
  const periods = (Array.isArray(taf?.fcsts) ? taf.fcsts : [])
    .map((period, index) => ({ period, index, from: Number(period?.timeFrom), to: Number(period?.timeTo) }))
    .filter(({ period, from, to }) => period?.timeFrom !== null && period?.timeFrom !== undefined
      && String(period.timeFrom).trim() !== "" && period?.timeTo !== null && period?.timeTo !== undefined
      && String(period.timeTo).trim() !== "" && Number.isFinite(from) && Number.isFinite(to) && from <= to
      && Number.isFinite(new Date(from * 1000).valueOf()) && Number.isFinite(new Date(to * 1000).valueOf()))
    .sort((left, right) => left.from - right.from || left.index - right.index);
  if (periods.length === 0) {
    list.hidden = true;
    status.textContent = "No decoded forecast periods are available; consult the raw TAF.";
    return;
  }
  const coverNames = { FEW: "Few", SCT: "Scattered", BKN: "Broken", OVC: "Overcast", VV: "Vertical visibility", SKC: "Sky clear", CLR: "Clear", NSC: "No significant clouds", NCD: "No cloud detected" };
  for (const { period, from, to } of periods) {
    const item = document.createElement("li");
    item.className = "taf-period";
    const heading = document.createElement("div");
    heading.className = "taf-period-heading";
    const time = document.createElement("time");
    time.dateTime = new Date(from * 1000).toISOString();
    time.textContent = `${formatTAFTime(from)} – ${formatTAFTime(to)}`;
    heading.append(time);
    const change = firstText(period, ["fcstChange"]).toUpperCase();
    const probability = Number(period.probability);
    const markers = [];
    if (change) markers.push(change);
    if (Number.isFinite(probability) && probability > 0) markers.push(`PROB ${probability}%`);
    if (markers.length) {
      const marker = document.createElement("span");
      marker.className = "taf-period-marker";
      marker.textContent = markers.join(" · ");
      heading.append(marker);
    }
    item.append(heading);
    const details = document.createElement("div");
    details.className = "weather-details taf-period-details";
    const values = [];
    const direction = firstText(period, ["wdir"]);
    const speed = firstText(period, ["wspd"]);
    const gust = firstText(period, ["wgst"]);
    if (speed) values.push(`Wind ${direction ? `${direction === "VRB" ? "VRB" : `${direction}°`} ` : ""}${speed} kt${gust ? ` gusting ${gust} kt` : ""}`);
    const visibility = firstText(period, ["visib"]);
    if (visibility) values.push(`Visibility ${visibility} SM`);
    const weather = firstText(period, ["wxString"]);
    if (weather) values.push(`Weather ${weather}`);
    const clouds = Array.isArray(period.clouds) ? period.clouds : [];
    const cloudLayers = clouds.map((cloud) => {
      const cover = String(cloud?.cover || "").toUpperCase();
      if (!cover) return "";
      const base = cloud.base;
      const height = base === null || base === undefined || !Number.isFinite(Number(base)) ? "" : ` ${Math.round(Number(base)).toLocaleString()} ft`;
      return `${coverNames[cover] || cover}${height}`;
    }).filter(Boolean);
    if (cloudLayers.length) values.push(`Clouds ${cloudLayers.join(" · ")}`);
    for (const label of values) {
      const chip = document.createElement("span");
      chip.className = "weather-chip";
      chip.textContent = label;
      details.append(chip);
    }
    if (values.length) item.append(details);
    list.append(item);
  }
  list.hidden = false;
  status.textContent = `${periods.length} decoded forecast period${periods.length === 1 ? "" : "s"} shown in UTC. Change groups and probabilities follow AviationWeather.gov.`;
}


function renderPIREPs(reports) {
  const section = document.querySelector("#pirep-section");
  const list = document.querySelector("#pirep-list");
  list.replaceChildren();
  section.hidden = false;
  document.querySelector("#pirep-status").textContent = reports.length ? `${reports.length} report${reports.length === 1 ? "" : "s"} · AviationWeather.gov; coverage is limited outside the U.S. and North Atlantic.` : "No reports returned. Coverage is limited outside the U.S. and North Atlantic; that does not confirm no pilot reports exist.";
  for (const report of reports) {
    const item = document.createElement("li");
    item.className = "pirep-item";
    const time = document.createElement("time");
    time.textContent = formatReportTime(report.obsTime || report.reportTime || report.receiptTime);
    const text = document.createElement("p");
    text.textContent = firstText(report, ["rawOb"]) || "Pilot report details unavailable.";
    item.append(time, text);
    list.append(item);
  }
}

let sigmetIndex = null;
const sigmetStorageKey = "fp-sigmet-index";
let selectedAirport = null;
let currentFavoriteStation = "";
let airspaceData = null;
let airspaceState = "unavailable";
let tfrData = null;
let tfrState = "unavailable";
let tfrLoadStarted = false;
let airspaceRequestKey = "";
let airportDirectoryCache;
let airportDirectoryLoadPromise = null;

function projectSIGMETCoordinate(coordinate, airport) {
  if (coordinate?.lat === null || coordinate?.lat === undefined || coordinate?.lon === null || coordinate?.lon === undefined) return null;
  const latitude = Number(coordinate.lat);
  const longitude = Number(coordinate.lon);
  const centerLatitude = Number(airport.lat);
  const centerLongitude = Number(airport.lon);
  if (![latitude, longitude, centerLatitude, centerLongitude].every(Number.isFinite) || Math.abs(latitude) > 90 || Math.abs(centerLatitude) > 90 || Math.abs(longitude) > 180 || Math.abs(centerLongitude) > 180) return null;
  const longitudeDelta = ((longitude - centerLongitude + 540) % 360) - 180;
  return {
    x: longitudeDelta * 60 * Math.cos(centerLatitude * Math.PI / 180),
    y: (latitude - centerLatitude) * 60
  };
}

function distanceFromOriginToSegmentSquared(start, end) {
  const dx = end.x - start.x;
  const dy = end.y - start.y;
  const lengthSquared = dx * dx + dy * dy;
  const fraction = lengthSquared === 0 ? 0 : Math.max(0, Math.min(1, -(start.x * dx + start.y * dy) / lengthSquared));
  const x = start.x + fraction * dx;
  const y = start.y + fraction * dy;
  return x * x + y * y;
}

function sigmetWithinRange(report, airport, radiusNM = 500) {
  const coordinates = Array.isArray(report.coords) ? report.coords : [];
  const points = coordinates.map((coordinate) => projectSIGMETCoordinate(coordinate, airport));
  if (points.length < 2 || points.some((point) => point === null)) return false;
  const area = report.geom === "AREA" && points.length >= 3;
  if (area) {
    let inside = false;
    for (let i = 0, j = points.length - 1; i < points.length; j = i++) {
      const a = points[i];
      const b = points[j];
      if ((a.y > 0) !== (b.y > 0) && 0 < (b.x - a.x) * -a.y / (b.y - a.y) + a.x) inside = !inside;
    }
    if (inside) return true;
  }
  const radiusSquared = radiusNM * radiusNM + 1e-6;
  for (const point of points) {
    if (point.x * point.x + point.y * point.y <= radiusSquared) return true;
  }
  const segmentCount = area ? points.length : points.length - 1;
  for (let i = 0; i < segmentCount; i++) {
    if (distanceFromOriginToSegmentSquared(points[i], points[(i + 1) % points.length]) <= radiusSquared) return true;
  }
  return false;
}

function polygonWithinRange(points, radiusNM) {
  if (points.length < 3 || points.some((point) => !point)) return false;
  const radiusSquared = radiusNM * radiusNM + 1e-6;
  let inside = false;
  for (let i = 0, j = points.length - 1; i < points.length; j = i++) {
    const a = points[i];
    const b = points[j];
    if ((a.y > 0) !== (b.y > 0) && 0 < (b.x - a.x) * -a.y / (b.y - a.y) + a.x) inside = !inside;
    if (a.x * a.x + a.y * a.y <= radiusSquared) return true;
    if (distanceFromOriginToSegmentSquared(a, b) <= radiusSquared) return true;
  }
  return inside;
}

function airspacePolygonRings(feature) {
  if (!feature || feature.type !== "Feature" || !feature.geometry) return [];
  const geometry = feature.geometry;
  if (geometry.type === "Polygon") return [geometry.coordinates];
  if (geometry.type === "MultiPolygon") return geometry.coordinates;
  return [];
}

function drawTFRs(appendSVG, airport, center, scale) {
  let polygonCount = 0;
  const nearby = new Map();
  for (const feature of tfrData?.features || []) {
    const geometry = feature?.geometry;
    const polygons = geometry?.type === "Polygon" ? [geometry.coordinates] : geometry?.type === "MultiPolygon" ? geometry.coordinates : [];
    let featureVisible = false;
    for (const polygon of polygons) {
      const ring = polygon?.[0];
      if (!Array.isArray(ring) || ring.length < 3) continue;
      const points = ring.map((coordinate) => Array.isArray(coordinate) && coordinate.length >= 2
        ? projectSIGMETCoordinate({ lon: coordinate[0], lat: coordinate[1] }, airport) : null);
      if (!polygonWithinRange(points, 500)) continue;
      const d = points.map((point, index) => `${index ? "L" : "M"} ${(center.x + point.x * scale).toFixed(2)} ${(center.y - point.y * scale).toFixed(2)}`).join(" ") + " Z";
      const shape = appendSVG("path", { d, class: "tfr-shape" });
      const properties = feature.properties || {};
      appendSVG("title", {}, shape).textContent = [properties.TITLE, properties.NOTAM_KEY, properties.LAST_MODIFICATION_DATETIME].filter(Boolean).join(" · ") || "FAA current TFR layer";
      featureVisible = true;
      polygonCount++;
    }
    if (featureVisible) {
      const properties = feature.properties || {};
      const key = properties.NOTAM_KEY || feature.id || properties.TITLE || String(nearby.size);
      if (!nearby.has(key)) nearby.set(key, properties);
    }
  }
  const list = document.querySelector("#tfr-list");
  list.replaceChildren();
  const records = [...nearby.values()].sort((a, b) => String(a.TITLE || "").localeCompare(String(b.TITLE || "")));
  for (const properties of records.slice(0, 20)) {
    const item = document.createElement("li");
    const title = document.createElement("strong");
    title.textContent = properties.TITLE || "FAA current TFR location";
    item.append(title);
    const details = [properties.NOTAM_KEY, properties.LAST_MODIFICATION_DATETIME ? `Last modified ${properties.LAST_MODIFICATION_DATETIME}` : ""].filter(Boolean);
    if (details.length) {
      const meta = document.createElement("span");
      meta.textContent = details.join(" · ");
      item.append(meta);
    }
    list.append(item);
  }
  if (!records.length) {
    const item = document.createElement("li");
    item.textContent = "No FAA layer polygons shown within this map range.";
    list.append(item);
  } else if (records.length > 20) {
    const item = document.createElement("li");
    item.textContent = `${records.length - 20} additional FAA layer locations not listed.`;
    list.append(item);
  }
  return { polygonCount, featureCount: records.length };
}

function drawAirspace(appendSVG, airport, center, scale) {
  let count = 0;
  if (!airspaceData || airspaceData.type !== "FeatureCollection" || !Array.isArray(airspaceData.features)) return count;
  for (const feature of airspaceData.features) {
    for (const polygon of airspacePolygonRings(feature)) {
      if (!Array.isArray(polygon)) continue;
      for (const ring of polygon) {
        if (!Array.isArray(ring) || ring.length < 3) continue;
        const points = ring.map((coordinate) => Array.isArray(coordinate) && coordinate.length >= 2
          ? projectSIGMETCoordinate({ lon: coordinate[0], lat: coordinate[1] }, airport) : null);
        if (points.some((point) => !point)) continue;
        if (points.every((point) => point.x * point.x + point.y * point.y > 250 * 250)) continue;
        const d = points.map((point, index) => `${index ? "L" : "M"} ${(center.x + point.x * scale).toFixed(2)} ${(center.y - point.y * scale).toFixed(2)}`).join(" ") + " Z";
        const shape = appendSVG("path", { d, class: "airspace-shape" });
        const properties = feature.properties || {};
        appendSVG("title", {}, shape).textContent = [properties.NAME, properties.IDENT, properties.LOWER_DESC, properties.UPPER_DESC].filter(Boolean).join(" · ") || "FAA class airspace";
        count++;
      }
    }
  }
  return count;
}

async function loadTFRs() {
  try {
    const response = await fetch("/api/v1/tfr", { headers: { Accept: "application/geo+json, application/json" } });
    const data = await response.json();
    if (!response.ok || data.type !== "FeatureCollection" || !Array.isArray(data.features)) throw new Error("Invalid TFR response");
    tfrData = data;
    tfrState = response.headers.get("X-FP-Offline-Cache") === "true" || !navigator.onLine ? "offline cached copy" : data.properties?.stale ? "stale provider cache" : "FAA provider response";
  } catch {
    tfrState = "unavailable";
  }
  if (sigmetIndex?.items) drawSIGMETMap(sigmetIndex.items.filter((report) => Number(report.validTimeFrom) <= Date.now() / 1000 && Date.now() / 1000 < Number(report.validTimeTo)));
}

async function loadAirspace(airport) {
  const lat = Number(airport?.lat), lon = Number(airport?.lon);
  if (!Number.isFinite(lat) || !Number.isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) return;
  const key = `${lat},${lon}`;
  if (key === airspaceRequestKey) return;
  airspaceRequestKey = key;
  airspaceData = null;
  airspaceState = "loading";
  const cacheName = "fp-faa-airspace-v1";
  const cacheKey = new Request(`/api/v1/airspace?lat=${encodeURIComponent(lat)}&lon=${encodeURIComponent(lon)}`);
  let cached = null;
  try {
    if ("caches" in window) cached = await (await caches.open(cacheName)).match(cacheKey);
    const response = await fetch(cacheKey, { headers: { Accept: "application/geo+json, application/json" } });
    const data = await response.json();
    if (!response.ok || data.type !== "FeatureCollection" || !Array.isArray(data.features)) throw new Error("Invalid airspace response");
    airspaceData = data;
    airspaceState = response.headers.get("X-FP-Offline-Cache") === "true" || !navigator.onLine
      ? "cached"
      : data.properties?.stale ? "stale" : "fresh";
    if ("caches" in window) {
      const cache = await caches.open(cacheName);
      await Promise.all((await cache.keys()).map((request) => cache.delete(request)));
      await cache.put(cacheKey, new Response(JSON.stringify(data), { headers: { "Content-Type": "application/geo+json" } }));
    }
  } catch {
    try {
      if (!cached && "caches" in window) cached = await (await caches.open(cacheName)).match(cacheKey);
      const data = cached && await cached.json();
      if (data?.type === "FeatureCollection" && Array.isArray(data.features)) {
        airspaceData = data;
        airspaceState = "cached";
      } else airspaceState = "unavailable";
    } catch { airspaceState = "unavailable"; }
  }
  if (sigmetIndex?.items) drawSIGMETMap(sigmetIndex.items.filter((report) => Number(report.validTimeFrom) <= Date.now() / 1000 && Date.now() / 1000 < Number(report.validTimeTo)));
}

function drawSIGMETMap(reports) {
  const shell = document.querySelector("#sigmet-map-shell");
  const map = document.querySelector("#sigmet-map");
  const status = document.querySelector("#sigmet-map-status");
  const latitude = Number(selectedAirport?.lat);
  const longitude = Number(selectedAirport?.lon);
  if (!Number.isFinite(latitude) || !Number.isFinite(longitude) || Math.abs(latitude) > 90 || Math.abs(longitude) > 180) {
    shell.hidden = true;
    return null;
  }

  shell.hidden = false;
  map.replaceChildren();
  const namespace = "http://www.w3.org/2000/svg";
  const center = { x: 380, y: 270 };
  const scale = 0.38;
  const appendSVG = (name, attributes, parent = map) => {
    const element = document.createElementNS(namespace, name);
    for (const [key, value] of Object.entries(attributes)) element.setAttribute(key, String(value));
    parent.append(element);
    return element;
  };

  for (const radius of [100, 250, 500]) {
    appendSVG("circle", { cx: center.x, cy: center.y, r: radius * scale, class: "sigmet-range-ring" });
    const label = appendSVG("text", { x: center.x + 5, y: center.y - radius * scale + 13, class: "sigmet-range-label" });
    label.textContent = `${radius} NM`;
  }
  const tfrResult = drawTFRs(appendSVG, selectedAirport, center, scale);
  const airspaceCount = drawAirspace(appendSVG, selectedAirport, center, scale);
  let nearbyCount = 0;
  for (const report of reports) {
    if (!sigmetWithinRange(report, selectedAirport)) continue;
    const points = report.coords.map((coordinate) => projectSIGMETCoordinate(coordinate, selectedAirport));
    const isArea = report.geom === "AREA" && points.length >= 3;
    const path = points.map((point, index) => `${index === 0 ? "M" : "L"} ${(center.x + point.x * scale).toFixed(2)} ${(center.y - point.y * scale).toFixed(2)}`).join(" ");
    const hazardClass = firstText(report, ["hazard"]).toLowerCase().replace(/[^a-z0-9-]/g, "") || "other";
    const shape = appendSVG("path", {
      d: `${path}${isArea ? " Z" : ""}`,
      class: `sigmet-shape sigmet-hazard-${hazardClass}${isArea ? " sigmet-area" : ""}`
    });
    const title = appendSVG("title", {}, shape);
    title.textContent = `${firstText(report, ["hazard"])} · ${firstText(report, ["firId", "firName"])}`;
    nearbyCount++;
  }
  let nearbyAirportCount = 0;
  const airportRecords = airportDirectoryCache?.airports;
  if (airportRecords) {
    const selectedCode = firstText(selectedAirport, ["ident", "icaoId"]).toUpperCase();
    const nearbyRadiusSquared = 250 * 250;
    for (const ident in airportRecords) {
      if (!Object.prototype.hasOwnProperty.call(airportRecords, ident)) continue;
      const airport = airportRecords[ident];
      const code = firstText(airport, ["icaoId", "ident"]) || ident;
      if (code.toUpperCase() === selectedCode) continue;
      const point = projectSIGMETCoordinate(airport, selectedAirport);
      if (!point || point.x * point.x + point.y * point.y > nearbyRadiusSquared) continue;
      const marker = appendSVG("circle", {
        cx: center.x + point.x * scale,
        cy: center.y - point.y * scale,
        r: 3,
        class: "sigmet-nearby-airport-marker"
      });
      const title = appendSVG("title", {}, marker);
      title.textContent = `${code} · ${firstText(airport, ["name"]) || "Airport"} · OurAirports reference`;
      nearbyAirportCount++;
    }
  }
  let nearbyNavaidCount = 0;
  for (const navaid of airportDirectoryCache?.navaids || []) {
    const point = projectSIGMETCoordinate(navaid, selectedAirport);
    if (!point || point.x * point.x + point.y * point.y > 250 * 250) continue;
    const x = center.x + point.x * scale;
    const y = center.y - point.y * scale;
    const marker = appendSVG("path", {
      d: `M ${x} ${y - 4} L ${x + 4} ${y + 4} L ${x - 4} ${y + 4} Z`,
      class: "sigmet-navaid-marker"
    });
    const title = appendSVG("title", {}, marker);
    title.textContent = `${firstText(navaid, ["ident"]) || "Navaid"} · ${firstText(navaid, ["type"])} · ${firstText(navaid, ["frequency"]) || "frequency unavailable"} · OurAirports reference`;
    nearbyNavaidCount++;
  }
  appendSVG("circle", { cx: center.x, cy: center.y, r: 6, class: "sigmet-airport-marker" });
  const airportTitle = appendSVG("title", {}, map.lastElementChild);
  airportTitle.textContent = `${firstText(selectedAirport, ["icaoId", "name"]) || "Selected airport"} · map center`;
  map.setAttribute("aria-label", `Schematic map centered on ${firstText(selectedAirport, ["icaoId", "ident", "name"]) || "selected airport"}, with ${tfrResult.polygonCount} FAA TFR polygons, ${nearbyCount} active SIGMET areas, ${airspaceCount} FAA airspace outlines, ${nearbyAirportCount} reference airports and ${nearbyNavaidCount} navigation aids`);
  const fetchedAt = airspaceData?.properties?.fetchedAt;
  const airspaceNote = airspaceData
    ? ` ${airspaceCount} FAA airspace polygon rings shown; ${airspaceState === "cached" ? "offline cached copy" : airspaceState === "stale" ? "stale provider data" : "provider response"}${fetchedAt ? ` fetched ${formatReportTime(fetchedAt)}` : ""}. FAA coverage: U.S., Puerto Rico, and U.S. Virgin Islands only. Reference only—not for navigation or clearances.`
    : ` FAA airspace ${airspaceState === "loading" ? "loading" : "unavailable"}; no overlay shown. FAA coverage is limited to the U.S., Puerto Rico, and U.S. Virgin Islands. Reference only—not for navigation or clearances.`;
  const tfrFetchedAt = tfrData?.properties?.fetchedAt;
  const tfrStatus = tfrData ? ` ${tfrResult.featureCount} FAA TFR location(s) in the FAA current layer within 500 NM; ${tfrState}; fetched ${formatReportTime(tfrFetchedAt)}.` : " FAA TFR overlay unavailable; verify directly with FAA.";
  status.textContent = `${nearbyCount} active SIGMET${nearbyCount === 1 ? "" : "s"} with mapped geometry within 500 NM. ${airportRecords ? `${nearbyAirportCount} OurAirports reference airports and ${nearbyNavaidCount} radio navigation aids within 250 NM.` : "Save the airport directory to add nearby airport and navigation-aid reference points."}${tfrStatus}${airspaceNote}`;
  return nearbyCount;
}


function renderSIGMETs(index, local = false) {
  sigmetIndex = index;
  const panel = document.querySelector("#sigmet-panel");
  const filter = document.querySelector("#sigmet-filter");
  const list = document.querySelector("#sigmet-list");
  const query = filter.value.trim().toLowerCase();
  const now = Date.now() / 1000;
  const active = (index.items || []).filter((report) => {
    const from = Number(report.validTimeFrom);
    const to = Number(report.validTimeTo);
    return Number.isFinite(from) && Number.isFinite(to) && from <= now && now < to;
  });
  const matches = active.filter((report) => {
    const searchable = ["firId", "firName", "hazard", "qualifier", "sigmetType", "rawSigmet"]
      .map((key) => firstText(report, [key])).join(" ").toLowerCase();
    return !query || searchable.includes(query);
  });
  list.replaceChildren();
  for (const report of matches) {
    const item = document.createElement("li");
    item.className = "sigmet-item";
    const title = document.createElement("h4");
    title.className = "sigmet-title";
    title.textContent = [
      firstText(report, ["hazard"]),
      firstText(report, ["qualifier"]),
      firstText(report, ["firId", "firName"]),
      firstText(report, ["sigmetType"])
    ].filter(Boolean).join(" · ") || "SIGMET";
    const validity = document.createElement("time");
    validity.className = "sigmet-validity";
    validity.textContent = `Valid ${formatReportTime(Number(report.validTimeFrom) * 1000)} – ${formatReportTime(Number(report.validTimeTo) * 1000)}`;
    item.append(title, validity);
    const raw = firstText(report, ["rawSigmet"]);
    if (raw) {
      const message = document.createElement("pre");
      message.textContent = raw;
      item.append(message);
    }
    list.append(item);
  }
  const cacheStatus = local || index.stale ? "Cached · possibly stale. " : "";
  const status = document.querySelector("#sigmet-status");
  const mappedCount = drawSIGMETMap(active);
  const mapSummary = mappedCount === null ? "Airport coordinates unavailable; map omitted." : `${mappedCount} mapped within 500 NM.`;
  status.textContent = active.length
    ? `${cacheStatus}${active.length} currently valid worldwide SIGMET${active.length === 1 ? "" : "s"}${query ? ` · ${matches.length} match your filter` : ""}. ${mapSummary} Verify the latest official briefing.`
    : `${cacheStatus}No currently valid SIGMETs returned by the global feed. ${mapSummary} This does not replace local advisories or official preflight briefing.`;
  const fetched = index.fetchedAt ? new Date(index.fetchedAt) : null;
  document.querySelector("#sigmet-updated").textContent = fetched && !Number.isNaN(fetched.valueOf()) ? `Feed fetched ${fetched.toLocaleString()}` : "AviationWeather.gov";
  filter.disabled = false;
  panel.hidden = false;
}

async function loadSIGMETs() {
  const panel = document.querySelector("#sigmet-panel");
  panel.hidden = false;
  document.querySelector("#sigmet-status").textContent = "Loading worldwide SIGMET feed…";
  try {
    await getSavedAirportDirectory();
    renderAirportOperations(selectedAirport || {});
    const response = await fetch("/api/v1/sigmets", { headers: { Accept: "application/json" } });
    const index = await response.json();
    if (!response.ok) throw new Error(index.error || `SIGMET feed failed (${response.status})`);
    localStorage.setItem(sigmetStorageKey, JSON.stringify(index));
    renderSIGMETs(index);
  } catch {
    const saved = localStorage.getItem(sigmetStorageKey);
    if (saved) {
      try {
        renderSIGMETs(JSON.parse(saved), true);
      } catch {
        document.querySelector("#sigmet-status").textContent = "Saved SIGMET data could not be read.";
      }
    } else {
      document.querySelector("#sigmet-status").textContent = "Connect to load the global SIGMET feed.";
      drawSIGMETMap([]);
    }
  }
}

function renderAirportOperations(airport) {
  const runwayList = document.querySelector("#runway-list");
  const frequencyList = document.querySelector("#frequency-list");
  runwayList.replaceChildren();
  frequencyList.replaceChildren();
  const runways = Array.isArray(airport.runways) ? airport.runways.filter((runway) => runway.id) : [];
  for (const runway of runways) {
    const item = document.createElement("li");
    const headings = [];
    if (runway.hasLeHeading) headings.push(`${runway.leHeadingDegT}°T`);
    if (runway.hasHeHeading) headings.push(`${runway.heHeadingDegT}°T`);
    const details = [runway.id, runway.dimension, runway.surface ? `Surface ${runway.surface}` : ""];
    if (headings.length) details.push(`True headings ${headings.join(" / ")}`);
    if (runway.lightingKnown) details.push(runway.lighted ? "Lighting listed" : "Unlighted per reference");
    if (runway.closureKnown) details.push(runway.closed ? "Marked closed by reference" : "Not marked closed by reference");
    item.textContent = details.filter(Boolean).join(" · ");
    runwayList.append(item);
  }
  const frequencies = firstText(airport, ["freqs"]).split(";").map((entry) => {
    const [name, ...values] = entry.split(",");
    return { name: name.trim(), value: values.join(",").trim() };
  }).filter((entry) => entry.name && entry.value);
  for (const frequency of frequencies) {
    const item = document.createElement("li");
    item.textContent = `${frequency.name} · ${frequency.value}`;
    frequencyList.append(item);
  }
  const nearbyNavaids = (airportDirectoryCache?.navaids || []).map((navaid) => {
    const point = projectSIGMETCoordinate(navaid, airport);
    return point ? { navaid, distance: Math.hypot(point.x, point.y) } : null;
  }).filter((entry) => entry && entry.distance <= 250).sort((a, b) => a.distance - b.distance).slice(0, 12);
  const navaidList = document.querySelector("#navaid-list");
  navaidList.replaceChildren();
  for (const { navaid, distance } of nearbyNavaids) {
    const item = document.createElement("li");
    item.textContent = [firstText(navaid, ["ident"]), firstText(navaid, ["name"]), firstText(navaid, ["type"]), firstText(navaid, ["frequency"]), `${Math.round(distance)} NM`].filter(Boolean).join(" · ");
    navaidList.append(item);
  }
  document.querySelector("#runway-group").hidden = runways.length === 0;
  document.querySelector("#frequency-group").hidden = frequencies.length === 0;
  document.querySelector("#navaid-group").hidden = nearbyNavaids.length === 0;
  document.querySelector("#airport-operations").hidden = runways.length === 0 && frequencies.length === 0 && nearbyNavaids.length === 0;
}

function show(bundle, local = false) {
  const airport = bundle.airport?.[0] || {};
  selectedAirport = airport;
  if (!tfrLoadStarted) {
    tfrLoadStarted = true;
    void loadTFRs();
  }
  const airspaceLocationKey = `${Number(airport.lat)},${Number(airport.lon)}`;
  if (airspaceLocationKey !== airspaceRequestKey) {
    airspaceRequestKey = "";
    airspaceData = null;
    airspaceState = "unavailable";
    void loadAirspace(airport);
  }
  const metar = bundle.metar?.[0] || {};
  const taf = bundle.taf?.[0] || {};
  const name = firstText(airport, ["name", "airportName", "siteName"]);
  const station = bundle.station || stationInput.value.toUpperCase();
  document.querySelector("#airport-title").textContent = name ? `${station} · ${name}` : station;
  currentFavoriteStation = String(station).toUpperCase();
  updateFavoriteAirportButton();
  document.querySelector("#source").textContent = bundle.source || "SAVED ON THIS DEVICE";
  const referenceOnly = bundle.source?.startsWith("OurAirports");
  const airportDataNote = document.querySelector("#airport-data-note");
  airportDataNote.hidden = !referenceOnly;
  airportDataNote.textContent = referenceOnly
    ? "Static OurAirports reference data only. It includes no live METAR/TAF, and its airport, runway, frequency, and radio-navigation-aid records have no accuracy guarantee. Verify all operational details against current official sources."
    : "";

  document.querySelector("#metar").textContent = firstText(metar, ["rawOb", "raw_text"]) || "No METAR available for this station.";
  document.querySelector("#taf").textContent = firstText(taf, ["rawTAF", "raw_text"]) || "No TAF available for this station.";
  document.querySelector("#metar-time").textContent = formatReportTime(metar.obsTime || metar.observation_time);
  document.querySelector("#taf-time").textContent = formatReportTime(taf.issueTime || taf.issue_time);
  renderTAFDetails(taf);
  renderMETARDetails(metar);
  renderPIREPs(bundle.pirep || []);

  const details = document.querySelector("#airport-details");
  details.replaceChildren();
  const country = firstText(airport, ["country"]).toUpperCase();
  const fields = [
    ["IATA", ["iataId"]],
    ...(country === "US" ? [["State", ["state"]]] : []),
    ...(country ? [["Country", ["country"]]] : []),
    ["Elevation", ["elev"]]
  ];
  for (const [label, keys] of fields) {
    const value = firstText(airport, keys);
    if (!value) continue;
    const chip = document.createElement("span");
    chip.className = "detail-chip";
    chip.textContent = `${label}: ${value}`;
    details.append(chip);
  }
  const latitude = formatCoordinate(airport.lat, "N", "S", 90);
  const longitude = formatCoordinate(airport.lon, "E", "W", 180);
  if (latitude && longitude) {
    const chip = document.createElement("span");
    chip.className = "detail-chip";
    chip.textContent = `Coordinates: ${latitude} ${longitude}`;
    details.append(chip);
  }
  renderAirportOperations(airport);
  const hasFAACoverage = configureRegion(country, station, firstText(airport, ["icaoId"]));

  const updated = bundle.fetchedAt ? new Date(bundle.fetchedAt) : null;
  const status = local || bundle.stale ? "Cached · possibly stale" : bundle.cached ? "Saved · refreshed recently" : "Current response";
  const freshness = document.querySelector("#freshness");
  freshness.textContent = updated && !Number.isNaN(updated.valueOf()) ? `${status} · data fetched ${updated.toLocaleString()}` : status;
  freshness.classList.toggle("stale", local || bundle.stale);
  briefing.hidden = false;
  void refreshOfflineChartLibrary();
  return hasFAACoverage;
}

const favoriteAirportsKey = "fp-favorite-airports-v1";

function getFavoriteAirports() {
  try {
    const favorites = JSON.parse(localStorage.getItem(favoriteAirportsKey) || "[]");
    if (!Array.isArray(favorites)) return [];
    return favorites.filter((favorite) =>
      favorite &&
      typeof favorite.station === "string" &&
      /^[A-Z0-9]{3,4}$/.test(favorite.station) &&
      (favorite.name === undefined || typeof favorite.name === "string")
    );
  } catch {
    return [];
  }
}

function updateFavoriteAirportButton() {
  const button = document.querySelector("#favorite-airport");
  const validStation = /^[A-Z0-9]{3,4}$/.test(currentFavoriteStation);
  const isFavorite = validStation && getFavoriteAirports().some((favorite) => favorite.station === currentFavoriteStation);
  button.disabled = !validStation;
  button.textContent = isFavorite ? "Remove favorite" : "Save airport";
  button.setAttribute("aria-pressed", String(isFavorite));
}

function refreshFavoriteAirports() {
  const list = document.querySelector("#saved-airports-list");
  const status = document.querySelector("#saved-airports-status");
  const favorites = getFavoriteAirports();
  list.replaceChildren();
  for (const favorite of favorites) {
    const item = document.createElement("li");
    const open = document.createElement("button");
    open.type = "button";
    open.className = "saved-airport-open";
    open.textContent = favorite.name ? `${favorite.station} · ${favorite.name}` : favorite.station;
    open.addEventListener("click", () => {
      stationInput.value = favorite.station;
      lookup(favorite.station);
    });
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "secondary-button";
    remove.textContent = "Remove";
    remove.setAttribute("aria-label", `Remove ${favorite.station} from saved airports`);
    remove.addEventListener("click", () => {
      try {
        const remaining = getFavoriteAirports().filter((entry) => entry.station !== favorite.station);
        localStorage.setItem(favoriteAirportsKey, JSON.stringify(remaining));
        refreshFavoriteAirports();
        status.textContent = `${favorite.station} removed from saved airports.`;
      } catch {
        status.textContent = "Could not update saved airports in this browser.";
      }
    });
    item.append(open, remove);
    list.append(item);
  }
  status.textContent = favorites.length
    ? `${favorites.length} airport shortcut${favorites.length === 1 ? "" : "s"} saved on this device. Selecting one tries live data, then this device's saved briefing or airport reference.`
    : "No favorites saved. Save an airport from its briefing to add an offline shortcut.";
  updateFavoriteAirportButton();
}

document.querySelector("#favorite-airport").addEventListener("click", () => {
  const status = document.querySelector("#saved-airports-status");
  try {
    const favorites = getFavoriteAirports();
    const existing = favorites.findIndex((favorite) => favorite.station === currentFavoriteStation);
    const added = existing < 0;
    if (added) {
      favorites.unshift({
        station: currentFavoriteStation,
        name: firstText(selectedAirport || {}, ["name", "airportName", "siteName"])
      });
    } else {
      favorites.splice(existing, 1);
    }
    localStorage.setItem(favoriteAirportsKey, JSON.stringify(favorites));
    refreshFavoriteAirports();
    document.querySelector("#saved-airports-panel").open = added;
    status.textContent = added
      ? `${currentFavoriteStation} saved as an airport shortcut on this device.`
      : `${currentFavoriteStation} removed from saved airports.`;
  } catch {
    status.textContent = "Could not update saved airports in this browser.";
  }
});
const procedurePrefix = "fp-procedures:";

function renderProcedures(index, local = false) {
  const items = document.querySelector("#procedure-list");
  items.replaceChildren();
  const cycle = document.querySelector("#procedure-cycle");
  cycle.textContent = `FAA d-TPP cycle ${index.cycle}${index.effective ? ` · ${index.effective}` : ""}${index.stale || local ? " · cached; check current cycle" : ""}`;
  document.querySelector("#procedure-status").textContent = index.items?.length ? `${index.items.length} procedures. Opening a PDF attempts to save it in this browser for offline access; storage may be limited or evicted.` : "No procedures listed for this station.";
  for (const procedure of index.items || []) {
    const item = document.createElement("li");
    item.className = "procedure-item";
    const name = document.createElement("div");
    name.className = "procedure-name";
    const title = document.createElement("strong");
    title.textContent = procedure.name;
    const category = document.createElement("span");
    category.textContent = procedure.type;
    name.append(title, category);
    const link = document.createElement("a");
    link.className = "procedure-link";
    link.href = `/api/v1/procedures/${encodeURIComponent(index.station)}/files/${encodeURIComponent(procedure.file)}`;
    link.target = "_blank";
    link.rel = "noreferrer";
    link.textContent = procedure.downloaded ? "Open saved PDF ↗" : "Open / save ↗";
    item.append(name, link);
    items.append(item);
  }
}

async function loadProcedures(station) {
  try {
    const response = await fetch(`/api/v1/procedures/${encodeURIComponent(station)}`, { headers: { Accept: "application/json" } });
    const index = await response.json();
    if (!response.ok) throw new Error(index.error || `Procedure search failed (${response.status})`);
    localStorage.setItem(procedurePrefix + station, JSON.stringify(index));
    renderProcedures(index);
  } catch {
    const saved = localStorage.getItem(procedurePrefix + station);
    if (saved) {
      try {
        renderProcedures(JSON.parse(saved), true);
      } catch {
        document.querySelector("#procedure-status").textContent = "Saved FAA procedure index could not be read.";
      }
    } else {
      document.querySelector("#procedure-cycle").textContent = "FAA d-TPP index unavailable.";
      document.querySelector("#procedure-status").textContent = "Connect to load official FAA procedures. Previously opened PDFs remain cached on this installation.";
      document.querySelector("#procedure-list").replaceChildren();
    }
  }
}

const vfrPrefix = "fp-vfr-index";
let vfrIndex = null;

function renderVFR(index, local = false) {
  vfrIndex = index;
  const filter = document.querySelector("#vfr-filter");
  filter.disabled = false;
  const query = filter.value.trim().toLowerCase();
  const charts = (index.charts || []).filter((chart) => chart.name.toLowerCase().includes(query));
  const list = document.querySelector("#vfr-list");
  list.replaceChildren();
  for (const chart of charts) {
    const item = document.createElement("li");
    item.className = "procedure-item";
    const name = document.createElement("div");
    name.className = "procedure-name";
    const title = document.createElement("strong");
    title.textContent = chart.name;
    const edition = document.createElement("span");
    edition.textContent = `Current edition: ${chart.edition}`;
    name.append(title, edition);
    const link = document.createElement("a");
    link.className = "procedure-link";
    link.href = `/api/v1/vfr/files/${encodeURIComponent(chart.file)}`;
    link.target = "_blank";
    link.rel = "noreferrer";
    link.textContent = chart.downloaded ? "Open saved PDF ↗" : "Open / save ↗";
    item.append(name, link);
    list.append(item);
  }
  const stale = index.stale || local ? " · cached; check current edition" : "";
  document.querySelector("#vfr-status").textContent = `${charts.length} FAA PDF chart${charts.length === 1 ? "" : "s"} shown${query ? ` for “${query}”` : ""}. Some charts exceed 100 MB; opening one attempts a browser-local offline copy, subject to storage limits${stale}.`;
}

async function loadVFRCharts() {
  const button = document.querySelector("#load-vfr");
  const status = document.querySelector("#vfr-status");
  button.disabled = true;
  status.textContent = "Loading FAA chart catalog…";
  try {
    const response = await fetch("/api/v1/vfr", { headers: { Accept: "application/json" } });
    const index = await response.json();
    if (!response.ok) throw new Error(index.error || `Chart catalog failed (${response.status})`);
    localStorage.setItem(vfrPrefix, JSON.stringify(index));
    renderVFR(index);
  } catch {
    const saved = localStorage.getItem(vfrPrefix);
    if (saved) {
      try {
        renderVFR(JSON.parse(saved), true);
      } catch {
        status.textContent = "Saved FAA chart catalog could not be read.";
      }
    } else {
      status.textContent = "Connect to load the official FAA VFR chart catalog.";
    }
  } finally {
    button.disabled = false;
  }
}

function showSavedVFR() {
  const saved = localStorage.getItem(vfrPrefix);
  if (!saved) return;
  try {
    renderVFR(JSON.parse(saved), true);
  } catch {
    document.querySelector("#vfr-status").textContent = "Saved FAA chart catalog could not be read.";
  }
}
const offlineChartCacheName = "fp-documents-v1";
const personalBriefingDBName = "fp-personal-briefings";

async function refreshOfflineChartLibrary() {
  const panel = document.querySelector("#offline-chart-library");
  const list = document.querySelector("#offline-chart-list");
  const status = document.querySelector("#offline-chart-status");
  if (!window.caches) {
    panel.hidden = true;
    return;
  }
  try {
    const cache = await caches.open(offlineChartCacheName);
    const documents = (await cache.keys()).flatMap((request) => {
      const url = new URL(request.url);
      if (url.origin !== location.origin) return [];
      const procedure = url.pathname.match(/^\/api\/v1\/procedures\/([A-Z0-9]{3,4})\/files\/([A-Za-z0-9_-]+\.pdf)$/);
      if (procedure) return [{ request, label: `FAA procedures · ${procedure[1]} · ${procedure[2]}` }];
      const vfr = url.pathname.match(/^\/api\/v1\/vfr\/files\/([A-Za-z0-9_-]+\.pdf)$/);
      if (vfr) return [{ request, label: `FAA VFR chart · ${vfr[1]}` }];
      return [];
    });
    list.replaceChildren();
    for (const documentInfo of documents) {
      const item = document.createElement("li");
      item.className = "procedure-item";
      const link = document.createElement("a");
      link.className = "procedure-link";
      link.href = documentInfo.request.url;
      link.target = "_blank";
      link.rel = "noreferrer";
      link.textContent = `Open ${documentInfo.label} ↗`;
      const remove = document.createElement("button");
      remove.className = "secondary-button";
      remove.type = "button";
      remove.textContent = "Remove";
      remove.setAttribute("aria-label", `Remove ${documentInfo.label} from this device`);
      remove.addEventListener("click", async () => {
        remove.disabled = true;
        try {
          await cache.delete(documentInfo.request);
          await refreshOfflineChartLibrary();
        } catch {
          status.textContent = "Could not remove this saved PDF from browser storage.";
          remove.disabled = false;
        }
      });
      item.append(link, remove);
      list.append(item);
    }
    panel.hidden = documents.length === 0;
    status.textContent = `${documents.length} FAA PDF${documents.length === 1 ? "" : "s"} saved in this browser. Browser storage may be limited or evicted; verify chart currency before use.`;
  } catch {
    panel.hidden = false;
    status.textContent = "Saved PDFs could not be read from browser storage.";
  }
}

function openPersonalBriefingDB() {
  return new Promise((resolve, reject) => {
    if (!window.indexedDB) {
      reject(new Error("This browser does not support local briefing storage."));
      return;
    }
    const request = indexedDB.open(personalBriefingDBName, 1);
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains("briefings")) {
        request.result.createObjectStore("briefings", { keyPath: "id" });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error("Could not open local briefing storage."));
  });
}

async function withPersonalBriefingStore(mode, operation) {
  const db = await openPersonalBriefingDB();
  try {
    return await new Promise((resolve, reject) => {
      const transaction = db.transaction("briefings", mode);
      let result;
      const request = operation(transaction.objectStore("briefings"));
      request.onsuccess = () => { result = request.result; };
      request.onerror = () => reject(request.error || transaction.error);
      transaction.oncomplete = () => resolve(result);
      transaction.onerror = () => reject(transaction.error || new Error("Local briefing transaction failed."));
      transaction.onabort = () => reject(transaction.error || new Error("Local briefing transaction was cancelled."));
    });
  } finally {
    db.close();
  }
}

async function refreshPersonalBriefings() {
  const list = document.querySelector("#personal-briefing-list");
  const status = document.querySelector("#personal-briefing-status");
  try {
    const briefings = await withPersonalBriefingStore("readonly", (store) => store.getAll());
    briefings.sort((a, b) => b.savedAt.localeCompare(a.savedAt));
    list.replaceChildren();
    for (const briefing of briefings) {
      const item = document.createElement("li");
      const details = document.createElement("details");
      const summary = document.createElement("summary");
      summary.textContent = `${briefing.source} · ${briefing.issueTime}`;
      const saved = document.createElement("p");
      saved.className = "personal-briefing-meta";
      saved.textContent = `Saved on this device ${formatReportTime(briefing.savedAt)} · not automatically refreshed`;
      const text = document.createElement("pre");
      text.className = "personal-briefing-text";
      text.textContent = briefing.text;
      details.append(summary, saved, text);
      const remove = document.createElement("button");
      remove.className = "secondary-button";
      remove.type = "button";
      remove.textContent = "Remove";
      remove.setAttribute("aria-label", `Remove saved briefing from ${briefing.source}`);
      remove.addEventListener("click", async () => {
        remove.disabled = true;
        try {
          await withPersonalBriefingStore("readwrite", (store) => store.delete(briefing.id));
          await refreshPersonalBriefings();
          status.textContent = "Removed the local briefing copy.";
        } catch {
          status.textContent = "Could not remove the local briefing copy.";
          remove.disabled = false;
        }
      });
      item.append(details, remove);
      list.append(item);
    }
    status.textContent = briefings.length
      ? `${briefings.length} personal briefing copy/copies stored only in this browser. Storage may be evicted; verify the current official source before flight.`
      : "No local briefing copies saved. Copies are stored only in this browser and are not verified or refreshed by FP.";
  } catch (error) {
    list.replaceChildren();
    status.textContent = error.message || "Local briefing storage is unavailable in this browser.";
  }
}

async function savePersonalBriefing(source, issueTime, text) {
  const id = window.crypto?.randomUUID
    ? window.crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  const briefing = {
    id,
    source,
    issueTime,
    text,
    savedAt: new Date().toISOString()
  };
  await withPersonalBriefingStore("readwrite", (store) => store.put(briefing));
  return briefing;
}


const aipAeroRoutes = new Map([
  ["AL", "al/en"], ["AM", "am/en"], ["AU", "au"], ["AT", "at/en"], ["AZ", "az/en"],
  ["BY", "by/en"], ["BE", "be"], ["BA", "ba/en"], ["BG", "bg/en"], ["CA", "ca"],
  ["HR", "hr/en"], ["CY", "cy/en"], ["CZ", "cz/en"], ["DK", "dk/en"], ["EE", "ee/en"],
  ["FI", "fi/en"], ["FR", "fr/en"], ["GF", "gf/en"], ["PF", "pf/en"], ["GE", "ge/en"],
  ["DE", "de/en"], ["GR", "gr/en"], ["GP", "gp/en"], ["HU", "hu/en"], ["IS", "is/en"],
  ["IE", "ie"], ["IT", "it/en"], ["KZ", "kz/en"], ["XK", "xk/en"], ["KG", "kg/en"],
  ["LV", "lv/en"], ["LT", "lt/en"], ["MT", "mt"], ["MQ", "mq/en"], ["MD", "md/en"],
  ["NA", "na"], ["NL", "nl/en"], ["NC", "nc/en"], ["NZ", "nz"], ["MK", "mk/en"],
  ["NO", "no/en"], ["PL", "pl/en"], ["PT", "pt/en"], ["RE", "re/en"], ["RO", "ro/en"],
  ["RU", "ru/en"], ["BL", "bl/en"], ["MF", "mf/en"], ["PM", "pm/en"], ["RS", "rs/en"],
  ["SK", "sk/en"], ["SI", "si/en"], ["ZA", "za"], ["ES", "es/en"], ["SE", "se/en"],
  ["CH", "ch/en"], ["TJ", "tj/en"], ["TR", "tr/en"], ["TM", "tm/en"], ["UA", "ua/en"],
  ["GB", "uk"], ["US", "us"], ["UZ", "uz/en"], ["WF", "wf/en"]
]);

const regionalNotamSources = new Map([
  ["CA", { name: "NAV CANADA CFPS", url: "https://plan.navcanada.ca/", note: "Official Canadian flight-planning service. Open it directly for route or geographic NOTAM lookup; FP does not retrieve or cache its data." }],
  ["MX", { name: "SENEAM NOTAM", url: "https://apps.seneam.gob.mx/notam/", note: "Official Mexican NOTAM portal. FP does not retrieve or cache its data." }],
  ["BR", { name: "AISWEB Brazil", url: "https://aisweb.decea.mil.br/?i=notam", note: "Official DECEA AIS portal with NOTAM listings. Verify the current briefing and validity on AISWEB." }],
  ["AR", { name: "ANAC Argentina NOTAM", url: "https://ais.anac.gob.ar/", note: "Official AIS reference summaries are not a PIB. Do not use this page as an operational briefing." }],
  ["CL", { name: "DGAC Chile IFIS", url: "https://aipchile.dgac.gob.cl/", note: "Official Chilean aeronautical information portal. Verify current NOTAM and briefing coverage directly." }],
  ["ZA", { name: "ATNS File2Fly", url: "https://file2fly.atns.co.za/", note: "Official service; free account registration and validation required. FP does not automate access or cache NOTAMs." }],
  ["QA", { name: "Qatar AIS NOTAM", url: "https://www.aim.gov.qa/", note: "Official Qatar AIM portal publishes NOTAM information. Check issue and validity dates on the source document." }],
  ["JP", { name: "Japan SWIM S2019", url: "https://top.swim.mlit.go.jp/swim/", note: "Japan's current SWIM service replaced the former AIS Japan NOTAM search. Account/service activation may be required." }],
  ["IN", { name: "AAI Online Flight Planning", url: "https://onlinefpl.aai.aero/ofpl/", note: "Official AAI flight-planning/PIB portal. AIM India public summaries cover selected airports only." }],
  ["AU", { name: "Airservices Australia NAIPS", url: "https://www.airservicesaustralia.com/naips/", note: "Free account required. Automated extraction and redistribution require written authorization; FP only links to NAIPS." }],
  ["NZ", { name: "Airways New Zealand IFIS", url: "https://www.ifis.airways.co.nz/", note: "Official New Zealand FIR briefing service. Use its current official briefing; FP does not retrieve or cache NOTAMs." }],
  ["ES", { name: "ENAIRE Spain NOTAM / ICARO", url: "https://aip.enaire.es/AIP/NOTAM-en.html", note: "Official ENAIRE AIP page links to ICARO PIB and NOTAM consultation. Direct ICARO access may be restricted; confirm a current operational PIB." }],
  ["FR", { name: "French SIA SOFIA-Briefing", url: "https://sofia-briefing.aviation-civile.gouv.fr/sofia/pages/notamform.html", note: "Official SIA NOTAM consultation and briefing service. Account access may be required." }],
  ["SG", { name: "CAAS Singapore NOTAM List", url: "https://aim-sg.caas.gov.sg/notam-list/", note: "Official monthly valid-NOTAM list, not a route PIB. Check its publication month and obtain a current flight briefing." }],
  ["AE", { name: "UAE GCAA NOTAM Information", url: "https://www.gcaa.gov.ae/en/ais/Pages/notam.aspx", note: "Official GCAA NOTAM information page; JavaScript and an account profile may be required to view documents." }],
  ["SA", { name: "Saudi SANS PIB", url: "https://aimss.sans.com.sa/en/pib", note: "Official SANS AIS publishes PIB, NOTAM checklists, and valid NOTAM lists. Verify the current briefing and validity." }],
  ["GH", { name: "Ghana GCAA NOTAM List", url: "https://www.gcaa.com.gh/notam/", note: "Official current A- and B-series lists. Check the UTC issue time and validity; use the applicable current PIB for route planning." }],
  ["EC", { name: "Ecuador DGAC IFIS NOTAM", url: "https://www.ais.aviacioncivil.gob.ec/notam", note: "Official DGAC information system; sign-in may be required. Its notice states that only digital AIP texts are authentic." }],
  ["MY", { name: "CAAM Malaysia eAIP / NOTAM", url: "https://aip.caam.gov.my/aip/eAIP/history-en-MS.html", note: "Official current eAIP portal includes NOTAM summaries and valid-NOTAM checklists, not a route-specific PIB. Check the effective issue and notice validity." }],
  ["ID", { name: "AirNav Indonesia AIS Center", url: "https://pia.airnavindonesia.co.id/", note: "Official national AIS advertises real-time NOTAM distribution; account sign-up is available on the portal." }],
  ["IR", { name: "Iran AIS Valid NOTAM List", url: "https://ais.airport.ir/85", note: "Official AIS list of valid A- and B-series NOTAMs with a last-updated time. Check freshness and the source documents before use." }],
  ["SR", { name: "Suriname AIS Valid NOTAMs", url: "https://cadsur.sr/ais/index.php/category/notices-to-airmen/list-of-valid-notam/", note: "Official AIS publication page with dated NOTAM posts; it is not a route-specific PIB. Verify current notices and complete coverage." }],
  ["DE", { name: "DFS Germany AIS Portal", url: "https://ais.dfs.de/pilotservice/home.jsp?lang=en", note: "Official route-related NOTAM briefings are free after AIS-Portal registration. Check the issue time and briefing scope." }],
  ["FI", { name: "Fintraffic Finland PIBs", url: "https://www.ais.fi/ais/bulletins/", note: "Official Finnish NOF publishes FIR and aerodrome pre-flight bulletins. Check bulletin scope and issue time." }],
  ["IT", { name: "ENAV Italy Pre-Flight Service", url: "https://www.enav.it/en/what-we-do/we-create-solutions-for-international-markets/aeronautical-information/pre-flight", note: "Official ENAV describes customer self-briefing and PIB retrieval including NOTAM. Confirm access and briefing validity with ENAV." }],
  ["NL", { name: "LVNL Netherlands Homebriefing", url: "https://hbs.ixosystem.eu/ixo/login.php", note: "Official LVNL Homebriefing supports flight preparation with AIP, NOTAM, and weather. Account access may be required." }],
  ["NO", { name: "Avinor Norway IPPC", url: "https://www.ippc.no/ippc/index.jsp", note: "Official briefing reports include NOTAM updated to report-production time. AIP Norway is copyrighted; follow Avinor's terms." }],
  ["GB", { name: "NATS Aeronautical Information", url: "https://www.nats.aero/ais/", note: "Official UK AIS source. PIB access requires registration." }],
  ["IE", { name: "AirNav Ireland Current PIB", url: "https://www.airnav.ie/air-traffic-management/notam-notice-to-airmen-24e348473e7b0153452e634c36c72213", note: "Official AirNav page links the current PIB containing notices issued by AirNav Ireland. Check its production/validity time and briefing scope." }],
  ["KE", { name: "Kenya KCAA Online Briefing", url: "https://aim.kcaa.or.ke/cronos-portal/", note: "KCAA says online NOTAM/PIB access is granted after submitting its flight-planning and online-briefing request form. Confirm access and current briefing scope." }],
  ["TH", { name: "AEROTHAI NOTAM Thai", url: "https://notamthai.aerothai.aero/main.aspx", note: "Official AEROTHAI portal offers NOTAM, SNOWTAM, ASHTAM, and PIB. Login/registration may be required and account activation may take up to 24 hours." }],
  ["TZ", { name: "Tanzania TCAA NOTAM Search", url: "https://eservices.tcaa.go.tz/notams", note: "Official search is sourced from TCAA's Daily List of Valid NOTAMs, but is labeled informational for planning/familiarization. Verify current notices and validity before operations." }],
  ["PE", { name: "CORPAC Peru NOTAM System", url: "https://www.gob.pe/institucion/corpac/pages/30918-notam-peru", note: "CORPAC's official instructions link its NOTAM system; username/password access is required and pilots must request credentials through AIS. FP does not retrieve or cache notices." }],
  ["PY", { name: "DINAC Paraguay Valid NOTAM Lists", url: "https://www.dinac.gov.py/v3/index.php/ais/documentacion-integrada/itemlist/category/75-lnv-lsita-de-notam-validos", note: "Official DINAC publishes valid-NOTAM lists by year. This is not a route-specific PIB; check the issue time and notice validity, and obtain the applicable current briefing." }],
]);

function setRegionalNotamSource(country) {
  const source = regionalNotamSources.get(country);
  const panel = document.querySelector("#regional-notam-source");
  const link = document.querySelector("#regional-notam-link");
  document.querySelector("#personal-briefing-source").placeholder =
    source ? source.name : country === "US" ? "FAA NOTAM Search" : "State AIS/AIM portal";
  panel.hidden = !source;
  if (!source) return;
  link.href = source.url;
  link.textContent = `Open ${source.name} ↗`;
  document.querySelector("#regional-notam-note").textContent = source.note;
}

function setAIPAirportLink(country, icao) {
  const link = document.querySelector("#aip-airport-link");
  const route = aipAeroRoutes.get(country);
  const airportCode = String(icao || "").toUpperCase();
  link.hidden = !route || !airportCode;
  if (!route || !airportCode) return;
  const url = new URL(`/${route}/vfr/`, "https://aip.aero");
  url.search = airportCode;
  link.href = url.toString();
  link.textContent = `Open ${airportCode} AIP charts ↗`;
}

function configureRegion(country, station, icao) {
  const hasFAACoverage = country === "US";
  document.querySelector("#procedure-panel").hidden = !hasFAACoverage;
  document.querySelector("#vfr-panel").hidden = !hasFAACoverage;
  document.querySelector("#notam-panel").hidden = !hasFAACoverage;
  document.querySelector("#regional-resources").hidden = hasFAACoverage;
  setAIPAirportLink(country, icao);
  setRegionalNotamSource(country);
  if (hasFAACoverage) {
    setNotamLink(icao || station);
  } else {
    document.querySelector("#procedure-list").replaceChildren();
    document.querySelector("#vfr-list").replaceChildren();
    document.querySelector("#vfr-filter").disabled = true;
    vfrIndex = null;
  }
  return hasFAACoverage;
}

function setNotamLink(station) {
  const url = new URL("https://notams.aim.faa.gov/notamSearch");
  url.search = new URLSearchParams({
    actionType: "notamRetrievalByICAOs",
    formatType: "DOMESTIC",
    method: "displayByICAOs",
    reportType: "RAW",
    retrieveLocId: station
  });
  document.querySelector("#notam-link").href = url.toString();
}

function openAirportDirectoryDB() {
  return new Promise((resolve, reject) => {
    if (!window.indexedDB) {
      reject(new Error("This browser does not support offline airport storage."));
      return;
    }
    const request = window.indexedDB.open(airportDirectoryDBName, 1);
    request.onupgradeneeded = () => request.result.createObjectStore("directories");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

async function saveAirportDirectoryLocally(directory) {
  const db = await openAirportDirectoryDB();
  try {
    await new Promise((resolve, reject) => {
      const transaction = db.transaction("directories", "readwrite");
      transaction.objectStore("directories").put(directory, "global");
      transaction.oncomplete = resolve;
      transaction.onerror = () => reject(transaction.error);
      transaction.onabort = () => reject(transaction.error);
    });
    airportDirectoryCache = directory;
    airportDirectoryLoadPromise = Promise.resolve(directory);
  } finally {
    db.close();
  }
}

async function readAirportDirectoryLocally() {
  const db = await openAirportDirectoryDB();
  try {
    return await new Promise((resolve, reject) => {
      const transaction = db.transaction("directories", "readonly");
      const request = transaction.objectStore("directories").get("global");
      request.onsuccess = () => resolve(request.result || null);
      request.onerror = () => reject(request.error);
    });
  } finally {
    db.close();
  }
}

function getSavedAirportDirectory() {
  if (airportDirectoryCache !== undefined) return Promise.resolve(airportDirectoryCache);
  if (!airportDirectoryLoadPromise) {
    airportDirectoryLoadPromise = readAirportDirectoryLocally().then((directory) => {
      airportDirectoryCache = directory;
      return directory;
    }).catch(() => {
      airportDirectoryCache = null;
      return null;
    });
  }
  return airportDirectoryLoadPromise;
}
async function searchAirportDirectory(query) {
  const normalized = query.trim().toLocaleLowerCase();
  if (normalized.length < 2) {
    airportSearchResults.hidden = true;
    airportSearchResults.replaceChildren();
    return;
  }
  const directory = await getSavedAirportDirectory();
  if (stationInput.value.trim().toLocaleLowerCase() !== normalized) return;
  airportSearchResults.replaceChildren();
  const appendMessage = (text) => {
    const item = document.createElement("li");
    item.className = "airport-search-empty";
    item.textContent = text;
    airportSearchResults.append(item);
  };
  if (!directory?.airports) {
    appendMessage("Save the worldwide airport directory to search airports offline.");
    airportSearchResults.hidden = false;
    return;
  }
  const matches = new Map();
  for (const airport of Object.values(directory.airports)) {
    const code = firstText(airport, ["icaoId", "ident"]).toUpperCase();
    if (!/^[A-Z0-9]{3,4}$/.test(code)) continue;
    const identifiers = [airport.icaoId, airport.ident, airport.iataId]
      .filter(Boolean).map((value) => String(value).toLocaleLowerCase());
    const name = firstText(airport, ["name"]);
    const normalizedName = name.toLocaleLowerCase();
    let rank = Infinity;
    if (identifiers.includes(normalized)) rank = 0;
    else if (identifiers.some((identifier) => identifier.startsWith(normalized))) rank = 1;
    else if (normalizedName.startsWith(normalized)) rank = 2;
    else if (normalizedName.includes(normalized)) rank = 3;
    const existing = matches.get(code);
    if (rank < Infinity && (!existing || rank < existing.rank)) matches.set(code, { airport, code, rank, name });
  }
  const sortedMatches = Array.from(matches.values()).sort((left, right) => left.rank - right.rank || left.name.localeCompare(right.name));
  if (sortedMatches.length === 0) appendMessage("No saved airport matches. Try an identifier or a longer name.");
  for (const { airport, code, name } of sortedMatches.slice(0, 8)) {
    const item = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.className = "airport-search-result";
    const iata = firstText(airport, ["iataId"]);
    const location = [firstText(airport, ["state"]), firstText(airport, ["country"])].filter(Boolean).join(", ");
    button.textContent = `${code}${iata && iata !== code ? ` (${iata})` : ""} · ${name}${location ? ` · ${location}` : ""}`;
    button.addEventListener("click", () => {
      clearTimeout(airportSearchTimer);
      stationInput.value = code;
      airportSearchResults.hidden = true;
      void lookup(code);
    });
    item.append(button);
    airportSearchResults.append(item);
  }
  airportSearchResults.hidden = false;
}


async function showOfflineAirport(station) {
  try {
    const directory = await getSavedAirportDirectory();
    const primaryIdent = directory?.aliases?.[station];
    const airport = directory?.airports?.[station] || directory?.airports?.[primaryIdent];
    if (!airport) return false;
    show({
      station,
      source: "OurAirports public-domain directory (reference only)",
      fetchedAt: directory.fetchedAt,
      stale: true,
      cached: true,
      airport: [airport],
      metar: [],
      taf: [],
      pirep: []
    }, true);
    void loadSIGMETs();
    message.textContent = "Live lookup unavailable. Showing this device’s saved airport reference only; live weather and operational data are unavailable.";
    return true;
  } catch {
    return false;
  }
}

async function lookup(station) {
  message.textContent = "";
  lookupButton.disabled = true;
  try {
    const response = await fetch(`/api/v1/airport/${encodeURIComponent(station)}`, { headers: { Accept: "application/json" } });
    const payload = await response.json();
    if (!response.ok) throw new Error(payload.error || `Request failed (${response.status})`);
    localStorage.setItem(localPrefix + station, JSON.stringify(payload));
    const hasFAACoverage = show(payload);
    void loadSIGMETs();
    if (hasFAACoverage) await loadProcedures(station);
  } catch (error) {
    const saved = localStorage.getItem(localPrefix + station);
    if (saved) {
      try {
        const hasFAACoverage = show(JSON.parse(saved), true);
        void loadSIGMETs();
        if (hasFAACoverage) {
          await loadProcedures(station);
          showSavedVFR();
        }
        message.textContent = "Network unavailable. Showing this device’s saved briefing; verify freshness before use.";
      } catch {
        message.textContent = "Saved briefing could not be read.";
      }
    } else if (!(await showOfflineAirport(station))) {
      briefing.hidden = true;
      emptyState.hidden = false;
      document.querySelector("#procedure-list").replaceChildren();
      message.textContent = error.message || "Unable to retrieve briefing.";
    }
  } finally {
    lookupButton.disabled = false;
  }
}

let airportSearchTimer = null;
stationInput.addEventListener("input", () => {
  clearTimeout(airportSearchTimer);
  airportSearchTimer = setTimeout(() => void searchAirportDirectory(stationInput.value), 120);
});
form.addEventListener("submit", (event) => {
  event.preventDefault();
  const query = stationInput.value.trim();
  const station = query.toUpperCase();
  if (/^[A-Z0-9]{3,4}$/.test(station)) {
    clearTimeout(airportSearchTimer);
    stationInput.value = station;
    airportSearchResults.hidden = true;
    lookup(station);
    return;
  }
  if (query.length < 2) {
    message.textContent = "Enter an airport code or at least two characters of an airport name.";
    return;
  }
  message.textContent = "";
  void searchAirportDirectory(query);
});
airportDirectoryButton.addEventListener("click", async () => {
  airportDirectoryButton.disabled = true;
  airportDirectoryStatus.textContent = "Downloading worldwide airport, runway, and frequency data…";
  try {
    const response = await fetch("/api/v1/airports/catalog", { method: "POST", headers: { Accept: "application/json" } });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || `Download failed (${response.status})`);
    const directoryResponse = await fetch("/api/v1/airports/catalog/data", { headers: { Accept: "application/json" } });
    const directory = await directoryResponse.json();
    if (!directoryResponse.ok) throw new Error(directory.error || `Directory download failed (${directoryResponse.status})`);
    await saveAirportDirectoryLocally(directory);
    void searchAirportDirectory(stationInput.value);
    airportDirectoryStatus.textContent = `Saved ${result.airports.toLocaleString()} airport and ${(result.navaids || 0).toLocaleString()} radio-navigation-aid records on this server and this device for offline reference · downloaded ${formatReportTime(result.fetchedAt)}. Not operational data.`;
    renderAirportOperations(selectedAirport || {});
    if (sigmetIndex) renderSIGMETs(sigmetIndex);
  } catch (error) {
    airportDirectoryStatus.textContent = error.message || "Airport directory download failed.";
  } finally {
    airportDirectoryButton.disabled = false;
  }
});

const personalBriefingForm = document.querySelector("#personal-briefing-form");
personalBriefingForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const source = document.querySelector("#personal-briefing-source").value.trim();
  const issueTime = document.querySelector("#personal-briefing-issued").value.trim();
  const text = document.querySelector("#personal-briefing-text").value;
  const saveButton = document.querySelector("#save-personal-briefing");
  const status = document.querySelector("#personal-briefing-status");
  if (!source || !issueTime || !text.trim()) {
    status.textContent = "Enter the source, its stated issue/effective time, and briefing text.";
    return;
  }
  saveButton.disabled = true;
  status.textContent = "Saving a local copy…";
  try {
    await savePersonalBriefing(source, issueTime, text);
    personalBriefingForm.reset();
    await refreshPersonalBriefings();
    status.textContent = "Saved on this device. This copy is not verified or automatically refreshed; check the current official source before flight.";
  } catch (error) {
    status.textContent = error.message || "Could not save this briefing in browser storage.";
  } finally {
    saveButton.disabled = false;
  }
});
void refreshPersonalBriefings();
document.querySelector("#load-vfr").addEventListener("click", loadVFRCharts);
document.querySelector("#refresh-offline-charts").addEventListener("click", refreshOfflineChartLibrary);
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) void refreshOfflineChartLibrary();
});
document.querySelector("#vfr-filter").addEventListener("input", () => {
  if (vfrIndex) renderVFR(vfrIndex);
});
document.querySelector("#sigmet-filter").addEventListener("input", () => {
  if (sigmetIndex) renderSIGMETs(sigmetIndex);
});
window.addEventListener("online", setConnection);
window.addEventListener("offline", setConnection);
refreshFavoriteAirports();
setConnection();
if ("serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => {});
const initialStation = new URLSearchParams(location.search).get("station");
if (initialStation && /^[A-Za-z0-9]{3,4}$/.test(initialStation)) {
  stationInput.value = initialStation.toUpperCase();
  lookup(stationInput.value);
}
