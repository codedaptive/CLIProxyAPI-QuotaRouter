# 6. Explicit client policy with persistent desktop bindings

Date: 2026-10-23. Status: accepted for the MiniLLM local revision.

Keep the existing CLI priority/reset-first behavior while desktop conversations
prefer larger plans and retain their chosen account through normal quota-ranking
changes. Select the desktop policy by CPA's authenticated caller scope, not by
User-Agent inference or prompt commands. Keep plan preferences separate from the
credential priorities used by CLI callers.

The extension is opt-in. Desktop bindings span models within a provider, inherit
to subagents and survive idle periods and restarts. They remain bounded by the
existing LRU cap. Quota exhaustion and permanent credential/model failures permit
rebinding; transient failures retain the binding. Once rebound, recovering accounts
do not take the session back.

ADR-0002 remains the baseline: session headers are preferred. A trusted host-derived
canonical session ID is accepted as a fallback when present, but no request-body
interceptor or inferred shared session is introduced. Missing session identity is
reported explicitly. This is a plugin-only change and does not modify CPA core.
