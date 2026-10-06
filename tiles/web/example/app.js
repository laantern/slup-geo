import * as maplibregl from '../tiles/vendor/maplibre-gl.mjs';

// Пример работает на том же сервисе: /v1/* и /tiles/* — относительные пути.
    const API = '';

    const LEVEL_LABEL = {
      house: 'дом', residential: 'жилой район', neighbourhood: 'микрорайон', quarter: 'квартал',
      city_district: 'район города', city: 'город', settlement: 'селение', raion: 'район',
      oblast: 'область', country: 'страна', landuse: 'зона', area: 'территория',
    };

    const EMPTY_FC = { type: 'FeatureCollection', features: [] };
    const POINT_COLOR = '#1d4ed8';

    const qInput = document.getElementById('q');
    const clearBtn = document.getElementById('clear');
    const suggestEl = document.getElementById('suggest');
    const selectionEl = document.getElementById('selection');
    const selName = document.getElementById('sel-name');
    const selMeta = document.getElementById('sel-meta');
    const zonesEl = document.getElementById('zones');
    const statEl = document.getElementById('stat');
    const statusDot = document.getElementById('status-dot');
    const hintEl = document.getElementById('hint');

    let map = null;
    let marker = null;

    let searchTimer = null;
    let searchAbort = null;
    let actionAbort = null;
    let lastPoint = null;
    let suggestSeq = 0;
    let reqCount = 0;
    let currentZones = [];
    const suggestCache = new Map();

    const nativeFetch = window.fetch.bind(window);
    window.fetch = (...args) => {
      reqCount++;
      return nativeFetch(...args);
    };

    function chip(className, text) {
      const el = document.createElement('span');
      el.className = 'chip ' + className;
      el.textContent = text;
      return el;
    }

    function muteRow(text) {
      const li = document.createElement('li');
      li.className = 'muted-row';
      li.textContent = text;
      return li;
    }

    function levelLabel(level) {
      return LEVEL_LABEL[String(level).toLowerCase()] || level;
    }

    function setStat(text) {
      statEl.textContent = text || '';
    }

    function positionSuggest() {
      const rect = qInput.getBoundingClientRect();
      suggestEl.style.left = `${rect.left}px`;
      suggestEl.style.top = `${rect.bottom + 6}px`;
      suggestEl.style.width = `${rect.width}px`;
    }

    function closeSuggest() {
      suggestEl.classList.remove('open');
    }

    // ---------- карта ----------
    async function initMap() {
      maplibregl.addProtocol('pmtiles', new pmtiles.Protocol().tile);

      // Если тайлы ещё не собраны — показываем карту без подложки (точки/границы работают).
      let style = { version: 8, sources: {}, layers: [] };
      try {
        const res = await fetch(`${API}/tiles/tiles.json`);
        const manifest = res.ok ? await res.json() : null;
        if (manifest && manifest.files && manifest.files.length) {
          style = `${API}/tiles/style.json`;
        } else {
          hintEl.textContent = 'Тайлы не собраны: дождитесь обновления данных или включите TILES_ENABLED.';
        }
      } catch {
        hintEl.textContent = 'Тайлы недоступны — карта без подложки.';
      }

      map = new maplibregl.Map({
        container: 'map',
        style,
        center: [27.57, 53.9],
        zoom: 10,
        attributionControl: {
          customAttribution:
            '<a href="https://openmaptiles.org/" target="_blank" rel="noopener">© OpenMapTiles</a> · ' +
            '<a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">© OpenStreetMap contributors</a>',
        },
      });
      map.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right');
      map.on('load', addZoneLayers);
      map.on('click', (e) => onMapClick(e.lngLat.lat, e.lngLat.lng));
    }

    function addZoneLayers() {
      if (!map || map.getSource('zone')) return;
      map.addSource('zone', { type: 'geojson', data: EMPTY_FC });
      map.addLayer({ id: 'zone-fill', type: 'fill', source: 'zone', paint: { 'fill-color': '#3b82f6', 'fill-opacity': 0.22 } });
      map.addLayer({ id: 'zone-line', type: 'line', source: 'zone', paint: { 'line-color': '#1d4ed8', 'line-width': 2.5 } });
    }

    function setZoneGeometry(geometry) {
      const source = map && map.getSource('zone');
      if (!source) return;
      source.setData(geometry ? { type: 'Feature', properties: {}, geometry } : EMPTY_FC);
    }

    function showMarker(lat, lon) {
      if (!marker) marker = new maplibregl.Marker({ color: POINT_COLOR });
      marker.setLngLat([lon, lat]).addTo(map);
    }

    function clearMarker() {
      if (marker) marker.remove();
    }

    function pointInRing(pt, ring) {
      let inside = false;
      for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
        const xi = ring[i][0], yi = ring[i][1];
        const xj = ring[j][0], yj = ring[j][1];
        if (((yi > pt[1]) !== (yj > pt[1])) && pt[0] < ((xj - xi) * (pt[1] - yi)) / (yj - yi) + xi) inside = !inside;
      }
      return inside;
    }

    // Камера: фитим ту часть геометрии, где лежит точка (у районов бывают далёкие эксклавы)
    function partBoundsFor(geom, pt) {
      const type = geom && geom.type;
      if (type !== 'Polygon' && type !== 'MultiPolygon') return null;
      const polys = type === 'Polygon' ? [geom.coordinates] : geom.coordinates;
      for (const poly of polys) {
        if (!pointInRing(pt, poly[0])) continue;
        let inHole = false;
        for (let h = 1; h < poly.length; h++) {
          if (pointInRing(pt, poly[h])) { inHole = true; break; }
        }
        if (inHole) continue;
        let minLng = 180, maxLng = -180, minLat = 90, maxLat = -90;
        const walk = (node) => {
          if (Array.isArray(node[0])) { node.forEach(walk); return; }
          const [lng, lat] = node;
          minLng = Math.min(minLng, lng);
          maxLng = Math.max(maxLng, lng);
          minLat = Math.min(minLat, lat);
          maxLat = Math.max(maxLat, lat);
        };
        poly.forEach(walk);
        return [[minLng, minLat], [maxLng, maxLat]];
      }
      return null;
    }

    function fitToGeometry(geom, lon, lat) {
      const part = partBoundsFor(geom, [lon, lat]);
      if (!part) {
        map.easeTo({ center: [lon, lat] });
        return;
      }
      const bounds = new maplibregl.LngLatBounds(part[0], part[1]);
      bounds.extend([lon, lat]);
      map.fitBounds(bounds, { padding: 60, maxZoom: 16 });
    }

    async function probe() {
      try {
        const res = await fetch(`${API}/v1/suggest?q=%D0%BC%D0%BD`);
        statusDot.className = 'dot ' + (res.ok ? 'ok' : 'err');
      } catch {
        statusDot.className = 'dot err';
      }
    }

    function resetAll() {
      if (searchAbort) searchAbort.abort();
      if (actionAbort) actionAbort.abort();
      clearTimeout(searchTimer);
      qInput.value = '';
      clearBtn.hidden = true;
      closeSuggest();
      selectionEl.hidden = true;
      clearMarker();
      setZoneGeometry(null);
      currentZones = [];
      setStat('');
    }

    // ---------- подсказки ----------
    async function runSuggest(q) {
      const seq = ++suggestSeq;
      if (searchAbort) searchAbort.abort();
      searchAbort = new AbortController();
      suggestEl.replaceChildren(muteRow('поиск…'));
      positionSuggest();
      suggestEl.classList.add('open');
      try {
        let data = suggestCache.get(q);
        if (!data) {
          if (suggestCache.size > 50) suggestCache.clear();
          const res = await fetch(`${API}/v1/suggest?q=${encodeURIComponent(q)}`, { signal: searchAbort.signal });
          if (!res.ok) {
            const problem = await res.json().catch(() => null);
            throw new Error(problem?.type || `HTTP ${res.status}`);
          }
          data = await res.json();
          suggestCache.set(q, data);
        }
        if (seq !== suggestSeq) return;
        renderSuggest(data.items || []);
      } catch (e) {
        if (e.name === 'AbortError') return;
        suggestEl.replaceChildren(muteRow('Ошибка: ' + e.message));
      }
    }

    function renderSuggest(items) {
      suggestEl.replaceChildren();
      if (!items.length) {
        suggestEl.appendChild(muteRow('Ничего не найдено'));
        return;
      }
      for (const item of items) suggestEl.appendChild(suggestionItem(item));
    }

    function suggestionItem(item) {
      const li = document.createElement('li');
      li.className = 'item';
      const name = document.createElement('div');
      name.className = 'rname';
      name.textContent = item.name;
      const meta = document.createElement('div');
      meta.className = 'rmeta';
      meta.appendChild(chip(String(item.level).toLowerCase() === 'house' ? 'house' : 'area', levelLabel(item.level)));
      if (item.subtitle) {
        const sub = document.createElement('span');
        sub.className = 'rtype';
        sub.textContent = item.subtitle;
        meta.appendChild(sub);
      }
      li.append(name, meta);
      li.addEventListener('click', () => selectSuggestion(item));
      return li;
    }

    async function selectSuggestion(item) {
      closeSuggest();
      qInput.value = item.name;
      clearBtn.hidden = false;
      await pointAt(item.center.lat, item.center.lon, item.id);
      selName.textContent = item.name;
      selMeta.textContent = item.subtitle || levelLabel(item.level);
    }

    // ---------- выбор точки ----------
    async function onMapClick(lat, lon) {
      const now = performance.now();
      if (lastPoint && now - lastPoint.time < 800
          && haversineMeters(lastPoint.lat, lastPoint.lon, lat, lon) < 10) return;
      lastPoint = { lat, lon, time: now };
      await pointAt(lat, lon, null);
    }

    function haversineMeters(lat1, lon1, lat2, lon2) {
      const rad = Math.PI / 180;
      const dLat = (lat2 - lat1) * rad;
      const dLon = (lon2 - lon1) * rad;
      const a = Math.sin(dLat / 2) ** 2
        + Math.cos(lat1 * rad) * Math.cos(lat2 * rad) * Math.sin(dLon / 2) ** 2;
      return 6371000 * 2 * Math.asin(Math.sqrt(a));
    }

    async function pointAt(lat, lon, preferredId) {
      if (actionAbort) actionAbort.abort();
      actionAbort = new AbortController();
      const signal = actionAbort.signal;
      reqCount = 0;
      const startedAt = performance.now();

      selectionEl.hidden = false;
      selName.textContent = '…';
      selMeta.textContent = `${lat.toFixed(6)}, ${lon.toFixed(6)}`;
      zonesEl.replaceChildren();
      clearMarker();
      setZoneGeometry(null);
      showMarker(lat, lon);

      try {
        const res = await fetch(`${API}/v1/point?lat=${lat}&lon=${lon}`, { signal });
        if (!res.ok) {
          const problem = await res.json().catch(() => null);
          selName.textContent = 'Ошибка';
          selMeta.textContent = problem?.type || `HTTP ${res.status}`;
          return;
        }
        const data = await res.json();
        currentZones = data.zones || [];
        selName.textContent = data.text || '—';
        selMeta.textContent = `${lat.toFixed(6)}, ${lon.toFixed(6)}`;
        renderZones(currentZones, lat, lon, preferredId);
        setStat(`${reqCount} запрос(а) · ${Math.round(performance.now() - startedAt)} мс`);
      } catch (e) {
        if (e.name === 'AbortError') return;
        selName.textContent = 'Сервис недоступен';
        selMeta.textContent = 'проверьте /health';
        setStat('');
      }
    }

    function renderZones(zones, lat, lon, preferredId) {
      zonesEl.replaceChildren();
      const items = zones.map((zone) => zoneItem(zone, lat, lon));
      for (const li of items) zonesEl.appendChild(li);
      if (!items.length) {
        zonesEl.appendChild(muteRow('зоны не найдены'));
        return;
      }
      const preferredIndex = preferredId ? zones.findIndex((z) => z.id === preferredId) : -1;
      const chosenIndex = preferredIndex >= 0 ? preferredIndex : 0;
      selectZone(zones[chosenIndex], items[chosenIndex], lat, lon);
    }

    function zoneLabel(zone) {
      const city = currentZones.find((z) => z.level === 'CITY' || z.level === 'SETTLEMENT');
      return city && city.name !== zone.name ? `${zone.name}, ${city.name}` : zone.name;
    }

    function zoneItem(zone, lat, lon) {
      const li = document.createElement('li');
      li.className = 'item';
      const name = document.createElement('div');
      name.className = 'rname';
      name.textContent = zone.name;
      const meta = document.createElement('div');
      meta.className = 'rmeta';
      meta.appendChild(chip(zone.level === 'HOUSE' ? 'house' : 'area', levelLabel(zone.level)));
      meta.appendChild(chip('picked', 'выбрано'));
      if (zone.areaM2 > 0) {
        const area = document.createElement('span');
        area.className = 'rtype';
        area.textContent = `${Math.round(zone.areaM2).toLocaleString('ru-RU')} м²`;
        meta.appendChild(area);
      }
      li.append(name, meta);
      li.title = 'Показать границу';
      li.addEventListener('click', () => selectZone(zone, li, lat, lon));
      return li;
    }

    async function selectZone(zone, li, lat, lon) {
      for (const el of zonesEl.querySelectorAll('li.picked')) el.classList.remove('picked');
      li.classList.add('picked');
      const label = zoneLabel(zone);
      selName.textContent = label;
      qInput.value = label;
      clearBtn.hidden = false;
      const startedAt = performance.now();
      const before = reqCount;
      await drawArea(zone.id, lat, lon);
      setStat(`${reqCount - before} запрос(а) · ${Math.round(performance.now() - startedAt)} мс`);
    }

    async function drawArea(id, lat, lon) {
      try {
        const res = await fetch(`${API}/v1/areas/${encodeURIComponent(id)}?simplify=display`, {
          signal: actionAbort ? actionAbort.signal : undefined,
        });
        if (!res.ok) {
          selMeta.textContent = `граница недоступна: HTTP ${res.status}`;
          return;
        }
        const area = await res.json();
        setZoneGeometry(area.geometry);
        showMarker(lat, lon);
        fitToGeometry(area.geometry, lon, lat);
        selMeta.textContent = `${area.id} · ${area.geometry.type} · ${area.name}`;
      } catch (e) {
        if (e.name === 'AbortError') return;
        selMeta.textContent = 'ошибка загрузки границы';
      }
    }

    // ---------- события ----------
    qInput.addEventListener('input', () => {
      clearBtn.hidden = !qInput.value;
      clearTimeout(searchTimer);
      const q = qInput.value.trim();
      if (q.length < 2) {
        if (searchAbort) searchAbort.abort();
        closeSuggest();
        return;
      }
      searchTimer = setTimeout(() => runSuggest(q), 350);
    });
    qInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        clearTimeout(searchTimer);
        const q = qInput.value.trim();
        if (q.length >= 2) runSuggest(q);
      }
      if (e.key === 'Escape') closeSuggest();
    });
    clearBtn.addEventListener('click', resetAll);
    document.addEventListener('click', (e) => {
      if (!e.target.closest('#panel') && !e.target.closest('#suggest')) closeSuggest();
    });
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') closeSuggest();
    });
    window.addEventListener('resize', () => {
      if (suggestEl.classList.contains('open')) positionSuggest();
    });

    initMap().then(probe);
