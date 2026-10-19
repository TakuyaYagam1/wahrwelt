"""Refuse local replacements before Home Manager relinks generated defaults."""

import argparse
import contextlib
import json
import os
import stat
import sys
from pathlib import Path

LIMIT = 8 * 1024 * 1024


class GuardError(Exception):
    pass


def relative_parts(value):
    parts = value.split("/")
    if not value or any(part in ("", ".", "..") for part in parts):
        raise GuardError(f"invalid relative config path: {value!r}")
    return parts


def read_regular(path, *, directory=None, nofollow=False):
    flags = os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC
    if nofollow:
        flags |= os.O_NOFOLLOW
    fd = os.open(path, flags, dir_fd=directory)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_size > LIMIT:
            raise GuardError(
                f"expected a regular config file of at most {LIMIT} bytes: {path}"
            )
        data = stream.read(LIMIT + 1)
        if len(data) > LIMIT:
            raise GuardError(f"config file exceeds size limit: {path}")
        return data


def user_location(name):
    if name.startswith("fish/"):
        return "fish/user/config.fish or fish/user/functions/"
    if name.startswith("nvim/"):
        return "nvim/lua/user/"
    return "hypr/user/"


def check_file(home_fd, root_parts, name, entry, previous_root):
    parts = relative_parts(name)
    source = entry["source"]
    if not os.path.isabs(source):
        raise GuardError(f"source must be absolute: {name}")
    expected = [source]
    if previous_root:
        old = str(Path(previous_root).joinpath(*parts))
        if os.path.exists(old):
            expected.append(old)
    # Explicit bootstrap templates permit a first install without treating an
    # arbitrary existing user file as an upstream baseline.
    baselines = entry.get("baselines", [])
    if not isinstance(baselines, list) or any(not os.path.isabs(p) for p in baselines):
        raise GuardError(f"invalid bootstrap baselines: {name}")
    with contextlib.ExitStack() as stack:
        parent = home_fd
        for component in root_parts + parts[:-1]:
            try:
                parent = os.open(
                    component,
                    os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                    dir_fd=parent,
                )
            except FileNotFoundError:
                return
            stack.callback(os.close, parent)
        try:
            info = os.stat(parts[-1], dir_fd=parent, follow_symlinks=False)
        except FileNotFoundError:
            return
        if stat.S_ISLNK(info.st_mode):
            # Resolve only the link's identity, never read arbitrary link data.
            target = os.readlink(parts[-1], dir_fd=parent)
            live = os.path.realpath(os.path.join(f"/proc/self/fd/{parent}", target))
            if any(
                os.path.exists(path) and live == os.path.realpath(path)
                for path in expected
            ):
                return
            reason = "unexpected symlink"
        elif stat.S_ISREG(info.st_mode):
            current = read_regular(parts[-1], directory=parent, nofollow=True)
            if any(current == read_regular(path) for path in expected + baselines):
                return
            reason = "locally edited replacement"
        else:
            reason = "directory or special file"
        raise GuardError(
            f"{name}: {reason}; refusing to overwrite it. Move your customizations "
            f"into {user_location(name)}, keep a backup, then remove the replacement "
            "and retry the update."
        )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--home", required=True)
    parser.add_argument("--root", required=True)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--previous-root")
    args = parser.parse_args()
    try:
        home, root = Path(args.home), Path(args.root)
        if not home.is_absolute() or not root.is_absolute() or ".." in root.parts:
            raise GuardError("home and config root must be absolute paths without '..'")
        try:
            relative = root.relative_to(home)
        except ValueError:
            raise GuardError("config root must be inside home") from None
        manifest = json.loads(read_regular(args.manifest))
        if manifest.get("version") != 1 or not isinstance(manifest.get("files"), dict):
            raise GuardError("unsupported managed config manifest")
        home_fd = os.open(home, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            for name, entry in sorted(manifest["files"].items()):
                try:
                    check_file(
                        home_fd, list(relative.parts), name, entry, args.previous_root
                    )
                except OSError as error:
                    raise GuardError(
                        f"{name}: cannot inspect config safely: {error.strerror}"
                    ) from error
        finally:
            os.close(home_fd)
    except (GuardError, OSError, ValueError, TypeError, KeyError) as error:
        print(f"wahrwelt: managed config check failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
