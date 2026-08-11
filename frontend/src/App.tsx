import React, { useEffect, useState } from 'react'

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

export default function App() {
  const [report, setReport] = useState<ProcessResponse>(emptyReport)
  const [status, setStatus] = useState<string>('No data loaded. Upload a CSV or load the default dataset.')
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
    setStatus(payload.vizbAvailable ? 'Backend processing complete.' : 'Processing complete, but vizb was not available on the backend.')
  }

  function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files && e.target.files[0]
    if (!f) return

    const fd = new FormData()
    fd.append('file', f)
    setError('')
    setStatus('Processing the uploaded CSV on the backend...')

    fetch('http://localhost:8080/api/upload', { method: 'POST', body: fd })
      .then((res) => res.json())
      .then(handleResponse)
      .catch(() => {
        setError('Upload failed')
        setStatus('No data loaded. Upload a CSV or load the default dataset.')
      })
  }

  async function loadDefault() {
    try {
      setError('')
      setStatus('Loading the default dataset and generating the report...')
      const r = await fetch('http://localhost:8080/api/process-default')
      const data = await r.json()
      handleResponse(data)
    } catch {
      setError('Failed to load the default CSV from the backend. Ensure the server is running.')
      setStatus('No data loaded. Upload a CSV or load the default dataset.')
    }
  }

  return (
    <div className="page">
      <main className="report">
        <header className="report-header">
          <div>
            <p className="eyebrow">CloudPulse</p>
            <h1>Performance Modeling Report</h1>
            <p className="subtitle">
              The backend processes the dataset, generates vizb charts, and returns the numerical summaries used in the report.
            </p>
          </div>

          <div className="controls">
            <label className="file-field">
              <span>Upload CSV</span>
              <input type="file" accept=".csv" onChange={handleFile} />
            </label>
          </div>
        </header>

        <section className="status-panel">
          <p>{status}</p>
          {error && <p className="error">{error}</p>}
          {report.message && <p>{report.message}</p>}
        </section>

        <section>
          <h2>Statistical Summary</h2>
          <div className="summary-grid">
            {report.overview.map((item) => (
              <article key={item.metric} className="summary-card">
                <h3>{item.metric}</h3>
                <dl>
                  <div><dt>Count</dt><dd>{item.count}</dd></div>
                  <div><dt>Mean</dt><dd>{item.mean.toFixed(2)}</dd></div>
                  <div><dt>Std Dev</dt><dd>{item.stdDev.toFixed(2)}</dd></div>
                  <div><dt>Min</dt><dd>{item.min.toFixed(2)}</dd></div>
                  <div><dt>Max</dt><dd>{item.max.toFixed(2)}</dd></div>
                  <div><dt>Median</dt><dd>{item.median.toFixed(2)}</dd></div>
                  <div><dt>P95</dt><dd>{item.p95.toFixed(2)}</dd></div>
                </dl>
              </article>
            ))}
          </div>
        </section>

        <section>
          <h2>Tabulated Results</h2>
          <div className="table-stack">
            {report.tables.map((table) => (
              <article key={table.title} className="table-card">
                <h3>{table.title}</h3>
                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        {table.headers.map((header) => (
                          <th key={header}>{header}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {table.rows.map((row, rowIndex) => (
                        <tr key={`${table.title}-${rowIndex}`}>
                          {row.map((cell, cellIndex) => (
                            <td key={`${table.title}-${rowIndex}-${cellIndex}`}>{cell}</td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </article>
            ))}
          </div>
        </section>

        <section>
          <h2>Generated Plots</h2>
          <div className="plot-grid">
            {report.dashboards.length === 0 ? (
              <article className="empty-state">
                <p>Run the backend to generate the charts.</p>
              </article>
            ) : (
              report.dashboards.map((dashboard) => (
                <article key={dashboard.file} className="plot-card">
                  <div className="plot-heading">
                    <h3>{dashboard.title}</h3>
                    <a href={`http://localhost:8080${dashboard.url}`} target="_blank" rel="noreferrer">
                      Open
                    </a>
                  </div>
                  <iframe title={dashboard.title} src={`http://localhost:8080${dashboard.url}`} className="plot-frame" />
                </article>
              ))
            )}
          </div>
        </section>
      </main>
    </div>
  )
}