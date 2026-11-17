"""Strict scalar edits in the repository's flat, two-space-indented fixture YAML.

This is deliberately not a general YAML parser; fail closed on missing/duplicate
sections or fields. Never rewrite unrelated settings such as orderNotify.
"""
import re


def core_settings(text, sample, max_open, max_idle):
    if not (1 <= sample <= 10000 and 1 <= max_idle <= max_open <= 128):
        raise ValueError("invalid fixture parameters")
    changes = {
        "log": {"sdkConfigSampleEvery": sample},
        "database": {"maxOpenConns": max_open, "maxIdleConns": max_idle},
    }
    sections = {}
    seen = set()
    section = None
    lines = text.splitlines(keepends=True)
    for idx, line in enumerate(lines):
        header = re.fullmatch(r"([A-Za-z][A-Za-z0-9_]*):[ \t]*(?:#[^\r\n]*)?[\r\n]*", line)
        if header:
            section = header.group(1)
            if section in changes:
                sections[section] = sections.get(section, 0) + 1
                if sections[section] != 1:
                    raise RuntimeError(f"Duplicate fixture section {section}")
        elif line.strip() and not line.startswith((" ", "\t", "#")):
            section = None
        if section not in changes:
            continue
        field = re.fullmatch(r"(  ([A-Za-z][A-Za-z0-9_]*):[ \t]*)(\d+)([ \t]*(?:#[^\r\n]*)?[\r\n]*)", line)
        if not field or field.group(2) not in changes[section]:
            continue
        key = field.group(2)
        identity = (section, key)
        if identity in seen:
            raise RuntimeError(f"Duplicate fixture field {section}.{key}")
        seen.add(identity)
        lines[idx] = field.group(1) + str(changes[section][key]) + field.group(4)
    expected = {(section, key) for section, fields in changes.items() for key in fields}
    if seen != expected:
        raise RuntimeError(f"Missing fixture fields: {sorted(expected - seen)}")
    return "".join(lines)
