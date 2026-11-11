import { useEffect, useState } from "react";
import type { PageProps } from "../App";
import { Banner, Pill, Skeleton } from "../components/ui";
import { toast, usePoll } from "../hooks";
import { applySettings } from "../settingsApi";
import type { Settings } from "../types";

export interface RoleTarget { model: string; effort: string; providers?: string[]; account_ids?: string[]; effort_map?: Record<string,string> }
export interface Role { name: string; enabled: boolean; allow_client_effort_override: boolean; cooldown_seconds: number; targets: RoleTarget[] }
interface RoleDecision { role: string; model: string; effort: string; provider: string; account_id: string; target: number; status: string; reason: string; at: string }
const efforts = ["auto", "none", "low", "medium", "high", "xhigh", "max"];
const split = (s: string) => s.split(",").map(x => x.trim()).filter(Boolean);
export function RolesPage({ reloadOverview }: PageProps) {
 const settings = usePoll<Settings>("/settings", 30000);
 const status = usePoll<{decisions: RoleDecision[] | null}>("/roles", 5000);
 const [draft, setDraft] = useState<Role[] | null>(null);
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState("");
 if (!settings.data) return <Skeleton rows={6} />;
 const roles = draft ?? (settings.data.config.roles as Role[] ?? []);
 const update = (i: number, patch: Partial<Role>) => setDraft(roles.map((r,n) => n === i ? {...r,...patch} : r));
 const target = (i: number, j: number, patch: Partial<RoleTarget>) => update(i, {targets: roles[i].targets.map((t,n) => n === j ? {...t,...patch} : t)});
 const move = (i: number, j: number, delta: number) => { const targets = [...roles[i].targets]; [targets[j],targets[j+delta]] = [targets[j+delta],targets[j]]; update(i,{targets}); };
 async function save() {
  setBusy(true); setError("");
  try { const v = await applySettings({roles}); if (!v.valid) {setError((v.errors ?? []).map(e => `${e.field}: ${e.message}`).join("; ")); return;}
   await settings.reload(); setDraft(null); await reloadOverview(); toast("Role routing saved");
  } catch(e) {setError(e instanceof Error ? e.message : String(e));} finally {setBusy(false);}
 }
 return <div className="role-page">
  <section className="panel panel-body">
   <h2>Role routing</h2>
   <p>Give your agents a role name. Each request tries targets in order, across eligible accounts, and returns to the preferred model when it recovers.</p>
   <p className="muted">Effort is fixed per target unless you allow client overrides. Desktop conversation affinity applies separately to ordinary model requests. Roles are available only in active mode.</p>
   <button className="btn" onClick={() => setDraft([...roles,{name:"",enabled:false,allow_client_effort_override:false,cooldown_seconds:30,targets:[{model:"",effort:"high"}]}])}>Add role</button>
  </section>
  {roles.map((r,i) => {
   const latest = [...(status.data?.decisions ?? [])].reverse().find(d => d.role === r.name);
   return <section className="panel" key={i} aria-label={`Role ${r.name || i+1}`}>
    <div className="panel-head"><h2>{r.name || "New role"}</h2><Pill tone={r.enabled ? "good" : "info"}>{r.enabled ? "Enabled" : "Disabled"}</Pill><span className="spacer"/><button className="btn sm" onClick={() => setDraft(roles.filter((_,n) => n !== i))}>Remove role</button></div>
    <div className="panel-body role-fields">
     <label>Role name<input className="input" aria-label={`Role ${i+1} name`} value={r.name} onChange={e => update(i,{name:e.target.value})} placeholder="designer" /></label>
     <label>Recovery cooldown (seconds)<input className="input" type="number" min="0" max="300" value={r.cooldown_seconds || 30} onChange={e => update(i,{cooldown_seconds:Number(e.target.value)})} /></label>
     <label className="role-check"><input type="checkbox" checked={r.enabled} onChange={e => update(i,{enabled:e.target.checked})}/> Enable role</label>
     <label className="role-check"><input type="checkbox" checked={r.allow_client_effort_override} onChange={e => update(i,{allow_client_effort_override:e.target.checked})}/> Allow client effort override (/effort)</label>
    </div>
    {r.targets.map((t,j) => <div className="role-target" key={j}>
     <div className="role-target-head"><strong>{j === 0 ? "Preferred" : `Fallback ${j}`}</strong><span className="spacer"/>
      <button className="btn sm" aria-label={`Move target ${j+1} up`} disabled={j === 0} onClick={() => move(i,j,-1)}>↑</button>
      <button className="btn sm" aria-label={`Move target ${j+1} down`} disabled={j === r.targets.length-1} onClick={() => move(i,j,1)}>↓</button>
      <button className="btn sm" disabled={r.targets.length === 1} onClick={() => update(i,{targets:r.targets.filter((_,n) => n !== j)})}>Remove target</button>
     </div>
     <div className="role-fields">
      <label>Model<input className="input" aria-label={`Role ${i+1} target ${j+1} model`} value={t.model} onChange={e => target(i,j,{model:e.target.value})} placeholder="claude-opus-5-5" /></label>
      <label>{r.allow_client_effort_override ? "Default effort" : "Fixed effort"}<select className="input" aria-label={`Role ${i+1} target ${j+1} effort`} value={t.effort} onChange={e => target(i,j,{effort:e.target.value})}>{efforts.map(x => <option key={x}>{x}</option>)}</select></label>
      <label>Providers (empty = all)<ListInput values={t.providers ?? []} onCommit={values => target(i,j,{providers:values})} placeholder="claude, antigravity"/></label>
      <label>Account IDs (empty = all)<ListInput values={t.account_ids ?? []} onCommit={values => target(i,j,{account_ids:values})} placeholder="Optional comma-separated IDs"/></label>
     </div>
     {r.allow_client_effort_override && <details><summary>Effort mappings for this target</summary><p className="muted">Map a client level to a supported target level. Unmapped levels pass through; invalid upstream settings return an error.</p><div className="role-fields">{efforts.filter(x => x !== "auto").map(level => <label key={level}>Client {level}<select className="input" value={t.effort_map?.[level] ?? ""} onChange={e => { const map={...t.effort_map}; if(e.target.value) map[level]=e.target.value; else delete map[level]; target(i,j,{effort_map:map}); }}><option value="">Same level</option>{efforts.map(x => <option key={x}>{x}</option>)}</select></label>)}</div></details>}
    </div>)}
    <div className="panel-body"><button className="btn sm" onClick={() => update(i,{targets:[...r.targets,{model:"",effort:"high"}]})}>Add fallback</button>
     {latest && <p className="muted" role="status">Last request: {latest.model || "no eligible model"}{latest.effort && ` / ${latest.effort}`} · {latest.status} · {latest.reason}{latest.account_id && ` · ${latest.provider} / ${latest.account_id}`} · {new Date(latest.at).toLocaleString()}</p>}
    </div>
   </section>;
  })}
  {status.error && <Banner tone="warning">Routing status could not be refreshed.</Banner>}
  {error && <Banner tone="critical">{error}</Banner>}
  {draft !== null && <div className="savebar"><strong>Unsaved role changes</strong><span className="spacer"/><button className="btn" disabled={busy} onClick={() => {setDraft(null);setError("");}}>Discard</button><button className="btn primary" disabled={busy} onClick={() => void save()}>{busy ? "Saving…" : "Save roles"}</button></div>}
 </div>;
}

function ListInput({values,onCommit,placeholder}:{values:string[];onCommit:(values:string[])=>void;placeholder:string}) { const joined=values.join(", ");const [text,setText]=useState(joined);useEffect(()=>setText(joined),[joined]);return <input className="input" value={text} placeholder={placeholder} onChange={e=>setText(e.target.value)} onBlur={()=>onCommit(split(text))}/>; }
