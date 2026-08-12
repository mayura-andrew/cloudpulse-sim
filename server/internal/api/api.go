// Package api implements the HTTP server, REST API endpoints, and
// visualization dashboard generation for the CloudPulse system.
//
// This package acts as the glue layer between:
//   - The frontend (React SPA)        — served via static file serving
//   - The simulation engine           — invoked via processor package
//   - The visualization tool (vizb)   — invoked via CLI subprocess
//
// API Endpoints:
//
//	POST /api/upload         — Upload a CSV dataset, run simulation, return JSON report
//	GET  /api/process-default — Process the default workspace dataset, return JSON report
//	GET  /vizb/*             — Serve generated HTML dashboard files (static)
//	GET  /                   — Serve the React frontend build (static)
//
// JSON Response Structure (processResponse):
//
//	{
//	  "dashboards":    [...],    // List of generated HTML chart file metadata
//	  "vizbAvailable": true,     // Whether the vizb CLI was found and charts generated
//	  "overview":      [...],    // Array of 7 metricSummary objects (CPU, RAM, etc.)
//	  "tables":        [...],    // Array of 5 reportTable objects (VM footprint, SLA, etc.)
//	  "message":       "..."     // Optional status message
//	}
package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"cloudpulse-sim/server/internal/processor"
)

// vizbOutputDir is the directory where generated HTML dashboard files
// are written. The vizb CLI tool produces self-contained HTML files
// with embedded Apache ECharts visualizations.
const vizbOutputDir = "generated/vizb"

// ============================================================================
// JSON RESPONSE TYPES
// ============================================================================
//
// These structs define the shape of the JSON response sent to the frontend.
// Each struct uses json tags for automatic serialization by encoding/json.

// dashboardInfo describes a single generated HTML dashboard chart.
// The frontend uses the URL field to embed each chart in an iframe.
type dashboardInfo struct {
	Title string `json:"title"` // Human-readable chart title (e.g., "Section 2 CPU Footprint").
	File  string `json:"file"`  // Filename of the HTML file (e.g., "fig_section2_cpu_footprint.html").
	URL   string `json:"url"`   // URL path to access the file via the /vizb/ endpoint.
}

// metricSummary holds the statistical profile for a single performance metric.
// Seven of these are returned in the "overview" array — one each for
// CPU, RAM, Disk IO, Network IO, Execution Time, Queue Wait, Total Response.
type metricSummary struct {
	Metric string  `json:"metric"` // Metric name (e.g., "CPU Usage", "Queue Wait").
	Count  int     `json:"count"`  // Number of data points (N = number of tasks).
	Mean   float64 `json:"mean"`   // Arithmetic mean.
	StdDev float64 `json:"stdDev"` // Population standard deviation.
	Min    float64 `json:"min"`    // Minimum observed value.
	Max    float64 `json:"max"`    // Maximum observed value.
	Median float64 `json:"median"` // 50th percentile (middle value).
	P95    float64 `json:"p95"`    // 95th percentile (tail-latency indicator).
}

// reportTable represents a tabular dataset with a title, column headers,
// and rows of string values. Five tables are returned:
//   1. VM Resource Footprint     (per-VM avg CPU, RAM, Disk, Network)
//   2. SLA Compliance Summary    (optimal vs non-optimal response times)
//   3. Priority Queue Wait       (mean queue delay per priority tier)
//   4. Histogram Bins            (execution and response time distributions)
//   5. VM SLA Ratio              (optimal % vs non-optimal % per VM)
type reportTable struct {
	Title   string     `json:"title"`   // Table title for display in the frontend.
	Headers []string   `json:"headers"` // Column header labels.
	Rows    [][]string `json:"rows"`    // Data rows (each row is a string array).
}

// processResponse is the top-level JSON structure returned by both
// /api/upload and /api/process-default endpoints.
type processResponse struct {
	Dashboards    []dashboardInfo `json:"dashboards"`          // List of all available chart dashboard files.
	VizbAvailable bool            `json:"vizbAvailable"`       // True if vizb CLI was found and charts were generated.
	Overview      []metricSummary `json:"overview"`             // Statistical summaries for 7 key metrics.
	Tables        []reportTable   `json:"tables"`               // Aggregated data tables for the frontend.
	Message       string          `json:"message,omitempty"`    // Optional informational message.
}

// vizbDashboardSpec defines the configuration for generating a single
// vizb HTML dashboard. Each spec maps to one vizb CLI invocation.
type vizbDashboardSpec struct {
	command string   // Vizb chart type: "bar" or "scatter".
	input   string   // Path to the input CSV file for this chart.
	output  string   // Path where the output HTML file should be written.
	args    []string // Additional CLI arguments (e.g., --select columns).
}

// ============================================================================
// HTTP SERVER SETUP
// ============================================================================

// StartServer initializes and starts the CloudPulse HTTP server on port 8080.
//
// It registers four route groups:
//   1. POST /api/upload         → handleUpload   (CSV upload + simulation)
//   2. GET  /api/process-default → handleProcessDefault (default dataset)
//   3. GET  /vizb/*             → static file server for HTML dashboards
//   4. GET  /                   → static file server for React frontend build
//
// The server blocks on http.ListenAndServe and runs until terminated.
func StartServer() {
	mux := http.NewServeMux()

	// Register API endpoint handlers.
	mux.HandleFunc("/api/upload", handleUpload)
	mux.HandleFunc("/api/process-default", handleProcessDefault)

	// Serve generated vizb HTML charts from the generated/vizb/ directory.
	// http.StripPrefix removes the "/vizb/" prefix so the file server
	// maps /vizb/fig_section2_cpu_footprint.html → generated/vizb/fig_section2_cpu_footprint.html.
	mux.Handle("/vizb/", http.StripPrefix("/vizb/", http.FileServer(http.Dir(vizbOutputDir))))

	// Ensure the vizb output directory exists (create if missing).
	if err := os.MkdirAll(vizbOutputDir, 0o755); err != nil {
		fmt.Printf("[WARNING] unable to prepare vizb output dir: %v\n", err)
	}

	// If the frontend production build exists at ../frontend/dist/,
	// serve it as the root route. This allows the backend to serve
	// the complete application from a single port (8080).
	dist := filepath.Join("..", "frontend", "dist")
	if _, err := os.Stat(dist); err == nil {
		mux.Handle("/", http.FileServer(http.Dir(dist)))
	}

	// Start the HTTP server. This call blocks until the server stops.
	addr := ":8080"
	fmt.Printf("[INFO] Starting CloudPulse server on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Printf("[ERROR] server failed: %v\n", err)
	}
}

// ============================================================================
// API ENDPOINT HANDLERS
// ============================================================================

// handleUpload handles POST /api/upload requests.
//
// Request flow:
//   1. Parse the multipart form data (max 32 MB upload size)
//   2. Extract the "file" field containing the uploaded CSV
//   3. Save the uploaded file to a temporary location
//   4. Parse the CSV into Task structs (LoadKaggleCSV)
//   5. Run the discrete-event queueing simulation (SimulateQueueingDynamics)
//   6. Export the processed dataset to processed_cloud_task_metrics.csv
//   7. Attempt to generate vizb HTML dashboards
//   8. Build and return the JSON report response
//
// CORS: Access-Control-Allow-Origin is set to "*" to allow the frontend
// dev server (localhost:5173) to call this API on localhost:8080.
func handleUpload(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for cross-origin requests from the frontend dev server.
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Handle CORS preflight OPTIONS request.
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Only accept POST requests for file uploads.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the multipart form with a 32 MB memory limit.
	// Files larger than this are stored in temporary disk files.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse multipart form", http.StatusBadRequest)
		return
	}

	// Extract the uploaded CSV file from the "file" form field.
	f, fh, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer f.Close()

	// Save the uploaded file to a temporary location for processing.
	tmp, err := saveUploadedFile(f, fh)
	if err != nil {
		http.Error(w, "failed to save uploaded file", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp) // Clean up the temp file after processing.

	// --- Core pipeline: Parse → Simulate → Export → Visualize → Respond ---

	// Step 1: Parse the CSV into Task structs.
	tasks, err := processor.LoadKaggleCSV(tmp)
	if err != nil {
		http.Error(w, "failed to parse CSV", http.StatusBadRequest)
		return
	}

	// Step 2: Run the discrete-event queueing simulation.
	// This populates ArrivalTime, QueueWaitTime, StartTime, etc.
	processor.SimulateQueueingDynamics(tasks)

	// Step 3: Export the processed dataset with all 14 columns.
	_ = processor.ExportCSV("processed_cloud_task_metrics.csv", tasks)

	// Step 4: Attempt to generate HTML dashboard visualizations via vizb CLI.
	vizbAvailable := triggerVizbDashboards(tasks, "processed_cloud_task_metrics.csv")

	// Step 5: Build and send the JSON report response to the frontend.
	writeReportJSON(w, tasks, vizbAvailable)
}

// handleProcessDefault handles GET /api/process-default requests.
//
// This endpoint processes a default dataset found in the workspace,
// allowing the frontend to automatically load results on page open
// without requiring the user to upload a file.
//
// Dataset resolution order (first match wins):
//   1. ../cloud_task_scheduling_dataset.csv     (1K dataset, relative to server/)
//   2. ../cloud_task_scheduling_dataset_20k.csv  (20K dataset)
//   3. ../dataset.csv                           (generic fallback)
//   4-6. Same filenames without ../ prefix       (if server runs from project root)
func handleProcessDefault(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for cross-origin requests.
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Handle CORS preflight OPTIONS request.
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Only accept GET requests.
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Try multiple candidate paths to find the default dataset.
	// The server runs from the server/ directory, so ../filename
	// looks in the project root where the CSV files are located.
	candidates := []string{
		filepath.Join("..", "cloud_task_scheduling_dataset.csv"),
		filepath.Join("..", "cloud_task_scheduling_dataset_20k.csv"),
		filepath.Join("..", "dataset.csv"),
		"cloud_task_scheduling_dataset.csv",
		"cloud_task_scheduling_dataset_20k.csv",
		"dataset.csv",
	}

	// Find the first candidate that exists on disk.
	var input string
	for _, cand := range candidates {
		if _, err := os.Stat(cand); err == nil {
			input = cand
			break
		}
	}
	if input == "" {
		http.Error(w, "no default dataset found in workspace", http.StatusNotFound)
		return
	}

	// --- Core pipeline: Parse → Simulate → Export → Visualize → Respond ---

	tasks, err := processor.LoadKaggleCSV(input)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to load dataset (%s): %v", input, err), http.StatusInternalServerError)
		return
	}
	processor.SimulateQueueingDynamics(tasks)
	_ = processor.ExportCSV("processed_cloud_task_metrics.csv", tasks)
	vizbAvailable := triggerVizbDashboards(tasks, "processed_cloud_task_metrics.csv")

	writeReportJSON(w, tasks, vizbAvailable)
}

// ============================================================================
// RESPONSE BUILDERS
// ============================================================================

// writeReportJSON constructs the complete JSON report payload and writes
// it to the HTTP response. This is called by both handleUpload and
// handleProcessDefault after the simulation completes.
//
// The response includes:
//   - Dashboard manifest (list of 9 generated chart files)
//   - Vizb availability flag
//   - Overview metrics (7 statistical summaries)
//   - Report tables (5 aggregated data tables)
func writeReportJSON(w http.ResponseWriter, tasks []processor.Task, vizbAvailable bool) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(processResponse{
		Dashboards:    vizbDashboardManifest(),
		VizbAvailable: vizbAvailable,
		Overview:      buildOverviewMetrics(tasks),
		Tables:        buildReportTables(tasks),
	})
}

// ============================================================================
// FILE HANDLING UTILITIES
// ============================================================================

// saveUploadedFile writes an uploaded multipart file to a temporary file
// on disk and returns the path. The caller is responsible for removing
// the temporary file after processing (using defer os.Remove).
func saveUploadedFile(f multipart.File, fh *multipart.FileHeader) (string, error) {
	tmpf, err := os.CreateTemp("", "uploaded-*.csv")
	if err != nil {
		return "", err
	}
	defer tmpf.Close()

	// Copy the uploaded file content to the temp file.
	if _, err := io.Copy(tmpf, f); err != nil {
		return "", err
	}
	return tmpf.Name(), nil
}

// ============================================================================
// DASHBOARD MANIFEST
// ============================================================================

// vizbDashboardManifest returns the ordered list of all 9 HTML dashboard
// files that the vizb CLI is expected to generate. This manifest is always
// included in the response regardless of whether vizb is available,
// allowing the frontend to show placeholder states when charts are missing.
//
// Dashboard organization follows the report structure:
//   Section 2 (System Architecture): 4 resource footprint bar charts
//   Section 3 (Performance Goals):   1 SLA compliance bar chart
//   Section 6 (Priority Analysis):   1 priority queue wait bar chart
//   Section 7 (Visualizations):      2 distribution charts + 1 SLA ratio chart
func vizbDashboardManifest() []dashboardInfo {
	return []dashboardInfo{
		{Title: "Section 2 CPU Footprint", File: "fig_section2_cpu_footprint.html", URL: "/vizb/fig_section2_cpu_footprint.html"},
		{Title: "Section 2 RAM Footprint", File: "fig_section2_ram_footprint.html", URL: "/vizb/fig_section2_ram_footprint.html"},
		{Title: "Section 2 Disk Footprint", File: "fig_section2_disk_footprint.html", URL: "/vizb/fig_section2_disk_footprint.html"},
		{Title: "Section 2 Network Footprint", File: "fig_section2_network_footprint.html", URL: "/vizb/fig_section2_network_footprint.html"},
		{Title: "Section 3 Performance Goals", File: "fig_section3_performance_goals.html", URL: "/vizb/fig_section3_performance_goals.html"},
		{Title: "Section 6 Priority Wait", File: "fig_section6_priority_wait.html", URL: "/vizb/fig_section6_priority_wait.html"},
		{Title: "Section 7 Histogram", File: "fig_section7_histogram.html", URL: "/vizb/fig_section7_histogram.html"},
		{Title: "Section 7 Queue Growth", File: "fig_section7_scatter_queue_growth.html", URL: "/vizb/fig_section7_scatter_queue_growth.html"},
		{Title: "Section 7 VM SLA Ratio", File: "fig_section7_vm_sla_ratio.html", URL: "/vizb/fig_section7_vm_sla_ratio.html"},
	}
}

// ============================================================================
// METRICS & TABLES BUILDERS
// ============================================================================

// buildOverviewMetrics extracts 7 key performance metrics from the task
// data and computes a full statistical summary for each one.
//
// The 7 metrics correspond to the frontend's KPI cards and overview table:
//   1. CPU Usage (%)         — compute resource utilization
//   2. RAM Usage (MB)        — memory allocation
//   3. Disk IO (MB/s)        — storage I/O throughput
//   4. Network IO (MB/s)     — network bandwidth
//   5. Execution Time (s)    — active service duration (T_Execution)
//   6. Queue Wait (s)        — buffering delay (T_Queue)
//   7. Total Response (s)    — end-to-end turnaround (T_Total)
func buildOverviewMetrics(tasks []processor.Task) []metricSummary {
	return []metricSummary{
		buildMetricSummary("CPU Usage", extractFloatField(tasks, func(t processor.Task) float64 { return t.CPUUsage })),
		buildMetricSummary("RAM Usage", extractFloatField(tasks, func(t processor.Task) float64 { return t.RAMUsage })),
		buildMetricSummary("Disk IO", extractFloatField(tasks, func(t processor.Task) float64 { return t.DiskIO })),
		buildMetricSummary("Network IO", extractFloatField(tasks, func(t processor.Task) float64 { return t.NetworkIO })),
		buildMetricSummary("Execution Time", extractFloatField(tasks, func(t processor.Task) float64 { return t.ExecutionTime })),
		buildMetricSummary("Queue Wait", extractFloatField(tasks, func(t processor.Task) float64 { return t.QueueWaitTime })),
		buildMetricSummary("Total Response", extractFloatField(tasks, func(t processor.Task) float64 { return t.TotalResponseTime })),
	}
}

// buildMetricSummary converts a processor.MetricSummary into the JSON-
// serializable metricSummary struct used in the API response.
func buildMetricSummary(metric string, values []float64) metricSummary {
	stats := processor.CalculateStats(values)
	return metricSummary{
		Metric: metric,
		Count:  stats.Count,
		Mean:   stats.Mean,
		StdDev: stats.StdDev,
		Min:    stats.Min,
		Max:    stats.Max,
		Median: stats.Median,
		P95:    stats.P95,
	}
}

// extractFloatField is a generic helper that extracts a single float64
// field from every Task using a provided getter function.
//
// Example usage:
//
//	cpuValues := extractFloatField(tasks, func(t processor.Task) float64 { return t.CPUUsage })
//
// This pattern avoids writing separate extraction functions for each field.
func extractFloatField(tasks []processor.Task, getter func(processor.Task) float64) []float64 {
	values := make([]float64, 0, len(tasks))
	for _, task := range tasks {
		values = append(values, getter(task))
	}
	return values
}

// buildReportTables constructs the 5 aggregated data tables included
// in every report response. Each table has a title, column headers,
// and rows of string data.
//
// Tables:
//   1. VM Resource Footprint  — per-VM average CPU, RAM, Disk IO, Network IO
//   2. SLA Compliance Summary — optimal vs non-optimal task counts and response times
//   3. Priority Queue Wait    — mean queue delay per priority tier (1, 2, 3)
//   4. Histogram Bins         — execution time and response time frequency bins
//   5. VM SLA Ratio           — optimal % vs non-optimal % per VM
func buildReportTables(tasks []processor.Task) []reportTable {
	return []reportTable{
		{Title: "VM Resource Footprint", Headers: []string{"VM ID", "CPU Avg", "RAM Avg", "Disk IO Avg", "Network IO Avg"}, Rows: processor.SummarizeVMFootprintRows(tasks)},
		{Title: "SLA Compliance Summary", Headers: []string{"Target", "Count", "Mean Total Response"}, Rows: processor.SummarizeSLAComplianceRows(tasks)},
		{Title: "Priority Queue Wait", Headers: []string{"Priority", "Mean Queue Wait"}, Rows: processor.SummarizePriorityWaitRows(tasks)},
		{Title: "Histogram Bins", Headers: []string{"Bin", "Execution Count", "Response Count"}, Rows: processor.SummarizeHistogramRows(tasks)},
		{Title: "VM SLA Ratio", Headers: []string{"VM ID", "Optimal %", "Non-Optimal %"}, Rows: processor.SummarizeVMSLARatioRows(tasks)},
	}
}

// ============================================================================
// VISUALIZATION GENERATION (vizb CLI Integration)
// ============================================================================

// triggerVizbDashboards attempts to generate 9 interactive HTML dashboard
// files using the vizb CLI tool (https://github.com/goptics/vizb).
//
// Vizb is an optional external dependency. If it's not installed, this
// function prints a warning and returns false, allowing the system to
// operate in "static mode" with only tabular data and KPI cards.
//
// Dashboard generation pipeline:
//   1. Check if vizb is installed (exec.LookPath)
//   2. Create the output directory (generated/vizb/)
//   3. Write 5 intermediate CSV files with aggregated data
//   4. Execute 9 vizb commands to produce HTML charts
//
// Each chart is a self-contained HTML file with embedded Apache ECharts,
// suitable for iframe embedding in the frontend or standalone viewing.
//
// Returns true if vizb was found and at least attempted chart generation.
func triggerVizbDashboards(tasks []processor.Task, csvPath string) bool {
	// Check if the vizb binary exists in the system PATH.
	if _, err := exec.LookPath("vizb"); err != nil {
		fmt.Println("[WARNING] 'vizb' CLI tool is not installed or not in PATH.")
		fmt.Println("          To install vizb run: go install github.com/goptics/vizb@latest")
		fmt.Println("          Or install binary: curl -fsSL https://vizb.goptics.org/install.sh | bash")
		return false
	}

	// Ensure the output directory exists.
	if err := os.MkdirAll(vizbOutputDir, 0o755); err != nil {
		fmt.Printf("[ERROR] Failed to prepare vizb output directory: %v\n", err)
		return false
	}

	// Write intermediate CSV files containing aggregated data.
	// These CSVs serve as input for the vizb chart generation commands.
	assetPaths, err := writeVisualizationAssets(tasks)
	if err != nil {
		fmt.Printf("[ERROR] Failed to prepare vizb inputs: %v\n", err)
		return false
	}

	// Define the 9 dashboard specifications.
	// Each spec maps to one vizb CLI invocation that produces one HTML file.
	configs := []vizbDashboardSpec{
		// Section 2: System Architecture — VM resource footprint bar charts.
		// Each chart selects VM_ID and one resource metric from the system architecture CSV.
		{command: "bar", input: assetPaths.systemArchitectureCSV, output: filepath.Join(vizbOutputDir, "fig_section2_cpu_footprint.html"), args: []string{"--select", "VM_ID,CPU_Usage_Pct"}},
		{command: "bar", input: assetPaths.systemArchitectureCSV, output: filepath.Join(vizbOutputDir, "fig_section2_ram_footprint.html"), args: []string{"--select", "VM_ID,RAM_Usage_MB"}},
		{command: "bar", input: assetPaths.systemArchitectureCSV, output: filepath.Join(vizbOutputDir, "fig_section2_disk_footprint.html"), args: []string{"--select", "VM_ID,Disk_IO_MBs"}},
		{command: "bar", input: assetPaths.systemArchitectureCSV, output: filepath.Join(vizbOutputDir, "fig_section2_network_footprint.html"), args: []string{"--select", "VM_ID,Network_IO_MBs"}},

		// Section 3: Performance Goals — SLA compliance grouped bar chart.
		{command: "bar", input: assetPaths.performanceGoalsCSV, output: filepath.Join(vizbOutputDir, "fig_section3_performance_goals.html"), args: []string{"--select", "Target,Count,Mean_Total_Response_s"}},

		// Section 6: Priority Analysis — queue wait by priority bar chart.
		{command: "bar", input: assetPaths.priorityWaitCSV, output: filepath.Join(vizbOutputDir, "fig_section6_priority_wait.html"), args: []string{"--select", "Priority,Mean_Queue_Wait_s"}},

		// Section 7: Queue Growth & Distribution charts.
		{command: "bar", input: assetPaths.histogramCSV, output: filepath.Join(vizbOutputDir, "fig_section7_histogram.html"), args: []string{"--select", "Bin,Execution_Time_Count,Total_Response_Count"}},
		// Scatter plot uses the full processed CSV to plot Task_ID vs Queue_Wait_s.
		// The --visualmap flag adds a color gradient showing queue delay intensity.
		{command: "scatter", input: csvPath, output: filepath.Join(vizbOutputDir, "fig_section7_scatter_queue_growth.html"), args: []string{"--select", "Task_ID,Queue_Wait_s", "--visualmap"}},
		{command: "bar", input: assetPaths.vmSlaRatioCSV, output: filepath.Join(vizbOutputDir, "fig_section7_vm_sla_ratio.html"), args: []string{"--select", "VM_ID,Optimal_Ratio,Non_Optimal_Ratio"}},
	}

	// Execute each vizb command. Failures are logged but do not stop
	// generation of remaining charts.
	for _, config := range configs {
		commandArgs := append([]string{config.command, config.input}, config.args...)
		commandArgs = append(commandArgs, "--output", config.output)
		cmd := exec.Command("vizb", commandArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("[ERROR] vizb %s dashboard failed: %v\nOutput: %s\n", config.command, err, string(out))
			continue
		}
		fmt.Printf("[SUCCESS] Generated %s\n", config.output)
	}

	return true
}

// ============================================================================
// VISUALIZATION ASSET WRITERS
// ============================================================================

// visualizationAssets holds the file paths of the 5 intermediate CSV files
// written for vizb chart generation. Each file contains pre-aggregated
// data in a format ready for vizb to consume.
type visualizationAssets struct {
	systemArchitectureCSV string // Per-VM resource footprint averages (10 rows).
	performanceGoalsCSV   string // SLA compliance counts and mean response (2 rows).
	priorityWaitCSV       string // Mean queue wait per priority tier (3 rows).
	histogramCSV          string // Execution and response time bin counts (10 rows).
	vmSlaRatioCSV         string // Optimal vs non-optimal percentages per VM (10 rows).
}

// writeVisualizationAssets generates the 5 intermediate CSV files that
// serve as input to the vizb chart generation commands.
//
// These CSVs contain pre-aggregated data (not the raw 1000-row dataset),
// keeping the chart data compact and appropriately summarized.
func writeVisualizationAssets(tasks []processor.Task) (visualizationAssets, error) {
	assets := visualizationAssets{
		systemArchitectureCSV: "viz_section2_system_architecture.csv",
		performanceGoalsCSV:   "viz_section3_performance_goals.csv",
		priorityWaitCSV:       "viz_section6_priority_wait.csv",
		histogramCSV:          "viz_section7_histogram.csv",
		vmSlaRatioCSV:         "viz_section7_vm_sla_ratio.csv",
	}

	// Write each CSV file using the aggregation functions from the processor package.
	// Each call computes the aggregation and writes the result to disk.
	if err := writeCSVFile(assets.systemArchitectureCSV, []string{"VM_ID", "CPU_Usage_Pct", "RAM_Usage_MB", "Disk_IO_MBs", "Network_IO_MBs"}, processor.SummarizeVMFootprintRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}
	if err := writeCSVFile(assets.performanceGoalsCSV, []string{"Target", "Count", "Mean_Total_Response_s"}, processor.SummarizeSLAComplianceRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}
	if err := writeCSVFile(assets.priorityWaitCSV, []string{"Priority", "Mean_Queue_Wait_s"}, processor.SummarizePriorityWaitRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}
	if err := writeCSVFile(assets.histogramCSV, []string{"Bin", "Execution_Time_Count", "Total_Response_Count"}, processor.SummarizeHistogramRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}
	if err := writeCSVFile(assets.vmSlaRatioCSV, []string{"VM_ID", "Optimal_Ratio", "Non_Optimal_Ratio"}, processor.SummarizeVMSLARatioRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}

	return assets, nil
}

// writeCSVFile is a generic utility that writes a CSV file with the
// given header row and data rows. Used to produce the intermediate
// CSV files consumed by the vizb chart generation pipeline.
func writeCSVFile(filename string, header []string, rows [][]string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write the header row first.
	if err := writer.Write(header); err != nil {
		return err
	}

	// Write each data row.
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return writer.Error()
}
