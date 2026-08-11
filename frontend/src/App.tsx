import React, { useEffect, useMemo, useState } from 'react'

type DashboardInfo = {
  title: string
  file: string
  url: string
}

type MetricSummary = {
  metric: string
  count: number
  mean: number
  stdDev: number
  min: number
  max: number
  median: number
  p95: number
}

type ReportTable = {
  title: string
  headers: string[]
  rows: string[][]
}

type ProcessResponse = {
  dashboards: DashboardInfo[]
  vizbAvailable: boolean
  overview: MetricSummary[]
  tables: ReportTable[]
  message?: string
}

const emptyReport: ProcessResponse = {
  dashboards: [],
  vizbAvailable: false,
  overview: [],
  tables: [],
}

type TabKey = 'overview' | 'resources' | 'queue' | 'sla' | 'tables'

const METRIC_UX_META: Record<
  string,
  { label: string; unit: string; description: string; category: string }
> = {
  'CPU Usage': {
    label: 'CPU Utilization',
    unit: '%',
    description: 'Percentage of VM compute capacity consumed during task processing',
    category: 'Compute Resource',
  },
  'RAM Usage': {
    label: 'Memory Allocation',
    unit: 'MB',
    description: 'Primary memory reserved per execution instance',
    category: 'Memory Resource',
  },
  'Disk IO': {
    label: 'Storage I/O Throughput',
    unit: 'MB/s',
    description: 'Disk read/write bandwidth rate during execution',
    category: 'I/O Resource',
  },
  'Network IO': {
    label: 'Network Bandwidth',
    unit: 'MB/s',
    description: 'Inbound and outbound network data transfer rate',
    category: 'I/O Resource',
  },
  'Execution Time': {
    label: 'Service Duration (T_Execution)',
    unit: 's',
    description: 'Active processing duration required on the assigned VM',
    category: 'Timing Metric',
  },
  'Queue Wait': {
    label: 'Queue Waiting Latency (T_Queue)',
    unit: 's',
    description: 'Delay spent buffered in queue prior to execution start',
    category: 'Timing Metric',
  },
  'Total Response': {
    label: 'End-to-End Turnaround Time (T_Total)',
    unit: 's',
    description: 'Total lifecycle duration (Queue Wait + Service Execution)',
    category: 'Timing Metric',
  },
}

const TABLE_UX_META: Record<string, { title: string; subtitle: string }> = {
  'VM Resource Footprint': {
    title: 'VM Cluster Resource Allocation',
    subtitle: 'Average compute, memory, and I/O footprint per virtual machine instance',
  },
  'SLA Compliance Summary': {
    title: 'SLA Target Performance & Response',
    subtitle: 'Turnaround duration comparison grouped by SLA compliance targets',
  },
  'Priority Queue Wait': {
    title: 'Queue Delay by Task Priority Tier',
    subtitle: 'Mean waiting latency across Priority 1 (High), Priority 2 (Medium), and Priority 3 (Low)',
  },
  'Histogram Bins': {
    title: 'Latency Distribution Intervals',
    subtitle: 'Binned frequency breakdown of service time versus total response time',
  },
  'VM SLA Ratio': {
    title: 'Per-VM SLA Success Rates',
    subtitle: 'Proportion of optimal vs non-optimal task completions across VM instances',
  },
}

const DASHBOARD_UX_TITLES: Record<string, { title: string; category: TabKey; description: string }> = {
  'fig_section2_cpu_footprint.html': {
    title: 'Compute Load Distribution Across VM Instances',
    category: 'resources',
    description: 'Compares mean CPU utilization percentage across all virtual machine instances.',
  },
  'fig_section2_ram_footprint.html': {
    title: 'Memory Consumption Footprint Across VM Instances',
    category: 'resources',
    description: 'Quantifies average RAM allocation (MB) utilized by tasks per VM node.',
  },
  'fig_section2_disk_footprint.html': {
    title: 'Disk I/O Throughput Demand by VM',
    category: 'resources',
    description: 'Evaluates read/write storage bandwidth requirements across VMs.',
  },
  'fig_section2_network_footprint.html': {
    title: 'Network Bandwidth Consumption by VM',
    category: 'resources',
    description: 'Illustrates network data transfer rate demands across compute nodes.',
  },
  'fig_section3_performance_goals.html': {
    title: 'SLA Turnaround Time vs. Target Compliance',
    category: 'sla',
    description: 'Contrasts task volume and mean turnaround latency between optimal and non-optimal outcomes.',
  },
  'fig_section6_priority_wait.html': {
    title: 'Priority-Driven Queue Congestion Analysis',
    category: 'queue',
    description: 'Demonstrates differential waiting times experienced across high, medium, and low priority tasks.',
  },
  'fig_section7_histogram.html': {
    title: 'Execution Duration vs. Total Latency Distributions',
    category: 'queue',
    description: 'Histogram comparison showing the frequency spread of active service vs total system turnaround times.',
  },
  'fig_section7_scatter_queue_growth.html': {
    title: 'Temporal Queue Buildup & Backlog Dynamics',
    category: 'queue',
    description: 'Scatter visualization tracing queue delay progression across sequential task arrivals.',
  },
  'fig_section7_vm_sla_ratio.html': {
    title: 'VM Instance SLA Efficiency Ratios',
    category: 'sla',
    description: 'Proportion of optimal versus non-optimal scheduling outcomes across individual VMs.',
  },
}

export default function App() {
  const [report, setReport] = useState<ProcessResponse>(emptyReport)
  const [activeTab, setActiveTab] = useState<TabKey>('overview')
  const [statusMessage, setStatusMessage] = useState<string>('Initializing simulation model...')
  const [isLoading, setIsLoading] = useState<boolean>(false)
  const [error, setError] = useState<string>('')

  useEffect(() => {
    void loadDefault()
  }, [])

  function handleResponse(data: unknown) {
    const payload = data as ProcessResponse
    setReport({
      dashboards: payload.dashboards || [],
      vizbAvailable: Boolean(payload.vizbAvailable),
      overview: payload.overview || [],
      tables: payload.tables || [],
      message: payload.message,
    })
    setIsLoading(false)
    const taskCount = payload.overview[0]?.count || 0
    setStatusMessage(
      payload.vizbAvailable
        ? `Simulation complete: ${taskCount.toLocaleString()} tasks evaluated with full interactive dashboards.`
        : `Simulation complete: ${taskCount.toLocaleString()} tasks evaluated (statistical tables & KPIs ready).`
    )
  }

  function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files && e.target.files[0]
    if (!f) return

    const fd = new FormData()
    fd.append('file', f)
    setError('')
    setIsLoading(true)
    setStatusMessage(`Uploading and simulating "${f.name}"...`)

    fetch('http://localhost:8080/api/upload', { method: 'POST', body: fd })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.json()
      })
      .then(handleResponse)
      .catch((err) => {
        setIsLoading(false)
        setError(`Failed to process dataset upload: ${err.message || 'Server unreachable'}`)
        setStatusMessage('Simulation halted. Please check server connection.')
      })
  }

  async function loadDefault() {
    try {
      setError('')
      setIsLoading(true)
      setStatusMessage('Loading benchmark dataset and evaluating queue dynamics...')
      const r = await fetch('http://localhost:8080/api/process-default')
      if (!r.ok) throw new Error(`HTTP ${r.status}`)
      const data = await r.json()
      handleResponse(data)
    } catch (err: unknown) {
      setIsLoading(false)
      const msg = err instanceof Error ? err.message : 'Server unreachable'
      setError(`Unable to connect to CloudPulse server at http://localhost:8080 (${msg}). Ensure the server is running.`)
      setStatusMessage('Waiting for backend server.')
    }
  }

  function exportReportJSON() {
    const dataStr = 'data:text/json;charset=utf-8,' + encodeURIComponent(JSON.stringify(report, null, 2))
    const downloadAnchor = document.createElement('a')
    downloadAnchor.setAttribute('href', dataStr)
    downloadAnchor.setAttribute('download', 'cloudpulse_performance_report.json')
    document.body.appendChild(downloadAnchor)
    downloadAnchor.click()
    downloadAnchor.remove()
  }

  const kpis = useMemo(() => {
    const findMetric = (name: string) => report.overview.find((m) => m.metric.toLowerCase().includes(name.toLowerCase()))
    const totalResp = findMetric('Total Response')
    const queueWait = findMetric('Queue Wait')
    const execTime = findMetric('Execution Time')
    const cpu = findMetric('CPU')
    const ram = findMetric('RAM')

    return {
      totalTasks: totalResp?.count ?? 0,
      avgTurnaround: totalResp ? `${totalResp.mean.toFixed(2)} s` : '—',
      p95Turnaround: totalResp ? `${totalResp.p95.toFixed(2)} s` : '—',
      avgQueueDelay: queueWait ? `${queueWait.mean.toFixed(2)} s` : '—',
      avgExecTime: execTime ? `${execTime.mean.toFixed(2)} s` : '—',
      avgCpu: cpu ? `${cpu.mean.toFixed(1)}%` : '—',
      avgRam: ram ? `${ram.mean.toFixed(0)} MB` : '—',
    }
  }, [report.overview])

  const filteredDashboards = useMemo(() => {
    if (activeTab === 'overview') return report.dashboards
    return report.dashboards.filter((d) => {
      const meta = DASHBOARD_UX_TITLES[d.file]
      return meta ? meta.category === activeTab : true
    })
  }, [report.dashboards, activeTab])

  return (
    <div className="app-shell">
      {/* Top Application Bar */}
      <header className="navbar">
        <div className="nav-container">
          <div className="brand-group">
            <div className="brand-badge">Sim</div>
            <div>
              <h1 className="brand-title">CloudPulse</h1>
              <p className="brand-subtitle">Cloud Task Performance Modeling & Evaluation System</p>
            </div>
          </div>

          <div className="nav-actions">
            <label className="btn btn-secondary file-upload-btn">
              <span>Select Task Dataset (.csv)</span>
              <input type="file" accept=".csv" onChange={handleFile} disabled={isLoading} />
            </label>

            <button className="btn btn-primary" onClick={loadDefault} disabled={isLoading}>
              {isLoading ? 'Simulating...' : 'Run Benchmark'}
            </button>

            <button className="btn btn-outline" onClick={exportReportJSON} disabled={report.overview.length === 0}>
              Export Report
            </button>
          </div>
        </div>
      </header>

      {/* Main Container */}
      <main className="main-content">
        {/* Status & Banner Notification */}
        <section className="status-banner">
          <div className="status-indicator">
            <span className={`status-dot ${isLoading ? 'loading' : error ? 'error' : 'ready'}`} />
            <span className="status-text">{statusMessage}</span>
          </div>
          {report.dashboards.length > 0 && !report.vizbAvailable && (
            <span className="info-chip">Static Mode (Install 'vizb' CLI for interactive HTML charts)</span>
          )}
        </section>

        {error && (
          <div className="alert alert-error" role="alert">
            <p><strong>Connection Notice:</strong> {error}</p>
          </div>
        )}

        {/* Executive KPI Summary Cards */}
        <section className="kpi-grid">
          <div className="kpi-card">
            <span className="kpi-label">Workload Volume</span>
            <div className="kpi-value">{kpis.totalTasks.toLocaleString()}</div>
            <span className="kpi-caption">Total Tasks Evaluated</span>
          </div>
          <div className="kpi-card highlight">
            <span className="kpi-label">Mean Turnaround (T_Total)</span>
            <div className="kpi-value">{kpis.avgTurnaround}</div>
            <span className="kpi-caption">P95: {kpis.p95Turnaround}</span>
          </div>
          <div className="kpi-card">
            <span className="kpi-label">Mean Queue Latency</span>
            <div className="kpi-value">{kpis.avgQueueDelay}</div>
            <span className="kpi-caption">Buffering Delay (T_Queue)</span>
          </div>
          <div className="kpi-card">
            <span className="kpi-label">Mean Service Duration</span>
            <div className="kpi-value">{kpis.avgExecTime}</div>
            <span className="kpi-caption">Active Compute (T_Execution)</span>
          </div>
          <div className="kpi-card">
            <span className="kpi-label">Cluster CPU Load</span>
            <div className="kpi-value">{kpis.avgCpu}</div>
            <span className="kpi-caption">Avg Memory: {kpis.avgRam}</span>
          </div>
        </section>

        {/* Navigation Tabs */}
        <nav className="tab-bar" aria-label="Analysis Sections">
          <button
            className={`tab-btn ${activeTab === 'overview' ? 'active' : ''}`}
            onClick={() => setActiveTab('overview')}
          >
            System Overview
          </button>
          <button
            className={`tab-btn ${activeTab === 'resources' ? 'active' : ''}`}
            onClick={() => setActiveTab('resources')}
          >
            VM Resource Allocation
          </button>
          <button
            className={`tab-btn ${activeTab === 'queue' ? 'active' : ''}`}
            onClick={() => setActiveTab('queue')}
          >
            Queue Dynamics & Latency
          </button>
          <button
            className={`tab-btn ${activeTab === 'sla' ? 'active' : ''}`}
            onClick={() => setActiveTab('sla')}
          >
            SLA & Priority Performance
          </button>
          <button
            className={`tab-btn ${activeTab === 'tables' ? 'active' : ''}`}
            onClick={() => setActiveTab('tables')}
          >
            Detailed Metrics Tables
          </button>
        </nav>

        {/* Tab 1: System Overview */}
        {activeTab === 'overview' && (
          <div className="tab-pane">
            <section className="section-block">
              <div className="section-header">
                <div>
                  <h2 className="section-title">Key Performance Indicators & Statistical Profile</h2>
                  <p className="section-subtitle">
                    Stochastic multi-server queue evaluation across compute, memory, I/O, and latency dimensions.
                  </p>
                </div>
              </div>

              <div className="metric-cards-grid">
                {report.overview.map((item) => {
                  const meta = METRIC_UX_META[item.metric] || {
                    label: item.metric,
                    unit: '',
                    description: 'System metric evaluation',
                    category: 'General',
                  }
                  return (
                    <article key={item.metric} className="metric-detail-card">
                      <div className="metric-header">
                        <div>
                          <span className="metric-category-tag">{meta.category}</span>
                          <h3 className="metric-name">{meta.label}</h3>
                        </div>
                        <span className="unit-badge">{meta.unit || 'scalar'}</span>
                      </div>
                      <p className="metric-desc">{meta.description}</p>
                      <div className="stat-rows">
                        <div className="stat-row primary">
                          <span>Average (Mean)</span>
                          <strong>{item.mean.toFixed(2)} {meta.unit}</strong>
                        </div>
                        <div className="stat-row">
                          <span>95th Percentile (P95)</span>
                          <span>{item.p95.toFixed(2)} {meta.unit}</span>
                        </div>
                        <div className="stat-row">
                          <span>Median (P50)</span>
                          <span>{item.median.toFixed(2)} {meta.unit}</span>
                        </div>
                        <div className="stat-row">
                          <span>Std. Deviation</span>
                          <span>{item.stdDev.toFixed(2)} {meta.unit}</span>
                        </div>
                        <div className="stat-row">
                          <span>Observed Range [Min - Max]</span>
                          <span>{item.min.toFixed(1)} – {item.max.toFixed(1)} {meta.unit}</span>
                        </div>
                      </div>
                    </article>
                  )
                })}
              </div>
            </section>

            {/* Quick Glimpse of Visualizations */}
            <section className="section-block">
              <div className="section-header">
                <div>
                  <h2 className="section-title">Interactive Performance Visualizations</h2>
                  <p className="section-subtitle">Explore behavior under varying load, priority queuing, and VM distribution.</p>
                </div>
              </div>
              <VisualizationsGrid dashboards={report.dashboards} />
            </section>
          </div>
        )}

        {/* Tab 2: VM Resource Allocation */}
        {activeTab === 'resources' && (
          <div className="tab-pane">
            <section className="section-block">
              <div className="section-header">
                <div>
                  <h2 className="section-title">Virtual Machine Resource Allocation & Footprints</h2>
                  <p className="section-subtitle">
                    Distribution of compute (CPU %), memory (RAM MB), storage (Disk I/O), and network across VM nodes.
                  </p>
                </div>
              </div>

              {/* Render Resource Table */}
              {report.tables
                .filter((t) => t.title.includes('Footprint') || t.title.includes('Architecture'))
                .map((table) => (
                  <TableCard key={table.title} table={table} />
                ))}

              <VisualizationsGrid dashboards={filteredDashboards} />
            </section>
          </div>
        )}

        {/* Tab 3: Queue Dynamics & Latency */}
        {activeTab === 'queue' && (
          <div className="tab-pane">
            <section className="section-block">
              <div className="section-header">
                <div>
                  <h2 className="section-title">Queue Waiting Dynamics & Latency Distribution</h2>
                  <p className="section-subtitle">
                    Temporal backlog growth, priority queue wait disparities, and latency frequency distributions.
                  </p>
                </div>
              </div>

              {report.tables
                .filter((t) => t.title.includes('Priority') || t.title.includes('Histogram'))
                .map((table) => (
                  <TableCard key={table.title} table={table} />
                ))}

              <VisualizationsGrid dashboards={filteredDashboards} />
            </section>
          </div>
        )}

        {/* Tab 4: SLA & Priority Performance */}
        {activeTab === 'sla' && (
          <div className="tab-pane">
            <section className="section-block">
              <div className="section-header">
                <div>
                  <h2 className="section-title">SLA Compliance & Priority Scheduling Evaluation</h2>
                  <p className="section-subtitle">
                    Analysis of optimal versus non-optimal scheduling outcomes and per-VM success ratios.
                  </p>
                </div>
              </div>

              {report.tables
                .filter((t) => t.title.includes('SLA'))
                .map((table) => (
                  <TableCard key={table.title} table={table} />
                ))}

              <VisualizationsGrid dashboards={filteredDashboards} />
            </section>
          </div>
        )}

        {/* Tab 5: Detailed Tabular Data */}
        {activeTab === 'tables' && (
          <div className="tab-pane">
            <section className="section-block">
              <div className="section-header">
                <div>
                  <h2 className="section-title">Comprehensive Tabulated Datasets</h2>
                  <p className="section-subtitle">
                    Full numerical results exported for formal report generation and verification.
                  </p>
                </div>
              </div>

              <div className="table-stack">
                {report.tables.map((table) => (
                  <TableCard key={table.title} table={table} />
                ))}
              </div>
            </section>
          </div>
        )}
      </main>

      {/* Footer */}
      <footer className="footer">
        <div className="footer-content">
          <p>CloudPulse Simulation Engine · Academic Performance Modelling & Evaluation · EEI6373</p>
        </div>
      </footer>
    </div>
  )
}

function TableCard({ table }: { table: ReportTable }) {
  const meta = TABLE_UX_META[table.title] || { title: table.title, subtitle: 'Summary Data' }
  return (
    <article className="card table-card">
      <div className="card-header">
        <div>
          <h3 className="card-title">{meta.title}</h3>
          <p className="card-subtitle">{meta.subtitle}</p>
        </div>
        <span className="badge">{table.rows.length} records</span>
      </div>
      <div className="table-container">
        <table className="data-table">
          <thead>
            <tr>
              {table.headers.map((h) => (
                <th key={h}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {table.rows.map((row, idx) => (
              <tr key={idx}>
                {row.map((cell, cIdx) => (
                  <td key={cIdx}>{cell}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </article>
  )
}

function VisualizationsGrid({ dashboards }: { dashboards: DashboardInfo[] }) {
  if (dashboards.length === 0) {
    return (
      <div className="empty-card">
        <h4>No Visualizations Available</h4>
        <p>Run the backend simulation or install the <code>vizb</code> CLI utility to generate interactive visual charts.</p>
      </div>
    )
  }

  return (
    <div className="visualizations-grid">
      {dashboards.map((dashboard) => {
        const meta = DASHBOARD_UX_TITLES[dashboard.file] || {
          title: dashboard.title,
          category: 'overview' as TabKey,
          description: 'Performance chart output',
        }
        return (
          <article key={dashboard.file} className="card viz-card">
            <div className="viz-card-header">
              <div>
                <h3 className="viz-title">{meta.title}</h3>
                <p className="viz-desc">{meta.description}</p>
              </div>
              <a
                href={`http://localhost:8080${dashboard.url}`}
                target="_blank"
                rel="noreferrer"
                className="btn-link"
              >
                View Fullscreen ↗
              </a>
            </div>
            <div className="viz-frame-wrap">
              <iframe
                title={meta.title}
                src={`http://localhost:8080${dashboard.url}`}
                className="viz-frame"
                loading="lazy"
              />
            </div>
          </article>
        )
      })}
    </div>
  )
}