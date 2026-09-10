#!/usr/bin/env python3
"""Index End4 wallpaper media without relying on filename suffixes alone."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
from urllib.parse import quote


FORMATS = {
    ".jpg": "static",
    ".jpeg": "static",
    ".png": "static",
    ".webp": "static",
    ".gif": "animated",
    ".mp4": "video",
    ".webm": "video",
    ".mkv": "video",
    ".mov": "video",
    ".avi": "video",
}
SORT_MODES = ("custom", "time", "time_rev", "name", "name_rev", "size", "size_rev")
NATURAL_PARTS = re.compile(r"(\d+)")


def webp_kind(path: Path) -> str:
    """Classify WebP animation from RIFF/VP8X/ANMF content."""

    try:
        data = path.read_bytes()
    except OSError:
        return "unknown"

    if len(data) < 12 or data[:4] != b"RIFF" or data[8:12] != b"WEBP":
        return "unknown"

    offset = 12
    while offset + 8 <= len(data):
        chunk = data[offset : offset + 4]
        size = int.from_bytes(data[offset + 4 : offset + 8], "little")
        payload_start = offset + 8
        payload_end = min(len(data), payload_start + size)
        if chunk == b"ANIM" or chunk == b"ANMF":
            return "animated_webp"
        if chunk == b"VP8X" and payload_end - payload_start >= 10:
            # The animation flag is bit 1 in the VP8X feature byte.
            if data[payload_start] & 0x02:
                return "animated_webp"
        offset = payload_start + size + (size & 1)

    return "static"


def media_kind(path: Path) -> str:
    suffix = path.suffix.casefold()
    if suffix == ".webp":
        return webp_kind(path)
    return FORMATS.get(suffix, "unknown")


def thumbnail_path(path: Path) -> str:
    cache_home = Path(os.environ.get("XDG_CACHE_HOME", Path.home() / ".cache"))
    encoded = "/".join(quote(part, safe="") for part in str(path).split("/"))
    digest = hashlib.md5(f"file://{encoded}".encode(), usedforsecurity=False).hexdigest()
    return str(cache_home / "thumbnails" / "x-large" / f"{digest}.png")


def item(path: Path) -> dict[str, object]:
    kind = "directory" if path.is_dir() else media_kind(path)
    try:
        stat = path.stat()
        size = stat.st_size
        modified = stat.st_mtime
    except OSError:
        size = 0
        modified = 0
    return {
        "fileName": path.name,
        "filePath": str(path),
        "fileUrl": path.as_uri(),
        "fileURL": path.as_uri(),
        "fileIsDir": path.is_dir(),
        "fileSize": size,
        "fileModified": modified,
        "mediaKind": kind,
        "thumbnailPath": "" if path.is_dir() else thumbnail_path(path),
        "thumbnailSourcePath": str(path),
    }


def matches_query(entry: dict[str, object], query: str) -> bool:
    terms = [term.casefold() for term in query.split() if term]
    name = str(entry["fileName"]).casefold()
    return all(term in name for term in terms)


def natural_key(value: str) -> list[tuple[int, object]]:
    """Match the native selector's case-insensitive numeric name ordering."""

    return [
        (1, int(part)) if part.isdigit() else (0, part.casefold())
        for part in NATURAL_PARTS.split(value)
    ]


def ordered_dirs_and_files(
    entries: list[dict[str, object]], key, reverse: bool = False
) -> list[dict[str, object]]:
    directories = [entry for entry in entries if entry["fileIsDir"]]
    files = [entry for entry in entries if not entry["fileIsDir"]]
    directories.sort(key=key, reverse=reverse)
    files.sort(key=key, reverse=reverse)
    return directories + files


def sort_entries(
    entries: list[dict[str, object]], sort_mode: str, custom_order: list[str]
) -> list[dict[str, object]]:
    if sort_mode == "name":
        return ordered_dirs_and_files(entries, lambda entry: natural_key(str(entry["fileName"])))
    if sort_mode == "name_rev":
        return ordered_dirs_and_files(
            entries, lambda entry: natural_key(str(entry["fileName"])), reverse=True
        )
    if sort_mode == "time":
        return ordered_dirs_and_files(
            entries, lambda entry: float(entry["fileModified"]), reverse=True
        )
    if sort_mode == "time_rev":
        return ordered_dirs_and_files(entries, lambda entry: float(entry["fileModified"]))
    if sort_mode == "size":
        return ordered_dirs_and_files(
            entries, lambda entry: int(entry["fileSize"]), reverse=True
        )
    if sort_mode == "size_rev":
        return ordered_dirs_and_files(entries, lambda entry: int(entry["fileSize"]))

    # Custom order keeps directories in natural name order, then follows the
    # persisted file-name list and finally places new files newest first.
    directories = [entry for entry in entries if entry["fileIsDir"]]
    files = [entry for entry in entries if not entry["fileIsDir"]]
    directories.sort(key=lambda entry: natural_key(str(entry["fileName"])))
    if not custom_order:
        files.sort(key=lambda entry: float(entry["fileModified"]), reverse=True)
        return directories + files
    order_lookup = {name: index for index, name in enumerate(custom_order)}
    ordered_files = [entry for entry in files if entry["fileName"] in order_lookup]
    remaining_files = [entry for entry in files if entry["fileName"] not in order_lookup]
    ordered_files.sort(key=lambda entry: order_lookup[str(entry["fileName"])])
    remaining_files.sort(key=lambda entry: float(entry["fileModified"]), reverse=True)
    return directories + ordered_files + remaining_files


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        usage="%(prog)s --directory DIRECTORY [QUERY] | %(prog)s --file FILE"
    )
    target = parser.add_mutually_exclusive_group(required=True)
    target.add_argument("--directory", metavar="DIRECTORY")
    target.add_argument("--file", metavar="FILE")
    parser.add_argument("query", nargs="?", default="")
    parser.add_argument("--sort", choices=SORT_MODES, default="custom")
    parser.add_argument("--custom-order", default="[]")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    target = Path(args.directory or args.file).expanduser().resolve()
    if args.file is not None:
        print(json.dumps(item(target), separators=(",", ":")))
        return 0
    if not target.is_dir():
        print("[]")
        return 0

    try:
        custom_order = json.loads(args.custom_order)
    except json.JSONDecodeError:
        custom_order = []
    if not isinstance(custom_order, list) or not all(
        isinstance(name, str) for name in custom_order
    ):
        custom_order = []

    entries = []
    try:
        children = sorted(target.iterdir(), key=lambda path: path.name.casefold())
    except OSError:
        children = []
    for child in children:
        entry = item(child)
        if not entry["fileIsDir"] and entry["mediaKind"] == "unknown":
            continue
        if matches_query(entry, args.query):
            entries.append(entry)
    entries = sort_entries(entries, args.sort, custom_order)
    print(json.dumps(entries, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
