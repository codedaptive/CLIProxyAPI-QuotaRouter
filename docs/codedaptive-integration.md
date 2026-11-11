# Codedaptive integration

The fork starts from kpatdev quota-router 1.1.0 with the MiniLLM extensions documented
in ADRs 0006–0008. Core integration target: CLIProxyAPI 8.0.67. SDK dependency
remains v8.0.40, the minimum used by the role API; the native plugin uses the host ABI.

Validated November 11, 2026 on macOS arm64:

- `go test -race ./...` and `go vet ./...` pass.
- Web tests: 35 pass; TypeScript and Vite build pass; embedded assets rebuilt.
- Native C shared-library build and package pass.
- Official v8.0.67 archive checksum verified.
- Isolated load test passes with desktop policy and weekly reserve enabled.
- Isolated role smoke test passes: account/provider fallback, effort override,
  recovery/failback, Chat Completions, Claude Messages, Responses, and streaming.

 Multi-platform
release CI must pass before publishing binaries for other platforms. No production
credentials or configuration are required for the isolated fixture tests.

For local development, install Go 1.26.10+, Node 24+, and pnpm 10.32.1:

```sh
cd web
pnpm install --frozen-lockfile
pnpm test
pnpm build
cd ..
go vet ./...
go test -race ./...
make build zip TARGET=darwin_arm64
```

Use the target matching your machine. The upstream Makefile documents the patched
runtime needed for Intel macOS. Do not load an arbitrary stock Go Intel plugin into
an existing host. Plugin policy defaults remain opt-in and exact account IDs must
come from your own proxy instance.
