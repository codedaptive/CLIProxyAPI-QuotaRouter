# 7. Plugin-owned roles with ordered model/effort targets

Date: 2026-10-23. Status: accepted for the MiniLLM revision.

A role publishes a client-visible model name and owns its ordered model/effort
pairs. The plugin uses CPA model registration, model routing, exact-account host
model execution and stream callbacks. It does not synthesize global/per-account
OAuth aliases or perform upstream HTTP/authentication itself.

Role preference is evaluated per request. Exhaust eligible accounts for a target
before moving to the next target; re-probe after cooldown and automatically return
to preferred targets. Do not retry malformed requests, cancellation or any stream
that has already delivered output. Fixed effort is default; client effort override
and per-target mappings are explicit opt-ins.

This extends ADR-0001's capability list without changing its plugin-only decision.
It supersedes ADR-0005's v7 compatibility claim for builds with role routing:
host.auth.models requires the newer SDK; the supported/tested baseline is v8.0.40.
ADR-0002 still applies to conversation identity. Role request bodies cross the ABI
because roles are executors; ordinary model requests do not acquire that cost.
Roles use a separate hashed session namespace so usage learning cannot rebind a
desktop conversation. The source of truth remains CPA config (ADR-0004).
