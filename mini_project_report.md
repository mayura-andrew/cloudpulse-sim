# CloudPulse: Performance Modeling and Evaluation of a Cloud Task Scheduling System

## Abstract

CloudPulse is a Go-based performance modeling project that studies how cloud tasks move through a multi-VM queueing system. The implementation loads a real CSV dataset, generates stochastic task arrivals, simulates waiting on VM queues, computes response-time metrics, and exports a processed dataset. It also produces statistical summaries and HTML visualizations for VM resource footprint, SLA compliance, priority delay, queue growth, histogram distributions, and VM-level SLA ratios. This report explains the system, the modeling assumptions, the Go functions, the simulation flow, the plots, and the limitations of the approach.

## 1. System Description and Performance Goals

The system being modeled is a cloud task scheduling environment where each task has a resource footprint and a designated VM. The workload is complex enough to expose queue buildup, resource imbalance, response-time variation, and SLA differences. The dataset includes CPU usage, RAM usage, disk I/O, network I/O, priority, task order, VM assignment, execution time, and SLA target labels.

The main performance goals are:

- reduce queue waiting time;
- reduce total response time;
- measure resource usage per VM;
- observe how priority influences delay;
- compare SLA outcomes across VMs;
- present the results as a formal report and visualization set.

## 2. Modeling Approach and Assumptions

The implementation uses a discrete-event queueing model with stochastic arrivals and deterministic service. Tasks are processed in sorted `Task_ID` order. For each task, the engine draws an exponential inter-arrival time, accumulates it into the task arrival timestamp, checks the target VM availability, and computes the service start and completion times.

The main assumptions are:

- tasks are processed in `Task_ID` order as a time proxy;
- each task remains on its assigned `VM_ID`;
- each VM serves one task at a time;
- the queue is FIFO at the VM level;
- priority affects arrival pressure, so lower-priority jobs experience heavier congestion;
- the simulation is synthetic but follows queueing theory structure.

The arrival model is expressed as:

$$
\lambda_p = \lambda_0 (1 + \alpha (p - 1))
$$

$$
T_{\text{arr}} \sim \text{Exp}(\lambda_p)
$$

This gives a Poisson-style arrival process with priority-dependent load.

## 3. Data Description and Methodology

The input dataset contains 20,000 task rows. The key fields are:

- `Task_ID`
- `CPU_Usage_Pct`
- `RAM_Usage_MB`
- `Disk_IO_MBs`
- `Network_IO_MBs`
- `Priority`
- `VM_ID`
- `Execution_Time_s`
- `Target_Optimal`

After simulation, the program exports the following derived fields:

- `Arrival_Time_s`
- `Queue_Wait_s`
- `Start_Time_s`
- `Completion_Time_s`
- `Total_Response_s`

The processed data are written to `processed_cloud_task_metrics.csv`, which is then used for both the numerical summaries and the plots.

## 4. Implementation Functions

The backend is organized into focused functions so that the logic is easy to follow and explain.

### 4.1 Core Simulation Functions

#### `LoadKaggleCSV`

This function opens the CSV file, checks that the header contains the expected fields, parses each row, and stores the result in a `Task` structure. It also sorts the tasks by `Task_ID` so the simulation runs in a stable order.

#### `SimulateQueueingDynamics`

This is the core simulation function. It creates a seeded random generator, draws an exponential inter-arrival time for each task, updates the cumulative arrival time, checks when the target VM becomes free, and computes queueing and completion metrics.

The function updates:

- `ArrivalTime`
- `QueueWaitTime`
- `StartTime`
- `CompletionTime`
- `TotalResponseTime`

#### `ExportCSV`

This function writes the original fields and simulated fields to the processed CSV file. It creates the final dataset that supports analysis, reporting, and visualization.

#### `CalculateStats`

This helper calculates count, mean, standard deviation, minimum, maximum, median, and 95th percentile for a numeric series. It is used for the report summary statistics.

### 4.2 Aggregation Functions

#### `SummarizeVMFootprintRows`

This function groups tasks by `VM_ID` and calculates average CPU usage, RAM usage, disk I/O, and network I/O. It supports the VM footprint plots and the table in the report.

#### `SummarizeSLAComplianceRows`

This function groups tasks by `Target_Optimal` and computes the count and mean total response time for each group. It supports the SLA compliance chart and summary table.

#### `SummarizePriorityWaitRows`

This function groups tasks by priority and computes the mean queue wait time for each priority level. It is used to show how queue delay changes under priority pressure.

#### `SummarizeHistogramRows`

This function builds histogram bins for execution time and total response time so the response-time distribution can be visualized.

#### `SummarizeVMSLARatioRows`

This function calculates the percentage of optimal and non-optimal tasks for each VM. It is used to understand SLA quality at the VM level.

### 4.3 API and Report Delivery Functions

#### `StartServer`

Creates the HTTP server and registers the endpoints for CSV upload, default processing, and dashboard file serving.

#### `handleUpload`

Accepts an uploaded CSV file, stores it temporarily, loads the tasks, runs the simulation, exports the processed CSV, generates the dashboards, and returns the report payload.

#### `handleProcessDefault`

Processes the workspace dataset without a file upload. This is the default report path used by the frontend.

#### `writeReportJSON`

Packages dashboards, overview statistics, and tabulated results into one JSON response for the frontend.

#### `triggerVizbDashboards`

Invokes the `vizb` CLI to generate the HTML dashboard files.

#### `writeVisualizationAssets`

Creates intermediate CSV files for the visualization engine.

#### `vizbDashboardManifest`

Returns the ordered list of dashboard file names and titles used by the frontend.

#### `saveUploadedFile`

Stores the uploaded CSV in a temporary file before it is parsed.

#### `writeCSVFile`

Writes helper CSV files used as input to the visualization stage.

## 5. Simulation Flow

The system works in the following sequence:

1. `LoadKaggleCSV` reads the raw data.
2. Tasks are sorted by `Task_ID`.
3. `SimulateQueueingDynamics` generates arrival times using exponential sampling.
4. The VM free time is read from a map of active VM completion times.
5. The start time is set to the maximum of arrival time and VM free time.
6. Queue waiting time is computed as the difference between start time and arrival time.
7. Completion time is computed by adding execution time to the start time.
8. The VM free time is updated to the new completion time.
9. The derived metrics are stored inside the task record.
10. `ExportCSV` writes the processed data.
11. The summary CSVs for plots are produced.
12. The HTML dashboards are generated.
13. The frontend loads the JSON response and displays the report.

## 6. Queueing Equations

The implementation directly follows the timing equations used in the report brief:

$$
t_{\text{start}} = \max(t_{\text{arr}}, t_{\text{VM\_free}})
$$

$$
t_{\text{comp}} = t_{\text{start}} + T_{\text{Execution}}
$$

$$
T_{\text{Queue}} = t_{\text{start}} - t_{\text{arr}}
$$

$$
T_{\text{Total}} = T_{\text{Queue}} + T_{\text{Execution}}
$$

These equations are consistent with the exported `Start_Time_s`, `Completion_Time_s`, `Queue_Wait_s`, and `Total_Response_s` fields.

## 7. Detailed Analysis and Findings

### 7.1 Section 2: System Architecture

The system architecture plots show the average resource footprint of each VM. CPU usage, RAM usage, disk I/O, and network I/O are summarized per VM. These plots reveal whether the workload is evenly distributed or whether one VM is carrying a heavier burden than the others.

#### Interpretation

- High CPU bars indicate CPU-intensive VMs.
- High RAM bars indicate memory-intensive VMs.
- High disk or network bars show storage-heavy or communication-heavy VMs.

### 7.2 Section 3: Performance Goals

The SLA compliance plot groups tasks by `Target_Optimal` and compares count with mean total response time. This plot shows whether optimal tasks and non-optimal tasks differ significantly in response behavior.

#### Interpretation

- A higher response time for non-optimal tasks suggests workload stress or slower completion under non-ideal conditions.
- The counts show how many tasks belong to each SLA class.

### 7.3 Section 6: Priority Analysis

The priority waiting plot shows the mean queue delay for each priority class.

#### Interpretation

- Priority 1 should typically have the smallest delay.
- Priority 2 should be intermediate.
- Priority 3 should experience the largest delay if the load pressure is working as intended.

This demonstrates how priority-sensitive workloads can create unfairness in waiting time.

### 7.4 Section 7: Queue Growth and Distribution

The histogram and scatter plots explain how response times are distributed and how queue delay evolves over time.

#### Interpretation

- The histogram shows whether execution times and response times are tightly clustered or widely spread.
- The scatter plot of `Task_ID` versus `Queue_Wait_s` shows whether queue delay increases as more jobs are processed.
- If the scatter plot rises over time, the system is accumulating backlog.

### 7.5 Section 7: VM SLA Ratio

The VM SLA ratio plot shows the percentage of optimal versus non-optimal tasks per VM.

#### Interpretation

- VMs with a high optimal percentage are handling their workload more effectively.
- VMs with a high non-optimal percentage may be under more load or associated with poorer outcomes.

## 8. Visualizations Summary

The project produces the following visual outputs:

- Section 2 CPU footprint by VM.
- Section 2 RAM footprint by VM.
- Section 2 disk footprint by VM.
- Section 2 network footprint by VM.
- Section 3 SLA compliance summary.
- Section 6 queue wait by priority.
- Section 7 execution-time and response-time histogram.
- Section 7 queue growth scatter plot.
- Section 7 VM SLA ratio plot.

Each visualization supports a specific part of the analysis and together they provide a full view of workload behavior.

## 9. Frontend Behavior

The frontend is intentionally simple and report-oriented. It does not recompute the simulation. Instead, it requests the backend report payload and renders the statistics and dashboards.

The frontend does the following:

- automatically loads the default report when the page opens;
- allows CSV upload for manual processing;
- shows numeric summary cards for the main metrics;
- displays tabulated results for VM footprint, SLA compliance, priority waits, histogram bins, and VM SLA ratio;
- embeds each generated HTML plot one by one.

This design keeps the frontend focused on presentation rather than duplicating the simulation logic.

## 10. Limitations

This model is useful for a compact performance study, but it still has limitations:

- the arrival process is synthetic rather than measured from a real trace;
- the VM service model is a single-server queue per VM;
- priority influences arrivals rather than directly controlling the scheduler;
- resource contention is summarized statistically rather than modeled as coupled contention.

## 11. Future Extensions

The model could be extended by:

- adding preemptive or non-preemptive priority scheduling;
- introducing load balancing across VMs;
- modeling CPU, RAM, and I/O contention penalties;
- comparing FCFS, priority scheduling, and load-balancing policies;
- replacing the synthetic arrival model with real trace data.

## 12. Conclusion

CloudPulse demonstrates a complete end-to-end performance modeling workflow in Go. The backend reads task data, simulates queueing on multiple VMs, computes timing metrics, exports the processed dataset, and generates dashboard-ready summaries. The frontend presents the numerical statistics and visualizations in a clean report layout. The project therefore satisfies the assignment requirements for system description, modeling approach, data methodology, analysis, visualizations, limitations, and interpretation of system behavior.

## References

- Kleinrock, L., *Queueing Systems, Volume 1: Theory*, Wiley.
- Jain, R., *The Art of Computer Systems Performance Analysis*, Wiley.
- Open University of Sri Lanka, EEI6373 Performance Modelling Mini Project Brief, 2026.

