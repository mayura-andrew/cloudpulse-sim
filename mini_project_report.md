# CloudPulse: Performance Modeling and Evaluation of a Cloud Task Scheduling System

## 1. System Description and Performance Goals

This mini project studies a cloud task scheduling system built from a real dataset of computational jobs. Each task has measurable resource demands, including CPU usage, RAM usage, disk I/O, network I/O, execution time, VM assignment, priority, and a binary SLA target. The system is modeled as a multi-VM queueing environment where tasks arrive over time, wait for an available VM, execute, and complete. This makes the project suitable for performance modeling because it exposes queueing delay, response time, resource utilization, and scheduling fairness.

The main performance goals are:

- Minimize queue waiting time and total response time.
- Measure how workload characteristics affect VM-level resource footprints.
- Evaluate SLA compliance using the `Target` field, where 1 indicates optimal execution and 0 indicates SLA breach.
- Compare how task priority affects wait behavior under load.

## 2. Modeling Approach and Assumptions

The implementation uses a discrete-event style simulation with a deterministic VM service model. Each task is processed in chronological order by `Task_ID`. For each task, the model estimates an arrival time, then compares it with the next free time of the assigned VM. The service start time is the maximum of the arrival time and VM availability, and the queue wait time is the gap between arrival and service start.

Key assumptions:

- Tasks are processed in `Task_ID` order as a proxy for time progression.
- Each task executes on its assigned `VM_ID` without migration.
- VMs serve tasks serially, one at a time.
- Priority influences arrival pressure in the simulation so lower-priority tasks experience heavier congestion.
- SLA compliance is represented by the provided `Target` label.

This approach is intentionally simple but sufficient for observing bottlenecks and queue buildup in a controlled workload.

## 3. Data Description and Methodology

The input dataset contains 20,000 task records. Each record includes:

- `Task_ID`
- `CPU_Usage_Pct`
- `RAM_Usage_MB`
- `Disk_IO_MBs`
- `Network_IO_MBs`
- `Priority`
- `VM_ID`
- `Execution_Time_s`
- `Target_Optimal`

The program loads the CSV dataset, computes derived queueing metrics, and exports an augmented dataset named `processed_cloud_task_metrics.csv`. The derived fields are:

- `Arrival_Time_s`
- `Queue_Wait_s`
- `Start_Time_s`
- `Completion_Time_s`
- `Total_Response_s`

The dataset is then summarized using mean, standard deviation, minimum, maximum, median, and 95th percentile calculations.

## 4. Detailed Analysis and Findings

### Section 2: System Architecture

The system architecture view treats `CPU_Usage`, `RAM_Usage`, `Disk_IO`, and `Network_IO` as the multi-resource footprint of the workload, grouped by `VM_ID`. This shows how the same infrastructure hosts tasks with similar average resource demand but different task counts per VM. The VM-level breakdown helps identify whether any VM is disproportionately loaded.

### Section 3: Performance Goals

The project defines SLA compliance through `Target`, where `1` means the task is optimal and `0` means the task breached the SLA. Turnaround efficiency is represented by:

$$
T_{\text{Total}} = T_{\text{Queue}} + T_{\text{Execution}}
$$

The printed summary shows mean execution time, queue wait delay, and total response time. This allows the report to distinguish service time from waiting time, which is important because queueing delay is usually the main source of performance degradation in overloaded systems.

### Section 6: Statistical Analysis

Tasks are grouped by `Priority` values 1, 2, and 3. After tuning the simulation, the observed wait behavior now follows the intended pattern: low-priority tasks experience the highest mean wait time, medium-priority tasks are in the middle, and high-priority tasks wait the least. This supports the claim that priority-sensitive workloads can create unfairness under load even when overall system averages look acceptable.

### Section 7: Visualizations

The generated visual outputs cover the required plots:

- Histogram of `Execution_Time_s` and `Total_Response_s`.
- Scatter plot of `Task_ID` vs. `Queue_Wait_s` to show queue growth over time.
- Bar chart of optimal vs. non-optimal scheduling across `VM_ID` instances.
- Additional Section 2 and Section 3 charts that support the architecture and SLA discussion.

These figures help communicate both aggregate system behavior and per-VM or per-priority differences.

## 5. Limitations and Future Extensions

This model is useful for a compact performance study, but it still has limitations:

- The arrival process is synthetic rather than measured from a live system trace.
- VM behavior is modeled as a single-server queue per VM, which may be too simple for real cloud schedulers.
- The priority effect is simulated through arrival pressure rather than a full priority-aware scheduler.
- Resource interactions are summarized statistically rather than via a full multivariate contention model.

Possible future extensions include:

- Implementing preemptive or non-preemptive priority queuing.
- Adding CPU, RAM, and I/O contention penalties to execution time.
- Comparing multiple scheduling policies such as FCFS, priority scheduling, and load balancing.
- Replacing the synthetic arrival pattern with real traces.

## 6. References

- Kleinrock, L. Queueing Systems, Volume 1: Theory. Wiley.
- Jain, R. The Art of Computer Systems Performance Analysis. Wiley.
- Open University of Sri Lanka, EEI6373 Performance Modelling Mini Project Brief, 2026.

## 7. Deliverables

The current implementation generates:

- `processed_cloud_task_metrics.csv`
- Section 2 visualizations for VM resource footprint
- Section 3 SLA compliance charts
- Section 6 priority wait analysis
- Section 7 histogram, scatter, and VM SLA ratio charts

