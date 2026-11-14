# Claude Ultra / Max weekly reserve

Local revision `1.1.0-minillm.4` supports the Ultra name for the opt-in reserve in the custom
`kpatdev-quota-router`. This revision was activated on MiniLLM on November 5, 2026; it replaces the
original `quota-reset-router`, which did not enforce the reserve.

Merge into `plugins.configs.kpatdev-quota-router`:

```yaml
mode: active
schedule_across_priorities: true
max_weekly_reserve:
  enabled: true
  remaining_percent: 10
  release_before: 24h
client_routing:
  # Merge into existing client routing settings; keep caller scopes/preferences.
  account_plans:
    claude-your-verified-ultra-account.json: ultra
```

The plan override applies even when `client_routing.enabled` is false. Use the
exact credential ID and a verified plan, never an email-only match. Claude Ultra and the older Max,
Max 5x and Max 20x names are covered. The `max_weekly_reserve` setting and
internal `max` plan identifier remain unchanged for compatibility.
`preferred_plans: {claude: [ultra]}` is equivalent to `[max]`. Provider-reported Max plan metadata is also used,
but Claude's quota endpoint does not always supply it: explicitly map every Max
credential before enabling the reserve. Unknown plans are not guessed to be Max.
Codex, Claude Pro/Team and API-key accounts are unaffected unless incorrectly
marked as a Claude Max account.

At **10% or less weekly quota remaining**, more than 24 hours before reset, the
account is reserved while another host-offered account can serve the requested
model. Normal routing tries those alternatives first. If none is usable, the
scheduler releases the percentage reserve for that request and selects a usable
reserved account. Protection returns automatically when an alternative recovers;
the setting is never toggled off globally.

This last-resort behavior starts in `1.1.0-minillm.6`. It applies to ordinary CLI,
desktop, inherited, mixed-provider, and pinned requests. A pin does not bypass a
usable alternative, and the plugin does not silently replace a pinned account.
Role routing tries ordinary permitted accounts for each target before reserved
accounts for that same model, then proceeds to the next model target if necessary.
Role filters and failure cooldowns remain in force; token counting uses the same
preference order. Quota on an account that cannot serve the requested model does
not prevent fallback.

Only the percentage floor is released. Actual weekly, five-hour, or model-specific
exhaustion, disabled/removed credentials, authentication failures, cooldowns, and
missing/stale reserve evidence still prevent last-resort use. An ordinary account
with unknown quota retains its normal opportunity before the reserve is spent.
Exactly 24 hours before reset, the time-based reserve release still applies.

For an existing bound session, the reserve causes a temporary failover and keeps
the binding. It may return to its original account after the reserve is released.
This is distinct from rebinding after confirmed exhaustion.

A known Max account with missing/stale weekly usage, missing reset time, or an
unconfirmed elapsed reset is protected until fresh evidence arrives. Freshness is
checked on the weekly window itself, not another window's update. Passive quota
headers and normal polling update the evidence; there are no inference probes.

When no eligible ordinary or reserved account can be chosen, the active scheduler
returns an explicit `weekly_reserve_protected` rejection instead of delegating a
protected pool back to the host. Shadow mode records the proposed behavior without
enforcing it. Mixed-provider pools use priority then credential ID among usable
choices; without reserve-related filtering normal mixed-provider delegation remains.

The plugin's Activity/decision views show `weekly_reserve_protected`,
`weekly_reserve_quota_unknown`, or `weekly_reserve_awaiting_reset`. Last-resort
selection is identified as `weekly_reserve_last_available`. Settings/export
preserve `max_weekly_reserve` across unrelated edits. Configure this block through
YAML or the plugin config API; no dedicated settings form has been added.

This is a **reported-quota cutoff, not an exact spending guarantee**. In-flight
requests, concurrent requests and provider reporting delays can carry the balance
below 10% before the next selection sees that usage. An exact floor would require
provider-side admission control or a reliable pre-request quota-cost reservation.

## Installation

Build/package with the existing darwin/arm64 workflow. Test in a separate host
with synthetic credentials. Do not overwrite a mapped native library in place.

When migrating from the original plugin, activation must
replace the old scheduler, migrate the Overview/collector's plugin-status readers,
and supply verified plan overrides. Only one scheduler should be active. Keep a
backup of the original plugin/config and validate host reload/drain behavior before
changing production; building this revision does not activate it.

Tests cover threshold/time boundaries, weekly-only/provider/plan scope, temporary
affinity failover, inherited sessions, CLI priority and desktop preference, unknown
or stale data, exhausted accounts, host pins, mixed pools, role eligibility, RPC
rejection versus host fallback, shadow mode, and settings reload.

## Verification — November 4, 2026

- `go vet ./...` and `go test -race -count=1 ./...`: passed.
- Native darwin/arm64 package loaded in a separate checksum-verified CLIProxyAPI
  8.0.40 process with desktop policy and the Max reserve enabled.
- `tests/role_smoke.py`: passed existing cross-provider/account fallback,
  effort, recovery, Claude/Chat Completions/Responses and streaming checks.
- Package: `dist/release/kpatdev-quota-router_1.1.0-minillm.3_darwin_arm64.zip`,
  with an adjacent SHA-256 file.
- No production configuration or native libraries were changed. The production
  proxy remained PID 2724. The reserve was not yet active at that verification. See the November 5 activation below.

## Ultra naming and upstream review — November 4, 2026

The current Claude subscription name **Ultra** is equivalent to the older Max
labels for reserve enforcement and desktop plan preference. Revision
`1.1.0-minillm.4` accepts `ultra`, `claude_ultra`, `ultra_5x` and `ultra_20x` while
preserving existing Max labels and configuration keys. Tests cover provider plan
metadata, explicit overrides, role eligibility, release at 24 hours, desktop
preference and unrelated-settings round trips.

Upstream checks:

- [kpatdev v1.1.0](https://github.com/kpatdev/quota-router/releases/tag/v1.1.0)
  remains the latest release. GitHub's comparison of `v1.1.0...main` reports
  identical (zero newer commits), and the open-PR search returned none. Our local
  fork already includes its Claude usage-parser and Fable quota fixes.
- [Original quota-reset-router v0.2.1](https://github.com/WebDevCaptain/quota-reset-router/releases/tag/v0.2.1)
  remains its latest release; `v0.2.1...main` is identical.
- [CLIProxyAPI v8.0.54](https://github.com/router-for-me/CLIProxyAPI/releases/tag/v8.0.54)
  is newer than production 8.0.40. The full comparison contains 216 commits,
  including merges. Relevant fixes include:
  - [Abort plugin replacement on quiesce failure and coordinate usage replay](https://github.com/router-for-me/CLIProxyAPI/commit/6e10359968392c5fef1483cb091393d5fe10aeac).
  - [Keep Fable-only 429s model-scoped when five-hour headers are absent](https://github.com/router-for-me/CLIProxyAPI/commit/0130d6f38c91e9bb3980484b8a2ef83be71c0a01), avoiding unnecessary Opus cooldown.
  - Structured provider usage capture, cached image/audio accounting, request
    selection IDs for scheduler/usage correlation, plugin stream ownership and
    translation fixes, and Gemini/Antigravity tool-result/signature fixes.
  - These changes are not evidence that Codex now reports exact cache-write
    counts. The reporting estimate must not be relabeled as measured without
    checking real upstream counters.

Recommend testing/upgrading the host before activating our scheduler. No upstream
plugin commits needed merging. The proxy checkout/dependency and production
binary have not been upgraded by this review.

Validation for `.4`: Go vet and all Go race tests passed. The package loaded in
isolated, checksum-verified 8.0.40 and 8.0.54 darwin/arm64 hosts with reserve and
desktop settings enabled. Role integration tests on 8.0.54 passed catalog,
account/model fallback, effort, recovery, Claude/Chat Completions/Responses and
streaming scenarios with mock upstreams. These checks do not establish every
production provider/account integration or live hot-reload behavior.

Package: `dist/release/kpatdev-quota-router_1.1.0-minillm.4_darwin_arm64.zip` and
its adjacent `.zip.sha256` file. The `.3` package is retained as the previous build.

## Deployment and quota polling

Install the plugin in an isolated instance first, using your own exact credential
IDs for plan overrides. Enable only one quota scheduler. Back up configuration,
plugin libraries, and persistent router state before changing the active library.

Reserve protection is opt-in. Set a 10% weekly reserve and a 24-hour release
horizon if those are your policy; desktop and role routing are separate opt-ins.
Verify decisions show `weekly_reserve_protected` for eligible protected accounts.
The original scheduler must not remain enabled alongside this plugin.

Use `poll_interval: 10m`, `max_age: 20m`, and `passive_skip: 5m` as a starting
point. Quota HTTP 429 responses indicate quota-read throttling, not exhausted
inference quota. The plugin waits at least the polling interval or Retry-After,
whichever is longer; manual refresh cannot bypass the persisted cooldown.

The Codedaptive dashboard consumes the active router's cached quota rather than
polling Claude usage again. The collector joins `/accounts` to credential IDs;
redacted display names are not identities. Keep the whole persistent state
directory when upgrading. Restore the prior library and relevant configuration
for rollback; disabling this scheduler also removes reserve protection.
