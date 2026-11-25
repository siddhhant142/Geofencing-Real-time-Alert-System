# GeoTrack Geofencing System

## Prerequisites

- Go 1.21 or newer
- Node.js 20 or newer
- Docker and Docker Compose (optional)

## Local setup

### Backend

```powershell
cd C:\geofencing-app\backend
go mod download
go run .
```

The API listens at `http://localhost:8080`. JSON state is stored in `backend/data/store.json`.

### Frontend

```powershell
cd C:\geofencing-app\frontend
npm install
npm run dev
```

Open `http://localhost:5173`.

Set `VITE_API_URL` to a remote API URL when deploying the frontend.

## Docker

```powershell
cd C:\geofencing-app
docker compose up --build
```

The frontend is available at `http://localhost:5173` and the API at `http://localhost:8080`.
The containerized frontend proxies API requests and the `/ws/alerts` WebSocket through its Nginx server.

## Publish the backend image

Add these repository Actions secrets under **Settings → Secrets and variables → Actions**:

- `DOCKERHUB_USERNAME`: your Docker Hub username
- `DOCKERHUB_TOKEN`: a Docker Hub access token with write access

Then run **Actions → Publish backend image → Run workflow**. The image is published as
`<your-dockerhub-username>/geofencing-real-time-alert-api:latest` and tagged with the commit SHA.

## Hosted deployment

The GitHub repository is private, so authorize Render and Vercel to access it when connecting the project. Deployment configuration is included in `render.yaml` and `frontend/vercel.json`.

### Free backend on Render

1. Sign in to Render, choose **New → Blueprint**, and connect `siddhhant142/Geofencing-Real-time-Alert-System`.
2. Render reads `render.yaml` and creates the Docker web service on its **Free** plan. Confirm the plan is Free before deploying.
3. When deployment finishes, copy the service URL. The Go server reads Render's `PORT` environment variable.

Free Render services sleep after 15 minutes without traffic, may take about a minute to wake, and have ephemeral filesystems. This app stores its demo data in `/tmp/geofencing-store.json`, so geofences, vehicles, and history can be lost when the service sleeps, restarts, or redeploys. Render persistent disks require a paid plan and are intentionally not configured here.

### Free frontend on Vercel

1. Sign in to Vercel Hobby (free) and import the same private repository. Authorize GitHub access if prompted.
2. Set the project root directory to `frontend`; `frontend/vercel.json` provides the Vite build and SPA fallback settings.
3. Add `VITE_API_URL` with the deployed Render service URL (for example, `https://your-service.onrender.com`) for Production and Preview.
4. Deploy. The dashboard uses this value for both REST requests and `/ws/alerts`.

### GitHub collaborator invitations

Open [repository access settings](https://github.com/siddhhant142/Geofencing-Real-time-Alert-System/settings/access) → **Add people** and invite the remaining seven assessment emails with **Write** access. Invitations are already pending for the two verified source-repository contributors, `vedant-patil-mapup` and `dewanshpatle`; avoid sending duplicate invitations for those accounts.

## API examples

Open the backend root URL in a browser to see the service status and links to the read endpoints. The root returns a small HTML status page; the API routes below return JSON.

Create a geofence:

```powershell
curl -X POST http://localhost:8080/geofences -H "Content-Type: application/json" -d "{\"name\":\"Downtown Delivery Zone\",\"description\":\"Main delivery area\",\"coordinates\":[[37.7749,-122.4194],[37.7849,-122.4194],[37.7849,-122.4094],[37.7749,-122.4094],[37.7749,-122.4194]],\"category\":\"delivery_zone\"}"
```

Register a vehicle:

```powershell
curl -X POST http://localhost:8080/vehicles -H "Content-Type: application/json" -d "{\"vehicle_number\":\"KA-01-AB-1234\",\"driver_name\":\"John Doe\",\"vehicle_type\":\"truck\",\"phone\":\"+1234567890\"}"
```

Update a location:

```powershell
curl -X POST http://localhost:8080/vehicles/location -H "Content-Type: application/json" -d "{\"vehicle_id\":\"veh_REPLACE\",\"latitude\":37.7799,\"longitude\":-122.4144,\"timestamp\":\"2025-01-15T10:35:00Z\"}"
```

Configure an alert:

```powershell
curl -X POST http://localhost:8080/alerts/configure -H "Content-Type: application/json" -d "{\"geofence_id\":\"geo_REPLACE\",\"vehicle_id\":\"veh_REPLACE\",\"event_type\":\"entry\"}"
```

## Frontend usage

1. Create geofences by entering coordinates or clicking polygons on the map.
2. Register vehicles.
3. Select a vehicle and update its coordinates.
4. Configure entry, exit, or both event rules.
5. Watch real-time alerts arrive through `/ws/alerts`.
6. Filter and inspect historical events.

## Architecture

- `backend/`: Go HTTP API, point-in-polygon checks, file-backed persistence, and Gorilla WebSocket alert broadcasting.
- `frontend/`: React/Vite dashboard with Leaflet map visualization.
- `docker-compose.yml`: Runs both containers and persists backend state in a Docker volume.
