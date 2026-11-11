# 8. Max weekly reserve is an eligibility restriction

Date: 2026-11-04. Status: accepted for the MiniLLM local revision.

The user requests that Claude Max retain 10% of weekly capacity until the final
24 hours before reset. Implement an opt-in eligibility restriction before ranking
and affinity, not a priority score or new-session-only preference. It covers
ordinary, desktop, inherited and role traffic offered to this plugin.

A reserve is temporary: existing bindings may fail over without rebinding. This
extends ADR-0006's temporary-failure behavior to deliberate quota protection.
Settings default to disabled for compatibility. Known Max plans require fresh
weekly evidence; exact credential overrides supply plan identity when providers
omit it. Unknown plans are not inferred from account names.

When all safe choices are unavailable, reject selection rather than delegate a
pool containing protected accounts. Mixed-provider delegation and host pins must
not bypass protection. This intentionally narrows the original delegation policy
only when a protected Max candidate is present. The host ABI cannot delegate a
filtered candidate pool, so mixed pools with protected candidates use a safe
host-offered alternative ordered by priority and ID, or reject if none exists. Requests outside the scheduler's execution path are not
covered.

The boundary is based on observed quota; concurrency and reporting delay mean
that no exact 10% floor can be promised. No inference request is sent to probe quota.
