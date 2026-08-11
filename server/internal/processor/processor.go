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

const arrivalSeed int64 = 20260811
const baseArrivalRate = 0.4

// Task represents a task from the Kaggle dataset and its queueing metrics.
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

	ArrivalTime       float64
	QueueWaitTime     float64
	StartTime         float64
	CompletionTime    float64
	TotalResponseTime float64
}

// MetricSummary stores key statistical properties.
type MetricSummary struct {
	Count  int
	Mean   float64
	StdDev float64
	Min    float64
	Max    float64
	Median float64
	P95    float64
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

// LoadKaggleCSV reads and parses the raw dataset.
func LoadKaggleCSV(filename string) ([]Task, error) {
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

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].TaskID < tasks[j].TaskID
	})

	return tasks, nil
}

// SimulateQueueingDynamics runs the discrete event simulation across VM instances.
func SimulateQueueingDynamics(tasks []Task) {
	rng := rand.New(rand.NewSource(arrivalSeed))
	vmFreeTimes := make(map[int]float64)
	currentArrival := 0.0

	for i := range tasks {
		priorityFactor := 1.0 + float64(tasks[i].Priority-1)*0.2
		arrivalRate := baseArrivalRate * priorityFactor
		interArrival := rng.ExpFloat64() / arrivalRate
		currentArrival += interArrival

		vm := tasks[i].VMID
		freeTime := vmFreeTimes[vm]

		startTime := math.Max(currentArrival, freeTime)
		queueWait := startTime - currentArrival
		completionTime := startTime + tasks[i].ExecutionTime

		vmFreeTimes[vm] = completionTime

		tasks[i].ArrivalTime = math.Round(currentArrival*100) / 100
		tasks[i].QueueWaitTime = math.Round(queueWait*100) / 100
		tasks[i].StartTime = math.Round(startTime*100) / 100
		tasks[i].CompletionTime = math.Round(completionTime*100) / 100
		tasks[i].TotalResponseTime = math.Round((completionTime-currentArrival)*100) / 100
	}
}

// ExportCSV writes the final dataset trace.
func ExportCSV(filename string, tasks []Task) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{
		"Task_ID", "CPU_Usage_Pct", "RAM_Usage_MB", "Disk_IO_MBs", "Network_IO_MBs",
		"Priority", "VM_ID", "Execution_Time_s", "Target_Optimal",
		"Arrival_Time_s", "Queue_Wait_s", "Start_Time_s", "Completion_Time_s", "Total_Response_s",
	}); err != nil {
		return err
	}

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

// CalculateStats determines mathematical mean, standard deviation, min, max, and percentiles.
func CalculateStats(data []float64) MetricSummary {
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

// SummarizeVMFootprintRows produces per-VM average resource rows for vizb.
func SummarizeVMFootprintRows(tasks []Task) [][]string {
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

// SummarizeSLAComplianceRows produces target/count/mean response rows for vizb.
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

// SummarizePriorityWaitRows returns mean queue wait by priority.
func SummarizePriorityWaitRows(tasks []Task) [][]string {
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
		stats := CalculateStats(priorityWaits[priority])
		rows = append(rows, []string{
			strconv.Itoa(priority),
			fmt.Sprintf("%.2f", stats.Mean),
		})
	}
	return rows
}

// SummarizeHistogramRows returns execution and response histogram bins.
func SummarizeHistogramRows(tasks []Task) [][]string {
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

// SummarizeVMSLARatioRows returns optimal versus non-optimal ratios by VM.
func SummarizeVMSLARatioRows(tasks []Task) [][]string {
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
