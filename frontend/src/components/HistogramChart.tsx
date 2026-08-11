import React, { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'

type Row = { [k: string]: string }

function histogram(values: number[], bins = 10) {
  const min = Math.min(...values)
  const max = Math.max(...values)
  const width = (max - min) / bins || 1
  const counts = new Array(bins).fill(0)
  for (const v of values) {
    let idx = Math.floor((v - min) / width)
    if (idx < 0) idx = 0
    if (idx >= bins) idx = bins - 1
    counts[idx]++
  }
  const labels = counts.map((_, i) => `${(min + i * width).toFixed(2)}-${(min + (i + 1) * width).toFixed(2)}`)
  return { counts, labels }
}

export default function HistogramChart({ rows, keyField, title }: { rows: Row[]; keyField: string; title: string }) {
  const option = useMemo(() => {
    const values = rows.map((r) => Number(r[keyField])).filter((v) => !isNaN(v))
    const { counts, labels } = histogram(values, 12)
    return {
      title: { text: title },
      xAxis: { type: 'category', data: labels },
      yAxis: { type: 'value' },
      series: [{ type: 'bar', data: counts }],
      tooltip: { trigger: 'axis' },
    }
  }, [rows, keyField, title])

  return <ReactECharts option={option} style={{ height: 300 }} />
}
