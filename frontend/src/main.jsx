import React, { useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { MapContainer, TileLayer, Polygon, Marker, Popup, useMapEvents } from 'react-leaflet';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import './styles.css';

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080';

const truckIcon = L.divIcon({
  className: 'truck-marker',
  html: '<div class="truck-pin">T</div>',
  iconSize: [38, 38],
  iconAnchor: [19, 38],
});

async function api(path, options = {}) {
  const res = await fetch(`${API}${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Request failed');
  return data;
}

function MapClick({ onPick }) {
  useMapEvents({ click: e => onPick([e.latlng.lat, e.latlng.lng]) });
  return null;
}

function App() {
  const [tab, setTab] = useState('overview');
  const [geofences, setGeofences] = useState([]);
  const [vehicles, setVehicles] = useState([]);
  const [alerts, setAlerts] = useState([]);
  const [violations, setViolations] = useState([]);
  const [locations, setLocations] = useState({});
  const [liveAlerts, setLiveAlerts] = useState([]);
  const [toast, setToast] = useState('');
  const [loading, setLoading] = useState(false);
  const [geoFilter, setGeoFilter] = useState('');
  const [geoForm, setGeoForm] = useState({ name: '', description: '', category: 'restricted_zone', coordinates: '' });
  const [vehicleForm, setVehicleForm] = useState({ vehicle_number: '', driver_name: '', vehicle_type: 'truck', phone: '' });
  const [locationForm, setLocationForm] = useState({ vehicle_id: '', latitude: '37.7799', longitude: '-122.4144', timestamp: '' });
  const [alertForm, setAlertForm] = useState({ geofence_id: '', vehicle_id: '', event_type: 'entry' });
  const [mapPickMode, setMapPickMode] = useState(false);
  const [drawPoints, setDrawPoints] = useState([]);

  const refresh = async () => {
    setLoading(true);
    try {
      const [g, v, a, h] = await Promise.all([
        api('/geofences'),
        api('/vehicles'),
        api('/alerts'),
        api('/violations/history?limit=100'),
      ]);
      setGeofences(g.geofences || []);
      setVehicles(v.vehicles || []);
      setAlerts(a.alerts || []);
      setViolations(h.violations || []);
      const nextLocations = {};
      await Promise.all((v.vehicles || []).map(async vehicle => {
        try {
          const loc = await api(`/vehicles/location/${vehicle.id}`);
          nextLocations[vehicle.id] = loc;
        } catch {}
      }));
      setLocations(nextLocations);
    } catch (err) {
      setToast(err.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { refresh(); }, []);

  useEffect(() => {
    const url = API.replace(/^http/, 'ws') + '/ws/alerts';
    let socket;
    let retry;
    const connect = () => {
      socket = new WebSocket(url);
      socket.onmessage = event => {
        const alert = JSON.parse(event.data);
        setLiveAlerts(items => [alert, ...items].slice(0, 30));
        setToast(`${alert.vehicle.vehicle_number} ${alert.event_type === 'entry' ? 'entered' : 'exited'} ${alert.geofence.geofence_name}`);
        refresh();
      };
      socket.onclose = () => { retry = window.setTimeout(connect, 3000); };
    };
    connect();
    return () => { clearTimeout(retry); socket?.close(); };
  }, []);

  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(''), 4000);
    return () => clearTimeout(timer);
  }, [toast]);

  const notify = (message) => setToast(message);

  const createGeofence = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      let coordinates;
      if (drawPoints.length >= 3) coordinates = [...drawPoints, drawPoints[0]];
      else coordinates = geoForm.coordinates.trim().split('\n').map(line => line.split(',').map(Number));
      await api('/geofences', { method: 'POST', body: JSON.stringify({ ...geoForm, coordinates }) });
      setGeoForm({ name: '', description: '', category: 'restricted_zone', coordinates: '' });
      setDrawPoints([]);
      notify('Geofence created');
      refresh();
    } catch (err) { notify(err.message); }
    finally { setLoading(false); }
  };

  const registerVehicle = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await api('/vehicles', { method: 'POST', body: JSON.stringify(vehicleForm) });
      setVehicleForm({ vehicle_number: '', driver_name: '', vehicle_type: 'truck', phone: '' });
      notify('Vehicle registered');
      refresh();
    } catch (err) { notify(err.message); }
    finally { setLoading(false); }
  };

  const updateLocation = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await api('/vehicles/location', { method: 'POST', body: JSON.stringify({
        vehicle_id: locationForm.vehicle_id,
        latitude: Number(locationForm.latitude),
        longitude: Number(locationForm.longitude),
        timestamp: locationForm.timestamp || new Date().toISOString(),
      }) });
      notify('Location updated');
      refresh();
    } catch (err) { notify(err.message); }
    finally { setLoading(false); }
  };

  const configureAlert = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await api('/alerts/configure', { method: 'POST', body: JSON.stringify({
        geofence_id: alertForm.geofence_id,
        vehicle_id: alertForm.vehicle_id || undefined,
        event_type: alertForm.event_type,
      }) });
      notify('Alert rule configured');
      refresh();
    } catch (err) { notify(err.message); }
    finally { setLoading(false); }
  };

  const handleMapClick = (point) => {
    if (tab === 'geofences') setDrawPoints(points => [...points, point]);
    if (tab === 'locations') setLocationForm(form => ({ ...form, latitude: point[0].toFixed(6), longitude: point[1].toFixed(6) }));
  };

  const filteredGeofences = geoFilter ? geofences.filter(g => g.category === geoFilter) : geofences;
  const categoryCounts = useMemo(() => geofences.reduce((acc, g) => {
    acc[g.category] = (acc[g.category] || 0) + 1; return acc;
  }, {}), [geofences]);

  const vehicleMarkers = vehicles.flatMap(vehicle => {
    const loc = locations[vehicle.id]?.current_location;
    return loc ? [{ vehicle, loc }] : [];
  });

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand"><div className="brand-mark">G</div><div><strong>GeoTrack</strong><span>Fleet command</span></div></div>
        <nav>
          {[
            ['overview', 'Overview'], ['geofences', 'Geofences'], ['vehicles', 'Vehicles'],
            ['locations', 'Locations'], ['alerts', 'Alert rules'], ['history', 'History'],
          ].map(([key, label]) => (
            <button key={key} className={tab === key ? 'active' : ''} onClick={() => setTab(key)}>
              <span className={`nav-icon nav-${key}`}></span>{label}
            </button>
          ))}
        </nav>
        <div className="system-status"><span></span><div><strong>System operational</strong><small>Live tracking enabled</small></div></div>
      </aside>

      <main>
        <header>
          <div><p className="eyebrow">CONTROL CENTER</p><h1>{tab === 'overview' ? 'Operations overview' : tab[0].toUpperCase() + tab.slice(1)}</h1></div>
          <div className="header-actions"><button className="icon-btn" onClick={refresh} title="Refresh">↻</button><div className="live-badge"><span></span> LIVE</div></div>
        </header>

        {toast && <div className="toast">{toast}</div>}

        {tab === 'overview' && (
          <>
            <section className="stats-grid">
              <Stat label="Active vehicles" value={vehicles.length} note="Registered fleet" tone="blue" />
              <Stat label="Geofence zones" value={geofences.length} note={`${categoryCounts.restricted_zone || 0} restricted zones`} tone="purple" />
              <Stat label="Live alerts" value={liveAlerts.length || violations.length} note="Events this session" tone="orange" />
              <Stat label="Tracked locations" value={vehicleMarkers.length} note="Current positions" tone="green" />
            </section>
            <section className="content-grid">
              <div className="card map-card">
                <div className="card-head"><div><p className="eyebrow">LIVE MAP</p><h2>Fleet activity</h2></div><span className="map-status">● Connected</span></div>
                <MapPanel geofences={filteredGeofences} vehicleMarkers={vehicleMarkers} onClick={handleMapClick} />
              </div>
              <div className="card feed-card">
                <div className="card-head"><div><p className="eyebrow">REAL-TIME</p><h2>Alert feed</h2></div><span className="count-badge">{liveAlerts.length}</span></div>
                <AlertFeed alerts={liveAlerts.length ? liveAlerts : violations.map(v => ({ event_id: v.id, event_type: v.event_type, timestamp: v.timestamp, vehicle: { vehicle_number: v.vehicle_number, driver_name: '' }, geofence: { geofence_name: v.geofence_name, category: 'zone' }, location: { latitude: v.latitude, longitude: v.longitude } }))} />
              </div>
            </section>
          </>
        )}

        {tab === 'geofences' && (
          <section className="management-grid">
            <div className="card form-card">
              <div className="card-head"><div><p className="eyebrow">NEW ZONE</p><h2>Create geofence</h2></div></div>
              <form onSubmit={createGeofence}>
                <label>Zone name<input required value={geoForm.name} onChange={e => setGeoForm({...geoForm, name: e.target.value})} placeholder="Downtown delivery zone" /></label>
                <label>Description<textarea value={geoForm.description} onChange={e => setGeoForm({...geoForm, description: e.target.value})} placeholder="Purpose of this zone" /></label>
                <label>Category<select value={geoForm.category} onChange={e => setGeoForm({...geoForm, category: e.target.value})}><option value="delivery_zone">Delivery zone</option><option value="restricted_zone">Restricted zone</option><option value="toll_zone">Toll zone</option><option value="customer_area">Customer area</option></select></label>
                <label>Coordinates <small>latitude,longitude per line</small><textarea value={geoForm.coordinates} onChange={e => setGeoForm({...geoForm, coordinates: e.target.value})} placeholder={'37.7749,-122.4194\n37.7849,-122.4194\n...'} /></label>
                <div className="draw-control"><button type="button" onClick={() => setDrawPoints([])}>Clear map points</button><span>{drawPoints.length} points drawn</span></div>
                <button className="primary" disabled={loading} type="submit">{loading ? 'Creating...' : 'Create geofence'}</button>
              </form>
            </div>
            <div className="card map-card management-map">
              <div className="card-head"><div><p className="eyebrow">BOUNDARY MAP</p><h2>Draw or input zones</h2></div></div>
              <MapPanel geofences={filteredGeofences} drawPoints={drawPoints} onClick={handleMapClick} />
            </div>
            <div className="card zone-list">
              <div className="card-head"><div><p className="eyebrow">ALL ZONES</p><h2>Geofences</h2></div><select className="small-select" value={geoFilter} onChange={e => setGeoFilter(e.target.value)}><option value="">All categories</option><option value="delivery_zone">Delivery</option><option value="restricted_zone">Restricted</option><option value="toll_zone">Toll</option><option value="customer_area">Customer</option></select></div>
              {filteredGeofences.map(g => <div className="list-row" key={g.id}><div className={`zone-dot ${g.category}`}><span></span></div><div><strong>{g.name}</strong><small>{g.category.replaceAll('_', ' ')}</small></div><b>{g.status || 'active'}</b></div>)}
            </div>
          </section>
        )}

        {tab === 'vehicles' && (
          <section className="management-grid vehicle-layout">
            <div className="card form-card">
              <div className="card-head"><div><p className="eyebrow">FLEET</p><h2>Register vehicle</h2></div></div>
              <form onSubmit={registerVehicle}>
                <label>Vehicle number<input required value={vehicleForm.vehicle_number} onChange={e => setVehicleForm({...vehicleForm, vehicle_number: e.target.value})} placeholder="KA-01-AB-1234" /></label>
                <label>Driver name<input required value={vehicleForm.driver_name} onChange={e => setVehicleForm({...vehicleForm, driver_name: e.target.value})} placeholder="John Doe" /></label>
                <label>Vehicle type<select value={vehicleForm.vehicle_type} onChange={e => setVehicleForm({...vehicleForm, vehicle_type: e.target.value})}><option>truck</option><option>car</option><option>van</option><option>motorcycle</option></select></label>
                <label>Phone<input required value={vehicleForm.phone} onChange={e => setVehicleForm({...vehicleForm, phone: e.target.value})} placeholder="+1234567890" /></label>
                <button className="primary" disabled={loading} type="submit">{loading ? 'Registering...' : 'Register vehicle'}</button>
              </form>
            </div>
            <div className="card vehicle-list">
              <div className="card-head"><div><p className="eyebrow">REGISTERED</p><h2>Fleet roster</h2></div><span className="count-badge">{vehicles.length}</span></div>
              {vehicles.map(v => {
                const loc = locations[v.id];
                return <div className="vehicle-row" key={v.id}><div className="vehicle-avatar">T</div><div className="vehicle-details"><strong>{v.vehicle_number}</strong><small>{v.driver_name} · {v.vehicle_type}</small></div><div className="vehicle-status"><b>{loc?.current_geofences?.length ? 'Inside zone' : 'On the move'}</b><small>{loc?.current_location ? `${loc.current_location.latitude.toFixed(4)}, ${loc.current_location.longitude.toFixed(4)}` : 'No location yet'}</small></div></div>;
              })}
            </div>
          </section>
        )}

        {tab === 'locations' && (
          <section className="management-grid location-layout">
            <div className="card form-card">
              <div className="card-head"><div><p className="eyebrow">POSITION UPDATE</p><h2>Update location</h2></div></div>
              <form onSubmit={updateLocation}>
                <label>Vehicle<select required value={locationForm.vehicle_id} onChange={e => setLocationForm({...locationForm, vehicle_id: e.target.value})}><option value="">Select vehicle</option>{vehicles.map(v => <option value={v.id} key={v.id}>{v.vehicle_number}</option>)}</select></label>
                <div className="form-two"><label>Latitude<input type="number" step="any" required value={locationForm.latitude} onChange={e => setLocationForm({...locationForm, latitude: e.target.value})} /></label><label>Longitude<input type="number" step="any" required value={locationForm.longitude} onChange={e => setLocationForm({...locationForm, longitude: e.target.value})} /></label></div>
                <label>Timestamp<input type="datetime-local" value={locationForm.timestamp} onChange={e => setLocationForm({...locationForm, timestamp: e.target.value ? new Date(e.target.value).toISOString() : ''})} /></label>
                <button className="primary" disabled={loading} type="submit">{loading ? 'Updating...' : 'Update location'}</button>
              </form>
            </div>
            <div className="card map-card management-map">
              <div className="card-head"><div><p className="eyebrow">LOCATION MAP</p><h2>Click map to set position</h2></div></div>
              <MapPanel geofences={geofences} vehicleMarkers={vehicleMarkers} onClick={handleMapClick} />
            </div>
          </section>
        )}

        {tab === 'alerts' && (
          <section className="management-grid alert-layout">
            <div className="card form-card">
              <div className="card-head"><div><p className="eyebrow">MONITORING</p><h2>Configure alert</h2></div></div>
              <form onSubmit={configureAlert}>
                <label>Geofence<select required value={alertForm.geofence_id} onChange={e => setAlertForm({...alertForm, geofence_id: e.target.value})}><option value="">Select zone</option>{geofences.map(g => <option value={g.id} key={g.id}>{g.name}</option>)}</select></label>
                <label>Vehicle <small>optional: leave empty for all</small><select value={alertForm.vehicle_id} onChange={e => setAlertForm({...alertForm, vehicle_id: e.target.value})}><option value="">All vehicles</option>{vehicles.map(v => <option value={v.id} key={v.id}>{v.vehicle_number}</option>)}</select></label>
                <label>Event type<select value={alertForm.event_type} onChange={e => setAlertForm({...alertForm, event_type: e.target.value})}><option value="entry">Entry</option><option value="exit">Exit</option><option value="both">Entry & exit</option></select></label>
                <button className="primary" disabled={loading} type="submit">{loading ? 'Saving...' : 'Save alert rule'}</button>
              </form>
            </div>
            <div className="card rule-list">
              <div className="card-head"><div><p className="eyebrow">ACTIVE RULES</p><h2>Alert configurations</h2></div><span className="count-badge">{alerts.length}</span></div>
              {alerts.map(a => <div className="rule-row" key={a.alert_id}><div className="rule-icon">!</div><div><strong>{a.geofence_name}</strong><small>{a.vehicle_number || 'All vehicles'} · {a.event_type} event</small></div><b>{a.status}</b></div>)}
            </div>
          </section>
        )}

        {tab === 'history' && <History violations={violations} geofences={geofences} vehicles={vehicles} onFilter={async params => {
          const qs = new URLSearchParams(params);
          try { const h = await api(`/violations/history?${qs}`); setViolations(h.violations || []); } catch (err) { notify(err.message); }
        }} />}
      </main>
    </div>
  );
}

function Stat({ label, value, note, tone }) {
  return <div className="stat-card"><div className={`stat-icon ${tone}`}></div><div><p>{label}</p><strong>{value}</strong><small>{note}</small></div></div>;
}

function MapPanel({ geofences = [], vehicleMarkers = [], drawPoints = [], onClick }) {
  const center = geofences[0]?.coordinates?.[0] || [37.7749, -122.4194];
  return <MapContainer center={center} zoom={12} className="leaflet-map" scrollWheelZoom>
    <TileLayer attribution="&copy; OpenStreetMap" url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
    <MapClick onPick={onClick} />
    {geofences.map(g => <Polygon key={g.id} positions={g.coordinates} pathOptions={{ color: g.category === 'restricted_zone' ? '#ef4444' : g.category === 'toll_zone' ? '#f59e0b' : '#4f46e5', fillOpacity: 0.16 }}><Popup><strong>{g.name}</strong><br />{g.category.replaceAll('_', ' ')}</Popup></Polygon>)}
    {drawPoints.length > 0 && <Polygon positions={drawPoints} pathOptions={{ color: '#ef4444', fillOpacity: 0.25 }} />}
    {vehicleMarkers.map(({ vehicle, loc }) => <Marker key={vehicle.id} position={[loc.latitude, loc.longitude]} icon={truckIcon}><Popup><strong>{vehicle.vehicle_number}</strong><br />{vehicle.driver_name}</Popup></Marker>)}
  </MapContainer>;
}

function AlertFeed({ alerts }) {
  if (!alerts.length) return <div className="empty-state"><span>✓</span><strong>No alerts yet</strong><small>Live events will appear here</small></div>;
  return <div className="alert-feed">{alerts.slice(0, 12).map((a, i) => <div className="feed-item" key={a.event_id || i}><div className={`event-icon ${a.event_type}`}><span>{a.event_type === 'entry' ? '↓' : '↑'}</span></div><div><strong>{a.vehicle.vehicle_number} {a.event_type === 'entry' ? 'entered' : 'exited'}</strong><small>{a.geofence.geofence_name}</small></div><time>{new Date(a.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time></div>)}</div>;
}

function History({ violations, geofences, vehicles, onFilter }) {
  const [filters, setFilters] = useState({ vehicle_id: '', geofence_id: '', start_date: '', end_date: '' });
  const apply = () => onFilter({
    vehicle_id: filters.vehicle_id,
    geofence_id: filters.geofence_id,
    start_date: filters.start_date ? new Date(filters.start_date).toISOString() : '',
    end_date: filters.end_date ? new Date(filters.end_date).toISOString() : '',
  });
  return <section className="card history-card">
    <div className="card-head"><div><p className="eyebrow">AUDIT LOG</p><h2>Violation history</h2></div></div>
    <div className="history-filters">
      <select value={filters.vehicle_id} onChange={e => setFilters({...filters, vehicle_id: e.target.value})}><option value="">All vehicles</option>{vehicles.map(v => <option key={v.id} value={v.id}>{v.vehicle_number}</option>)}</select>
      <select value={filters.geofence_id} onChange={e => setFilters({...filters, geofence_id: e.target.value})}><option value="">All geofences</option>{geofences.map(g => <option key={g.id} value={g.id}>{g.name}</option>)}</select>
      <input type="date" value={filters.start_date} onChange={e => setFilters({...filters, start_date: e.target.value})} />
      <input type="date" value={filters.end_date} onChange={e => setFilters({...filters, end_date: e.target.value})} />
      <button className="primary" onClick={apply}>Apply filters</button>
    </div>
    <div className="history-table"><div className="table-head"><span>VEHICLE</span><span>GEOFENCE</span><span>EVENT</span><span>LOCATION</span><span>TIME</span></div>
      {violations.map(v => <div className="table-row" key={v.id}><strong>{v.vehicle_number}</strong><span>{v.geofence_name}</span><b className={v.event_type}>{v.event_type}</b><span>{v.latitude.toFixed(4)}, {v.longitude.toFixed(4)}</span><time>{new Date(v.timestamp).toLocaleString()}</time></div>)}
    </div>
  </section>;
}

createRoot(document.getElementById('root')).render(<App />);
