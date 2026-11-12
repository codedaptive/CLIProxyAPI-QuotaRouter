# Codedaptive desktop routing

Desktop routing was introduced in `1.1.0-minillm.1`, based on kpatdev Quota Router
v1.1.0. See the [Codedaptive README](../README.md) for the current version and
installation steps. Building the plugin does not activate it in a running proxy.

## Behavior

With `client_routing.enabled: true`, authenticated caller scopes listed in
`desktop_caller_scopes` use desktop routing. Every other caller uses the legacy
Quota Reset Router selection order: highest host-offered priority tier, earliest
account weekly/monthly reset among eligible accounts, then account ID. CLI
requests do not create or consult bindings, including bindings learned from usage.
The updated upstream quota parser and health handling remain in use; this is
selection-policy compatibility, not emulation of the old parser's bugs.

Desktop conversations prefer Claude Ultra (formerly Max) or Codex Pro by default. Preferences are
provider-specific and apply before credential priorities when selecting an
account. Other eligible plans remain available as fallback. Within the same plan
tier, credential priority and reset-first ranking apply. Existing bindings take
precedence over all new-session preferences.

Bindings are per authenticated caller, provider and conversation, across models
of that provider. Subagents inherit their parent's account when usable. Bindings
survive restarts and idle periods, and do not snap back when a preferred account
recovers. Five-hour, weekly/monthly or applicable model-quota exhaustion causes
rebinding; rejection, disablement, removal or model unavailability also permit
recovery. Temporary rate limits/5xx fail over for that request without rebinding.
No idle TTL is applied to desktop bindings, but the existing `affinity_max_sessions`
LRU cap and explicit management clearing still apply.

Only host-offered credentials can be selected. If no account can serve the request,
the plugin delegates to CPA's normal retry/error handling. Fresh quota is preferred;
without fresh readings desktop requests may use a host-offered, otherwise usable
account, with the decision marked as fallback. Unknown plan labels do not receive
preferred status. A sessionless desktop request gets plan preference only and an
explicit `desktop_session_missing` diagnostic, never an invented shared session.

## Configuration

The extension is disabled by default. Existing kpatdev installations retain their
behavior until it is enabled. These are plugin settings, merged into
`plugins.configs.kpatdev-quota-router`:

```yaml
enabled: true
priority: 10
mode: shadow
schedule_across_priorities: true
state_dir: /absolute/private/path/minillm-quota-router-state
client_routing:
  enabled: true
  desktop_caller_scopes:
    # Replace with the SHA-256 caller scope printed by scripts/caller_scope.py.
    - REPLACE_WITH_64_CHARACTER_CALLER_SCOPE
  preferred_plans:
    claude: [max]
    codex: [pro]
  account_plans: {}
  # Optional overrides when the provider does not report a plan:
  # account_plans:
  #   claude-example.json: max_20x
  #   codex-example.json: pro
```

Plan labels come from the roster/quota data; exact credential-ID overrides take
precedence. `ultra`, `claude_ultra`, `ultra_5x`, `ultra_20x`, `max_5x` and `max_20x` normalize to the compatibility value `max`. An ordered list such as
`claude: [max, pro]` gives Pro preference over unlisted plans when Max is unavailable.
Overrides require verified account information, not guesses from email addresses.

Use dedicated native CPA client keys for the desktop apps. Compute their scopes
with `python3 scripts/caller_scope.py`; the script prompts without echo and outputs
only the hash. It does not create or register keys. Do not put upstream OAuth tokens
or raw client keys in these plugin settings. The plugin uses CPA's authenticated
`caller_scope` metadata; a User-Agent or client-supplied desktop header cannot opt
an unlisted key into the desktop policy.

Codex Desktop and CLI can share `~/.codex/config.toml`. Simply replacing its key
would classify both as desktop. Deployment must give the CLI its existing key via
a separate profile/launcher configuration and the desktop its dedicated key.
Claude Desktop and CLI should likewise retain distinct client credentials.

Session identity must reach the scheduler through recognized session/thread headers
or CPA's trusted `canonical_session_id`. A client key identifies the policy, not a
conversation. Validate two simultaneous chats in each actual desktop app before
claiming live affinity; body-only identity may require host support not present
before scheduler selection. No prompt content is scanned for commands.

## Deployment and rollback

Only one scheduler plugin is active: do not expect the old quota-reset router to
compose with this one. Test on a separate CPA instance with separate auth, plugin,
state and log directories. Never point a test instance at production OAuth files.

For production activation, back up config and plugin files, prepare desktop client
credentials and verified plan overrides, then replace the scheduler deliberately.
Use `routing.session-affinity: false` for the built-in fallback if CLI routing must
remain unpinned; this plugin owns desktop affinity independently. Keep the old
router binary/config for rollback. Loading a new native library may require a
proxy restart; don't restart production while coding streams must remain active.

The plugin dashboard shows separate next-account previews for CLI and desktop,
and desktop sessions show `No idle expiry`. The Activity decision records identify
`cli_priority_reset_first` or `desktop_preferred_plan_affinity`. Configure the
extension in CPA's YAML editor/API; the upstream plugin settings form retains the
extension during unrelated edits but does not provide dedicated policy controls.

## Verification

Tests cover separate desktop/CLI selection, plan inference and overrides, quota
rebinding without snap-back, temporary failure handling, subagent inheritance,
caller/session isolation, absence of CLI bindings, persistence beyond idle TTL,
JSON RPC plus real state-file restart, and configuration reload/validation.
They use synthetic credentials and do not consume subscription quota.

Build the embedded dashboard before the library. Build the dylib for darwin/arm64,
package with `scripts/package_release.py`, and run `tests/load_smoke.py` against a
checksum-verified CPA release. The load test starts a separate loopback process
with no credentials, verifies registration/management resources, then stops it.
That confirms native loading, not end-to-end behavior of a real desktop client.

### Local build verification — 2026-10-23

- `go test -race -count=1 ./...` and `go vet ./...`: passed.
- Dashboard: 33 tests passed; TypeScript and production asset build passed.
- Native load against checksum-verified CLIProxyAPI 8.0.40 darwin/arm64:
  passed with the extension disabled and with `--desktop-policy` enabled.
  The enabled check also verifies settings retention and separate CLI/desktop
  previews through the running host's management API.
- Package: `dist/release/kpatdev-quota-router_1.1.0-minillm.1_darwin_arm64.zip`.
  Its companion `.zip.sha256` file records the checksum.
- Production was not changed or restarted. Dedicated desktop keys, verified plan
  mappings, and two simultaneous real chats per desktop app remain deployment
  checks; synthetic session tests do not establish what those apps transmit.
