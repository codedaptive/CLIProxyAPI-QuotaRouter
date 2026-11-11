# Role routing

For a step-by-step handoff and a reusable YAML example, see
[Create and use role routing configurations](role-routing-howto.md).

MiniLLM plugin `1.1.0-minillm.2` adds role routing alongside desktop affinity.
The native library targets **CLIProxyAPI 8.0.40** on darwin/arm64. This revision
uses newer host callbacks and does not retain the old v7 compatibility claim.

Define roles in the plugin's **Role routing** page. The plugin publishes enabled
role names in `/v1/models` and handles their execution. No global OAuth aliases,
credential-file aliases or upstream tokens are created or changed. Other model
requests keep the existing routing behavior. Roles are advertised and executed
only when the plugin is in active mode; leave the roles disabled while preparing
them. Native library installation/activation is a separate deployment step.

## Example configuration

Under `plugins.configs.kpatdev-quota-router`:

```yaml
mode: active
roles:
  - name: designer
    enabled: true
    allow_client_effort_override: false
    cooldown_seconds: 30
    targets:
      - model: claude-opus-5-5
        effort: high
      - model: gpt-6.1-sol
        effort: xhigh
        # Optional: providers: [codex]
        # Optional: account_ids: [your-credential-id]
        # Used only when client effort override is enabled:
        # effort_map: {max: xhigh}
```

Clients request `designer` as their model. The dashboard edits ordered targets,
fixed/default effort, account/provider restrictions, recovery cooldown, and effort
mappings. Saving validates the complete settings and uses CPA's existing config
persistence/reload endpoint. It does not write a second configuration file.

Model names must match the host's registered model names, including any existing
aliases. The plugin does not guess that two differently named models are
identical. A role cannot target another role. Conflicts with credential-backed
model/alias names return HTTP 409; avoid collisions with other plugins' static
model names too.

## Selection and recovery

Every request starts at the first target. The plugin discovers accounts that
advertise that target from the host model registry, applies restrictions and
known quota/health, then tries eligible accounts before considering the next
model. Account priority determines order within a target when the host supplies
it; IDs break ties. Config-backed API accounts absent from the auth-file listing
remain eligible through the model catalog, with the host validating the exact
account pin. Native authentication/access restrictions still apply.

The plugin reuses its Claude/Codex quota observations. Other providers can serve
roles using host availability and observed execution errors; they do not acquire
Claude/Codex-style quota polling. Subscription quotas are not fabricated for API
keys. Quota reset recovery is recognized through normal quota refresh/passive
observations; it is not guaranteed at the exact reset timestamp.

HTTP 401/403/404/408/429 and 500/502/503/504 permit another account/target. Invalid
requests such as 400/422 and client cancellation stop the chain. Failed account +
model + effort combinations get a cooldown (30 seconds by default; configurable
up to 300 seconds). After expiry, one request probes that combination while other
requests may continue to fallbacks. A successful probe restores the preferred
model for later requests. Cooldowns are transient and do not survive a restart.

No background generations are used as health probes. There is no mid-response
failback. Role executions use a separate, hashed session namespace for scheduling
and usage; they do not create/rebind desktop affinity records. Original Codex
thread/session headers remain intact. Role model preference always wins over
ordinary desktop account preferences.

## Effort

Each target has an effort. **Fixed pairing** is the default and overrides incoming
model suffixes, effort fields, and thinking budgets. **Allow client effort override**
uses a submitted explicit effort; missing or `auto` uses the target default.
Request-model suffix takes precedence over request-body effort. Supported input
fields are Claude `output_config.effort`, Responses `reasoning.effort`, and Chat
Completions `reasoning_effort`.

The plugin cannot distinguish a user issuing `/effort` from a client sending a
saved/default setting. Optional per-target `effort_map` makes mappings explicit.
The real model with its effective effort suffix goes through CPA's normal thinking
translation and final payload rules. Configure accurate target capabilities;
upstream/native capability validation still applies, and global payload overrides
can override final requests. The selected effort shown by the plugin is the effort
it requested, not a claim about an upstream model's internal reasoning.

## Streaming, token counting and observability

The plugin uses host model-execution callbacks for translation, credentials,
transport, error handling and usage reporting. Streaming uses bounded host bridges;
it does not buffer the complete response. Fallback is permitted before output.
After the first non-empty chunk is delivered, a failure terminates that stream
without replaying the request on another model. Client disconnects do not start a
fallback generation. Stream errors after HTTP headers are sent are stream errors,
not a replacement HTTP response.

The Roles page shows the last decision per role, target, account, requested effort
and reason. The management `/roles` endpoint retains the last 100 attempt/decision
records in memory without prompts, client keys or OAuth tokens. The executor supplies
`X-Role-Name`, `X-Role-Model` and `X-Role-Effort` on non-streaming responses;
whether clients receive these headers depends on CPA's response-header policy.
Streaming diagnostic headers identify only the role because the backend can change
before its first chunk. Use the Roles page to inspect routing decisions.

Claude `count_tokens` requests route to the chosen native provider/model and apply
account restrictions in the scheduler. They never run a generation as a token
estimate. A provider without a native count route can return unsupported. The native test
also covers token counting through a custom compatibility provider. Unsupported
client protocols and cross-provider continuation IDs are not emulated; supply full
conversation context when switching models. We have not tested every provider's
server tools, signed thinking blocks or stored-response continuation semantics.

## Verification and deployment

`go test -race -count=1 ./...`, `go vet ./...`, dashboard tests and the production
asset build cover existing desktop routing and the new role behavior. Unit tests
cover ordered cross-provider/account fallback, recovery, fixed/overridden/mapped
effort, malformed requests, account restrictions, name conflicts, streaming failures,
cancellation, session separation and settings persistence.

`tests/role_smoke.py` runs a checksum-verified official CPA 8.0.40 binary with a
separate config, ports, state and synthetic API credentials. Mock upstreams validate
actual catalog registration, account/model fallback, effort translation, recovery,
Claude Messages, Chat Completions, Responses streaming, and no fallback after
partial output. It does not use production OAuth files or subscription quota.

Build artifact:
`dist/release/kpatdev-quota-router_1.1.0-minillm.2_darwin_arm64.zip`.
Back up the existing scheduler/config before installation. Keep only one scheduler
active. Loading a new native library may require a controlled restart: development
and testing do not deploy this build or restart production.

Validation on 2026-10-23 also included a real Claude executor against a local
mock, confirmation that role usage creates no desktop bindings, browser-based
settings save/reload, and visual checks at 1440px and 390px widths.
