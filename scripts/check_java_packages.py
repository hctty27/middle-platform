#!/usr/bin/env python3
"""Verify that Java packages follow file paths and module layout."""

from pathlib import Path
import re
import subprocess
import sys

MODULES = ("config-center-server", "config-client-starter", "config-client-demo")
SOURCE_DIRS = ("src/main/java", "src/test/java")
PATTERN = re.compile(r"^package\s+([\w.]+)\s*;", re.MULTILINE)


def main():
    problems = []
    count = 0
    for module in MODULES:
        for source_dir in SOURCE_DIRS:
            root = Path(module, source_dir)
            if not root.exists():
                continue
            for file in root.rglob("*.java"):
                count += 1
                found = PATTERN.search(file.read_text(encoding="utf-8"))
                actual = found.group(1) if found else "<missing>"
                expected = ".".join(file.relative_to(root).parts[:-1])
                if actual != expected:
                    problems.append(f"{file}: package {actual}, expected {expected}")

    server = Path("config-center-server/src/main/java/com/hitall/platform/config")
    starter = Path("config-client-starter/src/main/java/com/hitall/platform/client")
    for area in ("node", "sync", "subscriber", "webhook"):
        if not (server / area).is_dir():
            problems.append(f"Missing business module: {area}")
    for area in ("config", "callback", "sync", "remote", "repository"):
        if not (starter / area).is_dir():
            problems.append(f"Missing starter module: {area}")
    for root in (starter, server / "node", server / "webhook"):
        if list(root.glob("*.java")):
            problems.append(f"Unexpected flat Java files in {root}")


    # HTTP entry-point naming convention: api package and *Api class.
    expected_apis = (
        "config-center-server/src/main/java/com/hitall/platform/config/node/api/AdminNodeApi.java",
        "config-center-server/src/main/java/com/hitall/platform/config/node/api/NodeApi.java",
        "config-center-server/src/main/java/com/hitall/platform/config/node/api/NodeNavigationApi.java",
        "config-center-server/src/main/java/com/hitall/platform/config/sync/api/SyncApi.java",
        "config-center-server/src/main/java/com/hitall/platform/config/subscriber/api/SubscriberApi.java",
        "config-client-starter/src/main/java/com/hitall/platform/client/callback/api/ConfigCallbackApi.java",
        "config-client-demo/src/main/java/com/hitall/platform/demo/api/DemoApi.java",
    )
    for path in expected_apis:
        if not Path(path).is_file():
            problems.append(f"Missing API class: {path}")

    for module in MODULES:
        root = Path(module, "src/main/java")
        for source in root.rglob("*.java"):
            if "controller" in source.parts or source.stem.endswith("Controller"):
                problems.append(f"Legacy controller naming: {source}")
            if "@RestController" in source.read_text(encoding="utf-8"):
                if source.parent.name != "api" or not source.stem.endswith("Api"):
                    problems.append(f"REST endpoint must use api/*Api.java: {source}")

    # Prevent deprecated versioned HTTP endpoints from returning in application code,
    # frontend, Starter, CI tests, or operational documentation.
    legacy_urls = re.compile(r"/api/(?:v[0-9]+|config/v[0-9]+)(?:/|\\b)")
    tracked = subprocess.check_output(["git", "ls-files"], text=True).splitlines()
    scan_extensions = {".java", ".py", ".ts", ".vue", ".md", ".yml", ".yaml", ".conf", ".json"}
    for name in tracked:
        path = Path(name)
        if not path.is_file() or path.suffix not in scan_extensions:
            continue
        contents = path.read_text(encoding="utf-8")
        if legacy_urls.search(contents):
            problems.append(f"Deprecated API prefix in {path}")

    frontend_http = Path("config-center-admin/src/api/http.ts").read_text(encoding="utf-8")
    if "baseURL: '/api/config'" not in frontend_http:
        problems.append("Vue management HTTP client must use /api/config")
    remote_http = Path("config-client-starter/src/main/java/com/hitall/platform/client/remote/ConfigCenterRemoteClient.java").read_text(encoding="utf-8")
    for suffix in ("/sync/revision", "/sync/changes", "/sync/snapshot", "/nodes/batch-get"):
        if '"/api/config' + suffix + '"' not in remote_http:
            problems.append(f"Starter remote client must use /api/config{suffix}")

    if problems:
        print("\n".join(problems), file=sys.stderr)
        return 1
    print(f"PASS: {count} Java files have matching package declarations and paths")
    return 0


if __name__ == "__main__":
    sys.exit(main())
