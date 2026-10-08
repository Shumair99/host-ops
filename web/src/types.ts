export type State = 'pass' | 'warn' | 'fail' | 'unknown' | 'skip'
export interface Check { host_id: string; site_id: string; layer: string; state: State; summary: string; latency_ms?: number; status_code?: number }
export interface Diagnosis { finding: string; confidence: string; layer: string; evidence: string[]; investigation: string }
export interface Site { id: string; host_id: string; name: string; repository?: string; branch?: string; runtime: { type: string; container?: string; network?: string; port?: number; directory?: string; unit?: string }; proxy: { domain?: string; upstream?: string }; health: { public_url?: string }; business?: { client?: string; monthly_fee?: number }; recovery?: { rto_target_minutes?: number; rpo_target_hours?: number } }
export interface Runtime { status: string; image?: string; image_id?: string; commit?: string; restart_policy?: string; restart_count: number; started_at?: string; memory_limit: number; privileged: boolean; read_only: boolean }
export interface Report { host_id: string; site_id: string; name: string; observed_at: string; vantage: string; overall: string; runtime: Runtime; checks: Check[]; diagnoses: Diagnosis[] }
export interface Sample { host_id: string; site_id: string; observed_at: string; values: Record<string, number>; unavailable: string[] }
export interface EvidenceRecord { id: number; host_id: string; site_id: string; kind: string; observed_at: string; data: Record<string, unknown> }
export interface Dashboard { sites: Site[]; reports: Report[]; host: Sample | null; events: EvidenceRecord[] }
