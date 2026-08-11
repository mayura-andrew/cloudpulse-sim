# CloudPulse Server

This directory contains the Go backend for CloudPulse.

Run the server in development:

```bash
cd server
go run ./cmd/server serve
```

Build the server binary:

```bash
cd server
go build -o cloudpulse-server ./cmd/server
```

The backend exposes:

- `POST /api/upload` for CSV uploads
- `GET /api/process-default` for processing the workspace dataset
- `/vizb/` for generated HTML dashboards