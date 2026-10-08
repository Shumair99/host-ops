import { useResource } from './api'
import { Badge, Empty, ErrorBox, Loading, Panel } from './ui'
import type { State } from './types'

interface Rule { id: string; category: string; title: string; state: State; points: number; max_points: number; evidence: string; investigation: string; priority: number }
interface Assessment { site_id: string; name: string; score: number; max_score: number; provisional: boolean; rules: Rule[] }

// Intent: evidence-led improvement decisions. Scores always keep their rules,
// provisional evidence is explicit; same lamps, border depth and 4px rhythm.
function AssessmentPanel({ value }: { value: Assessment }) { return <Panel title={value.name} note="Production readiness · explicit rules, without AI judgement"><div className="readiness-score"><strong>{value.score}<small> / {value.max_score}</small></strong><div><h3>{value.provisional ? 'Provisional assessment' : 'Evidence assessed'}</h3><p>{value.provisional ? 'Unverified evidence earns no points. Review the rules below.' : 'Every point is backed by the listed observations.'}</p></div><a className="text-link" href={`#/sites/${encodeURIComponent(value.site_id)}`}>Open property ↗</a></div><div className="readiness-rules">{value.rules.map(rule => <article key={rule.id}><div className="rule-label"><span className="eyebrow">{rule.category}</span><h3>{rule.title}</h3><p>{rule.evidence || 'No evidence recorded.'}</p>{rule.state !== 'pass' ? <small>{rule.investigation}</small> : null}</div><Badge state={rule.state} /><span className="mono">{rule.points}/{rule.max_points}</span></article>)}</div></Panel> }
export function Readiness({ token, site }: { token: string; site?: string }) { const { value, loading, error } = useResource<Assessment[] | Assessment>(site ? `/api/sites/${encodeURIComponent(site)}/readiness` : '/api/readiness', token); return loading ? <Loading /> : error ? <ErrorBox message={error} /> : value ? <>{(Array.isArray(value) ? value : [value]).map(item => <AssessmentPanel key={item.site_id} value={item} />)}</> : <Empty title="No readiness assessment" text="Register sites and collect evidence to assess them." /> }
