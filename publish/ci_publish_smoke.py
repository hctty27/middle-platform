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

    def make_package(self, frontend=True, extra=None, omit=None):
        files = {f"bin/{name}": b"#!/bin/sh\nexit 0\n" for name in BINARIES if name != omit}
        files["sql/config-center/initialize.sql"] = b"DO NOT EXECUTE"
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

    def run_publish(self, digest=None, machine="x86_64"):
        digest = digest or hashlib.sha256(self.archive.read_bytes()).hexdigest()
        return subprocess.run(
            [BASH, "-c", 'uname() { printf "%s\\n" "$TEST_MACHINE"; }; export -f uname; exec bash "$@"',
             "test", bash_path(SCRIPTS / "publish-remote.sh"), bash_path(self.root),
             bash_path(self.archive), digest],
            env={**os.environ, "TEST_MACHINE": machine}, capture_output=True, text=True,
        )

    def seed(self, relative, content):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def test_full_update_preserves_configuration_and_old_assets(self):
        self.make_package()
        protected = [self.seed(path, "keep") for path in
                     ("config/prod.yaml", "logs/service.log", "admin/config.js", "admin/assets/old.js")]
        self.seed("bin/sdk-core", "old-binary")
        result = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.root / "bin/sdk-core").read_bytes(), b"#!/bin/sh\nexit 0\n")
        self.assertEqual((self.root / "admin/index.html").read_bytes(), b"new-index")
        for path in protected:
            self.assertEqual(path.read_text(), "keep")
        self.assertTrue((self.root / "packages" / ARCHIVE).is_file())
        self.assertFalse(self.stage.exists())
        self.assertIn("SQL was NOT executed", result.stdout)

    def test_backend_only_does_not_touch_frontend(self):
        self.make_package(frontend=False)
        index = self.seed("admin/index.html", "old-index")
        result = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(index.read_text(), "old-index")

    def test_first_frontend_install_includes_config(self):
        self.make_package()
        result = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "admin/config.js").read_bytes(), b"default-config")

    def assert_rejected(self, **kwargs):
        old = self.seed("bin/sdk-core", "old-binary")
        result = self.run_publish(**kwargs)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertEqual(old.read_text(), "old-binary")
        self.assertTrue(self.archive.exists())
        return result

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
        (package_scripts / "package.ps1").write_text('''param($Arch, $Version, [switch]$SkipFrontend)
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
[IO.File]::WriteAllText((Join-Path $repoRoot 'runtime-path.txt'), $PSScriptRoot)
$prefix = [IO.File]::ReadAllBytes($PSCommandPath)
if ($prefix[0] -ne 239 -or $prefix[1] -ne 187 -or $prefix[2] -ne 191) { throw 'Missing UTF-8 BOM' }
$helper = [IO.File]::ReadAllBytes((Join-Path $PSScriptRoot 'package-archive.go'))
if ($helper -contains [byte]0) { throw 'NUL in temporary helper' }
Write-Host 'UTF8-CHECK: 中文正常'
if ($global:TestMode -eq 'build-fail') { throw 'Simulated build failure' }
$dir = Join-Path $repoRoot 'dist'
New-Item -ItemType Directory -Force $dir | Out-Null
[IO.File]::WriteAllText((Join-Path $dir "middle-platform-$Version-linux-$Arch.tar.gz"), 'fake-package')
''', encoding="utf-8")
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
$publishArgs = @{ Version = 'smoke'; SkipFrontend = $true }
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
            self.assertFalse(Path(runtime_path.read_text()).exists(), "Temporary build files were not cleaned up")
        calls_file = self.root / "calls.txt"
        calls = calls_file.read_text(encoding="utf-8-sig").splitlines() if calls_file.exists() else []
        return result, calls

    def write_config(self, host="10.0.0.8", port=2222, user="deploy", directory="/srv/app", password=""):
        payload = {
            "host": host,
            "port": port,
            "user": user,
            "remoteDirectory": directory,
            "password": password,
        }
        (self.root / "publish" / "publish.config.json").write_text(
            json.dumps(payload), encoding="utf-8"
        )

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
        self.assertIn("No services were restarted", result.stdout)
        self.assertIn("UTF8-CHECK: 中文正常", result.stdout)

    def test_cli_host_overrides_config(self):
        self.write_config()
        (self.root / "cli-override.txt").write_text("ServerHost=10.9.8.7\n", encoding="ascii")
        result, calls = self.invoke("success")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("ssh|-p|2222|deploy@10.9.8.7", calls[0])
        self.assertIn("/srv/app/.publish-", calls[2])

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
        self.assertIn("layout has changed", result.stdout)

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


if __name__ == "__main__":
    unittest.main(verbosity=2)
