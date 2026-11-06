"""Normalize testdata payloads so golden files stay machine independent.

- directories are rewritten to a fixed path outside any repository
- time related fields are zeroed so output never depends on the clock
- real e-mail addresses are replaced with an example address
"""
import glob
import json
import os

FIXED = "C:/fixture/project"
EMAIL = "user@example.com"

root = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "testdata", "payloads")
for path in sorted(glob.glob(os.path.join(root, "*.json"))):
    with open(path, encoding="utf-8") as fh:
        data = json.load(fh)
    for node in (data, data.get("workspace") or {}):
        if not isinstance(node, dict):
            continue
        for key in ("cwd", "current_dir", "project_dir"):
            if key in node:
                node[key] = FIXED

    # Zero out time-dependent fields.
    def strip_times(node):
        if isinstance(node, dict):
            for key in ("reset_in_seconds", "reset_time", "resets_at"):
                if key in node:
                    node[key] = 0
            for value in node.values():
                strip_times(value)
        elif isinstance(node, list):
            for value in node:
                strip_times(value)

    strip_times(data)

    # Scrub real e-mail addresses.
    def scrub(node):
        if isinstance(node, dict):
            for key, value in list(node.items()):
                if key == "email" and isinstance(value, str):
                    node[key] = EMAIL
                else:
                    scrub(value)
        elif isinstance(node, list):
            for value in node:
                scrub(value)

    scrub(data)
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(data, fh, ensure_ascii=False, separators=(",", ":"))
        fh.write("\n")
    print("rewrote", os.path.basename(path))
