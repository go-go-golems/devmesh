#!/usr/bin/env python3
"""Fail on reachable Go vulnerabilities except reviewed, no-fix Docker Engine advisories."""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

MODULE = "github.com/go-go-golems/devmesh"
ALLOWED = {
    "GO-2026-4883": "Docker Engine legacy-plugin privilege validation; no fixed github.com/docker/docker module release exists.",
    "GO-2026-4887": "Docker Engine AuthZ-plugin bypass; no fixed github.com/docker/docker module release exists.",
}


def decode_stream(data: str) -> list[dict[str, object]]:
    decoder = json.JSONDecoder()
    index = 0
    messages: list[dict[str, object]] = []
    while index < len(data):
        while index < len(data) and data[index].isspace():
            index += 1
        if index == len(data):
            break
        message, index = decoder.raw_decode(data, index)
        if not isinstance(message, dict):
            raise ValueError("govulncheck emitted a non-object JSON message")
        messages.append(message)
    return messages


def is_reachable_here(finding: dict[str, object]) -> bool:
    trace = finding.get("trace")
    if not isinstance(trace, list):
        return False
    return any(isinstance(frame, dict) and frame.get("module") == MODULE for frame in trace)


def main() -> int:
    result = subprocess.run(
        ["govulncheck", "-json", "./..."],
        cwd=Path(__file__).resolve().parents[1],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        sys.stderr.write(result.stderr)
        return result.returncode

    try:
        messages = decode_stream(result.stdout)
    except ValueError as err:
        print(f"invalid govulncheck JSON stream: {err}", file=sys.stderr)
        return 1

    reachable = {
        finding["osv"]
        for message in messages
        if isinstance((finding := message.get("finding")), dict)
        and isinstance(finding.get("osv"), str)
        and is_reachable_here(finding)
    }
    unexpected = sorted(reachable - ALLOWED.keys())
    if unexpected:
        print("unexpected reachable Go vulnerabilities: " + ", ".join(unexpected), file=sys.stderr)
        return 1

    for vuln_id in sorted(reachable):
        print(f"accepted reachable advisory {vuln_id}: {ALLOWED[vuln_id]}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
