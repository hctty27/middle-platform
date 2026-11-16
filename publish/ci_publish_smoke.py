"""Local publish smoke tests. SSH, SCP, and package builds are mocked; no network access."""
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest


SCRIPTS = Path(__file__).resolve().parent
ARCHIVE = "middle-platform-smoke-linux-amd64.tar.gz"
BINARIES = ("config-center", "config-center-access", "sdk-core", "sdk-service")


def find_bash():
    # Windows' bash.exe may be a WSL launcher rather than the installed Git Bash.
    git = shutil.which("git")
    if os.name == "nt" and git:
        for parent in Path(git).resolve().parents:
            candidate = parent / "usr/bin/bash.exe"
            if candidate.is_file():
                return str(candidate)
    return shutil.which("bash")


BASH = find_bash()


def bash_path(path):
    if os.name == "nt":
        return subprocess.check_output(
            [BASH, "-c", 'cygpath -u "$1"', "test", str(path)], text=True
        ).strip()
    return str(path)


@unittest.skipUnless(BASH, "Bash is required")
class RemotePublishTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="publish-smoke-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "target"
        self.stage = self.root / (".publish-" + "a" * 32)
        self.stage.mkdir(parents=True)
        self.archive = self.stage / ARCHIVE
        self.events = self.root / "events.log"
        self.nginx_control = self.root / "ops/nginx/upstream-control.sh"
        self.nginx_control.parent.mkdir(parents=True, exist_ok=True)
        self.nginx_control.write_bytes((
            b"#!/usr/bin/env bash\n"
            b"printf '%s %s %s\n' \"$1\" \"$2\" \"$3\" >> \"$TEST_EVENTS\"\n"
        ))
        self.nginx_control.chmod(0o755)

    def make_package(self, frontend=True, extra=None, omit=None, only=None):
        files = {f"bin/{name}": b"#!/bin/sh\nexit 0\n" for name in BINARIES
                 if name != omit and (only is None or name == only)}
        files["sql/full/config_center.sql"] = b"DO NOT EXECUTE"
        if frontend:
            files.update({"admin/index.html": b"new-index", "admin/config.js": b"default-config"})
        with tarfile.open(self.archive, "w:gz") as archive:
            for name, content in files.items():
                entry = tarfile.TarInfo(name)
                entry.size = len(content)
                entry.mode = 0o755 if name.startswith("bin/") else 0o644
                archive.addfile(entry, io.BytesIO(content))
            if extra:
                archive.addfile(extra)

    def run_publish(self, digest=None, machine="x86_64", service=None,
                    restart=False, restart_fail=False, plan=None):
        digest = digest or hashlib.sha256(self.archive.read_bytes()).hexdigest()
        if plan is None:
            plan = ",".join(f"sdk-core-{i}:{8210 + i}" for i in range(1, 5)) if restart else "-"
        extra_args = ["1" if restart else "0", service, plan] if service else []
        commands = """
uname() { printf '%s\\n' "$TEST_MACHINE"; }
flock() { return 0; }
export -f uname flock
if [[ "$TEST_RESTART" == 1 ]]; then
    supervisorctl() {
        case "$1" in
            status) printf '%s RUNNING pid 100\\n' "$2" ;;
            restart)
                printf 'restart %s\\n' "$2" >> "$TEST_EVENTS"
                [[ "$TEST_RESTART_FAIL" == 0 ]]
                ;;
        esac
    }
    curl() { printf 'ready\\n' >> "$TEST_EVENTS"; return 0; }
    export -f supervisorctl curl
fi
exec bash "$@"
"""
        return subprocess.run(
            [BASH, "-c", commands, "test", bash_path(SCRIPTS / "publish-remote.sh"),
             bash_path(self.root), bash_path(self.archive), digest, *extra_args],
            env={**os.environ, "TEST_MACHINE": machine,
                 "TEST_RESTART": "1" if restart else "0",
                 "TEST_RESTART_FAIL": "1" if restart_fail else "0",
                 "TEST_EVENTS": bash_path(self.events)},
            capture_output=True, text=True,
        )

    def seed(self, relative, content):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def test_full_update_preserves_configuration_and_old_assets(self):
        self.make_package()
        protected = [self.seed(path, "keep") for path in
                     ("config/prod.yaml", "logs/service.log", "admin/config.js", "admin/assets/old.js",
                      "ops/README.md", "ops/monit/check-http-live.sh", "run/nginx/sdk-core-upstream.conf")]
        helper_before = self.nginx_control.read_bytes()
        self.seed("bin/sdk-core", "old-binary")
        result = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / "bin/sdk-core").read_bytes(), b"#!/bin/sh\nexit 0\n")
        self.assertEqual((self.root / "admin/index.html").read_bytes(), b"new-index")
        for path in protected:
            self.assertEqual(path.read_text(encoding="utf-8"), "keep")
        self.assertTrue((self.root / "packages" / ARCHIVE).is_file())
        self.assertEqual(self.nginx_control.read_bytes(), helper_before)
        self.assertFalse(self.stage.exists())
        self.assertIn("SQL was NOT executed", result.stdout)

    def test_backend_only_does_not_touch_frontend(self):
        self.make_package(frontend=False)
        index = self.seed("admin/index.html", "old-index")
        result = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(index.read_text(encoding="utf-8"), "old-index")

    def test_first_frontend_install_includes_config(self):
        self.make_package()
        result = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "admin/config.js").read_bytes(), b"default-config")

    def assert_rejected(self, **kwargs):
        old = self.seed("bin/sdk-core", "old-binary")
        result = self.run_publish(**kwargs)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(old.read_text(encoding="utf-8"), "old-binary")
        self.assertTrue(self.archive.exists())
        return result

    def test_partial_sdk_core_does_not_replace_other_binaries(self):
        self.make_package(frontend=False, only="sdk-core")
        other = self.seed("bin/config-center", "original")
        result = self.run_publish(service="sdk-core")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(other.read_text(encoding="utf-8"), "original")
        self.assertTrue((self.root / "bin/sdk-core").is_file())

    def test_mismatched_service_selector_does_not_change_files(self):
        self.make_package(frontend=False)
        self.assert_rejected(service="sdk-core")

    def test_admin_only_archive_does_not_require_binaries(self):
        self.nginx_control.unlink()
        self.make_package(only="admin")
        result = self.run_publish(service="admin")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / "admin/index.html").read_bytes(), b"new-index")
        self.assertFalse((self.root / "bin/sdk-core").exists())

    def test_core_instances_restart_sequentially_and_pass_health(self):
        self.make_package(frontend=False, only="sdk-core")
        helper_before = self.nginx_control.read_bytes()
        result = self.run_publish(service="sdk-core", restart=True)
        self.assertEqual(self.nginx_control.read_bytes(), helper_before)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Healthy: sdk-core-1", result.stdout)
        for i in range(1, 5):
            self.assertIn(f"Healthy: sdk-core-{i} (port {8210 + i})", result.stdout)
        self.assertIn("Draining sdk-core-1 from Nginx", result.stdout)
        self.assertLess(result.stdout.index("Draining sdk-core-1 from Nginx"),
                        result.stdout.index("Restarting sdk-core-1"))
        self.assertLess(result.stdout.index("Healthy: sdk-core-1"),
                        result.stdout.index("Restored sdk-core-1 to Nginx traffic"))
        events = self.events.read_text(encoding="utf-8").splitlines()
        self.assertEqual(sum(line.startswith("validate sdk-core") for line in events), 4)
        self.assertLess(events.index("drain sdk-core 8211"), events.index("restart sdk-core:sdk-core-1"))
        self.assertLess(events.index("restart sdk-core:sdk-core-1"), events.index("enable sdk-core 8211"))

    def test_config_center_instances_use_configured_ports(self):
        self.make_package(frontend=False, only="config-center")
        result = self.run_publish(
            service="config-center", restart=True,
            plan="config-center-1:8201,config-center-2:8202")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Healthy: config-center-1 (port 8201)", result.stdout)
        self.assertIn("Healthy: config-center-2 (port 8202)", result.stdout)
        self.assertNotIn("Restarting config-center-access", result.stdout)
        self.assertLess(result.stdout.index("Restarting config-center-1"),
                        result.stdout.index("Restarting config-center-2"))

    def test_config_center_access_not_matched_to_config_center(self):
        self.make_package(frontend=False, only="config-center")
        self.assert_rejected(service="config-center", restart=True,
                             plan="config-center-access:8291")

    def test_invalid_restart_plan_is_rejected_before_replacing_files(self):
        self.make_package(frontend=False, only="sdk-core")
        for plan in ("-", "sdk-core-1:65536", "sdk-core-1:8211,sdk-core-1:8211",
                     "sdk-core-a:8211", "config-center-1:8201",
                     "sdk-core-1:8211;whoami", "sdk-core-1:8211"):
            with self.subTest(plan=plan):
                self.assert_rejected(service="sdk-core", restart=True, plan=plan)

    def test_full_publish_restarts_only_configured_instances(self):
        self.make_package(frontend=False)
        full_plan = ",".join(
            [f"config-center-{i}:{8200 + i}" for i in range(1, 3)] +
            [f"sdk-core-{i}:{8210 + i}" for i in range(1, 5)] +
            [f"sdk-service-{i}:{8220 + i}" for i in range(1, 5)]
        )
        result = self.run_publish(service="all", restart=True, plan=full_plan)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Healthy: config-center-1 (port 8201)", result.stdout)
        self.assertIn("Healthy: sdk-core-4 (port 8214)", result.stdout)
        self.assertIn("Healthy: sdk-service-4 (port 8224)", result.stdout)
        self.assertNotIn("Restarting config-center-access", result.stdout)

    def test_restart_failure_reports_error_without_rollback(self):
        self.make_package(frontend=False, only="sdk-core")
        result = self.run_publish(service="sdk-core", restart=True, restart_fail=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.root / "bin/sdk-core").is_file())
        self.assertIn("Restarting sdk-core-1", result.stdout)
        events = self.events.read_text(encoding="utf-8").splitlines()
        self.assertIn("drain sdk-core 8211", events)
        self.assertNotIn("enable sdk-core 8211", events)

    def test_missing_installed_nginx_helper_rejects_rolling_publish(self):
        self.nginx_control.unlink()
        self.make_package(frontend=False, only="sdk-core")
        result = self.assert_rejected(service="sdk-core", restart=True)
        self.assertIn("Missing or unsafe Nginx upstream control helper", result.stderr)

    def test_skip_restart_does_not_require_nginx_helper(self):
        self.nginx_control.unlink()
        self.make_package(frontend=False, only="sdk-core")
        result = self.run_publish(service="sdk-core")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(self.nginx_control.exists())

    def test_access_restart_does_not_require_nginx_helper(self):
        self.nginx_control.unlink()
        self.make_package(frontend=False, only="config-center-access")
        result = self.run_publish(service="config-center-access", restart=True,
                                  plan="config-center-access:8291")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Healthy: config-center-access", result.stdout)

    def test_rejects_ops_and_runtime_entries(self):
        for name in ("ops/nginx/upstream-control.sh", "run/nginx/sdk-core-upstream.conf"):
            with self.subTest(name=name):
                self.make_package(extra=tarfile.TarInfo(name))
                result = self.assert_rejected()
                self.assertIn("Unexpected archive entry:", result.stderr)

    def test_bad_checksum(self):
        self.make_package()
        result = self.assert_rejected(digest="0" * 64)
        self.assertIn("SHA-256 mismatch", result.stderr)

    def test_wrong_architecture(self):
        self.make_package()
        self.assert_rejected(machine="aarch64")

    def test_missing_binary(self):
        self.make_package(omit="sdk-service")
        self.assert_rejected()

    def test_path_traversal(self):
        self.make_package(extra=tarfile.TarInfo("bin/../../escaped"))
        self.assert_rejected()
        self.assertFalse((self.root / "escaped").exists())

    def test_archive_symlink(self):
        entry = tarfile.TarInfo("admin/link")
        entry.type = tarfile.SYMTYPE
        entry.linkname = "../../config"
        self.make_package(extra=entry)
        self.assert_rejected()

    def test_unexpected_config_entry(self):
        self.make_package(extra=tarfile.TarInfo("config/prod.yaml"))
        self.assert_rejected()


@unittest.skipUnless(shutil.which("powershell.exe") or shutil.which("pwsh"), "PowerShell is required")
class LocalPublishTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="publish local's smoke-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        scripts = self.root / "publish"
        scripts.mkdir()
        shutil.copyfile(SCRIPTS / "publish.ps1", scripts / "publish.ps1")
        shutil.copyfile(SCRIPTS / "publish-remote.sh", scripts / "publish-remote.sh")
        shutil.copyfile(SCRIPTS / "package-local.ps1", scripts / "package-local.ps1")
        package_scripts = self.root / "scripts"
        package_scripts.mkdir()
        (package_scripts / "package-archive.go").write_bytes(b"package main\nfunc main() {}\n")
        (package_scripts / "package.ps1").write_text('''param($Arch, $Version, [switch]$SkipFrontend, $Service = 'all', [switch]$Fast)
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$prefix = [IO.File]::ReadAllBytes($PSCommandPath)
if ($prefix[0] -ne 239 -or $prefix[1] -ne 187 -or $prefix[2] -ne 191) { throw 'Missing UTF-8 BOM' }
$helper = [IO.File]::ReadAllBytes((Join-Path $PSScriptRoot 'package-archive.go'))
if ($helper -contains [byte]0) { throw 'NUL in temporary helper' }
Write-Host 'UTF8-CHECK: 中文正常'
if ($global:TestMode -eq 'build-fail') { throw 'Simulated build failure' }
$dir = Join-Path $repoRoot 'dist'
New-Item -ItemType Directory -Force $dir | Out-Null
[IO.File]::WriteAllText((Join-Path $dir "middle-platform-$Version-linux-$Arch.tar.gz"), 'fake-package')
''', encoding="utf-8-sig")
        self.originals = {path: path.read_bytes() for path in package_scripts.iterdir()}
        self.wrapper = self.root / "test.ps1"
        self.wrapper.write_text('''param($Mode)
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$global:TestMode = $Mode
$global:Calls = New-Object 'System.Collections.Generic.List[string]'
function global:Note-AskPass {
    if (-not $env:SSH_ASKPASS) { return }
    $global:Calls.Add('askpass|set')
    if ($env:SSH_ASKPASS_REQUIRE -ne 'force') { throw 'askpass was not forced' }
    if ($global:TestMode -ne 'password') { return }
    $fed = @(& $env:SSH_ASKPASS 'password:' 2>$null)
    if (($fed -join '') -cne 's3cret-value') { throw 'askpass mismatch' }
    $global:Calls.Add('askpass|ok')
}
function global:ssh {
    Note-AskPass
    $global:Calls.Add('ssh|' + ($args -join '|'))
    $global:LASTEXITCODE = 0
    if ($global:TestMode -eq 'extract-fail' -and $global:Calls.Count -eq 3) { $global:LASTEXITCODE = 7 }
}
function global:scp {
    Note-AskPass
    $global:Calls.Add('scp|' + ($args -join '|'))
    $global:LASTEXITCODE = 0
    if ($global:TestMode -eq 'upload-fail') { $global:LASTEXITCODE = 8 }
}
$code = 0
$publishArgs = @{ Version = 'smoke'; SkipFrontend = $true; SkipRestart = $true }
if ($Mode -eq 'restart-plan') {
    $publishArgs.SkipRestart = $false
    $publishArgs.Service = 'config-center'
}
$overrideFile = Join-Path $PSScriptRoot 'cli-override.txt'
if (Test-Path -LiteralPath $overrideFile) {
    foreach ($line in @(Get-Content -LiteralPath $overrideFile)) {
        $pair = $line.Split('=', 2)
        $publishArgs[$pair[0]] = $pair[1]
    }
}
try { & (Join-Path $PSScriptRoot 'publish/publish.ps1') @publishArgs }
catch { Write-Output $_; $code = 1 }
$global:Calls | Set-Content (Join-Path $PSScriptRoot 'calls.txt')
exit $code
''', encoding="utf-8")

    def invoke(self, mode):
        shell = shutil.which("powershell.exe") or shutil.which("pwsh")
        result = subprocess.run([shell, "-NoProfile", "-NonInteractive", "-File", str(self.wrapper), mode],
                                encoding="utf-8", errors="replace", capture_output=True)
        for path, original in self.originals.items():
            if mode not in ("nul-source", "changed-layout"):
                self.assertEqual(path.read_bytes(), original)
        runtime_path = self.root / "runtime-path.txt"
        if runtime_path.exists():
            self.assertFalse(Path(runtime_path.read_text(encoding="utf-8")).exists(), "Temporary build files were not cleaned up")
        calls_file = self.root / "calls.txt"
        calls = calls_file.read_text(encoding="utf-8-sig").splitlines() if calls_file.exists() else []
        return result, calls

    def write_config(self, host="10.0.0.8", port=2222, user="deploy",
                     directory="/srv/app", password="", services=None):
        payload = {
            "host": host,
            "port": port,
            "user": user,
            "remoteDirectory": directory,
            "password": password,
        }
        if services is not None:
            payload["services"] = services
        (self.root / "publish" / "publish.config.json").write_text(
            json.dumps(payload), encoding="utf-8"
        )

    def test_local_services_selects_config_center_instances(self):
        self.write_config()
        result, calls = self.invoke("restart-plan")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        remote_calls = [line for line in calls if line.startswith("ssh|")]
        self.assertIn("config-center-1:8201,config-center-2:8202", remote_calls[-1])
        self.assertNotIn("config-center-access:8291", remote_calls[-1])
        self.assertNotIn("sdk-core-3:8213", remote_calls[-1])

    def test_empty_services_uses_fixed_topology(self):
        self.write_config(services={})
        result, calls = self.invoke("restart-plan")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("config-center-1:8201,config-center-2:8202", calls[-1])

    def test_optional_access_and_legacy_configuration(self):
        self.write_config(services={"config-center-1": 8201, "config-center-access": 8291})
        (self.root / "cli-override.txt").write_text("Service=all\n", encoding="ascii")
        result, calls = self.invoke("restart-plan")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        remote = [line for line in calls if line.startswith("ssh|")][-1]
        self.assertIn("sdk-core-4:8214", remote)
        self.assertIn("sdk-service-4:8224", remote)
        self.assertIn("config-center-access:8291", remote)

    def test_access_requires_optional_configuration(self):
        self.write_config()
        (self.root / "cli-override.txt").write_text("Service=config-center-access\n", encoding="ascii")
        result, calls = self.invoke("restart-plan")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

    def test_invalid_local_services_fails_before_upload(self):
        for services in (
            {"config-center-access": 8292},
            {"config-center-1": "8201"},
            {"config-center-1": 65536},
            {"other-project": 9000},
            [],
        ):
            with self.subTest(services=services):
                self.write_config(services=services)
                result, calls = self.invoke("restart-plan")
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, [])

    def test_missing_config_does_not_connect(self):
        result, calls = self.invoke("success")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])
        self.assertIn("publish.config.example.json", result.stdout + result.stderr)

    def test_config_supplies_target_and_blank_password_stays_interactive(self):
        self.write_config(password="")
        result, calls = self.invoke("success")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("ssh|-p|2222|deploy@10.0.0.8", calls[0])
        self.assertIn("scp|-P|2222|", calls[1])
        self.assertIn("/srv/app/.publish-", calls[2])
        self.assertNotIn("askpass|set", calls)
        self.assertNotIn("PubkeyAuthentication=no", calls[0])
        self.assertIn("已跳过 Supervisor 重启", result.stdout)
        self.assertIn("UTF8-CHECK: 中文正常", result.stdout)

    def test_cli_host_overrides_config(self):
        self.write_config()
        (self.root / "cli-override.txt").write_text("ServerHost=10.9.8.7\n", encoding="ascii")
        result, calls = self.invoke("success")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("ssh|-p|2222|deploy@10.9.8.7", calls[0])
        self.assertIn("/srv/app/.publish-", calls[2])

    @unittest.skipUnless(os.name == 'nt', 'Windows-only .NET Framework password helper')
    def test_configured_password_is_given_to_ssh_without_being_logged(self):
        self.write_config(password="s3cret-value")
        result, calls = self.invoke("password")
        combined = result.stdout + result.stderr + "\n" + "\n".join(calls)
        self.assertEqual(result.returncode, 0, combined)
        self.assertNotIn("s3cret-value", combined)
        self.assertEqual(calls.count("askpass|ok"), 3)
        ssh_lines = [line for line in calls if line.startswith("ssh|")]
        self.assertIn("|-p|2222|deploy@10.0.0.8|", ssh_lines[0])
        self.assertIn("PubkeyAuthentication=no", ssh_lines[0])
        self.assertIn("/srv/app/.publish-", ssh_lines[1])
        self.assertIn("|-P|2222|", next(line for line in calls if line.startswith("scp|")))

    def test_invalid_config_does_not_connect(self):
        (self.root / "publish" / "publish.config.json").write_text("{", encoding="utf-8")
        result, calls = self.invoke("success")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])
        self.assertIn("Invalid JSON", result.stdout + result.stderr)

    def test_multiline_password_does_not_connect(self):
        self.write_config(password="one\ntwo")
        result, calls = self.invoke("success")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])
        self.assertIn("single line", result.stdout + result.stderr)

    def test_rejects_nul_source_without_connecting(self):
        self.write_config()
        (self.root / "scripts/package-archive.go").write_bytes(b"package main\x00")
        result, calls = self.invoke("nul-source")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])
        self.assertIn("contains NUL bytes", result.stdout)

    def test_rejects_changed_original_layout_without_connecting(self):
        self.write_config()
        path = self.root / "scripts/package.ps1"
        path.write_text("param($Arch, $Version, [switch]$SkipFrontend)\n", encoding="utf-8")
        result, calls = self.invoke("changed-layout")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])
        self.assertIn("Build did not produce the expected archive", result.stdout)

    def test_build_failure_does_not_connect(self):
        self.write_config()
        result, calls = self.invoke("build-fail")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

    def test_upload_failure_does_not_extract(self):
        self.write_config()
        result, calls = self.invoke("upload-fail")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(calls), 2)
        self.assertNotIn("Published archive:", result.stdout)

    def test_extraction_failure_is_reported(self):
        self.write_config()
        result, calls = self.invoke("extract-fail")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(calls), 3)
        self.assertNotIn("Published archive:", result.stdout)


class DeploymentTopologyTests(unittest.TestCase):
    def test_supervisor_monit_nginx_and_publish_share_the_2_4_4_topology(self):
        root = SCRIPTS.parent
        expected = {}
        for index in range(1, 3):
            expected[f"config-center-{index}"] = ("config-center", 8200 + index)
        for index in range(1, 5):
            expected[f"sdk-core-{index}"] = ("sdk-core", 8210 + index)
            expected[f"sdk-service-{index}"] = ("sdk-service", 8220 + index)

        supervisor = (root / "ops/supervisor/middle-platform.conf").read_text(encoding="utf-8")
        monit = (root / "ops/monit/middle-platform.conf").read_text(encoding="utf-8")
        config = json.loads((root / "publish/publish.config.example.json").read_text(encoding="utf-8"))
        self.assertNotIn("services", config)
        publish_script = (root / "publish/publish.ps1").read_text(encoding="utf-8-sig")
        for name, (module, port) in expected.items():
            self.assertIn(f"[program:{name}]", supervisor)
            self.assertIn(f"127.0.0.1:{port}", supervisor)
            self.assertIn(f"check-http-live.sh {name} {port}", monit)
            self.assertIn(f"restart-supervisor-program.sh {name}", monit)
            self.assertIn(f"Name = '{name}'; Module = '{module}'; Port = {port}", publish_script)

        self.assertFalse((root / "ops/nginx/runtime").exists())
        self.assertEqual(len(expected), 10)
        self.assertEqual(config["remoteDirectory"], "/data/middle-platform")
        self.assertEqual(supervisor.count("user=root"), 10)
        self.assertEqual(supervisor.count('GOMEMLIMIT="128MiB"'), 10)
        for group in ("config-center", "sdk-core", "sdk-service"):
            self.assertIn(f"[group:{group}]", supervisor)
        check_script = (root / "ops/monit/check-http-live.sh").read_text(encoding="utf-8")
        restart_script = (root / "ops/monit/restart-supervisor-program.sh").read_text(encoding="utf-8")
        self.assertIn('supervisor_process="sdk-core:$program"', check_script)
        self.assertIn('supervisor_process="sdk-core:$1"', restart_script)
        nginx_config = (root / "ops/nginx/middle-platform.conf").read_text(encoding="utf-8")
        self.assertIn("upstream config_center", nginx_config)
        self.assertIn("upstream sdk_core", nginx_config)
        self.assertIn("upstream sdk_service", nginx_config)
        self.assertIn("include /data/middle-platform/run/nginx/config-center-upstream.conf;", nginx_config)
        self.assertIn("include /data/middle-platform/run/nginx/sdk-core-upstream.conf;", nginx_config)
        self.assertIn("include /data/middle-platform/run/nginx/sdk-service-upstream.conf;", nginx_config)
        self.assertIn("location ^~ /api/config/", nginx_config)
        self.assertIn("location ^~ /api/core/", nginx_config)
        self.assertIn("location ^~ /api/service/", nginx_config)
        self.assertIn("proxy_next_upstream error timeout invalid_header http_502 http_504;", nginx_config)
        self.assertFalse((root / "ops/nginx/middle-platform-upstreams.conf").exists())
        self.assertFalse((root / "ops/nginx/middle-platform-locations.conf").exists())
        self.assertFalse((root / "ops/nginx/proxy-common.conf").exists())
        self.assertFalse((root / "ops/nginx/install-runtime.sh").exists())
        self.assertIn(".publish.lock", (root / "ops/monit/check-http-live.sh").read_text(encoding="utf-8"))
        self.assertIn(".publish.lock", (root / "ops/monit/restart-supervisor-program.sh").read_text(encoding="utf-8"))
        self.assertIn(".publish.lock", (root / "publish/publish-remote.sh").read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
