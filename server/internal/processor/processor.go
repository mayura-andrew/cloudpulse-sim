// Package processor implements the core discrete-event queueing simulation
// engine and statistical analysis functions for the CloudPulse system.
//
// It provides three primary capabilities:
//   1. CSV data ingestion:  LoadKaggleCSV parses the raw cloud task dataset.
//   2. Queue simulation:    SimulateQueueingDynamics runs the M/D/k-style
//                           discrete-event simulation across multiple VM queues.
//   3. Statistical analysis: CalculateStats and the Summarize* family of
//                           functions aggregate results for reporting.
//
// Queueing Model Overview:
//
//   The simulation models a multi-server queueing system where:
//     - 10 VMs (VM 100–109) each act as independent single-server FIFO queues
//     - Task arrivals follow a Poisson process with priority-dependent rates
//     - Service times are deterministic (taken directly from the dataset)
//     - This corresponds to an M/D/k model in Kendall's notation
//
//   Key equations implemented:
//     λ_p = λ_0 × (1 + α × (p - 1))        — priority-adjusted arrival rate
//     t_start = max(t_arrival, t_VM_free)    — service start time
//     T_queue = t_start - t_arrival          — queue waiting delay
//     T_total = T_queue + T_execution        — end-to-end turnaround time
package processor

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
)

// arrivalSeed is the fixed random seed used for the pseudo-random number
// generator (PRNG). Using a constant seed ensures that every simulation
// run produces identical, reproducible results — essential for academic
// verification and debugging. The value 20260811 encodes the project date.
const arrivalSeed int64 = 20260811

// baseArrivalRate (λ_0) defines the base Poisson arrival rate in tasks
// per second. This is the rate for Priority 1 (High) tasks before any
// priority scaling is applied.
//
// With 10 VMs and a mean execution time of ~5.48s, the effective
// per-VM arrival rate is approximately 0.4/10 × 2.16 ≈ 0.086 tasks/s,
// which is below the service rate of ~0.18 tasks/s per VM, ensuring
// the system operates below saturation on average while still producing
// observable queueing during burst periods.
const baseArrivalRate = 0.4

// ============================================================================
// DATA STRUCTURES
// ============================================================================

// Task represents a single cloud task record from the Kaggle dataset,
// augmented with computed queueing metrics after simulation.
//
// Fields are organized into two groups:
//   - Input fields (from CSV):  TaskID through Target
//   - Computed fields (from simulation): ArrivalTime through TotalResponseTime
type Task struct {
	// --- Input fields (parsed from CSV columns) ---

	TaskID        int     // Unique task identifier (Column 0). Used as temporal ordering proxy.
	CPUUsage      float64 // CPU utilization percentage (Column 1). Range: 30–94%.
	RAMUsage      float64 // RAM allocation in megabytes (Column 2). Range: 562–16,373 MB.
	DiskIO        float64 // Disk I/O throughput in MB/s (Column 3). Range: 5–99 MB/s.
	NetworkIO     float64 // Network bandwidth in MB/s (Column 4). Range: 1–49 MB/s.
	Priority      int     // Task priority tier (Column 5). Values: 1=High, 2=Medium, 3=Low.
	VMID          int     // Assigned virtual machine ID (Column 6). Range: 100–109.
	ExecutionTime float64 // Deterministic service duration in seconds (Column 7). Range: 1.01–9.98s.
	Target        int     // SLA optimality label (Column 8). Values: 0=Non-Optimal, 1=Optimal.

	// --- Computed fields (populated by SimulateQueueingDynamics) ---

	ArrivalTime       float64 // Simulated arrival timestamp (cumulative exponential draws).
	QueueWaitTime     float64 // Time spent waiting in VM queue before execution begins.
	StartTime         float64 // Actual execution start time = max(ArrivalTime, VM_free_time).
	CompletionTime    float64 // Execution completion timestamp = StartTime + ExecutionTime.
	TotalResponseTime float64 // End-to-end turnaround = QueueWaitTime + ExecutionTime.
}

// MetricSummary stores the key statistical properties computed for any
// numeric metric (CPU, RAM, queue wait, response time, etc.).
// These statistics are reported in the frontend KPI cards and tables.
type MetricSummary struct {
	Count  int     // Number of data points (N).
	Mean   float64 // Arithmetic mean: sum(x_i) / N.
	StdDev float64 // Population standard deviation: sqrt(sum((x_i - mean)^2) / N).
	Min    float64 // Minimum observed value.
	Max    float64 // Maximum observed value.
	Median float64 // Middle value of sorted data (P50 percentile).
	P95    float64 // 95th percentile value — useful for tail-latency analysis.
}

// vmFootprintSummary accumulates resource usage sums for a single VM.
// After accumulation, values are divided by Count to compute averages.
// This intermediate structure is used by summarizeVMFootprints.
type vmFootprintSummary struct {
	Count     int     // Number of tasks assigned to this VM.
	CPUUsage  float64 // Sum (then average) of CPU usage percentages.
	RAMUsage  float64 // Sum (then average) of RAM usage in MB.
	DiskIO    float64 // Sum (then average) of Disk I/O in MB/s.
	NetworkIO float64 // Sum (then average) of Network I/O in MB/s.
}

// slaComplianceSummary accumulates response time totals for a single
// SLA target group (Target=0 or Target=1). After accumulation,
// MeanTotalResponse is divided by Count to compute the average.
type slaComplianceSummary struct {
	Count             int     // Number of tasks in this SLA group.
	MeanTotalResponse float64 // Sum (then average) of TotalResponseTime values.
}

// ============================================================================
// DATA INGESTION
// ============================================================================

// LoadKaggleCSV reads and parses the raw cloud task scheduling dataset from
// a CSV file. The expected CSV schema has 9 columns:
//
//	Task_ID, CPU_Usage(%), RAM_Usage(MB), Disk_IO(MB/s), Network_IO(MB/s),
//	Priority, VM_ID, Execution_Time(s), Target(Optimal Scheduling)
//
// After parsing, tasks are sorted by TaskID in ascending order to establish
// a stable causal ordering for the simulation. TaskID serves as a proxy
// for arrival sequence — Task 1 arrives before Task 2, and so on.
//
// Returns an error if the file cannot be opened or the header has fewer
// than 9 columns. Individual row parsing errors are silently skipped to
// handle minor CSV formatting issues gracefully.
func LoadKaggleCSV(filename string) ([]Task, error) {
	// Open the CSV file for reading.
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Read and validate the CSV header row.
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("unable to read CSV header: %w", err)
	}
	// The dataset must have at least 9 columns for the expected schema.
	if len(header) < 9 {
		return nil, fmt.Errorf("invalid CSV schema: expected 9 columns, found %d", len(header))
	}

	// Parse each data row into a Task struct.
	var tasks []Task
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break // End of file — all rows processed.
		}
		if err != nil {
			continue // Skip malformed rows (e.g., wrong column count).
		}

		// Parse each column from string to its appropriate type.
		// strings.TrimSpace handles any trailing whitespace or \r characters.
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

	// Sort tasks by TaskID ascending. This ensures deterministic processing
	// order regardless of the original CSV row ordering, and establishes
	// the arrival sequence for the queueing simulation.
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].TaskID < tasks[j].TaskID
	})

	return tasks, nil
}

// ============================================================================
// CORE SIMULATION ENGINE
// ============================================================================

// SimulateQueueingDynamics runs the discrete-event queueing simulation
// across all VM instances. This is the heart of the CloudPulse system.
//
// Algorithm overview:
//   For each task (in TaskID order):
//     1. Compute a priority-adjusted arrival rate: λ_p = λ_0 × (1 + 0.2 × (p-1))
//     2. Draw an exponential inter-arrival gap: T_inter = Exp(1) / λ_p
//     3. Advance the global clock: t_arrival += T_inter
//     4. Look up when the target VM becomes free
//     5. Compute: t_start = max(t_arrival, t_VM_free)
//     6. Compute: queue_wait = t_start - t_arrival
//     7. Compute: t_completion = t_start + execution_time
//     8. Update the VM's free time to t_completion
//
// Priority effect:
//   Priority 1 (High):   λ_p = 0.40 → E[inter-arrival] = 2.50s (less congestion)
//   Priority 2 (Medium): λ_p = 0.48 → E[inter-arrival] = 2.08s
//   Priority 3 (Low):    λ_p = 0.56 → E[inter-arrival] = 1.79s (more congestion)
//
//   Higher λ for low-priority tasks creates shorter gaps between arrivals,
//   causing more frequent VM contention and longer queue waits for those tasks.
//
// The function modifies tasks in-place, populating the computed fields:
// ArrivalTime, QueueWaitTime, StartTime, CompletionTime, TotalResponseTime.
func SimulateQueueingDynamics(tasks []Task) {
	// Create a seeded random number generator for reproducibility.
	// Every run with the same seed produces identical arrival sequences.
	rng := rand.New(rand.NewSource(arrivalSeed))

	// vmFreeTimes tracks when each VM will finish its current task.
	// Key = VM ID (100–109), Value = completion timestamp of the last task.
	// A VM with freeTime <= currentArrival has no queue — the task starts immediately.
	// A VM with freeTime > currentArrival forces the task to wait in queue.
	vmFreeTimes := make(map[int]float64)

	// currentArrival is the running simulation clock (cumulative arrival time).
	// It advances by a random exponential increment for each task.
	currentArrival := 0.0

	for i := range tasks {
		// --- Step 1: Compute priority-adjusted arrival rate ---
		// Formula: λ_p = λ_0 × (1 + α × (p - 1)), where α = 0.2
		// Priority 1 → factor 1.0, Priority 2 → factor 1.2, Priority 3 → factor 1.4
		priorityFactor := 1.0 + float64(tasks[i].Priority-1)*0.2
		arrivalRate := baseArrivalRate * priorityFactor

		// --- Step 2: Draw exponential inter-arrival time ---
		// rng.ExpFloat64() returns a sample from Exp(1) (rate=1).
		// Dividing by arrivalRate scales it to Exp(λ_p).
		// Higher arrivalRate → shorter inter-arrival gaps → denser traffic.
		interArrival := rng.ExpFloat64() / arrivalRate

		// --- Step 3: Advance the global simulation clock ---
		currentArrival += interArrival

		// --- Step 4: Look up target VM availability ---
		vm := tasks[i].VMID
		freeTime := vmFreeTimes[vm] // Defaults to 0.0 if VM hasn't been used yet.

		// --- Step 5: Determine when execution can start ---
		// If the VM is free (freeTime <= currentArrival), the task starts immediately.
		// If the VM is busy (freeTime > currentArrival), the task must wait.
		startTime := math.Max(currentArrival, freeTime)

		// --- Step 6: Calculate queue waiting delay ---
		// This is the time the task spends buffered in the VM queue.
		// If startTime == currentArrival, queueWait is 0 (no waiting).
		queueWait := startTime - currentArrival

		// --- Step 7: Calculate completion time ---
		// The task runs for its full ExecutionTime once it starts.
		completionTime := startTime + tasks[i].ExecutionTime

		// --- Step 8: Block the VM until this task finishes ---
		// The next task arriving at this VM will see this completion time
		// as the VM's free time, potentially causing it to queue.
		vmFreeTimes[vm] = completionTime

		// Store all computed metrics in the task record, rounded to 2 decimal
		// places for clean CSV output and consistent numerical display.
		tasks[i].ArrivalTime = math.Round(currentArrival*100) / 100
		tasks[i].QueueWaitTime = math.Round(queueWait*100) / 100
		tasks[i].StartTime = math.Round(startTime*100) / 100
		tasks[i].CompletionTime = math.Round(completionTime*100) / 100
		tasks[i].TotalResponseTime = math.Round((completionTime-currentArrival)*100) / 100
	}
}

// ============================================================================
// DATA EXPORT
// ============================================================================

// ExportCSV writes the complete processed dataset (original fields + simulated
// timing metrics) to a CSV file. The output has 14 columns per row:
//
//	Task_ID, CPU_Usage_Pct, RAM_Usage_MB, Disk_IO_MBs, Network_IO_MBs,
//	Priority, VM_ID, Execution_Time_s, Target_Optimal,
//	Arrival_Time_s, Queue_Wait_s, Start_Time_s, Completion_Time_s, Total_Response_s
//
// This file serves as the primary data artifact for downstream analysis,
// visualization, and academic reporting.
func ExportCSV(filename string, tasks []Task) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write the 14-column header row.
	if err := writer.Write([]string{
		"Task_ID", "CPU_Usage_Pct", "RAM_Usage_MB", "Disk_IO_MBs", "Network_IO_MBs",
		"Priority", "VM_ID", "Execution_Time_s", "Target_Optimal",
		"Arrival_Time_s", "Queue_Wait_s", "Start_Time_s", "Completion_Time_s", "Total_Response_s",
	}); err != nil {
		return err
	}

	// Write each task as a row, formatting floats to 2 decimal places.
	for _, t := range tasks {
		if err := writer.Write([]string{
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
		}); err != nil {
			return err
		}
	}

	return writer.Error()
}

// ============================================================================
// STATISTICAL ANALYSIS
// ============================================================================

// CalculateStats computes a comprehensive statistical summary for any
// numeric data series. This is the core analytics function used to
// generate the KPI cards and overview tables in the frontend.
//
// Computed statistics:
//   - Count:   Total number of data points (N)
//   - Mean:    Arithmetic average = Σx_i / N
//   - StdDev:  Population standard deviation = √(Σ(x_i - μ)² / N)
//   - Min/Max: Extreme values of the sorted dataset
//   - Median:  Middle value (P50) of the sorted dataset
//   - P95:     95th percentile — the value below which 95% of data falls.
//              Critical for tail-latency analysis in performance modeling.
//
// Note: This uses population standard deviation (divides by N, not N-1)
// since we are analyzing the complete simulated dataset, not a sample.
func CalculateStats(data []float64) MetricSummary {
	if len(data) == 0 {
		return MetricSummary{}
	}

	// Create a sorted copy to compute order statistics (median, P95, min, max)
	// without modifying the original data slice.
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)

	// Compute sum for mean calculation; extract min and max from sorted endpoints.
	sum := 0.0
	min := sorted[0]
	max := sorted[len(sorted)-1]

	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	// Compute population variance: Σ(x_i - μ)² / N
	// Then take the square root for standard deviation.
	varianceSum := 0.0
	for _, v := range sorted {
		varianceSum += math.Pow(v-mean, 2)
	}
	stdDev := math.Sqrt(varianceSum / float64(len(sorted)))

	// Median: the middle element of the sorted array.
	// For even-length arrays, this takes the upper-middle element.
	median := sorted[len(sorted)/2]

	// P95: the value at index floor(0.95 × N) in the sorted array.
	// This means 95% of observations fall at or below this value.
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

// ============================================================================
// DATA AGGREGATION FUNCTIONS
// ============================================================================
//
// These functions group task data by various dimensions (VM ID, Priority,
// SLA Target) and compute aggregate statistics. They produce string-based
// row arrays suitable for both CSV export and JSON serialization to the
// frontend tables.

// summarizeVMFootprints groups tasks by VM_ID and computes the average
// resource usage (CPU, RAM, Disk I/O, Network I/O) per VM.
//
// Algorithm:
//   Pass 1: Accumulate sums and counts per VM.
//   Pass 2: Divide sums by counts to compute averages.
//
// This reveals which VMs carry heavier resource burdens, supporting
// the system architecture analysis in the report (Section 2).
func summarizeVMFootprints(tasks []Task) map[int]vmFootprintSummary {
	result := make(map[int]vmFootprintSummary)

	// Pass 1: Accumulate resource usage sums per VM.
	for _, task := range tasks {
		bucket := result[task.VMID]
		bucket.Count++
		bucket.CPUUsage += task.CPUUsage
		bucket.RAMUsage += task.RAMUsage
		bucket.DiskIO += task.DiskIO
		bucket.NetworkIO += task.NetworkIO
		result[task.VMID] = bucket
	}

	// Pass 2: Convert sums to averages by dividing by task count.
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

// SummarizeVMFootprintRows produces per-VM average resource usage as
// string rows for the "VM Resource Footprint" table and vizb bar charts.
//
// Output format per row: [VM_ID, CPU_Avg, RAM_Avg, DiskIO_Avg, NetworkIO_Avg]
// Rows are sorted by VM_ID ascending (100, 101, ..., 109).
func SummarizeVMFootprintRows(tasks []Task) [][]string {
	vmIDs := make([]int, 0)
	footprints := summarizeVMFootprints(tasks)
	for vmID := range footprints {
		vmIDs = append(vmIDs, vmID)
	}
	sort.Ints(vmIDs) // Sort for deterministic, ascending VM order.

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

// summarizeSLACompliance groups tasks by their SLA Target label (0 or 1)
// and computes the count and mean total response time for each group.
//
// This reveals whether tasks labeled as "optimal" (Target=1) actually
// have better response times than "non-optimal" (Target=0) tasks,
// supporting the performance goals analysis (Section 3).
func summarizeSLACompliance(tasks []Task) map[int]slaComplianceSummary {
	result := make(map[int]slaComplianceSummary)

	// Accumulate total response time sums per target group.
	for _, task := range tasks {
		bucket := result[task.Target]
		bucket.Count++
		bucket.MeanTotalResponse += task.TotalResponseTime
		result[task.Target] = bucket
	}

	// Convert accumulated sums to averages.
	for target, bucket := range result {
		if bucket.Count > 0 {
			bucket.MeanTotalResponse /= float64(bucket.Count)
			result[target] = bucket
		}
	}

	return result
}

// SummarizeSLAComplianceRows produces SLA target group statistics as
// string rows for the "SLA Compliance Summary" table and vizb bar charts.
//
// Output format per row: [Target, Count, MeanTotalResponse]
// Rows are sorted by Target ascending (0 then 1).
func SummarizeSLAComplianceRows(tasks []Task) [][]string {
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

// SummarizePriorityWaitRows groups tasks by Priority level (1, 2, 3)
// and computes the mean queue waiting time for each group.
//
// This is the key function for demonstrating priority-induced congestion:
// the simulation is designed so that lower-priority tasks experience
// higher arrival rates, creating measurably more queueing delay.
//
// Expected output pattern:
//   Priority 1 (High):   ~0.92s mean queue wait (least congestion)
//   Priority 2 (Medium): ~1.07s mean queue wait
//   Priority 3 (Low):    ~1.38s mean queue wait (most congestion)
//
// Output format per row: [Priority, MeanQueueWait]
func SummarizePriorityWaitRows(tasks []Task) [][]string {
	// Collect all queue wait values grouped by priority level.
	priorityWaits := make(map[int][]float64)
	for _, task := range tasks {
		priorityWaits[task.Priority] = append(priorityWaits[task.Priority], task.QueueWaitTime)
	}

	// Sort priority keys for deterministic output order.
	priorities := make([]int, 0, len(priorityWaits))
	for priority := range priorityWaits {
		priorities = append(priorities, priority)
	}
	sort.Ints(priorities)

	// Compute mean queue wait for each priority using CalculateStats.
	rows := make([][]string, 0, len(priorities))
	for _, priority := range priorities {
		stats := CalculateStats(priorityWaits[priority])
		rows = append(rows, []string{
			strconv.Itoa(priority),
			fmt.Sprintf("%.2f", stats.Mean),
		})
	}
	return rows
}

// SummarizeHistogramRows builds 10 equal-width histogram bins covering
// the combined range of execution times and total response times.
//
// For each bin, it counts:
//   - How many tasks have their ExecutionTime in that bin
//   - How many tasks have their TotalResponseTime in that bin
//
// This dual-histogram reveals how queueing delay extends the response
// time distribution beyond the pure execution time distribution. Tasks
// in bins beyond the max execution time exist purely due to queue delay.
//
// Output format per row: [BinRange, ExecutionCount, ResponseCount]
// Example: ["1.01-3.52", "287", "226"]
func SummarizeHistogramRows(tasks []Task) [][]string {
	if len(tasks) == 0 {
		return nil
	}

	// Collect all execution times AND response times to determine
	// the global min/max range for the histogram bins.
	values := make([]float64, 0, len(tasks)*2)
	for _, task := range tasks {
		values = append(values, task.ExecutionTime, task.TotalResponseTime)
	}

	// Find the global minimum and maximum across both distributions.
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

	// Calculate bin width: divide the full range into 10 equal intervals.
	binCount := 10
	if maxValue == minValue {
		maxValue = minValue + 1 // Prevent division by zero for uniform data.
	}
	binWidth := (maxValue - minValue) / float64(binCount)
	if binWidth <= 0 {
		binWidth = 1
	}

	// Count how many tasks fall into each bin for both distributions.
	executionCounts := make([]int, binCount)
	responseCounts := make([]int, binCount)
	for _, task := range tasks {
		// Map each value to a bin index: floor((value - min) / binWidth).
		executionIndex := int((task.ExecutionTime - minValue) / binWidth)
		responseIndex := int((task.TotalResponseTime - minValue) / binWidth)

		// Clamp indices to valid range [0, binCount-1].
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

	// Format each bin as a string row with the range label and both counts.
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

// SummarizeVMSLARatioRows computes the percentage of optimal (Target=1)
// versus non-optimal (Target=0) task outcomes for each VM instance.
//
// This identifies which VMs achieve the best scheduling quality and
// which VMs are associated with poorer SLA outcomes. VMs with low
// optimal ratios may be candidates for workload rebalancing.
//
// Output format per row: [VM_ID, OptimalPercentage, NonOptimalPercentage]
// Percentages sum to 100% per VM.
func SummarizeVMSLARatioRows(tasks []Task) [][]string {
	// vmRatio tracks the count of optimal and non-optimal tasks per VM.
	type vmRatio struct {
		optimal    int
		nonOptimal int
	}

	// Count optimal vs non-optimal tasks per VM.
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

	// Sort VM IDs for deterministic output order.
	vmIDs := make([]int, 0, len(ratioByVM))
	for vmID := range ratioByVM {
		vmIDs = append(vmIDs, vmID)
	}
	sort.Ints(vmIDs)

	// Convert counts to percentages for each VM.
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
