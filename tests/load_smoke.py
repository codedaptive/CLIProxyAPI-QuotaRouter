"""Load a release zip into the official CLIProxyAPI binary on this machine.

Usage: load_smoke.py <cpa-archive> <cpa-checksums.txt> <plugin-zip> [--desktop-policy] [--max-reserve]

Installs the plugin where the CLIProxyAPI plugin store puts it
(plugins/<goos>/<goarch>/<id>-v<version>.<ext>), starts CLIProxyAPI with no
credentials, and checks that the plugin registers and serves its status
route. Without credentials the plugin makes no network requests.
"""

import hashlib
import json
import re
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

PLUGIN = "kpatdev-quota-router"
EXTENSIONS = {"linux": ".so", "darwin": ".dylib", "windows": ".dll"}
CPA_ARCH = {"amd64": "amd64", "arm64": "aarch64"}
MANAGEMENT_KEY = "load-smoke-management-key"
STATUS_PATH = f"/v0/management/plugins/{PLUGIN}/status"


def verify_checksum(archive, checksums):
    expected = {}
    for line in checksums.read_text().splitlines():
        parts = line.split()
        if len(parts) == 2:
            expected[parts[1]] = parts[0]
    if expected.get(archive.name) != hashlib.sha256(archive.read_bytes()).hexdigest():
        sys.exit(f"checksum mismatch for {archive.name}")


def extract_cpa(archive, dest):
    if archive.name.endswith(".zip"):
        name = "cli-proxy-api.exe"
        with zipfile.ZipFile(archive) as bundle:
            data = bundle.read(name)
    else:
        name = "cli-proxy-api"
        with tarfile.open(archive) as bundle:
            member = bundle.extractfile(name)
            if member is None:
                sys.exit(f"{name} is not a regular file in {archive.name}")
            data = member.read()
    binary = dest / name
    binary.write_bytes(data)
    binary.chmod(0o755)
    return binary


def install_plugin(plugin_zip, plugins_dir, cpa_archive):
    match = re.fullmatch(
        rf"{PLUGIN}_(\d+(?:\.\d+)+(?:-[a-z0-9.]+)?)_(linux|darwin|windows)_(amd64|arm64)\.zip",
        plugin_zip.name,
    )
    if match is None:
        sys.exit(f"unexpected plugin archive name {plugin_zip.name}")
    version, goos, goarch = match.groups()
    if f"_{goos}_{CPA_ARCH[goarch]}." not in cpa_archive.name:
        sys.exit(f"{cpa_archive.name} is not the CLIProxyAPI build for {goos}/{goarch}")
    library = PLUGIN + EXTENSIONS[goos]
    with zipfile.ZipFile(plugin_zip) as bundle:
        libraries = [
            n
            for n in bundle.namelist()
            if n.lower().endswith((".so", ".dylib", ".dll"))
        ]
        if libraries != [library]:
            sys.exit(
                f"{plugin_zip.name} must contain only {library} at its root, found {libraries}"
            )
        data = bundle.read(library)
    target = plugins_dir / goos / goarch / f"{PLUGIN}-v{version}{EXTENSIONS[goos]}"
    target.parent.mkdir(parents=True)
    target.write_bytes(data)
    target.chmod(0o755)
    return version, f"{goos}/{goarch}"


def free_port():
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


def write_config(path, port, auth_dir, plugins_dir, desktop_policy=False, max_reserve=False):
    lines = [
        "host: 127.0.0.1",
        f"port: {port}",
        f"auth-dir: {json.dumps(auth_dir.as_posix())}",
        "api-keys: [load-smoke-client-key]",
        "remote-management:",
        "  allow-remote: false",
        f"  secret-key: {MANAGEMENT_KEY}",
        "  disable-control-panel: true",
        "plugins:",
        "  enabled: true",
        f"  dir: {json.dumps(plugins_dir.as_posix())}",
        "  configs:",
        f"    {PLUGIN}:",
        "      enabled: true",
        "      priority: 10",
        "      mode: shadow",
    ]
    if desktop_policy:
        scope = hashlib.sha256(b"cli-proxy-api:caller-scope:v1\x00load-smoke-client-key").hexdigest()
        lines.extend([
            "      client_routing:",
            "        enabled: true",
            f"        desktop_caller_scopes: [{scope}]",
        ])
    if max_reserve:
        lines.extend(["      max_weekly_reserve: {enabled: true, remaining_percent: 10, release_before: 24h}"])
    path.write_text("\n".join(lines) + "\n")


def read_status(port, proc, timeout):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}{STATUS_PATH}",
        headers={"Authorization": "Bearer " + MANAGEMENT_KEY},
    )
    deadline = time.monotonic() + timeout
    last = "no response"
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise AssertionError(f"CLIProxyAPI exited with code {proc.returncode}")
        try:
            with opener.open(request, timeout=5) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            last = f"HTTP {error.code}"
        except (urllib.error.URLError, OSError, ValueError) as error:
            last = str(error)
        time.sleep(0.5)
    raise AssertionError(f"plugin status unavailable after {timeout}s: {last}")


def check_status(status, version):
    expected = {
        "version": version,
        "mode": "shadow",
        "selection_policy": "weekly_reset_first",
        "accounts": {},
        "worker_failed": False,
    }
    for key, value in expected.items():
        if status.get(key) != value:
            raise AssertionError(f"status {key} = {status.get(key)!r}, want {value!r}")
    if "list_error" in status:
        raise AssertionError(f"credential list callback failed: {status['list_error']}")


def check_desktop_policy(port):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    base = f"http://127.0.0.1:{port}/v0/management/plugins/{PLUGIN}"

    def get(path):
        request = urllib.request.Request(base + path, headers={"Authorization": "Bearer " + MANAGEMENT_KEY})
        with opener.open(request, timeout=5) as response:
            return json.load(response)

    config = get("/settings")["config"]["client_routing"]
    assert config["enabled"] is True, config
    assert len(config["desktop_caller_scopes"]) == 1, config
    actual = {(item["provider"], item["policy"]) for item in get("/overview")["next_up"]}
    expected = {(provider, policy) for provider in ("claude", "codex") for policy in ("cli_priority_reset_first", "desktop_preferred_plan_affinity")}
    assert actual == expected, actual


def stop(proc):
    if proc.poll() is None:
        proc.terminate()
        try:
            proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


def main():
    args = sys.argv[1:]
    desktop_policy = "--desktop-policy" in args
    max_reserve = "--max-reserve" in args
    args = [arg for arg in args if arg not in ("--desktop-policy", "--max-reserve")]
    if len(args) != 3:
        sys.exit(__doc__)
    cpa_archive, cpa_checksums, plugin_zip = (
        Path(arg).resolve() for arg in args
    )
    verify_checksum(cpa_archive, cpa_checksums)
    with tempfile.TemporaryDirectory(
        prefix="qrr-load-", ignore_cleanup_errors=True
    ) as tmp:
        root = Path(tmp)
        binary = extract_cpa(cpa_archive, root)
        plugins_dir = root / "plugins"
        version, platform = install_plugin(plugin_zip, plugins_dir, cpa_archive)
        auth_dir = root / "auths"
        auth_dir.mkdir()
        port = free_port()
        config = root / "config.yaml"
        write_config(config, port, auth_dir, plugins_dir, desktop_policy, max_reserve)
        log_path = root / "cpa.log"
        with log_path.open("w") as log:
            proc = subprocess.Popen(
                [str(binary), "--config", str(config), "--local-model"],
                cwd=root,
                stdout=log,
                stderr=subprocess.STDOUT,
            )
            try:
                check_status(read_status(port, proc, 120), version)
                # Read again after the first quota refresh has had time to run.
                time.sleep(3)
                check_status(read_status(port, proc, 30), version)
                if desktop_policy:
                    check_desktop_policy(port)
                if max_reserve:
                    request = urllib.request.Request(
                        f"http://127.0.0.1:{port}/v0/management/plugins/{PLUGIN}/settings",
                        headers={"Authorization": "Bearer " + MANAGEMENT_KEY})
                    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=5) as response:
                        reserve = json.load(response)["config"]["max_weekly_reserve"]
                    assert reserve == {"enabled": True, "remaining_percent": 10, "release_before": "24h0m0s"}, reserve
                print(
                    f"PASS: {PLUGIN} {version} loaded and registered in {cpa_archive.name} ({platform}); desktop policy {'enabled' if desktop_policy else 'disabled'}; Max reserve {'enabled' if max_reserve else 'disabled'}"
                )
            except BaseException:
                log.flush()
                print(log_path.read_text(errors="replace"), file=sys.stderr)
                raise
            finally:
                stop(proc)


if __name__ == "__main__":
    main()
