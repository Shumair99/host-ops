import type { ReactNode } from 'react'
import type { Report } from './types'

// Intent: help an operator locate the failing layer. Graphite/chalk/semantic lamps,
// border-only depth, close surfaces, Plex labels + mono facts, 4px spacing.
export const layers = [['runtime', 'Runtime'], ['local_http', 'Local'], ['proxy_route', 'Proxy'], ['dns', 'DNS'], ['tls', 'TLS'], ['public_http', 'Public']] as const
export const bytes = (value?: number) => {
  if (value === undefined || !Number.isFinite(value)) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']; const index = value > 0 ? Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1) : 0
  return `${(value / 1024 ** Math.max(index, 0)).toFixed(index > 0 ? 1 : 0)} ${units[Math.max(index, 0)]}`
}
export const percent = (value?: number) => value === undefined ? '—' : `${value.toFixed(1)}%`
export const time = (value: string) => new Date(value).toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' })
export const date = (value: string) => new Date(value).toLocaleString('en-GB', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })
export function Badge({ state }: { state: string }) { return <span className={`badge ${state}`}><i aria-hidden="true" />{state === 'critical' ? 'Critical' : state === 'healthy' ? 'Healthy' : state === 'warning' ? 'Warning' : state === 'pass' ? 'Passed' : state === 'fail' ? 'Failed' : state === 'warn' ? 'Warning' : state === 'skip' ? 'N/A' : 'Unknown'}</span> }
export function Chain({ report, labels = false }: { report?: Report; labels?: boolean }) { return <div className={`chain ${labels ? 'labelled' : ''}`} aria-label="Production health layers">{layers.map(([key, label]) => { const check = report?.checks.find(item => item.layer === key); return <span key={key} className={`chain-node ${check?.state || 'unknown'}`} title={`${label}: ${check?.summary || 'Not yet observed'}`} tabIndex={0} aria-label={`${label}: ${check?.state || 'unknown'}. ${check?.summary || 'Not yet observed'}`}><i aria-hidden="true" />{labels ? <small>{label}</small> : null}</span> })}</div> }
export function Panel({ title, note, children, action }: { title: string; note?: string; children: ReactNode; action?: ReactNode }) { return <section className="panel"><div className="panel-head"><div><h2>{title}</h2>{note ? <p>{note}</p> : null}</div>{action}</div>{children}</section> }
export function Empty({ title, text }: { title: string; text: string }) { return <div className="empty"><span className="empty-mark" aria-hidden="true">○</span><h3>{title}</h3><p>{text}</p></div> }
export function Loading() { return <div className="empty" role="status">Reading the latest observations…</div> }
export function ErrorBox({ message }: { message: string }) { return <div className="error-box" role="alert">{message}</div> }
export function Gauge({ label, value, detail }: { label: string; value?: number; detail?: string }) { const state = value === undefined ? 'unknown' : value >= 95 ? 'critical' : value >= 80 ? 'warning' : 'healthy'; return <div className="gauge"><div><span>{label}</span><strong className={state}>{percent(value)}</strong></div><progress className={state} value={value || 0} max={100} aria-label={label} /><small>{detail || (value === undefined ? 'Awaiting host observation' : 'Latest measured usage')}</small></div> }
