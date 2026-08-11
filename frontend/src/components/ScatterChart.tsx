import React, { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'

type Row = { [k: string]: string }

export default function ScatterChart({ rows, xKey, yKey }: { rows: Row[]; xKey: string; yKey: string }) {
  const option = useMemo(() => {
    const data = rows.map((r) => [Number(r[xKey]), Number(r[yKey])])
    return {
      xAxis: { type: 'value', name: xKey },
      yAxis: { type: 'value', name: yKey },
      series: [{ symbolSize: 6, data, type: 'scatter' }],
      tooltip: { trigger: 'item' },
    }
  }, [rows, xKey, yKey])

  return <ReactECharts option={option} style={{ height: 400 }} />
}
