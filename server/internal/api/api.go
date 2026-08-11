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

const vizbOutputDir = "generated/vizb"

type dashboardInfo struct {
	Title string `json:"title"`
	File  string `json:"file"`
	URL   string `json:"url"`
}

type metricSummary struct {
	Metric string  `json:"metric"`
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stdDev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
}

type reportTable struct {
	Title   string     `json:"title"`
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

type processResponse struct {
	Dashboards    []dashboardInfo `json:"dashboards"`
	VizbAvailable bool            `json:"vizbAvailable"`
	Overview      []metricSummary `json:"overview"`
	Tables        []reportTable   `json:"tables"`
	Message       string          `json:"message,omitempty"`
}

type vizbDashboardSpec struct {
	command string
	input   string
	output  string
	args    []string
}

// StartServer starts the HTTP server and registers handlers.
func StartServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/upload", handleUpload)
	mux.HandleFunc("/api/process-default", handleProcessDefault)
	mux.Handle("/vizb/", http.StripPrefix("/vizb/", http.FileServer(http.Dir(vizbOutputDir))))

	if err := os.MkdirAll(vizbOutputDir, 0o755); err != nil {
		fmt.Printf("[WARNING] unable to prepare vizb output dir: %v\n", err)
	}

	dist := filepath.Join("..", "frontend", "dist")
	if _, err := os.Stat(dist); err == nil {
		mux.Handle("/", http.FileServer(http.Dir(dist)))
	}

	addr := ":8080"
	fmt.Printf("[INFO] Starting CloudPulse server on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Printf("[ERROR] server failed: %v\n", err)
	}
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse multipart form", http.StatusBadRequest)
		return
	}
	f, fh, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer f.Close()

	tmp, err := saveUploadedFile(f, fh)
	if err != nil {
		http.Error(w, "failed to save uploaded file", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp)

	tasks, err := processor.LoadKaggleCSV(tmp)
	if err != nil {
		http.Error(w, "failed to parse CSV", http.StatusBadRequest)
		return
	}

	processor.SimulateQueueingDynamics(tasks)
	_ = processor.ExportCSV("processed_cloud_task_metrics.csv", tasks)
	vizbAvailable := triggerVizbDashboards(tasks, "processed_cloud_task_metrics.csv")

	writeReportJSON(w, tasks, vizbAvailable)
}

func handleProcessDefault(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	candidates := []string{
		filepath.Join("..", "cloud_task_scheduling_dataset.csv"),
		filepath.Join("..", "cloud_task_scheduling_dataset_20k.csv"),
		filepath.Join("..", "dataset.csv"),
		"cloud_task_scheduling_dataset.csv",
		"cloud_task_scheduling_dataset_20k.csv",
		"dataset.csv",
	}
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

func writeReportJSON(w http.ResponseWriter, tasks []processor.Task, vizbAvailable bool) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(processResponse{
		Dashboards:    vizbDashboardManifest(),
		VizbAvailable: vizbAvailable,
		Overview:      buildOverviewMetrics(tasks),
		Tables:        buildReportTables(tasks),
	})
}

func saveUploadedFile(f multipart.File, fh *multipart.FileHeader) (string, error) {
	tmpf, err := os.CreateTemp("", "uploaded-*.csv")
	if err != nil {
		return "", err
	}
	defer tmpf.Close()
	if _, err := io.Copy(tmpf, f); err != nil {
		return "", err
	}
	return tmpf.Name(), nil
}

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

func extractFloatField(tasks []processor.Task, getter func(processor.Task) float64) []float64 {
	values := make([]float64, 0, len(tasks))
	for _, task := range tasks {
		values = append(values, getter(task))
	}
	return values
}

func buildReportTables(tasks []processor.Task) []reportTable {
	return []reportTable{
		{Title: "VM Resource Footprint", Headers: []string{"VM ID", "CPU Avg", "RAM Avg", "Disk IO Avg", "Network IO Avg"}, Rows: processor.SummarizeVMFootprintRows(tasks)},
		{Title: "SLA Compliance Summary", Headers: []string{"Target", "Count", "Mean Total Response"}, Rows: processor.SummarizeSLAComplianceRows(tasks)},
		{Title: "Priority Queue Wait", Headers: []string{"Priority", "Mean Queue Wait"}, Rows: processor.SummarizePriorityWaitRows(tasks)},
		{Title: "Histogram Bins", Headers: []string{"Bin", "Execution Count", "Response Count"}, Rows: processor.SummarizeHistogramRows(tasks)},
		{Title: "VM SLA Ratio", Headers: []string{"VM ID", "Optimal %", "Non-Optimal %"}, Rows: processor.SummarizeVMSLARatioRows(tasks)},
	}
}

func triggerVizbDashboards(tasks []processor.Task, csvPath string) bool {
	if _, err := exec.LookPath("vizb"); err != nil {
		fmt.Println("[WARNING] 'vizb' CLI tool is not installed or not in PATH.")
		fmt.Println("          To install vizb run: go install github.com/goptics/vizb@latest")
		fmt.Println("          Or install binary: curl -fsSL https://vizb.goptics.org/install.sh | bash")
		return false
	}

	if err := os.MkdirAll(vizbOutputDir, 0o755); err != nil {
		fmt.Printf("[ERROR] Failed to prepare vizb output directory: %v\n", err)
		return false
	}

	assetPaths, err := writeVisualizationAssets(tasks)
	if err != nil {
		fmt.Printf("[ERROR] Failed to prepare vizb inputs: %v\n", err)
		return false
	}

	configs := []vizbDashboardSpec{
		{command: "bar", input: assetPaths.systemArchitectureCSV, output: filepath.Join(vizbOutputDir, "fig_section2_cpu_footprint.html"), args: []string{"--select", "VM_ID,CPU_Usage_Pct"}},
		{command: "bar", input: assetPaths.systemArchitectureCSV, output: filepath.Join(vizbOutputDir, "fig_section2_ram_footprint.html"), args: []string{"--select", "VM_ID,RAM_Usage_MB"}},
		{command: "bar", input: assetPaths.systemArchitectureCSV, output: filepath.Join(vizbOutputDir, "fig_section2_disk_footprint.html"), args: []string{"--select", "VM_ID,Disk_IO_MBs"}},
		{command: "bar", input: assetPaths.systemArchitectureCSV, output: filepath.Join(vizbOutputDir, "fig_section2_network_footprint.html"), args: []string{"--select", "VM_ID,Network_IO_MBs"}},
		{command: "bar", input: assetPaths.performanceGoalsCSV, output: filepath.Join(vizbOutputDir, "fig_section3_performance_goals.html"), args: []string{"--select", "Target,Count,Mean_Total_Response_s"}},
		{command: "bar", input: assetPaths.priorityWaitCSV, output: filepath.Join(vizbOutputDir, "fig_section6_priority_wait.html"), args: []string{"--select", "Priority,Mean_Queue_Wait_s"}},
		{command: "bar", input: assetPaths.histogramCSV, output: filepath.Join(vizbOutputDir, "fig_section7_histogram.html"), args: []string{"--select", "Bin,Execution_Time_Count,Total_Response_Count"}},
		{command: "scatter", input: csvPath, output: filepath.Join(vizbOutputDir, "fig_section7_scatter_queue_growth.html"), args: []string{"--select", "Task_ID,Queue_Wait_s", "--visualmap"}},
		{command: "bar", input: assetPaths.vmSlaRatioCSV, output: filepath.Join(vizbOutputDir, "fig_section7_vm_sla_ratio.html"), args: []string{"--select", "VM_ID,Optimal_Ratio,Non_Optimal_Ratio"}},
	}

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

type visualizationAssets struct {
	systemArchitectureCSV string
	performanceGoalsCSV   string
	priorityWaitCSV       string
	histogramCSV          string
	vmSlaRatioCSV         string
}

func writeVisualizationAssets(tasks []processor.Task) (visualizationAssets, error) {
	assets := visualizationAssets{
		systemArchitectureCSV: "viz_section2_system_architecture.csv",
		performanceGoalsCSV:   "viz_section3_performance_goals.csv",
		priorityWaitCSV:       "viz_section6_priority_wait.csv",
		histogramCSV:          "viz_section7_histogram.csv",
		vmSlaRatioCSV:         "viz_section7_vm_sla_ratio.csv",
	}

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

func writeCSVFile(filename string, header []string, rows [][]string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return writer.Error()
}
