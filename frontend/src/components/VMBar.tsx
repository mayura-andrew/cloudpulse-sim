import React, { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'

type Row = { [k: string]: string }

export default function VMBar({ rows, metricKey, title }: { rows: Row[]; metricKey: string; title: string }) {
  const option = useMemo(() => {
    const agg = new Map<string, { sum: number; count: number }>()
    for (const r of rows) {
      const vm = r['VM_ID'] || r['VM_ID']
      const v = parseFloat(r[metricKey] || '0')
      const cur = agg.get(vm) || { sum: 0, count: 0 }
      cur.sum += isNaN(v) ? 0 : v
      cur.count += 1
      agg.set(vm, cur)
    }
    const vmIDs = Array.from(agg.keys()).sort((a, b) => Number(a) - Number(b))
    const values = vmIDs.map((id) => {
      const b = agg.get(id)!
      return +(b.sum / b.count).toFixed(2)
    })

    return {
      title: { text: title },
      xAxis: { type: 'category', data: vmIDs },
      yAxis: { type: 'value' },
      series: [{ type: 'bar', data: values }],
    }
  }, [rows, metricKey, title])

  return <ReactECharts option={option} style={{ height: 300 }} />
}
