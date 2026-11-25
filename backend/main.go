package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Geofence struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Coordinates [][]float64 `json:"coordinates"`
	Category    string      `json:"category"`
	Status      string      `json:"status,omitempty"`
	CreatedAt   string      `json:"created_at,omitempty"`
}

type Vehicle struct {
	ID            string `json:"id"`
	VehicleNumber string `json:"vehicle_number"`
	DriverName    string `json:"driver_name"`
	VehicleType   string `json:"vehicle_type"`
	Phone         string `json:"phone"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timestamp string  `json:"timestamp"`
}

type LocationRecord struct {
	VehicleID string   `json:"vehicle_id"`
	Latitude  float64  `json:"latitude"`
	Longitude float64  `json:"longitude"`
	Timestamp string   `json:"timestamp"`
}

type AlertConfig struct {
	AlertID   string `json:"alert_id"`
	GeofenceID string `json:"geofence_id"`
	VehicleID string `json:"vehicle_id,omitempty"`
	EventType string `json:"event_type"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type Violation struct {
	ID            string  `json:"id"`
	VehicleID     string  `json:"vehicle_id"`
	VehicleNumber string  `json:"vehicle_number"`
	GeofenceID    string  `json:"geofence_id"`
	GeofenceName  string  `json:"geofence_name"`
	EventType     string  `json:"event_type"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	Timestamp     string  `json:"timestamp"`
}

type Store struct {
	Geofences []Geofence   `json:"geofences"`
	Vehicles []Vehicle     `json:"vehicles"`
	Locations map[string]Location `json:"locations"`
	Alerts []AlertConfig   `json:"alerts"`
	Violations []Violation `json:"violations"`
	Previous map[string]map[string]bool `json:"previous"`
}

type Server struct {
	mu sync.RWMutex
	store Store
	file string
	clients map[*websocket.Conn]struct{}
	clientMu sync.Mutex
	upgrader websocket.Upgrader
	sequence int
}

func (s *Server) persist() error {
	data, err := json.MarshalIndent(s.store, "", "  ")
	if err != nil { return err }
	if err := os.MkdirAll(filepath.Dir(s.file), 0755); err != nil { return err }
	return os.WriteFile(s.file, data, 0644)
}

func withTiming(w http.ResponseWriter, start time.Time, status int, payload interface{}) {
	m := map[string]interface{}{}
	if p, ok := payload.(map[string]interface{}); ok {
		for k, v := range p { m[k] = v }
	} else {
		data, _ := json.Marshal(payload)
		json.Unmarshal(data, &m)
	}
	m["time_ns"] = strconv.FormatInt(time.Since(start).Nanoseconds(), 10)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(m)
}

func writeError(w http.ResponseWriter, start time.Time, status int, message string) {
	withTiming(w, start, status, map[string]interface{}{"error": message})
}

const rootStatusPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GeoTrack API</title>
  <style>
    :root { color-scheme: dark; font-family: system-ui, sans-serif; background: #0b1220; color: #e8eef8; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; }
    main { box-sizing: border-box; width: min(680px, calc(100% - 32px)); padding: 36px; border: 1px solid #26354d; border-radius: 18px; background: #111b2c; }
    .status { color: #7ee0aa; font-size: .85rem; font-weight: 700; letter-spacing: .08em; }
    h1 { margin: 14px 0 8px; font-size: clamp(2rem, 6vw, 3rem); }
    p { color: #aab8cd; line-height: 1.6; }
    a { color: #91bbff; }
    .dashboard { display: inline-block; margin: 8px 0 20px; padding: 11px 16px; border-radius: 9px; background: #2463eb; color: white; font-weight: 650; text-decoration: none; }
    h2 { margin-top: 20px; font-size: 1.1rem; }
    ul { padding-left: 20px; line-height: 1.9; }
    code { color: #d3def0; }
  </style>
</head>
<body>
  <main>
    <div class="status">● API ONLINE</div>
    <h1>GeoTrack backend</h1>
    <p>The service is healthy and ready to handle geofencing and fleet requests.</p>
    <a class="dashboard" href="https://geofencing-real-time-alert-system-chi.vercel.app/">Open the GeoTrack dashboard</a>
    <h2>Available API endpoints</h2>
    <ul>
      <li><a href="/vehicles">Vehicles</a></li>
      <li><a href="/geofences">Geofences</a></li>
      <li><a href="/alerts">Alerts</a></li>
      <li><a href="/violations/history">Violation history</a></li>
    </ul>
    <p>Live alert updates use the <code>/ws/alerts</code> WebSocket endpoint.</p>
  </main>
</body>
</html>`

func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, time.Now(), http.StatusNotFound, "route not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, rootStatusPage)
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions { w.WriteHeader(http.StatusOK); return }
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	start := time.Now()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, start, http.StatusBadRequest, "Invalid JSON request body")
		return false
	}
	return true
}

func nextID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func validateCoordinates(coords [][]float64) error {
	if len(coords) < 4 { return fmt.Errorf("coordinates must contain at least 4 points") }
	if len(coords[0]) != 2 || len(coords[len(coords)-1]) != 2 { return fmt.Errorf("each coordinate must be [latitude, longitude]") }
	first, last := coords[0], coords[len(coords)-1]
	if first[0] != last[0] || first[1] != last[1] { return fmt.Errorf("first and last coordinates must be identical") }
	for _, c := range coords {
		if len(c) != 2 { return fmt.Errorf("each coordinate must be [latitude, longitude]") }
		if c[0] < -90 || c[0] > 90 { return fmt.Errorf("latitude must be between -90 and 90") }
		if c[1] < -180 || c[1] > 180 { return fmt.Errorf("longitude must be between -180 and 180") }
	}
	unique := 0
	seen := map[string]bool{}
	for _, c := range coords[:len(coords)-1] {
		key := fmt.Sprintf("%g,%g", c[0], c[1])
		if !seen[key] { seen[key] = true; unique++ }
	}
	if unique < 3 { return fmt.Errorf("polygon must contain at least 3 unique points") }
	return nil
}

func pointInPolygon(lat, lng float64, polygon [][]float64) bool {
	inside := false
	j := len(polygon) - 1
	for i := 0; i < len(polygon); i++ {
		yi, xi := polygon[i][0], polygon[i][1]
		yj, xj := polygon[j][0], polygon[j][1]
		intersect := ((yi > lat) != (yj > lat)) &&
			(lng < (xj-xi)*(lat-yi)/(yj-yi+0.0000000001)+xi)
		if intersect { inside = !inside }
		j = i
	}
	return inside
}

func (s *Server) geofenceStatus(vehicleID string) []map[string]interface{} {
	loc, ok := s.store.Locations[vehicleID]
	if !ok { return []map[string]interface{}{} }
	result := []map[string]interface{}{}
	for _, g := range s.store.Geofences {
		if pointInPolygon(loc.Latitude, loc.Longitude, g.Coordinates) {
			result = append(result, map[string]interface{}{
				"geofence_id": g.ID, "geofence_name": g.Name,
				"category": g.Category, "status": "inside",
			})
		}
	}
	return result
}

func (s *Server) handleGeofences(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method == http.MethodPost {
		var input struct {
			Name string `json:"name"`; Description string `json:"description"`
			Coordinates [][]float64 `json:"coordinates"`; Category string `json:"category"`
		}
		if !decodeJSON(w, r, &input) { return }
		if strings.TrimSpace(input.Name) == "" { writeError(w, start, 400, "name is required"); return }
		categories := map[string]bool{"delivery_zone": true, "restricted_zone": true, "toll_zone": true, "customer_area": true}
		if !categories[input.Category] { writeError(w, start, 400, "invalid category"); return }
		if err := validateCoordinates(input.Coordinates); err != nil { writeError(w, start, 400, err.Error()); return }
		s.mu.Lock()
		g := Geofence{ID: nextID("geo"), Name: input.Name, Description: input.Description, Coordinates: input.Coordinates, Category: input.Category, Status: "active", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		s.store.Geofences = append(s.store.Geofences, g)
		err := s.persist()
		s.mu.Unlock()
		if err != nil { writeError(w, start, 500, "failed to persist geofence"); return }
		withTiming(w, start, 201, map[string]interface{}{"id": g.ID, "name": g.Name, "status": "active"})
		return
	}
	category := r.URL.Query().Get("category")
	s.mu.RLock()
	result := []Geofence{}
	for _, g := range s.store.Geofences { if category == "" || g.Category == category { result = append(result, g) } }
	s.mu.RUnlock()
	withTiming(w, start, 200, map[string]interface{}{"geofences": result})
}

func (s *Server) handleVehicles(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method == http.MethodPost {
		var input Vehicle
		if !decodeJSON(w, r, &input) { return }
		if strings.TrimSpace(input.VehicleNumber) == "" || strings.TrimSpace(input.DriverName) == "" || strings.TrimSpace(input.VehicleType) == "" || strings.TrimSpace(input.Phone) == "" {
			writeError(w, start, 400, "vehicle_number, driver_name, vehicle_type and phone are required"); return
		}
		s.mu.Lock()
		for _, v := range s.store.Vehicles {
			if v.VehicleNumber == input.VehicleNumber { s.mu.Unlock(); writeError(w, start, 409, "vehicle number already exists"); return }
		}
		v := Vehicle{ID: nextID("veh"), VehicleNumber: input.VehicleNumber, DriverName: input.DriverName, VehicleType: input.VehicleType, Phone: input.Phone, Status: "active", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		s.store.Vehicles = append(s.store.Vehicles, v)
		err := s.persist()
		s.mu.Unlock()
		if err != nil { writeError(w, start, 500, "failed to persist vehicle"); return }
		withTiming(w, start, 201, map[string]interface{}{"id": v.ID, "vehicle_number": v.VehicleNumber, "status": "active"})
		return
	}
	s.mu.RLock(); vehicles := append([]Vehicle{}, s.store.Vehicles...); s.mu.RUnlock()
	withTiming(w, start, 200, map[string]interface{}{"vehicles": vehicles})
}

func (s *Server) broadcast(payload interface{}) {
	data, _ := json.Marshal(payload)
	s.clientMu.Lock(); defer s.clientMu.Unlock()
	for conn := range s.clients {
		if err := conn.WriteJSON(payload); err != nil { conn.Close(); delete(s.clients, conn) }
	}
	_ = data
}

func (s *Server) handleLocationUpdate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var input LocationRecord
	if !decodeJSON(w, r, &input) { return }
	if input.Latitude < -90 || input.Latitude > 90 || input.Longitude < -180 || input.Longitude > 180 {
		writeError(w, start, 400, "invalid latitude or longitude"); return
	}
	if strings.TrimSpace(input.Timestamp) == "" { writeError(w, start, 400, "timestamp is required"); return }
	if _, err := time.Parse(time.RFC3339, input.Timestamp); err != nil { writeError(w, start, 400, "timestamp must be ISO 8601"); return }

	s.mu.Lock()
	var vehicle *Vehicle
	for i := range s.store.Vehicles { if s.store.Vehicles[i].ID == input.VehicleID { vehicle = &s.store.Vehicles[i]; break } }
	if vehicle == nil { s.mu.Unlock(); writeError(w, start, 404, "vehicle not found"); return }
	s.store.Locations[input.VehicleID] = Location{Latitude: input.Latitude, Longitude: input.Longitude, Timestamp: input.Timestamp}
	if s.store.Previous == nil { s.store.Previous = map[string]map[string]bool{} }
	if s.store.Previous[input.VehicleID] == nil { s.store.Previous[input.VehicleID] = map[string]bool{} }
	previous := s.store.Previous[input.VehicleID]
	current := map[string]bool{}
	currentGeofences := []map[string]interface{}{}
	events := []Violation{}
	for _, g := range s.store.Geofences {
		inside := pointInPolygon(input.Latitude, input.Longitude, g.Coordinates)
		current[g.ID] = inside
		if inside { currentGeofences = append(currentGeofences, map[string]interface{}{"geofence_id": g.ID, "geofence_name": g.Name, "status": "inside", "category": g.Category}) }
		eventType := ""
		if inside && !previous[g.ID] { eventType = "entry" } else if !inside && previous[g.ID] { eventType = "exit" }
		if eventType == "" { continue }
		matched := false
		for _, cfg := range s.store.Alerts {
			if cfg.Status != "active" || cfg.GeofenceID != g.ID { continue }
			if cfg.VehicleID != "" && cfg.VehicleID != input.VehicleID { continue }
			if cfg.EventType == "both" || cfg.EventType == eventType { matched = true; break }
		}
		if !matched && g.Category != "restricted_zone" { continue }
		if g.Category == "restricted_zone" { matched = true }
		if matched {
			v := Violation{ID: nextID("viol"), VehicleID: vehicle.ID, VehicleNumber: vehicle.VehicleNumber, GeofenceID: g.ID, GeofenceName: g.Name, EventType: eventType, Latitude: input.Latitude, Longitude: input.Longitude, Timestamp: input.Timestamp}
			s.store.Violations = append(s.store.Violations, v)
			events = append(events, v)
		}
	}
	s.store.Previous[input.VehicleID] = current
	err := s.persist()
	vehicleCopy := *vehicle
	s.mu.Unlock()
	if err != nil { writeError(w, start, 500, "failed to persist location"); return }
	for _, v := range events {
		var g Geofence
		for _, candidate := range s.store.Geofences { if candidate.ID == v.GeofenceID { g = candidate; break } }
		alert := map[string]interface{}{
			"event_id": nextID("evt"), "event_type": v.EventType, "timestamp": v.Timestamp,
			"vehicle": map[string]interface{}{"vehicle_id": v.VehicleID, "vehicle_number": v.VehicleNumber, "driver_name": vehicleCopy.DriverName},
			"geofence": map[string]interface{}{"geofence_id": g.ID, "geofence_name": g.Name, "category": g.Category},
			"location": map[string]interface{}{"latitude": v.Latitude, "longitude": v.Longitude},
		}
		go s.broadcast(alert)
	}
	withTiming(w, start, 200, map[string]interface{}{"vehicle_id": input.VehicleID, "location_updated": true, "current_geofences": currentGeofences})
}

func (s *Server) handleVehicleLocation(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := strings.TrimPrefix(r.URL.Path, "/vehicles/location/")
	s.mu.RLock()
	var vehicle *Vehicle
	for i := range s.store.Vehicles { if s.store.Vehicles[i].ID == id { copy := s.store.Vehicles[i]; vehicle = &copy; break } }
	loc, ok := s.store.Locations[id]
	status := s.geofenceStatusLocked(id)
	s.mu.RUnlock()
	if vehicle == nil { writeError(w, start, 404, "vehicle not found"); return }
	if !ok { withTiming(w, start, 200, map[string]interface{}{"vehicle_id": id, "vehicle_number": vehicle.VehicleNumber, "current_location": nil, "current_geofences": []interface{}{}}); return }
	withTiming(w, start, 200, map[string]interface{}{"vehicle_id": id, "vehicle_number": vehicle.VehicleNumber, "current_location": loc, "current_geofences": status})
}

func (s *Server) geofenceStatusLocked(vehicleID string) []map[string]interface{} {
	loc, ok := s.store.Locations[vehicleID]
	if !ok { return []map[string]interface{}{} }
	result := []map[string]interface{}{}
	for _, g := range s.store.Geofences {
		if pointInPolygon(loc.Latitude, loc.Longitude, g.Coordinates) {
			result = append(result, map[string]interface{}{"geofence_id": g.ID, "geofence_name": g.Name, "category": g.Category})
		}
	}
	return result
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method == http.MethodPost {
		var input AlertConfig
		if !decodeJSON(w, r, &input) { return }
		if input.GeofenceID == "" || (input.EventType != "entry" && input.EventType != "exit" && input.EventType != "both") {
			writeError(w, start, 400, "geofence_id and a valid event_type are required"); return
		}
		s.mu.Lock()
		foundGeo := false
		for _, g := range s.store.Geofences { if g.ID == input.GeofenceID { foundGeo = true; break } }
		if !foundGeo { s.mu.Unlock(); writeError(w, start, 404, "geofence not found"); return }
		if input.VehicleID != "" {
			foundVehicle := false
			for _, v := range s.store.Vehicles { if v.ID == input.VehicleID { foundVehicle = true; break } }
			if !foundVehicle { s.mu.Unlock(); writeError(w, start, 404, "vehicle not found"); return }
		}
		a := AlertConfig{AlertID: nextID("alert"), GeofenceID: input.GeofenceID, VehicleID: input.VehicleID, EventType: input.EventType, Status: "active", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		s.store.Alerts = append(s.store.Alerts, a)
		err := s.persist()
		s.mu.Unlock()
		if err != nil { writeError(w, start, 500, "failed to persist alert"); return }
		withTiming(w, start, 201, map[string]interface{}{"alert_id": a.AlertID, "geofence_id": a.GeofenceID, "vehicle_id": a.VehicleID, "event_type": a.EventType, "status": "active"})
		return
	}
	geoID := r.URL.Query().Get("geofence_id"); vehicleID := r.URL.Query().Get("vehicle_id")
	s.mu.RLock()
	result := []map[string]interface{}{}
	for _, a := range s.store.Alerts {
		if geoID != "" && a.GeofenceID != geoID { continue }
		if vehicleID != "" && a.VehicleID != vehicleID { continue }
		geoName, vehicleNumber := "", ""
		for _, g := range s.store.Geofences { if g.ID == a.GeofenceID { geoName = g.Name; break } }
		for _, v := range s.store.Vehicles { if v.ID == a.VehicleID { vehicleNumber = v.VehicleNumber; break } }
		result = append(result, map[string]interface{}{"alert_id": a.AlertID, "geofence_id": a.GeofenceID, "geofence_name": geoName, "vehicle_id": a.VehicleID, "vehicle_number": vehicleNumber, "event_type": a.EventType, "status": a.Status, "created_at": a.CreatedAt})
	}
	s.mu.RUnlock()
	withTiming(w, start, 200, map[string]interface{}{"alerts": result})
}

func (s *Server) handleViolations(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v); if err == nil && n > 0 { limit = n }
	}
	if limit > 500 { limit = 500 }
	parseDate := func(value string) (time.Time, bool) {
		if value == "" { return time.Time{}, true }
		t, err := time.Parse(time.RFC3339, value); return t, err == nil
	}
	startDate, ok1 := parseDate(q.Get("start_date")); endDate, ok2 := parseDate(q.Get("end_date"))
	if !ok1 || !ok2 { writeError(w, start, 400, "invalid date filter"); return }
	s.mu.RLock()
	filtered := []Violation{}
	for i := len(s.store.Violations)-1; i >= 0; i-- {
		v := s.store.Violations[i]
		if q.Get("vehicle_id") != "" && v.VehicleID != q.Get("vehicle_id") { continue }
		if q.Get("geofence_id") != "" && v.GeofenceID != q.Get("geofence_id") { continue }
		if !startDate.IsZero() || !endDate.IsZero() {
			t, err := time.Parse(time.RFC3339, v.Timestamp)
			if err == nil {
				if !startDate.IsZero() && t.Before(startDate) { continue }
				if !endDate.IsZero() && t.After(endDate) { continue }
			}
		}
		filtered = append(filtered, v)
	}
	total := len(filtered)
	if len(filtered) > limit { filtered = filtered[:limit] }
	s.mu.RUnlock()
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Timestamp > filtered[j].Timestamp })
	withTiming(w, start, 200, map[string]interface{}{"violations": filtered, "total_count": total})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil { log.Println("websocket upgrade:", err); return }
	s.clientMu.Lock(); s.clients[conn] = struct{}{}; s.clientMu.Unlock()
	defer func() { s.clientMu.Lock(); delete(s.clients, conn); s.clientMu.Unlock(); conn.Close() }()
	for { if _, _, err := conn.ReadMessage(); err != nil { return } }
}

func main() {
	file := os.Getenv("DATA_FILE")
	if file == "" { file = "./data/store.json" }
	store := Store{
		Geofences: []Geofence{}, Vehicles: []Vehicle{}, Alerts: []AlertConfig{},
		Violations: []Violation{}, Locations: map[string]Location{},
		Previous: map[string]map[string]bool{},
	}
	if data, err := os.ReadFile(file); err == nil {
		if err := json.Unmarshal(data, &store); err != nil { log.Println("could not read stored data:", err) }
	}
	server := &Server{
		store: store, file: file, clients: map[*websocket.Conn]struct{}{},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/geofences", server.handleGeofences)
	mux.HandleFunc("/vehicles", server.handleVehicles)
	mux.HandleFunc("/vehicles/location", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vehicles/location" { server.handleLocationUpdate(w, r); return }
		server.handleVehicleLocation(w, r)
	})
	mux.HandleFunc("/vehicles/location/", server.handleVehicleLocation)
	mux.HandleFunc("/alerts", server.handleAlerts)
	mux.HandleFunc("/alerts/configure", server.handleAlerts)
	mux.HandleFunc("/violations/history", server.handleViolations)
	mux.HandleFunc("/ws/alerts", server.handleWS)
	mux.HandleFunc("/", handleRoot)

	port := os.Getenv("PORT")
	if port == "" { port = "8080" }
	log.Printf("Geofencing API listening on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, server.cors(mux)))
}
