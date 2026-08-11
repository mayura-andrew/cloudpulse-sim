package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Task represents a task from the Kaggle dataset and its queueing metrics
type Task struct {
	TaskID        int
	CPUUsage      float64
	RAMUsage      float64
	DiskIO        float64
	NetworkIO     float64
	Priority      int
	VMID          int
	ExecutionTime float64
	Target        int

	// Simulation Derived Queueing Metrics
	ArrivalTime       float64
	QueueWaitTime     float64
	StartTime         float64
	CompletionTime    float64
	TotalResponseTime float64
}

// MetricSummary stores key statistical properties
type MetricSummary struct {
	Count  int
	Mean   float64
	StdDev float64
	Min    float64
	Max    float64
	Median float64
	P95    float64
}

func main() {
	// If run with `serve` argument, start HTTP server for frontend integration.
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		startServer()
		return
	}

	inputFile := "dataset.csv"
	outputFile := "processed_cloud_task_metrics.csv"

	fmt.Println("==========================================================")
	fmt.Println(" CloudPulse: Data Center Performance Modeling Engine (Go) ")
	fmt.Println("==========================================================")

	// 1. Ingest Kaggle Dataset
	tasks, err := loadKaggleCSV(inputFile)
	if err != nil {
		fmt.Printf("[ERROR] Failed to load dataset: %v\n", err)
		fmt.Println("[INFO] Ensure 'cloud-task-scheduling-dataset.csv' is placed in the project root.")
		return
	}
	fmt.Printf("[SUCCESS] Ingested %d task records from %s\n", len(tasks), inputFile)

	// 2. Execute Discrete Event Queueing Simulation
	simulateQueueingDynamics(tasks)

	// 3. Export Augmented Dataset
	err = exportCSV(outputFile, tasks)
	if err != nil {
		fmt.Printf("[ERROR] Failed to export processed dataset: %v\n", err)
		return
	}
	fmt.Printf("[SUCCESS] Saved processed metrics to %s\n", outputFile)

	// 4. Print Statistical Analysis Report
	printStatisticalReport(tasks)

	// 5. Invoke vizb Visualization Engine
	triggerVizbDashboards(tasks, outputFile)
}

// startServer runs an HTTP server that accepts CSV uploads and returns processed JSON rows.
func startServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/upload", handleUpload)
	mux.HandleFunc("/api/process-default", handleProcessDefault)

	// Serve frontend static if present (built files in frontend/dist)
	dist := filepath.Join("frontend", "dist")
	if _, err := os.Stat(dist); err == nil {
		mux.Handle("/", http.FileServer(http.Dir(dist)))
	}

	addr := ":8080"
	fmt.Printf("[INFO] Starting CloudPulse server on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Printf("[ERROR] server failed: %v\n", err)
	}
}

// handleUpload accepts multipart file upload (field name 'file'), processes it and returns JSON rows.
func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	err := r.ParseMultipartForm(32 << 20)
	if err != nil {
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

	tasks, err := loadKaggleCSV(tmp)
	if err != nil {
		http.Error(w, "failed to parse CSV", http.StatusBadRequest)
		return
	}

	simulateQueueingDynamics(tasks)

	// produce processed CSV in workspace
	_ = exportCSV("processed_cloud_task_metrics.csv", tasks)

	// return JSON rows for frontend
	rows := tasksToJSONRows(tasks)
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.Encode(rows)
}

// handleProcessDefault processes the workspace dataset.csv and returns JSON rows.
func handleProcessDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	input := "dataset.csv"
	if _, err := os.Stat(input); err != nil {
		http.Error(w, "dataset.csv not found in workspace", http.StatusNotFound)
		return
	}
	tasks, err := loadKaggleCSV(input)
	if err != nil {
		http.Error(w, "failed to load dataset.csv", http.StatusInternalServerError)
		return
	}
	simulateQueueingDynamics(tasks)
	_ = exportCSV("processed_cloud_task_metrics.csv", tasks)
	rows := tasksToJSONRows(tasks)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
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

func tasksToJSONRows(tasks []Task) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(tasks))
	for _, t := range tasks {
		rows = append(rows, map[string]interface{}{
			"Task_ID":           t.TaskID,
			"CPU_Usage_Pct":     t.CPUUsage,
			"RAM_Usage_MB":      t.RAMUsage,
			"Disk_IO_MBs":       t.DiskIO,
			"Network_IO_MBs":    t.NetworkIO,
			"Priority":          t.Priority,
			"VM_ID":             t.VMID,
			"Execution_Time_s":  t.ExecutionTime,
			"Target_Optimal":    t.Target,
			"Arrival_Time_s":    t.ArrivalTime,
			"Queue_Wait_s":      t.QueueWaitTime,
			"Start_Time_s":      t.StartTime,
			"Completion_Time_s": t.CompletionTime,
			"Total_Response_s":  t.TotalResponseTime,
		})
	}
	return rows
}

// loadKaggleCSV reads and parses the raw dataset
func loadKaggleCSV(filename string) ([]Task, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("unable to read CSV header: %w", err)
	}

	// Verify essential headers
	if len(header) < 9 {
		return nil, fmt.Errorf("invalid CSV schema: expected 9 columns, found %d", len(header))
	}

	var tasks []Task
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		taskID, _ := strconv.Atoi(strings.TrimSpace(record[0]))
		cpu, _ := strconv.ParseFloat(strings.TrimSpace(record[1]), 64)
		ram, _ := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		disk, _ := strconv.ParseFloat(strings.TrimSpace(record[3]), 64)
		net, _ := strconv.ParseFloat(strings.TrimSpace(record[4]), 64)
		prio, _ := strconv.Atoi(strings.TrimSpace(record[5]))
		vmID, _ := strconv.Atoi(strings.TrimSpace(record[6]))
		execTime, _ := strconv.ParseFloat(strings.TrimSpace(record[7]), 64)
		target, _ := strconv.Atoi(strings.TrimSpace(record[8]))

		tasks = append(tasks, Task{
			TaskID:        taskID,
			CPUUsage:      cpu,
			RAMUsage:      ram,
			DiskIO:        disk,
			NetworkIO:     net,
			Priority:      prio,
			VMID:          vmID,
			ExecutionTime: execTime,
			Target:        target,
		})
	}

	// Sort tasks chronologically by TaskID
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].TaskID < tasks[j].TaskID
	})

	return tasks, nil
}

// simulateQueueingDynamics runs the Discrete Event Simulation across VM instances
func simulateQueueingDynamics(tasks []Task) {
	vmFreeTimes := make(map[int]float64)
	currentArrival := 0.0

	for i := range tasks {
		// Stochastic Poisson Inter-arrival process (Mean ~ 2.5 seconds)
		// Scale inter-arrival dynamically so lower-priority tasks face heavier load.
		prioWeight := 1.6 - (float64(tasks[i].Priority)-1.0)*0.2
		interArrival := (1.5 + math.Mod(float64(tasks[i].TaskID*13), 3.5)) * prioWeight
		currentArrival += interArrival

		vm := tasks[i].VMID
		freeTime := vmFreeTimes[vm]

		// Discrete Event Time Calculations
		startTime := math.Max(currentArrival, freeTime)
		queueWait := startTime - currentArrival
		completionTime := startTime + tasks[i].ExecutionTime

		// Update server state tracker
		vmFreeTimes[vm] = completionTime

		// Store calculated performance metrics
		tasks[i].ArrivalTime = math.Round(currentArrival*100) / 100
		tasks[i].QueueWaitTime = math.Round(queueWait*100) / 100
		tasks[i].StartTime = math.Round(startTime*100) / 100
		tasks[i].CompletionTime = math.Round(completionTime*100) / 100
		tasks[i].TotalResponseTime = math.Round((completionTime-currentArrival)*100) / 100
	}
}

// exportCSV writes the final dataset trace
func exportCSV(filename string, tasks []Task) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write CSV Header
	writer.Write([]string{
		"Task_ID", "CPU_Usage_Pct", "RAM_Usage_MB", "Disk_IO_MBs", "Network_IO_MBs",
		"Priority", "VM_ID", "Execution_Time_s", "Target_Optimal",
		"Arrival_Time_s", "Queue_Wait_s", "Start_Time_s", "Completion_Time_s", "Total_Response_s",
	})

	for _, t := range tasks {
		writer.Write([]string{
			strconv.Itoa(t.TaskID),
			fmt.Sprintf("%.2f", t.CPUUsage),
			fmt.Sprintf("%.2f", t.RAMUsage),
			fmt.Sprintf("%.2f", t.DiskIO),
			fmt.Sprintf("%.2f", t.NetworkIO),
			strconv.Itoa(t.Priority),
			strconv.Itoa(t.VMID),
			fmt.Sprintf("%.2f", t.ExecutionTime),
			strconv.Itoa(t.Target),
			fmt.Sprintf("%.2f", t.ArrivalTime),
			fmt.Sprintf("%.2f", t.QueueWaitTime),
			fmt.Sprintf("%.2f", t.StartTime),
			fmt.Sprintf("%.2f", t.CompletionTime),
			fmt.Sprintf("%.2f", t.TotalResponseTime),
		})
	}
	return nil
}

// printStatisticalReport computes statistical summaries for the final report
func printStatisticalReport(tasks []Task) {
	fmt.Println("\n----------------------------------------------------------")
	fmt.Println("             SYSTEM PERFORMANCE METRIC SUMMARY             ")
	fmt.Println("----------------------------------------------------------")
	fmt.Println("\n--- Section 2: System Architecture ---")
	printSystemArchitectureSummary(tasks)

	fmt.Println("\n--- Section 3: Performance Goals ---")
	printPerformanceGoalsSummary(tasks)

	queueWaits := make([]float64, len(tasks))
	execTimes := make([]float64, len(tasks))
	responseTimes := make([]float64, len(tasks))

	prioGroupWait := make(map[int][]float64)

	for i, t := range tasks {
		queueWaits[i] = t.QueueWaitTime
		execTimes[i] = t.ExecutionTime
		responseTimes[i] = t.TotalResponseTime
		prioGroupWait[t.Priority] = append(prioGroupWait[t.Priority], t.QueueWaitTime)
	}

	qSummary := calculateStats(queueWaits)
	eSummary := calculateStats(execTimes)
	rSummary := calculateStats(responseTimes)

	fmt.Printf("%-22s %-8s %-8s %-8s %-8s %-8s\n", "Metric", "Mean", "StdDev", "Min", "Max", "P95")
	fmt.Println("----------------------------------------------------------")
	fmt.Printf("%-22s %-8.2f %-8.2f %-8.2f %-8.2f %-8.2f\n", "Execution Time (s)", eSummary.Mean, eSummary.StdDev, eSummary.Min, eSummary.Max, eSummary.P95)
	fmt.Printf("%-22s %-8.2f %-8.2f %-8.2f %-8.2f %-8.2f\n", "Queue Wait Delay (s)", qSummary.Mean, qSummary.StdDev, qSummary.Min, qSummary.Max, qSummary.P95)
	fmt.Printf("%-22s %-8.2f %-8.2f %-8.2f %-8.2f %-8.2f\n", "Total Response (s)", rSummary.Mean, rSummary.StdDev, rSummary.Min, rSummary.Max, rSummary.P95)
	fmt.Println("----------------------------------------------------------")

	fmt.Println("\n--- Priority Latency Degradation Breakdown ---")
	for p := 1; p <= 3; p++ {
		if val, exists := prioGroupWait[p]; exists {
			pStat := calculateStats(val)
			pName := "Low (3)"
			if p == 1 {
				pName = "High (1)"
			} else if p == 2 {
				pName = "Medium (2)"
			}
			fmt.Printf("Priority %-10s | Count: %-4d | Mean Wait: %6.2fs | Max Wait: %6.2fs\n", pName, pStat.Count, pStat.Mean, pStat.Max)
		}
	}
	fmt.Println("----------------------------------------------------------\n")
}

// printSystemArchitectureSummary shows the multi-resource footprint by VM instance.
func printSystemArchitectureSummary(tasks []Task) {
	vmFootprints := summarizeVMFootprints(tasks)
	if len(vmFootprints) == 0 {
		fmt.Println("No VM footprint data available.")
		return
	}

	vmIDs := make([]int, 0, len(vmFootprints))
	for vmID := range vmFootprints {
		vmIDs = append(vmIDs, vmID)
	}
	sort.Ints(vmIDs)

	fmt.Printf("%-8s %-12s %-12s %-12s %-12s %-8s\n", "VM_ID", "CPU_Usage", "RAM_Usage", "Disk_IO", "Network_IO", "Count")
	for _, vmID := range vmIDs {
		footprint := vmFootprints[vmID]
		fmt.Printf("%-8d %-12.2f %-12.2f %-12.2f %-12.2f %-8d\n", vmID, footprint.CPUUsage, footprint.RAMUsage, footprint.DiskIO, footprint.NetworkIO, footprint.Count)
	}
}

// printPerformanceGoalsSummary shows SLA compliance and turnaround efficiency by Target.
func printPerformanceGoalsSummary(tasks []Task) {
	compliance := summarizeSLACompliance(tasks)
	if len(compliance) == 0 {
		fmt.Println("No SLA compliance data available.")
		return
	}

	targets := make([]int, 0, len(compliance))
	for target := range compliance {
		targets = append(targets, target)
	}
	sort.Ints(targets)

	fmt.Printf("%-8s %-12s %-18s %-18s\n", "Target", "Count", "Mean T_Total", "SLA Rate")
	for _, target := range targets {
		bucket := compliance[target]
		slaRate := 0.0
		if bucket.Count > 0 {
			slaRate = float64(bucket.Count) / float64(len(tasks)) * 100
		}
		fmt.Printf("%-8d %-12d %-18.2f %-17.2f%%\n", target, bucket.Count, bucket.MeanTotalResponse, slaRate)
	}
}

// calculateStats determines mathematical mean, standard deviation, min, max, and percentiles
func calculateStats(data []float64) MetricSummary {
	if len(data) == 0 {
		return MetricSummary{}
	}

	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)

	sum := 0.0
	min := sorted[0]
	max := sorted[len(sorted)-1]

	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	varianceSum := 0.0
	for _, v := range sorted {
		varianceSum += math.Pow(v-mean, 2)
	}
	stdDev := math.Sqrt(varianceSum / float64(len(sorted)))

	median := sorted[len(sorted)/2]
	p95Idx := int(0.95 * float64(len(sorted)))
	if p95Idx >= len(sorted) {
		p95Idx = len(sorted) - 1
	}

	return MetricSummary{
		Count:  len(data),
		Mean:   mean,
		StdDev: stdDev,
		Min:    min,
		Max:    max,
		Median: median,
		P95:    sorted[p95Idx],
	}
}

type vmFootprintSummary struct {
	Count     int
	CPUUsage  float64
	RAMUsage  float64
	DiskIO    float64
	NetworkIO float64
}

type slaComplianceSummary struct {
	Count             int
	MeanTotalResponse float64
}

type vizbDashboardSpec struct {
	command string
	input   string
	output  string
	args    []string
}

// triggerVizbDashboards invokes the goptics/vizb CLI tool for all requested views.
func triggerVizbDashboards(tasks []Task, csvPath string) {
	fmt.Println("[INFO] Checking for goptics/vizb installation...")
	_, err := exec.LookPath("vizb")
	if err != nil {
		fmt.Println("[WARNING] 'vizb' CLI tool is not installed or not in PATH.")
		fmt.Println("          To install vizb run: go install github.com/goptics/vizb@latest")
		fmt.Println("          Or install binary: curl -fsSL https://vizb.goptics.org/install.sh | bash")
		return
	}

	assetPaths, err := writeVisualizationAssets(tasks)
	if err != nil {
		fmt.Printf("[ERROR] Failed to prepare vizb inputs: %v\n", err)
		return
	}

	configs := []vizbDashboardSpec{
		{
			command: "bar",
			input:   assetPaths.systemArchitectureCSV,
			output:  "fig_section2_cpu_footprint.html",
			args:    []string{"--select", "VM_ID,CPU_Usage_Pct"},
		},
		{
			command: "bar",
			input:   assetPaths.systemArchitectureCSV,
			output:  "fig_section2_ram_footprint.html",
			args:    []string{"--select", "VM_ID,RAM_Usage_MB"},
		},
		{
			command: "bar",
			input:   assetPaths.systemArchitectureCSV,
			output:  "fig_section2_disk_footprint.html",
			args:    []string{"--select", "VM_ID,Disk_IO_MBs"},
		},
		{
			command: "bar",
			input:   assetPaths.systemArchitectureCSV,
			output:  "fig_section2_network_footprint.html",
			args:    []string{"--select", "VM_ID,Network_IO_MBs"},
		},
		{
			command: "bar",
			input:   assetPaths.performanceGoalsCSV,
			output:  "fig_section3_performance_goals.html",
			args:    []string{"--select", "Target,Count,Mean_Total_Response_s"},
		},
		{
			command: "bar",
			input:   assetPaths.priorityWaitCSV,
			output:  "fig_section6_priority_wait.html",
			args:    []string{"--select", "Priority,Mean_Queue_Wait_s"},
		},
		{
			command: "bar",
			input:   assetPaths.histogramCSV,
			output:  "fig_section7_histogram.html",
			args:    []string{"--select", "Bin,Execution_Time_Count,Total_Response_Count"},
		},
		{
			command: "scatter",
			input:   csvPath,
			output:  "fig_section7_scatter_queue_growth.html",
			args:    []string{"--select", "Task_ID,Queue_Wait_s", "--visualmap"},
		},
		{
			command: "bar",
			input:   assetPaths.vmSlaRatioCSV,
			output:  "fig_section7_vm_sla_ratio.html",
			args:    []string{"--select", "VM_ID,Optimal_Ratio,Non_Optimal_Ratio"},
		},
	}

	for _, config := range configs {
		fmt.Printf("[INFO] Invoking vizb %s dashboard -> %s\n", config.command, config.output)
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

	fmt.Println("[INFO] Vizb dashboard generation complete.")
}

type visualizationAssets struct {
	systemArchitectureCSV string
	performanceGoalsCSV   string
	priorityWaitCSV       string
	histogramCSV          string
	vmSlaRatioCSV         string
}

func writeVisualizationAssets(tasks []Task) (visualizationAssets, error) {
	assets := visualizationAssets{
		systemArchitectureCSV: "viz_section2_system_architecture.csv",
		performanceGoalsCSV:   "viz_section3_performance_goals.csv",
		priorityWaitCSV:       "viz_section6_priority_wait.csv",
		histogramCSV:          "viz_section7_histogram.csv",
		vmSlaRatioCSV:         "viz_section7_vm_sla_ratio.csv",
	}

	if err := writeCSVFile(assets.systemArchitectureCSV, []string{"VM_ID", "CPU_Usage_Pct", "RAM_Usage_MB", "Disk_IO_MBs", "Network_IO_MBs"}, summarizeVMFootprintRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}
	if err := writeCSVFile(assets.performanceGoalsCSV, []string{"Target", "Count", "Mean_Total_Response_s"}, summarizeSLAComplianceRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}
	if err := writeCSVFile(assets.priorityWaitCSV, []string{"Priority", "Mean_Queue_Wait_s"}, summarizePriorityWaitRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}
	if err := writeCSVFile(assets.histogramCSV, []string{"Bin", "Execution_Time_Count", "Total_Response_Count"}, summarizeHistogramRows(tasks)); err != nil {
		return visualizationAssets{}, err
	}
	if err := writeCSVFile(assets.vmSlaRatioCSV, []string{"VM_ID", "Optimal_Ratio", "Non_Optimal_Ratio"}, summarizeVMSLARatioRows(tasks)); err != nil {
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

func summarizeVMFootprints(tasks []Task) map[int]vmFootprintSummary {
	result := make(map[int]vmFootprintSummary)
	for _, task := range tasks {
		bucket := result[task.VMID]
		bucket.Count++
		bucket.CPUUsage += task.CPUUsage
		bucket.RAMUsage += task.RAMUsage
		bucket.DiskIO += task.DiskIO
		bucket.NetworkIO += task.NetworkIO
		result[task.VMID] = bucket
	}

	for vmID, bucket := range result {
		if bucket.Count == 0 {
			continue
		}
		bucket.CPUUsage /= float64(bucket.Count)
		bucket.RAMUsage /= float64(bucket.Count)
		bucket.DiskIO /= float64(bucket.Count)
		bucket.NetworkIO /= float64(bucket.Count)
		result[vmID] = bucket
	}

	return result
}

func summarizeVMFootprintRows(tasks []Task) [][]string {
	vmIDs := make([]int, 0)
	footprints := summarizeVMFootprints(tasks)
	for vmID := range footprints {
		vmIDs = append(vmIDs, vmID)
	}
	sort.Ints(vmIDs)

	rows := make([][]string, 0, len(vmIDs))
	for _, vmID := range vmIDs {
		bucket := footprints[vmID]
		rows = append(rows, []string{
			strconv.Itoa(vmID),
			fmt.Sprintf("%.2f", bucket.CPUUsage),
			fmt.Sprintf("%.2f", bucket.RAMUsage),
			fmt.Sprintf("%.2f", bucket.DiskIO),
			fmt.Sprintf("%.2f", bucket.NetworkIO),
		})
	}
	return rows
}

func summarizeSLACompliance(tasks []Task) map[int]slaComplianceSummary {
	result := make(map[int]slaComplianceSummary)
	for _, task := range tasks {
		bucket := result[task.Target]
		bucket.Count++
		bucket.MeanTotalResponse += task.TotalResponseTime
		result[task.Target] = bucket
	}

	for target, bucket := range result {
		if bucket.Count > 0 {
			bucket.MeanTotalResponse /= float64(bucket.Count)
			result[target] = bucket
		}
	}

	return result
}

func summarizeSLAComplianceRows(tasks []Task) [][]string {
	targets := make([]int, 0)
	compliance := summarizeSLACompliance(tasks)
	for target := range compliance {
		targets = append(targets, target)
	}
	sort.Ints(targets)

	rows := make([][]string, 0, len(targets))
	for _, target := range targets {
		bucket := compliance[target]
		rows = append(rows, []string{
			strconv.Itoa(target),
			strconv.Itoa(bucket.Count),
			fmt.Sprintf("%.2f", bucket.MeanTotalResponse),
		})
	}
	return rows
}

func summarizePriorityWaitRows(tasks []Task) [][]string {
	priorityWaits := make(map[int][]float64)
	for _, task := range tasks {
		priorityWaits[task.Priority] = append(priorityWaits[task.Priority], task.QueueWaitTime)
	}

	priorities := make([]int, 0, len(priorityWaits))
	for priority := range priorityWaits {
		priorities = append(priorities, priority)
	}
	sort.Ints(priorities)

	rows := make([][]string, 0, len(priorities))
	for _, priority := range priorities {
		stats := calculateStats(priorityWaits[priority])
		rows = append(rows, []string{
			strconv.Itoa(priority),
			fmt.Sprintf("%.2f", stats.Mean),
		})
	}
	return rows
}

func summarizeHistogramRows(tasks []Task) [][]string {
	if len(tasks) == 0 {
		return nil
	}

	values := make([]float64, 0, len(tasks)*2)
	for _, task := range tasks {
		values = append(values, task.ExecutionTime, task.TotalResponseTime)
	}

	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}

	binCount := 10
	if maxValue == minValue {
		maxValue = minValue + 1
	}
	binWidth := (maxValue - minValue) / float64(binCount)
	if binWidth <= 0 {
		binWidth = 1
	}

	executionCounts := make([]int, binCount)
	responseCounts := make([]int, binCount)
	for _, task := range tasks {
		executionIndex := int((task.ExecutionTime - minValue) / binWidth)
		responseIndex := int((task.TotalResponseTime - minValue) / binWidth)
		if executionIndex < 0 {
			executionIndex = 0
		} else if executionIndex >= binCount {
			executionIndex = binCount - 1
		}
		if responseIndex < 0 {
			responseIndex = 0
		} else if responseIndex >= binCount {
			responseIndex = binCount - 1
		}
		executionCounts[executionIndex]++
		responseCounts[responseIndex]++
	}

	rows := make([][]string, 0, binCount)
	for binIndex := 0; binIndex < binCount; binIndex++ {
		startValue := minValue + (float64(binIndex) * binWidth)
		endValue := startValue + binWidth
		rows = append(rows, []string{
			fmt.Sprintf("%.2f-%.2f", startValue, endValue),
			strconv.Itoa(executionCounts[binIndex]),
			strconv.Itoa(responseCounts[binIndex]),
		})
	}

	return rows
}

func summarizeVMSLARatioRows(tasks []Task) [][]string {
	type vmRatio struct {
		optimal    int
		nonOptimal int
	}

	ratioByVM := make(map[int]vmRatio)
	for _, task := range tasks {
		bucket := ratioByVM[task.VMID]
		if task.Target == 1 {
			bucket.optimal++
		} else {
			bucket.nonOptimal++
		}
		ratioByVM[task.VMID] = bucket
	}

	vmIDs := make([]int, 0, len(ratioByVM))
	for vmID := range ratioByVM {
		vmIDs = append(vmIDs, vmID)
	}
	sort.Ints(vmIDs)

	rows := make([][]string, 0, len(vmIDs))
	for _, vmID := range vmIDs {
		bucket := ratioByVM[vmID]
		total := bucket.optimal + bucket.nonOptimal
		optimalRatio := 0.0
		nonOptimalRatio := 0.0
		if total > 0 {
			optimalRatio = float64(bucket.optimal) / float64(total) * 100
			nonOptimalRatio = float64(bucket.nonOptimal) / float64(total) * 100
		}
		rows = append(rows, []string{
			strconv.Itoa(vmID),
			fmt.Sprintf("%.2f", optimalRatio),
			fmt.Sprintf("%.2f", nonOptimalRatio),
		})
	}

	return rows
}
