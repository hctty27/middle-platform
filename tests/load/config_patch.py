"""Strict fixture YAML edits for HTTP access logging and the SDK Core DB pool.

Only the disposable Runner configuration is edited. Reject missing or duplicate
settings; never rewrite unrelated sections such as orderNotify.
"""
import re


def core_settings(text, access_enabled, max_open):
    if type(access_enabled) is not bool or type(max_open) is not int or not (1 <= max_open <= 128):
        raise ValueError("invalid fixture parameters")
    changes = {
        "log": {"accessEnabled": str(access_enabled).lower()},
        "database": {"maxOpenConns": str(max_open)},
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
        field = re.fullmatch(
            r"(  ([A-Za-z][A-Za-z0-9_]*):[ \t]*)(true|false|[0-9]+)([ \t]*(?:#[^\r\n]*)?[\r\n]*)",
            line,
        )
        if not field or field.group(2) not in changes[section]:
            continue
        key = field.group(2)
        identity = (section, key)
        if identity in seen:
            raise RuntimeError(f"Duplicate fixture field {section}.{key}")
        seen.add(identity)
        lines[idx] = field.group(1) + changes[section][key] + field.group(4)
    expected = {(section, key) for section, fields in changes.items() for key in fields}
    if seen != expected:
        raise RuntimeError(f"Missing fixture fields: {sorted(expected - seen)}")
    return "".join(lines)
