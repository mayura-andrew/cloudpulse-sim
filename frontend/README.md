# CloudPulse Visualization Frontend

This is a minimal React + ECharts frontend for visualizing `processed_cloud_task_metrics.csv` produced by the CloudPulse Go engine.

Quick start:

Install dependencies in the `frontend` folder and run the dev server:

```bash
cd frontend
npm install
npm run dev
```

Backend integration options:

- Development: run the Go backend from the `server` module. This exposes processing endpoints on `http://localhost:8080`:

```bash
cd server
go run ./cmd/server serve
```

- The frontend upload button will POST the CSV to `http://localhost:8080/api/upload` and receive processed JSON rows for visualization.

Usage:
- Use the file input to upload a CSV file; the frontend will send it to the Go backend for processing.
- Or click `Load processed_cloud_task_metrics.csv` to ask the backend to process the workspace `dataset.csv` and return results.

The app renders per-VM bar charts for CPU/RAM/Disk/Network, a scatter plot of `Task_ID` vs `Queue_Wait_s`, and histograms for `Execution_Time_s` and `Total_Response_s`.
