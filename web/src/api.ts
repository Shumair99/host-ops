import { useEffect, useState } from 'react'
import type { Dashboard, EvidenceRecord, Report, Sample, Site } from './types'

export async function request<T>(path: string, token: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { headers: { Authorization: `Bearer ${token}` }, signal, cache: 'no-store' })
  if (!response.ok) throw new Error(response.status === 401 ? 'Access token rejected. Reconnect with the agent token.' : `The agent could not complete this request (${response.status}).`)
  return response.json() as Promise<T>
}

export function useDashboard(token: string) {
  const [data, setData] = useState<Dashboard | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    async function load() {
      try {
        const [sites, reports, host, events] = await Promise.all([
          request<Site[]>('/api/sites', token, controller.signal), request<Report[]>('/api/status', token, controller.signal),
          request<Sample | null>('/api/host', token, controller.signal), request<EvidenceRecord[]>('/api/events', token, controller.signal),
        ])
        if (!controller.signal.aborted) { setData({ sites, reports, host, events }); setError('') }
      } catch (cause) { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Agent unavailable.') }
      finally { if (!controller.signal.aborted) { setLoading(false); timer = setTimeout(load, 20000) } }
    }
    void load()
    return () => { controller.abort(); clearTimeout(timer) }
  }, [token, refresh])
  return { data, error, loading, refresh: () => setRefresh(value => value + 1) }
}

export function useResource<T>(path: string, token: string) {
  const [value, setValue] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setValue(null); setError('')
    void request<T>(path, token, controller.signal).then(result => { if (!controller.signal.aborted) setValue(result) }).catch(cause => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Request failed.') }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [path, token])
  return { value, error, loading }
}
