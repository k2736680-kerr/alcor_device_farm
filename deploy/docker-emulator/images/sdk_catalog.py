#!/usr/bin/env python3
"""Parse only sdkmanager's stable Available Packages section."""

import json
import re
import sys
from pathlib import Path


PACKAGE = re.compile(
    r"^system-images;android-(33|34|35|36);(google_apis|google_play);(x86_64)\s*\|\s*([^|\s]+)"
)


def available_entries(text: str) -> list[dict]:
    entries: list[dict] = []
    seen: set[str] = set()
    in_available = False
    for raw in text.splitlines():
        line = raw.strip()
        if line == "Available Packages:":
            in_available = True
            continue
        if in_available and line.endswith("Packages:"):
            break
        if not in_available:
            continue
        match = PACKAGE.match(line)
        if not match:
            continue
        api, image_type, abi, revision = match.groups()
        package_name = f"system-images;android-{api};{image_type};{abi}"
        if package_name in seen:
            continue
        seen.add(package_name)
        entries.append(
            {
                "package_name": package_name,
                "api_level": int(api),
                "image_type": image_type,
                "abi": abi,
                "revision": revision,
            }
        )
    return sorted(entries, key=lambda item: (item["api_level"], item["image_type"], item["abi"]))


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: sdk_catalog.py <sdkmanager-list-output>", file=sys.stderr)
        return 64
    entries = available_entries(Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace"))
    if not entries:
        print("stable Android System Image catalog is empty", file=sys.stderr)
        return 1
    print("DEVICE_FARM_IMAGE_RESULT=" + json.dumps({"entries": entries}, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
