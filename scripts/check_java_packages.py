#!/usr/bin/env python3
"""Verify that Java packages follow file paths and module layout."""

from pathlib import Path
import re
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

    if problems:
        print("\n".join(problems), file=sys.stderr)
        return 1
    print(f"PASS: {count} Java files have matching package declarations and paths")
    return 0


if __name__ == "__main__":
    sys.exit(main())
