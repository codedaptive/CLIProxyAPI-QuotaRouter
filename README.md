<div align="center">

# CLIProxyAPI QuotaRouter — Codedaptive

A fork of [kpatdev/quota-router](https://github.com/kpatdev/quota-router), optimized for use with
**[CodeTeam - CE](https://github.com/codedaptive/codeteam-ce)**.

### Put the right account and model behind each request.

**Use quota before it resets. Keep desktop chats steady. Give agents stable roles.**

[![Core target](https://img.shields.io/badge/core%20target-v8.0.67-blue)](docs/codedaptive-integration.md)
[![Platforms](https://img.shields.io/badge/platforms-Linux%20%7C%20macOS%20%7C%20Windows-blue)](#prerequisites)
[![License: MIT](https://img.shields.io/badge/license-MIT-success)](LICENSE)

[Start here](#start-here) · [Works with](#works-with) · [Documentation](#documentation) · [Issues](https://github.com/codedaptive/CLIProxyAPI-QuotaRouter/issues)

</div>

CodeTeam - CE is optional: this fork also works with other compatible clients and
agent workflows.

Multiple subscriptions only help if requests reach accounts that can serve them.
A busy agent team also needs predictable model choices, while a desktop chat
benefits from staying on the same account across turns.

This native CLIProxyAPI plugin combines quota-aware scheduling with Codedaptive's
**desktop account preferences**, **persistent conversation affinity**,
**model/effort roles**, and **Claude Ultra/Max weekly reserve**. It builds on
[kpatdev/quota-router](https://github.com/kpatdev/quota-router), which in turn builds
on [WebDevCaptain/quota-reset-router](https://github.com/WebDevCaptain/quota-reset-router).

## What it does

| Capability | Behavior |
| --- | --- |
| **Quota scheduling** | Prefers accounts whose weekly/monthly quota resets soonest, with quota-pressure, balanced, and manual-priority policies also available. |
| **Desktop policy** | Opt-in client-key scopes prefer configured plan tiers and keep each recognized conversation on its account until it needs to move. Other callers use the extension's CLI priority/reset-first policy. |
| **Model/effort roles** | Publishes names such as `designer`, tries ordered model/effort targets across eligible accounts, falls back on availability failures, and tries the preferred target again after recovery. |
| **Weekly reserve** | Opt-in protection stops selecting eligible Claude Ultra/Max accounts at the configured reported weekly balance until the final reset horizon, with last-resort access when no other account can serve the model. |
| **Quota observations** | Reads provider quota endpoints and response headers, with cooldown handling for quota-read throttling. No inference calls are made just to measure quota. |
| **Built-in dashboard** | Shows accounts, sessions, routing decisions, role attempts, usage, and settings. |

Desktop policy, roles, and reserve protection are separate opt-ins. The normal
scheduler's affinity remains available when desktop policy is disabled. Role
routing evaluates model preference per request; it does not create a desktop
session binding. See each feature's guide before combining policies.

## Works with

The three CLIProxy repositories form one stack. Each has a separate job and can be
updated independently:

| Project | What it does |
| --- | --- |
| **[CLIProxyAPI](https://github.com/codedaptive/CLIProxyAPI)** | Serves model APIs, connects provider accounts, and loads native plugins. |
| **[CLIProxyAPI-QuotaRouter](https://github.com/codedaptive/CLIProxyAPI-QuotaRouter)** | Selects accounts by quota/reset policy, keeps desktop sessions attached, and routes model/effort roles. |
| **[CLIProxyAPI-MgmtCenterWithForecasting](https://github.com/codedaptive/CLIProxyAPI-MgmtCenterWithForecasting)** | Manages the proxy and adds usage history, spend analysis, quota forecasts, and subscription planning. |

These Codedaptive projects complement the stack on the agent side:

- **[CodeTeam - CE](https://github.com/codedaptive/codeteam-ce)** runs a coordinated
  team of coding agents. Its agents can use the proxy for model access and the
  router's role names for task-specific model preferences.
- **[MOOTx01 CE](https://github.com/codedaptive/mootx01-ce)** gives agents persistent
  memory across sessions, models, and machines.
- **[CodeGraph Hub](https://github.com/codedaptive/codegraph-hub)** supplies a shared
  code knowledge graph for finding symbols, callers, and change impact.

MOOTx01 and CodeGraph connect to the agents through their own integrations; they
are optional and are not installed by the proxy.

## Prerequisites

- A CLIProxyAPI host with native plugin support. The current integration target is
  **v8.0.67**; see the [integration record](docs/codedaptive-integration.md).
- Supported account credentials already configured in the host. Quota scheduling
  observes Claude and Codex OAuth accounts; role routing uses the models and
  providers registered by the host.
- To build: **Go 1.26.10**, a C toolchain, **Node.js 24+**, **pnpm 10.32.1**, Make,
  and Python 3. Linux/Windows build targets use Docker; macOS targets need macOS.

The plugin ID stays **`kpatdev-quota-router`** so existing configuration and state
remain compatible. Install one copy and use one active quota scheduler.

## Start here

For the complete stack, use the **[Codedaptive setup guide](https://github.com/codedaptive/CLIProxyAPI-MgmtCenterWithForecasting/blob/main/docs/codedaptive-setup.md)**.
To build this plugin from source on Apple Silicon:

```sh
git clone https://github.com/codedaptive/CLIProxyAPI-QuotaRouter.git
cd CLIProxyAPI-QuotaRouter
make web
make test TARGET=darwin_arm64
make build zip TARGET=darwin_arm64
make load-test TARGET=darwin_arm64
```

The ZIP is written to `dist/release/`. Choose a target that matches the intended
host: `linux_amd64`, `linux_arm64`, `darwin_arm64`, `darwin_amd64`, or
`windows_amd64`. Load tests must run on the matching platform. Intel macOS requires
the Makefile's patched Go toolchain; do not substitute a stock Go shared-library build.

Alternatively, use an explicitly published Codedaptive platform ZIP from
[Releases](https://github.com/codedaptive/CLIProxyAPI-QuotaRouter/releases) and verify
its published checksums. A source checkout or store registry alone is not a binary
release.

Extract the native library into the host's `plugins.dir`, then merge this into its
existing configuration:

```yaml
plugins:
  enabled: true
  dir: plugins
  configs:
    kpatdev-quota-router:
      enabled: true
      priority: 10
      mode: shadow
      strategy: reset_first
```

Start with a separate test instance. Load the plugin, inspect the dashboard's
Activity decisions in `shadow` mode, then switch to `active` when the selection
matches your policy. `priority` here orders plugins, not provider accounts.

The plugin dashboard is served at:

```text
http://<proxy-host>:8317/v0/resource/plugins/kpatdev-quota-router/index.html
```

Sign in with the host's management key. The `/v0/` resource and extension paths
are declared by the plugin and remain intentional on the v8 host. See the
[detailed reference](ROUTER_REFERENCE.md) for API paths, Docker, and store setup.

## Configure the Codedaptive policies

- **Desktop vs. CLI:** use dedicated client keys, calculate their caller scopes,
  choose preferred plans, and validate conversation identity. Follow
  [desktop routing](docs/minillm-desktop-routing.md).
- **Task roles:** define a YAML list of model/effort targets, apply it through the
  plugin settings, and request the role as a model. Follow the
  [role-routing how-to](docs/role-routing-howto.md). A role can prefer Opus at
  `high`, fall back to Sol at `xhigh`, and return to Opus on later requests after
  recovery. It does not require duplicate OAuth aliases.
- **Weekly reserve:** configure a remaining percentage and release horizon, such
  as 10% until the last 24 hours. Follow the [reserve guide](docs/max-weekly-reserve.md).
  Enforcement uses reported quota; in-flight requests and reporting delays can
  carry usage below the threshold.

## What is in this repository

| Path | Purpose |
| --- | --- |
| `internal/` | Scheduler, account/quota state, role execution, management API, and embedded UI |
| `web/` | React/TypeScript plugin dashboard |
| `tests/` | Native loading, host integration, role fallback, and persistence fixtures |
| `scripts/` | Packaging, caller-scope helper, and Intel macOS toolchain patch |
| `deploy/` | Docker deployment examples |
| `docs/` | Policy guides, architecture, ADRs, and compatibility evidence |
| `registry.json` | Custom plugin-store metadata |
| `ROUTER_REFERENCE.md` | Detailed base-router configuration and operations |

## Documentation

- [Desktop account preferences and affinity](docs/minillm-desktop-routing.md)
- [Create and use model/effort roles](docs/role-routing-howto.md) · [YAML example](docs/examples/roles.example.yaml)
- [Role execution and fallback](docs/role-routing.md)
- [Claude Ultra/Max weekly reserve](docs/max-weekly-reserve.md)
- [Detailed routing, configuration, Docker, and API reference](ROUTER_REFERENCE.md)
- [Architecture](docs/architecture.md) · [Limitations](docs/limitations.md)
- [Codedaptive integration record](docs/codedaptive-integration.md) · [Upstream changelog](CHANGELOG.md)

## Development and tests

```sh
make web                         # Rebuild the embedded dashboard
pnpm --dir web test               # Dashboard unit tests
make test TARGET=darwin_arm64      # Formatting, vet, and Go race tests
make build zip TARGET=darwin_arm64
make load-test TARGET=darwin_arm64 # Isolated load and role/fallback smoke tests
```

Use the target matching your host. Additional native, host, and Docker persistence
checks are documented in [the reference](ROUTER_REFERENCE.md#development).
Fixtures use synthetic credentials; they do not require production accounts.

## Releases and updates

The fork is based on **kpatdev v1.1.0**; its current custom source version is
**`1.1.0-minillm.6`**. Upstream downloads do not contain the Codedaptive extensions.
The [CI workflow](https://github.com/codedaptive/CLIProxyAPI-QuotaRouter/actions/workflows/ci.yml)
builds and tests supported platform targets.

Back up the plugin, configuration, and persistent state before replacement. Do not
overwrite a mapped native library in place. Use the host's supported plugin lifecycle
or a controlled restart, and preserve the existing plugin ID and state directory.

## Contributing and support

[Open an issue](https://github.com/codedaptive/CLIProxyAPI-QuotaRouter/issues) with
the plugin/core versions, platform, policy, expected account selection, and a
redacted Activity decision. Include session/role behavior and reset times when
relevant. Pull requests should cover changed selection and fallback behavior with
regression tests.

Native plugins run inside the proxy with access to its traffic and credentials.
Install trusted builds, and keep keys, auth files, account identities, and private
state out of public reports.

## License and upstream credit

[MIT](LICENSE). Credit to **kpatdev**, **Shreyash / WebDevCaptain**, and the
CLIProxyAPI contributors. Original notices are retained; see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for bundled dependencies.
Codedaptive maintains the desktop, role-routing, and reserve extensions. This
project is not affiliated with the model providers.
