# Create and use role routing configurations

This guide is for a person or Factory AI generating task roles for MiniLLM's
customized `kpatdev-quota-router` plugin. A role gives agents a stable model name
such as `designer`, backed by an ordered list of model and effort pairings.
The proxy tries eligible accounts for the preferred model before trying the next
model, and returns to the preferred model on later requests after recovery.

This feature requires **MiniLLM plugin 1.1.0-minillm.2** and the tested
**CLIProxyAPI 8.0.40** baseline. The packaged build is macOS Apple Silicon.
At this handoff, the build has been tested but has **not been deployed to
production**. Installing the native plugin and activating its scheduler are
separate deployment tasks. These instructions configure roles after that setup.

## Create a YAML file

Start with [roles.example.yaml](examples/roles.example.yaml). The file is a
**plugin configuration fragment**, not a complete proxy configuration:

```yaml
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

  - name: reviewer
    enabled: true
    allow_client_effort_override: true
    cooldown_seconds: 30
    targets:
      - model: claude-opus-5-5
        effort: high
        effort_map:
          xhigh: high
      - model: gpt-6.1-sol
        effort: xhigh
        effort_map:
          max: xhigh
```

Replace the example models and effort levels with combinations available on your
proxy. Model names must match its registered model IDs, including any existing
aliases. The examples do not create provider connections or authorize accounts.

Use spaces for indentation, lowercase `true` and `false`, and a YAML list for
`roles` and `targets`. Target order is preference order. There is no CSV importer
or automatic watcher for `roles.example.yaml`; apply the fragment as described below.

## Field reference

| Role field | Meaning |
| --- | --- |
| `name` | Required, unique client-visible model name. Start with a lowercase letter; then use lowercase letters, digits, `_` or `-`. Maximum 80 characters. |
| `enabled` | Set explicitly to `true` to publish and serve the role. Omitted means disabled. |
| `allow_client_effort_override` | `false` by default: always use each target's configured effort. `true`: accept explicit client effort and apply the target's mapping. |
| `cooldown_seconds` | Retry delay for a failed account/model/effort combination. Integer 0–300; omitted or 0 means 30 seconds, not no cooldown. |
| `targets` | Required ordered list of 1–16 model/effort pairings. |

| Target field | Meaning |
| --- | --- |
| `model` | Required exact registered model ID. Do not add an effort suffix such as `(high)` here. A target cannot be another role. |
| `effort` | Required: `auto`, `none`, `low`, `medium`, `high`, `xhigh` or `max`. Actual support depends on the target model and CPA's capability metadata. |
| `providers` | Optional list of exact provider identifiers. Omit or use `[]` to allow all providers advertising this model. |
| `account_ids` | Optional list of exact CPA credential/auth IDs. Omit or use `[]` to allow all eligible accounts. These are not display names, plan names, email addresses or client API keys. |
| `effort_map` | Optional mapping from an explicit client effort to the effort requested for this target. Used only when client override is enabled. Unmapped effort passes through. `auto` cannot be a mapping key; it can be a value. |

There may be at most 100 roles. Avoid names already used by models or aliases,
including models published by other plugins. Credential-backed name collisions
are rejected at execution with HTTP 409.

Filters are combined: when both lists are present, an account must match the
provider list **and** the account list. Get exact IDs from the proxy's account
metadata; do not infer them from an account's email address. For example:

```yaml
targets:
  - model: gpt-6.1-sol
    effort: xhigh
    providers: [codex]
    account_ids: [replace-with-actual-auth-id.json]
```

Leave filters out when the intent is “try this model everywhere it is available.”

## Choose how effort works

For a proven task/model pairing, use `allow_client_effort_override: false`.
In the `designer` example, the preferred target always requests `high` and the
fallback requests `xhigh`, even if the caller submits `low`.

For interactive control, set it to `true`. The `reviewer` example behaves as follows:

| Client effort | Preferred target effort | Fallback target effort |
| --- | --- | --- |
| Missing or `auto` | `high` | `xhigh` |
| `low` | `low` | `low` |
| `xhigh` | `high` through the mapping | `xhigh` |
| `max` | `max` | `xhigh` through the mapping |

The preferred target in the last row must support `max`; otherwise choose a
supported mapping. Mapping labels express your policy, not equivalent amounts of
reasoning across providers. CPA's native capability validation and final payload
rules still apply.

The plugin accepts a request-model suffix such as `reviewer(low)` or a protocol's
effort field. A suffix takes precedence over body effort. Missing or `auto` uses
the target default. Setting a target's own effort to `auto` leaves effort to the
native model default. Incoming thinking budgets are removed so they cannot compete
with the role's selected effort.

If a client implements `/effort` by sending an effort field, the override setting
controls whether it takes effect. The proxy cannot distinguish an intentional
`/effort` command from a client sending its saved default on every request.

## Apply the YAML by hand

1. Back up the current CPA `config.yaml`.
2. Find `plugins.configs.kpatdev-quota-router` in that file.
3. Merge the fragment's `roles` list into that existing plugin block. Preserve
   every unrelated proxy/plugin setting and every existing role you want to keep.
4. The plugin must be enabled, loaded, and in `mode: active` to serve roles.
   Shadow mode does not publish or execute roles.
5. Use the deployment's configuration reload workflow and verify the result in
   the plugin dashboard. The management API workflow below validates and
   hot-reloads settings; merely creating a separate YAML file does nothing.

The resulting placement looks like this; it is an excerpt, not a replacement file:

```yaml
plugins:
  # Keep the existing plugin loader settings here.
  configs:
    kpatdev-quota-router:
      enabled: true
      mode: active
      # Keep existing strategy, affinity, quota and other settings here.
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
```

Do not add duplicate `plugins`, `configs` or plugin keys. Do not copy the fragment
into OAuth credential files or create matching global aliases.

## Apply generated YAML through the management API

Factory AI can use the same workflow as the plugin's **Role routing** page.
Use the deployment's existing management origin and a **management key** in
`Authorization: Bearer <management-key>`. This is distinct from an inference
client key. Keep credentials outside generated YAML and source control.

All paths below are relative to the management origin. Let
`P = /v0/management/plugins/kpatdev-quota-router`.

1. `GET P/settings`. Keep the returned `config.roles` and `generation` for review
   and rollback. Retain existing roles unless explicitly replacing/removing them.
2. Parse YAML with a YAML parser and produce a JSON object containing only
   `{"roles": [...]}`. This is the plugin fragment, without a `plugins` wrapper.
3. `POST P/settings/validate` with that JSON and `Content-Type: application/json`.
   Stop on validation errors; require `valid: true` and retain the returned
   expected `generation`. Validation checks configuration, not live model access.
4. `PATCH P/config` with the same JSON. CPA persists it to its configuration and
   hot-reloads the plugin. A `roles` patch replaces the **entire roles list**;
   it does not append roles or merge them by name.
5. Poll `GET P/settings` about every 500 ms for up to 15 seconds, matching its
   `generation` to the validated expected generation. If it never matches,
   inspect the current configuration and CPA logs before retrying the write.
6. Check model discovery and make a small inference request as shown below.

An empty `roles: []` removes all roles. To disable one while preserving it, set
its `enabled` to `false` and submit the complete retained list. Restore the saved
roles through the same validate/patch/read-back workflow to roll back a change.
Avoid concurrent settings edits: this workflow does not provide a compare-and-swap
lock against another editor changing roles.

## Use roles from an agent or application

Keep the existing proxy connection, TLS trust and inference client key. Change
only the requested model to the role name. Do not configure a new upstream account
for each role. Enabled roles are published in the proxy's `GET /v1/models` response.

For Claude Code already configured to use this proxy:

```sh
claude --model designer
```

For a Factory AI agent, set its configured model ID or request `model` to
`designer`. Its connection must still point to the proxy and use a supported
protocol. If the client restricts model selection to its own list, register the
role as a custom model in that client. No Factory-specific setting names are
assumed here.

These are minimal inference request bodies; send them to the matching endpoint
under the existing proxy origin with your normal inference authentication:

**OpenAI Chat Completions — `POST /v1/chat/completions`:**

```json
{"model":"designer","messages":[{"role":"user","content":"Reply with OK."}]}
```

**OpenAI Responses — `POST /v1/responses`:**

```json
{"model":"reviewer","reasoning":{"effort":"low"},"input":"Reply with OK."}
```

**Anthropic Messages — `POST /v1/messages`:**

```json
{"model":"reviewer","max_tokens":64,"output_config":{"effort":"low"},"messages":[{"role":"user","content":"Reply with OK."}]}
```

Use the normal protocol headers, including `Content-Type: application/json` and,
for Anthropic-compatible clients, their normal `anthropic-version` header.
Chat Completions expresses client effort as top-level `reasoning_effort`.

Check **Role routing** in the plugin dashboard to see the actual selected target,
account and requested effort. Automated monitoring can read `GET P/roles` using
the management key. It retains the last 100 attempt/decision records in memory.
Do not infer the selected upstream solely from the response's `model` label.

## Understand fallback during a swarm task

Each request starts at the first target and considers its eligible accounts
before moving to the next target. Known quota exhaustion, account restrictions,
cooldowns and host availability can make a candidate ineligible. Account priority,
when available, orders accounts within a target; it does not reorder the targets.

Availability failures such as 429 or 503 allow fallback. The implemented retryable
HTTP statuses are 401, 403, 404, 408, 429, 500, 502, 503 and 504. Malformed requests
such as 400/422 and cancellations stop the chain. If nothing can serve the role,
the request fails; there is no unlisted catch-all model.

After a failure cooldown expires, one subsequent request can probe that candidate.
There are no background billable health generations. Quota recovery depends on
the normal quota observations. Successful recovery brings the preferred target
back for later requests, without changing an in-progress response.

Once a stream has delivered its first nonempty chunk, the plugin will not replay
the request on another target. Role routing does not anchor a conversation to an
account; its model preference is evaluated per request and does not create or
rebind ordinary desktop affinity records.

For cross-provider fallback, supply portable conversation context. Do not assume
one provider can resume another provider's stored response ID, signed thinking
blocks or server tools. The test suite covers the three protocols above with mock
upstreams; validate the tools and conversation features your factory actually uses.

## Instructions to give Factory AI

> Generate a YAML plugin fragment with a top-level `roles` list, following this
> schema. Use exact model IDs available on our proxy. Give each role an ordered
> list of model/effort targets, with fixed effort unless I request client overrides.
> Preserve existing roles and unrelated settings. Do not create OAuth aliases or
> include credentials. Return the YAML and explain the preference order. If asked
> to apply it, use the management validation, patch and generation verification
> workflow above, then verify model discovery and a small request. Installation
> or replacement of the native plugin is a separate deployment task.

See [implementation behavior and validation](role-routing.md) for further details.
