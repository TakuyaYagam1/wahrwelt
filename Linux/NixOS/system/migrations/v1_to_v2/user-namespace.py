"""One-shot v1_to_v2 migration for host-local config and installer state."""

from __future__ import annotations

import ctypes
import errno
import hashlib
import json
import os
import re
import secrets
import stat
import sys
from pathlib import Path


class MigrationError(RuntimeError):
    pass


PROC_FD_PATH = re.compile(r"/proc/self/fd/([0-9]+)")


def inherited_fd_number(path: Path) -> int | None:
    match = PROC_FD_PATH.fullmatch(str(path))
    if match is None:
        return None
    return int(match.group(1))


def duplicate_directory(path: Path) -> int:
    inherited = inherited_fd_number(path)
    if inherited is not None:
        fd = os.dup(inherited)
        os.set_inheritable(fd, False)
    else:
        flags = os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        fd = os.open(path, flags)
    if not stat.S_ISDIR(os.fstat(fd).st_mode):
        os.close(fd)
        raise MigrationError(
            f"ownership collision: {path} must be an ordinary directory"
        )
    return fd


def create_owned_temp(kind: str, parent: Path, prefix: str) -> None:
    if kind not in ("directory", "file"):
        raise MigrationError(f"invalid temporary object kind: {kind}")
    if not prefix or "/" in prefix or prefix in (".", ".."):
        raise MigrationError(f"invalid temporary object prefix: {prefix!r}")

    parent_fd = duplicate_directory(parent)
    try:
        for _ in range(128):
            name = prefix + secrets.token_hex(8)
            try:
                if kind == "directory":
                    os.mkdir(name, 0o700, dir_fd=parent_fd)
                    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC
                    if hasattr(os, "O_NOFOLLOW"):
                        flags |= os.O_NOFOLLOW
                    created_fd = os.open(name, flags, dir_fd=parent_fd)
                else:
                    flags = os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC
                    if hasattr(os, "O_NOFOLLOW"):
                        flags |= os.O_NOFOLLOW
                    created_fd = os.open(name, flags, 0o600, dir_fd=parent_fd)
            except FileExistsError:
                continue
            try:
                created = os.fstat(created_fd)
                visible = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
                expected_type = stat.S_ISDIR if kind == "directory" else stat.S_ISREG
                if not expected_type(created.st_mode) or not expected_type(
                    visible.st_mode
                ):
                    raise MigrationError(
                        f"ownership collision: created {kind} changed type before handoff: {name}"
                    )
                if (created.st_dev, created.st_ino) != (visible.st_dev, visible.st_ino):
                    raise MigrationError(
                        f"ownership collision: created {kind} changed identity before handoff: {name}"
                    )
                os.fchmod(created_fd, 0o700 if kind == "directory" else 0o600)
                print(f"{name}\t{created.st_dev}:{created.st_ino}")
                return
            finally:
                os.close(created_fd)
    finally:
        os.close(parent_fd)
    raise MigrationError(f"could not allocate a unique temporary {kind} in {parent}")


def find_owned_temp(parent: Path, identity: str) -> None:
    try:
        device_text, inode_text = identity.split(":", 1)
        expected = int(device_text), int(inode_text)
    except (TypeError, ValueError) as error:
        raise MigrationError(
            f"invalid temporary object identity: {identity!r}"
        ) from error

    parent_fd = duplicate_directory(parent)
    try:
        for name in sorted(os.listdir(parent_fd)):
            info = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
            if (info.st_dev, info.st_ino) == expected:
                print(name)
    finally:
        os.close(parent_fd)


def lstat_optional(path: Path) -> os.stat_result | None:
    try:
        return path.lstat()
    except FileNotFoundError:
        return None


def require_directory(path: Path, info: os.stat_result | None) -> None:
    if info is not None and not stat.S_ISDIR(info.st_mode):
        raise MigrationError(
            f"ownership collision: {path} must be an ordinary directory"
        )


def require_regular(path: Path, info: os.stat_result | None) -> None:
    if info is not None and not stat.S_ISREG(info.st_mode):
        raise MigrationError(
            f"ownership collision: {path} must be an ordinary regular file"
        )


def mount_points() -> set[str]:
    try:
        lines = Path("/proc/self/mountinfo").read_text(encoding="utf-8").splitlines()
    except OSError as error:
        raise MigrationError("cannot inspect /proc/self/mountinfo") from error
    decoded: set[str] = set()
    for line in lines:
        fields = line.split()
        if len(fields) < 5:
            continue
        path = fields[4]
        for escaped, value in (
            ("\\040", " "),
            ("\\011", "\t"),
            ("\\012", "\n"),
            ("\\134", "\\"),
        ):
            path = path.replace(escaped, value)
        decoded.add(os.path.abspath(path))
    return decoded


def reject_mountpoint(
    path: Path, info: os.stat_result | None, mounted: set[str]
) -> None:
    if info is not None and os.path.abspath(path) in mounted:
        raise MigrationError(f"ownership collision: {path} is a mountpoint")


MAX_INSTALLER_STATE_BYTES = 1024 * 1024
COMMON_STATE_FIELDS = {
    "host",
    "user",
    "locale",
    "git",
    "packages",
    "display",
    "hardware",
    "features",
    "dots",
}


def reject_json_constant(value: str) -> object:
    raise MigrationError(f"invalid JSON constant {value!r}")


def reject_duplicate_json_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise MigrationError(f"duplicate JSON field {key!r}")
        result[key] = value
    return result


def require_json_object(value: object, display: str) -> dict[str, object]:
    if type(value) is not dict:
        raise MigrationError(f"{display} must be a JSON object")
    return value


def require_json_shape(
    value: object,
    display: str,
    required: set[str],
    optional: set[str] | None = None,
) -> dict[str, object]:
    payload = require_json_object(value, display)
    allowed = required | (optional or set())
    missing = sorted(required - payload.keys())
    unknown = sorted(payload.keys() - allowed)
    if missing:
        raise MigrationError(
            f"{display} is missing required fields: {', '.join(missing)}"
        )
    if unknown:
        raise MigrationError(f"{display} has unknown fields: {', '.join(unknown)}")
    return payload


def require_json_strings(value: object, display: str, fields: set[str]) -> None:
    payload = require_json_shape(value, display, fields)
    for field in sorted(fields):
        if type(payload[field]) is not str:
            raise MigrationError(f"{display}.{field} must be a JSON string")


def require_json_bools(
    value: object,
    display: str,
    required: set[str],
    optional: set[str] | None = None,
) -> None:
    payload = require_json_shape(value, display, required, optional)
    for field in sorted(payload):
        if type(payload[field]) is not bool:
            raise MigrationError(f"{display}.{field} must be a JSON boolean")


def validate_installer_state_payload(payload: bytes, path: Path) -> None:
    try:
        text = payload.decode("utf-8")
    except UnicodeDecodeError as error:
        raise MigrationError("state payload is not valid UTF-8 JSON") from error
    try:
        decoded = json.loads(
            text,
            object_pairs_hook=reject_duplicate_json_keys,
            parse_constant=reject_json_constant,
        )
    except json.JSONDecodeError as error:
        raise MigrationError(f"state payload is malformed JSON: {error.msg}") from error
    root = require_json_object(decoded, "state payload")
    raw_version = root.get("schemaVersion", 0)
    if type(raw_version) is not int:
        raise MigrationError("state payload.schemaVersion must be a JSON integer")
    version = raw_version
    if version < 0 or version > 7:
        raise MigrationError(f"state payload schemaVersion {version} is unsupported")

    required = set(COMMON_STATE_FIELDS)
    optional: set[str] = set()
    if version == 0:
        optional.add("schemaVersion")
    else:
        required.add("schemaVersion")
    if version <= 2:
        required.add("shell")
    elif version == 3:
        optional.add("shell")
    if version <= 4:
        required.add("services")
    elif version == 5:
        optional.add("services")
    if version <= 6:
        required.add("zapret")
    else:
        optional.add("zapret")
    if version >= 6:
        required.add("source")
    if version >= 7:
        required.add("noctalia")
    require_json_shape(root, "state payload", required, optional)

    require_json_strings(
        root["host"], "state payload.host", {"hostname", "stateVersion"}
    )
    require_json_strings(
        root["user"],
        "state payload.user",
        {"username", "fullName", "homeDirectory"},
    )
    require_json_strings(
        root["locale"],
        "state payload.locale",
        {
            "timeZone",
            "defaultLocale",
            "extraLocale",
            "consoleKeyMap",
            "weatherLocation",
            "keyboardLayouts",
            "keyboardToggle",
        },
    )
    require_json_strings(root["git"], "state payload.git", {"username", "email"})
    require_json_strings(root["packages"], "state payload.packages", {"preset"})
    require_json_strings(root["hardware"], "state payload.hardware", {"gpu"})

    display = require_json_shape(
        root["display"],
        "state payload.display",
        {"monitorName", "monitorMode", "monitorPosition", "monitorScale"},
        {"extraMonitors"} if version >= 5 else set(),
    )
    for field in ("monitorName", "monitorMode", "monitorPosition", "monitorScale"):
        if type(display[field]) is not str:
            raise MigrationError(f"state payload.display.{field} must be a JSON string")
    if "extraMonitors" in display:
        monitors = display["extraMonitors"]
        if type(monitors) is not list:
            raise MigrationError(
                "state payload.display.extraMonitors must be a JSON array"
            )
        for index, monitor in enumerate(monitors):
            require_json_strings(
                monitor,
                f"state payload.display.extraMonitors[{index}]",
                {"name", "mode", "position", "scale"},
            )

    feature_fields = {"secureBoot", "ctfTools", "omniRouter"}
    feature_optional: set[str] = set()
    if version >= 4:
        feature_fields.add("observability")
    if version <= 4:
        feature_fields.add("russiaMode")
    elif version == 5:
        feature_optional.add("russiaMode")
    if version == 7:
        feature_optional.add("portainer")
    require_json_bools(
        root["features"],
        "state payload.features",
        feature_fields,
        feature_optional,
    )
    features = require_json_object(root["features"], "state payload.features")
    if version == 5 and "services" in root and "russiaMode" not in features:
        raise MigrationError(
            "state payload has an unknown schema 5 services/Russia mode combination"
        )
    if version == 7 and "zapret" in root and "portainer" in features:
        raise MigrationError(
            "state payload has an unknown schema 7 Zapret/Portainer combination"
        )

    dot_fields = {"hypr", "zenTheme", "sine", "neovim", "v2rayN", "wallpapers"}
    dot_optional: set[str] = set()
    if 4 <= version <= 5:
        dot_fields.add("neovimCleanState")
    elif version == 6:
        dot_optional.add("neovimCleanState")
    require_json_bools(root["dots"], "state payload.dots", dot_fields, dot_optional)

    if "shell" in root:
        require_json_strings(root["shell"], "state payload.shell", {"profile"})
    if "services" in root:
        require_json_strings(
            root["services"],
            "state payload.services",
            {"pgAdminEmail"},
        )
    if "zapret" in root:
        zapret = require_json_shape(
            root["zapret"],
            "state payload.zapret",
            {"enable", "config"},
        )
        if type(zapret["enable"]) is not bool or type(zapret["config"]) is not str:
            raise MigrationError(
                "state payload.zapret requires boolean enable and string config"
            )
    if "source" in root:
        require_json_strings(root["source"], "state payload.source", {"channel"})
    if "noctalia" in root:
        require_json_strings(root["noctalia"], "state payload.noctalia", {"version"})


def regular_state_snapshot(path: Path) -> dict[str, object]:
    flags = os.O_RDONLY | os.O_CLOEXEC
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    fd = os.open(path, flags)
    try:
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode):
            raise MigrationError(
                f"ownership collision: {path} must remain a regular file"
            )
        if before.st_nlink != 1:
            raise MigrationError(
                f"ownership collision: {path} has unsupported hardlink count {before.st_nlink}"
            )
        if before.st_size > MAX_INSTALLER_STATE_BYTES:
            raise MigrationError(
                f"ownership collision: {path} exceeds the installer state size limit"
            )
        chunks: list[bytes] = []
        total = 0
        while True:
            chunk = os.read(fd, min(65536, MAX_INSTALLER_STATE_BYTES + 1 - total))
            if not chunk:
                break
            chunks.append(chunk)
            total += len(chunk)
            if total > MAX_INSTALLER_STATE_BYTES:
                raise MigrationError(
                    f"ownership collision: {path} exceeds the installer state size limit"
                )
        after = os.fstat(fd)
        fields = (
            "st_mode",
            "st_dev",
            "st_ino",
            "st_size",
            "st_mtime_ns",
            "st_ctime_ns",
            "st_nlink",
        )
        if any(getattr(before, field) != getattr(after, field) for field in fields):
            raise MigrationError(
                f"ownership collision: {path} changed while being validated"
            )
        content = b"".join(chunks)
        return {
            "device": before.st_dev,
            "inode": before.st_ino,
            "mode": before.st_mode,
            "size": before.st_size,
            "mtime_ns": before.st_mtime_ns,
            "ctime_ns": before.st_ctime_ns,
            "nlink": before.st_nlink,
            "digest": hashlib.sha256(content).hexdigest(),
            "content": content,
        }
    finally:
        os.close(fd)


def validate_legacy_installer_state(path: Path) -> dict[str, object]:
    try:
        proof = regular_state_snapshot(path)
        validate_installer_state_payload(bytes(proof["content"]), path)
        return proof
    except (MigrationError, OSError, UnicodeError, ValueError, TypeError) as error:
        if isinstance(error, MigrationError) and str(error).startswith(
            "ownership collision:"
        ):
            raise
        raise MigrationError(
            f"ownership collision: {path} is not a recognized Wahrwelt/MySetup installer state: {error}"
        ) from error


def require_unchanged_legacy_installer_state(
    path: Path, expected: dict[str, object]
) -> None:
    try:
        actual = regular_state_snapshot(path)
    except (MigrationError, OSError) as error:
        raise MigrationError(
            f"ownership collision: {path} changed after validation: {error}"
        ) from error
    if actual != expected:
        raise MigrationError(f"ownership collision: {path} changed after validation")


def installer_state_matches_published_proof(
    actual: dict[str, object], expected: dict[str, object]
) -> bool:
    fields = (
        "device",
        "inode",
        "mode",
        "size",
        "mtime_ns",
        "nlink",
        "digest",
        "content",
    )
    return all(actual[field] == expected[field] for field in fields)


def require_published_legacy_installer_state(
    source: Path, target: Path, expected: dict[str, object]
) -> None:
    try:
        actual = regular_state_snapshot(target)
    except (MigrationError, OSError) as error:
        raise MigrationError(
            f"ownership collision: published installer state at {target} cannot be verified: {error}"
        ) from error
    if installer_state_matches_published_proof(actual, expected):
        return
    expected_identity = (expected["device"], expected["inode"])
    actual_identity = (actual["device"], actual["inode"])
    if actual_identity != expected_identity:
        raise MigrationError(
            f"ownership collision: canonical installer state replacement retained at {target}; "
            "the validated published state identity was displaced"
        )
    flags = os.O_RDONLY | os.O_CLOEXEC
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    try:
        published_fd = os.open(target, flags)
    except OSError as open_error:
        raise MigrationError(
            f"ownership collision: canonical installer state changed before recovery; "
            f"the current entry is retained at {target}: {open_error}"
        ) from open_error
    try:
        published = os.fstat(published_fd)
        if (published.st_dev, published.st_ino) != expected_identity:
            raise MigrationError(
                f"ownership collision: canonical installer state replacement retained at {target}; "
                "the entry changed before recovery"
            )
        source_parent_fd = duplicate_directory(source.parent)
        try:
            try:
                os.link(
                    f"/proc/self/fd/{published_fd}",
                    source.name,
                    dst_dir_fd=source_parent_fd,
                    follow_symlinks=True,
                )
            except FileExistsError as link_error:
                raise MigrationError(
                    f"ownership collision: exact published state remains at {target}; "
                    f"recovery path collision retained at {source}"
                ) from link_error
            recovered = os.stat(
                source.name, dir_fd=source_parent_fd, follow_symlinks=False
            )
            if (recovered.st_dev, recovered.st_ino) != expected_identity:
                raise MigrationError(
                    f"ownership collision: recovery identity changed at {source}"
                )
        finally:
            os.close(source_parent_fd)
    finally:
        os.close(published_fd)
    raise MigrationError(
        f"ownership collision: published installer state changed in place; "
        f"canonical entry retained at {target} and exact recovery retained at {source}"
    )


def inspect_namespace(root: Path) -> dict[str, object]:
    root_info = lstat_optional(root)
    if root_info is None or not stat.S_ISDIR(root_info.st_mode):
        raise MigrationError(
            f"ownership collision: {root} must be an ordinary directory"
        )

    private = root / "private"
    user = root / "user"
    legacy_wahrwelt_parent = root / "wahrwelt"
    legacy_wahrwelt_state = legacy_wahrwelt_parent / "state.json"
    legacy_mysetup_parent = root / "mysetup"
    legacy_mysetup_state = legacy_mysetup_parent / "state.json"
    canonical_state = root / "installer-state.json"

    private_info = lstat_optional(private)
    user_info = lstat_optional(user)
    legacy_wahrwelt_parent_info = lstat_optional(legacy_wahrwelt_parent)
    legacy_mysetup_parent_info = lstat_optional(legacy_mysetup_parent)
    canonical_state_info = lstat_optional(canonical_state)

    mounted = mount_points()
    reject_mountpoint(root, root_info, mounted)

    require_directory(private, private_info)
    require_directory(user, user_info)
    require_directory(legacy_wahrwelt_parent, legacy_wahrwelt_parent_info)
    require_directory(legacy_mysetup_parent, legacy_mysetup_parent_info)
    require_regular(canonical_state, canonical_state_info)
    for path, info in (
        (private, private_info),
        (user, user_info),
        (legacy_wahrwelt_parent, legacy_wahrwelt_parent_info),
        (legacy_mysetup_parent, legacy_mysetup_parent_info),
        (canonical_state, canonical_state_info),
    ):
        reject_mountpoint(path, info, mounted)

    legacy_wahrwelt_state_info = None
    if legacy_wahrwelt_parent_info is not None:
        legacy_wahrwelt_state_info = lstat_optional(legacy_wahrwelt_state)
        require_regular(legacy_wahrwelt_state, legacy_wahrwelt_state_info)
        reject_mountpoint(legacy_wahrwelt_state, legacy_wahrwelt_state_info, mounted)
    legacy_mysetup_state_info = None
    if legacy_mysetup_parent_info is not None:
        legacy_mysetup_state_info = lstat_optional(legacy_mysetup_state)
        require_regular(legacy_mysetup_state, legacy_mysetup_state_info)
        reject_mountpoint(legacy_mysetup_state, legacy_mysetup_state_info, mounted)

    if private_info is not None and user_info is not None:
        raise MigrationError(f"ownership collision: both {private} and {user} exist")
    legacy_states = [
        path
        for path, info in (
            (legacy_wahrwelt_state, legacy_wahrwelt_state_info),
            (legacy_mysetup_state, legacy_mysetup_state_info),
        )
        if info is not None
    ]
    if len(legacy_states) > 1:
        raise MigrationError(
            "ownership collision: multiple legacy installer state files exist: "
            + ", ".join(str(path) for path in legacy_states)
        )
    if legacy_states and canonical_state_info is not None:
        raise MigrationError(
            f"ownership collision: both {legacy_states[0]} and {canonical_state} exist"
        )

    legacy_state_path = legacy_states[0] if legacy_states else None
    legacy_state_proof = (
        validate_legacy_installer_state(legacy_state_path)
        if legacy_state_path is not None
        else None
    )

    return {
        "legacy_user": private_info is not None,
        "canonical_user": user_info is not None,
        "legacy_wahrwelt_state": legacy_wahrwelt_state_info is not None,
        "legacy_mysetup_state": legacy_mysetup_state_info is not None,
        "legacy_state": bool(legacy_states),
        "canonical_state": canonical_state_info is not None,
        "legacy_state_path": legacy_state_path,
        "legacy_state_proof": legacy_state_proof,
    }


def skip_block_comment(source: str, index: int) -> int:
    depth = 1
    index += 2
    while index < len(source) and depth:
        if source.startswith("/*", index):
            depth += 1
            index += 2
        elif source.startswith("*/", index):
            depth -= 1
            index += 2
        else:
            index += 1
    return index


NIX_URI_TOKEN = re.compile(r"[A-Za-z][A-Za-z0-9+.-]*:[A-Za-z0-9%/?:@&=+$,_.!~*'-]+")
NIX_SEARCH_PATH_TOKEN = re.compile(r"<[A-Za-z0-9._+-]+(?:/[A-Za-z0-9._+-]+)*>")
NIX_PATH_TOKEN = re.compile(r"[A-Za-z0-9._+-]*(?:/[A-Za-z0-9._+-]+)+/?")
NIX_HOME_PATH_TOKEN = re.compile(r"~/(?:[A-Za-z0-9._+-]+/)*[A-Za-z0-9._+-]*")
NIX_INPATH_LITERAL = re.compile(r"[A-Za-z0-9._+/-]+")
NIX_IDENTIFIER_TOKEN = re.compile(r"[A-Za-z_][A-Za-z0-9_'-]*")


def rewrite_legacy_path_token(token: str) -> str:
    if token == "./private":
        return "./user"
    if token.startswith("./private/"):
        return "./user/" + token[len("./private/") :]
    return token


def filesystem_path_token(source: str, index: int) -> re.Match[str] | None:
    path = NIX_PATH_TOKEN.match(source, index)
    if path is not None:
        return path
    return NIX_HOME_PATH_TOKEN.match(source, index)


def rewrite_interpolated_path(source: str, index: int) -> tuple[str, int] | None:
    path = filesystem_path_token(source, index)
    if path is None:
        return None
    output = [rewrite_legacy_path_token(path.group(0))]
    cursor = path.end()
    leading_literal = NIX_INPATH_LITERAL.match(source, cursor)
    if leading_literal is not None:
        output.append(leading_literal.group(0))
        cursor = leading_literal.end()
    if not source.startswith("${", cursor):
        return None
    while cursor < len(source):
        if source.startswith("${", cursor):
            output.append("${")
            expression, cursor, closed = rewrite_nix_code(source, cursor + 2, True)
            if not closed:
                raise MigrationError("unterminated Nix interpolation in path")
            output.extend((expression, "}"))
            cursor += 1
            continue
        literal = NIX_INPATH_LITERAL.match(source, cursor)
        if literal is None:
            break
        output.append(literal.group(0))
        cursor = literal.end()
    return "".join(output), cursor


def rewrite_nix_token(source: str, index: int) -> tuple[str, int] | None:
    uri = NIX_URI_TOKEN.match(source, index)
    if uri is not None:
        return uri.group(0), uri.end()
    search_path = NIX_SEARCH_PATH_TOKEN.match(source, index)
    if search_path is not None:
        return search_path.group(0), search_path.end()
    if source.startswith("//", index):
        return "//", index + 2
    path = NIX_PATH_TOKEN.match(source, index)
    if path is not None:
        return rewrite_legacy_path_token(path.group(0)), path.end()
    home_path = NIX_HOME_PATH_TOKEN.match(source, index)
    if home_path is not None:
        return home_path.group(0), home_path.end()
    identifier = NIX_IDENTIFIER_TOKEN.match(source, index)
    if identifier is not None:
        return identifier.group(0), identifier.end()
    return None


def rewrite_quoted_string(source: str, index: int) -> tuple[str, int]:
    output = ['"']
    index += 1
    while index < len(source):
        if source[index] == "\\":
            end = min(index + 2, len(source))
            output.append(source[index:end])
            index = end
            continue
        if source.startswith("${", index):
            output.append("${")
            expression, index, closed = rewrite_nix_code(source, index + 2, True)
            output.append(expression)
            if not closed:
                raise MigrationError("unterminated Nix interpolation in quoted string")
            output.append("}")
            index += 1
            continue
        output.append(source[index])
        index += 1
        if source[index - 1] == '"':
            break
    return "".join(output), index


def rewrite_indented_string(source: str, index: int) -> tuple[str, int]:
    output = ["''"]
    index += 2
    while index < len(source):
        if source.startswith("''", index) and index + 2 < len(source):
            escaped = source[index + 2]
            if escaped in ("$", "'"):
                output.append(source[index : index + 3])
                index += 3
                continue
            if escaped == "\\":
                end = min(index + 4, len(source))
                output.append(source[index:end])
                index = end
                continue
        if source.startswith("''", index):
            output.append("''")
            return "".join(output), index + 2
        if source.startswith("${", index):
            output.append("${")
            expression, index, closed = rewrite_nix_code(source, index + 2, True)
            output.append(expression)
            if not closed:
                raise MigrationError(
                    "unterminated Nix interpolation in indented string"
                )
            output.append("}")
            index += 1
            continue
        output.append(source[index])
        index += 1
    return "".join(output), index


def rewrite_nix_code(
    source: str, index: int = 0, stop_at_closing_brace: bool = False
) -> tuple[str, int, bool]:
    output: list[str] = []
    brace_depth = 0
    while index < len(source):
        start = index
        if source[index] == "#":
            end = source.find("\n", index + 1)
            index = len(source) if end < 0 else end
            output.append(source[start:index])
            continue
        if source.startswith("/*", index):
            index = skip_block_comment(source, index)
            output.append(source[start:index])
            continue
        if source[index] == '"':
            rewritten, index = rewrite_quoted_string(source, index)
            output.append(rewritten)
            continue
        if source.startswith("''", index):
            rewritten, index = rewrite_indented_string(source, index)
            output.append(rewritten)
            continue
        interpolated_path = rewrite_interpolated_path(source, index)
        if interpolated_path is not None:
            rewritten, index = interpolated_path
            output.append(rewritten)
            continue
        if source[index] == "{":
            brace_depth += 1
            output.append(source[index])
            index += 1
            continue
        if source[index] == "}":
            if stop_at_closing_brace and brace_depth == 0:
                return "".join(output), index, True
            brace_depth = max(0, brace_depth - 1)
            output.append(source[index])
            index += 1
            continue
        token = rewrite_nix_token(source, index)
        if token is not None:
            replacement, index = token
            output.append(replacement)
            continue
        output.append(source[index])
        index += 1
    return "".join(output), index, not stop_at_closing_brace


def rewrite_private_paths(source: str) -> str:
    rewritten, _, _ = rewrite_nix_code(source)
    return rewritten


def root_nix_entries(root: Path) -> list[Path]:
    return [
        path
        for path in sorted(root.iterdir())
        if path.name.endswith(".nix") and path.name != "hashed-password.nix"
    ]


def read_regular(path: Path) -> str:
    inherited = inherited_fd_number(path)
    if inherited is not None:
        fd = os.dup(inherited)
        os.set_inheritable(fd, False)
        os.lseek(fd, 0, os.SEEK_SET)
    else:
        flags = os.O_RDONLY | os.O_CLOEXEC
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        fd = os.open(path, flags)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode):
            raise MigrationError(f"ownership collision: {path} stopped being regular")
        with os.fdopen(fd, "r", encoding="utf-8", closefd=False) as source:
            return source.read()
    finally:
        os.close(fd)


def write_regular(path: Path, content: str) -> None:
    flags = os.O_WRONLY | os.O_TRUNC | os.O_CLOEXEC
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    fd = os.open(path, flags)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode):
            raise MigrationError(f"ownership collision: {path} stopped being regular")
        data = content.encode("utf-8")
        view = memoryview(data)
        while view:
            written = os.write(fd, view)
            if written == 0:
                raise MigrationError(f"short write while updating {path}")
            view = view[written:]
    finally:
        os.close(fd)


def configuration_migration_plan(root: Path) -> tuple[Path, str, str] | None:
    """Plan the one owned root rewrite without mutating arbitrary modules."""
    plan: tuple[Path, str, str] | None = None
    for path in root_nix_entries(root):
        info = lstat_optional(path)
        if info is None:
            continue
        if not stat.S_ISREG(info.st_mode):
            if path.name == "configuration.nix":
                raise MigrationError(
                    f"ownership collision: owned v1 target {path} must be an ordinary regular file"
                )
            continue
        source = read_regular(path)
        migrated = rewrite_private_paths(source)
        if migrated == source:
            continue
        if path.name != "configuration.nix":
            raise MigrationError(
                f"ownership collision: unowned top-level module {path} contains a real "
                "./private Nix path token; move that reference manually before retrying"
            )
        plan = path, source, migrated
    return plan


def has_legacy_nix_path(root: Path) -> bool:
    return configuration_migration_plan(root) is not None


def legacy_state_validation_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_LEGACY_STATE_VALIDATED_READY_FD")
    proceed = os.environ.get("WAHRWELT_TEST_LEGACY_STATE_VALIDATED_CONTINUE_FD")
    if ready is None and proceed is None:
        return
    if ready is None or proceed is None:
        raise MigrationError("incomplete legacy state validation test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        raise MigrationError("legacy state validation test barrier was not released")


def legacy_state_published_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_LEGACY_STATE_PUBLISHED_READY_FD")
    proceed = os.environ.get("WAHRWELT_TEST_LEGACY_STATE_PUBLISHED_CONTINUE_FD")
    if ready is None and proceed is None:
        return
    if ready is None or proceed is None:
        raise MigrationError("incomplete legacy state publication test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        raise MigrationError("legacy state publication test barrier was not released")


def renameat2_noreplace(
    source_parent_fd: int,
    source_name: str,
    target_parent_fd: int,
    target_name: str,
) -> None:
    for name in (source_name, target_name):
        if not name or "/" in name or name in (".", ".."):
            raise MigrationError(f"invalid no-replace rename entry: {name!r}")
    renameat2 = getattr(ctypes.CDLL(None, use_errno=True), "renameat2", None)
    if renameat2 is None:
        raise MigrationError(
            "renameat2 is unavailable; installer state cannot be published safely"
        )
    renameat2.argtypes = [
        ctypes.c_int,
        ctypes.c_char_p,
        ctypes.c_int,
        ctypes.c_char_p,
        ctypes.c_uint,
    ]
    renameat2.restype = ctypes.c_int
    if (
        renameat2(
            source_parent_fd,
            os.fsencode(source_name),
            target_parent_fd,
            os.fsencode(target_name),
            1,
        )
        != 0
    ):
        error_number = ctypes.get_errno()
        if error_number in (errno.EEXIST, errno.ENOTEMPTY):
            raise FileExistsError(error_number, os.strerror(error_number), target_name)
        raise OSError(error_number, os.strerror(error_number), source_name)


COMPLETION_NAME = "v1_to_v2.complete"
COMPLETION_PAYLOAD = b"wahrwelt-v1-to-v2-complete\n"


def inspect_completion(root: Path) -> str:
    root_fd = duplicate_directory(root)
    try:
        root_info = os.fstat(root_fd)
        if root_info.st_uid != os.geteuid() or root_info.st_mode & 0o022:
            raise MigrationError(
                f"ownership collision: completion root {root} must be owned by the caller "
                "and not group/world-writable"
            )
        try:
            visible = os.stat(COMPLETION_NAME, dir_fd=root_fd, follow_symlinks=False)
        except FileNotFoundError:
            return "absent"
        if not stat.S_ISREG(visible.st_mode) or visible.st_nlink != 1:
            raise MigrationError(
                f"ownership collision: completion marker {root / COMPLETION_NAME} "
                "must be an ordinary single-link regular file"
            )
        if visible.st_uid != os.geteuid() or stat.S_IMODE(visible.st_mode) != 0o600:
            raise MigrationError(
                f"ownership collision: completion marker {root / COMPLETION_NAME} "
                "must be caller-owned with mode 0600"
            )
        flags = os.O_RDONLY | os.O_CLOEXEC
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        marker_fd = os.open(COMPLETION_NAME, flags, dir_fd=root_fd)
        try:
            before = os.fstat(marker_fd)
            payload = os.read(marker_fd, len(COMPLETION_PAYLOAD) + 1)
            after = os.fstat(marker_fd)
            final_visible = os.stat(
                COMPLETION_NAME, dir_fd=root_fd, follow_symlinks=False
            )
        finally:
            os.close(marker_fd)
        identity = lambda info: (
            info.st_dev,
            info.st_ino,
            info.st_mode,
            info.st_nlink,
            info.st_uid,
            info.st_gid,
            info.st_size,
            info.st_mtime_ns,
            info.st_ctime_ns,
        )
        if identity(before) != identity(after) or identity(after) != identity(
            final_visible
        ):
            raise MigrationError(
                f"ownership collision: completion marker {root / COMPLETION_NAME} changed while being validated"
            )
        if payload != COMPLETION_PAYLOAD:
            raise MigrationError(
                f"ownership collision: completion marker {root / COMPLETION_NAME} has unknown content"
            )
        return "complete"
    finally:
        os.close(root_fd)


def publish_completion(root: Path) -> None:
    if inspect_completion(root) != "absent":
        raise MigrationError(
            f"ownership collision: completion marker already exists at {root / COMPLETION_NAME}"
        )
    root_fd = duplicate_directory(root)
    marker_fd = -1
    try:
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        try:
            marker_fd = os.open(COMPLETION_NAME, flags, 0o600, dir_fd=root_fd)
        except FileExistsError as error:
            raise MigrationError(
                f"ownership collision: completion marker appeared at {root / COMPLETION_NAME}; it was preserved"
            ) from error
        view = memoryview(COMPLETION_PAYLOAD)
        while view:
            written = os.write(marker_fd, view)
            if written == 0:
                raise MigrationError(
                    f"short write while publishing completion marker {root / COMPLETION_NAME}"
                )
            view = view[written:]
        os.fsync(marker_fd)
        created = os.fstat(marker_fd)
        visible = os.stat(COMPLETION_NAME, dir_fd=root_fd, follow_symlinks=False)
        if (
            not stat.S_ISREG(created.st_mode)
            or created.st_nlink != 1
            or created.st_uid != os.geteuid()
            or stat.S_IMODE(created.st_mode) != 0o600
            or (created.st_dev, created.st_ino) != (visible.st_dev, visible.st_ino)
        ):
            raise MigrationError(
                f"ownership collision: completion marker changed during publication at {root / COMPLETION_NAME}; "
                "the visible entry was preserved"
            )
        os.fsync(root_fd)
    finally:
        if marker_fd >= 0:
            os.close(marker_fd)
        os.close(root_fd)
    if inspect_completion(root) != "complete":
        raise MigrationError(
            f"completion marker could not be verified at {root / COMPLETION_NAME}"
        )


def directory_entry_identity(parent_fd: int, name: str) -> tuple[int, int] | None:
    try:
        info = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    except FileNotFoundError:
        return None
    return info.st_dev, info.st_ino


def quarantine_exact_empty_legacy_state_parent(
    root: Path,
    root_fd: int,
    parent_name: str,
    parent_fd: int,
    expected_identity: tuple[int, int],
) -> None:
    if directory_entry_identity(root_fd, parent_name) != expected_identity:
        raise MigrationError(
            f"ownership collision: legacy state parent changed before quarantine: {root / parent_name}"
        )
    pinned = os.fstat(parent_fd)
    if (
        not stat.S_ISDIR(pinned.st_mode)
        or (pinned.st_dev, pinned.st_ino) != expected_identity
    ):
        raise MigrationError(
            f"ownership collision: pinned legacy state parent changed before quarantine: {root / parent_name}"
        )
    if os.listdir(parent_fd):
        return

    prefix = f".{parent_name}.installer-state-parent."
    quarantine_name = ""
    for _ in range(128):
        candidate = prefix + secrets.token_hex(8)
        try:
            renameat2_noreplace(root_fd, parent_name, root_fd, candidate)
            quarantine_name = candidate
            break
        except FileExistsError:
            continue
        except FileNotFoundError as error:
            raise MigrationError(
                f"ownership collision: legacy state parent changed during quarantine: {root / parent_name}"
            ) from error
    if not quarantine_name:
        raise MigrationError(
            f"ownership collision: cannot allocate legacy state parent quarantine beneath {root}"
        )

    quarantined_identity = directory_entry_identity(root_fd, quarantine_name)
    if quarantined_identity != expected_identity:
        try:
            renameat2_noreplace(root_fd, quarantine_name, root_fd, parent_name)
        except (FileExistsError, OSError) as rollback_error:
            raise MigrationError(
                f"ownership collision: unknown legacy parent retained at {root / quarantine_name}; "
                f"quarantine rollback was refused: {rollback_error}"
            ) from rollback_error
        raise MigrationError(
            f"ownership collision: legacy state parent changed during quarantine and was restored at {root / parent_name}"
        )
    if directory_entry_identity(root_fd, parent_name) is not None:
        raise MigrationError(
            f"ownership collision: a new legacy state parent appeared at {root / parent_name}; "
            f"exact original retained at {root / quarantine_name}"
        )
    after = os.fstat(parent_fd)
    if (after.st_dev, after.st_ino) != expected_identity or os.listdir(parent_fd):
        raise MigrationError(
            f"ownership collision: quarantined legacy state parent changed at {root / quarantine_name}"
        )
    print(f"quarantined empty legacy state parent at {root / quarantine_name}")


def migrate_stage(root: Path) -> None:
    state = inspect_namespace(root)
    configuration_plan = configuration_migration_plan(root)

    legacy_state_path = state["legacy_state_path"]
    legacy_state_proof = state["legacy_state_proof"]
    if legacy_state_path is not None:
        if not isinstance(legacy_state_path, Path) or not isinstance(
            legacy_state_proof, dict
        ):
            raise MigrationError("invalid internal legacy state proof")
        root_fd = duplicate_directory(root)
        parent_fd = -1
        try:
            parent_name = legacy_state_path.parent.name
            parent_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC
            if hasattr(os, "O_NOFOLLOW"):
                parent_flags |= os.O_NOFOLLOW
            parent_fd = os.open(parent_name, parent_flags, dir_fd=root_fd)
            parent_info = os.fstat(parent_fd)
            parent_identity = parent_info.st_dev, parent_info.st_ino
            if directory_entry_identity(root_fd, parent_name) != parent_identity:
                raise MigrationError(
                    f"ownership collision: legacy state parent changed while being pinned: {legacy_state_path.parent}"
                )

            legacy_state_validation_barrier()
            require_unchanged_legacy_installer_state(
                legacy_state_path, legacy_state_proof
            )
            if directory_entry_identity(root_fd, parent_name) != parent_identity:
                raise MigrationError(
                    f"ownership collision: legacy state parent changed before state publication: {legacy_state_path.parent}"
                )
            canonical_state_path = root / "installer-state.json"
            try:
                renameat2_noreplace(
                    parent_fd,
                    legacy_state_path.name,
                    root_fd,
                    canonical_state_path.name,
                )
            except FileExistsError as error:
                raise MigrationError(
                    f"ownership collision: canonical installer state appeared at {canonical_state_path}"
                ) from error
            legacy_state_published_barrier()
            require_published_legacy_installer_state(
                legacy_state_path, canonical_state_path, legacy_state_proof
            )
            quarantine_exact_empty_legacy_state_parent(
                root, root_fd, parent_name, parent_fd, parent_identity
            )
        finally:
            if parent_fd >= 0:
                os.close(parent_fd)
            os.close(root_fd)

    if state["legacy_user"]:
        os.rename(root / "private", root / "user")

    if configuration_plan is not None:
        path, source, migrated = configuration_plan
        if read_regular(path) != source:
            raise MigrationError(
                f"ownership collision: owned v1 target {path} changed after validation"
            )
        write_regular(path, migrated)

    validate_migrated(root)


def validate_migrated(root: Path) -> None:
    state = inspect_namespace(root)
    if state["legacy_user"]:
        raise MigrationError(
            f"migration left legacy user directory at {root / 'private'}"
        )
    if state["legacy_state"]:
        raise MigrationError("migration left a legacy installer state file")
    if has_legacy_nix_path(root):
        raise MigrationError("migration left a real ./private Nix path token")


def regular_digest(path: Path) -> str:
    flags = os.O_RDONLY | os.O_CLOEXEC
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    digest = hashlib.sha256()
    fd = os.open(path, flags)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise MigrationError(f"snapshot collision: {path} stopped being regular")
        while True:
            block = os.read(fd, 1024 * 1024)
            if not block:
                return digest.hexdigest()
            digest.update(block)
    finally:
        os.close(fd)


def xattr_snapshot(path: Path) -> list[list[str]]:
    try:
        names = os.listxattr(path, follow_symlinks=False)
    except OSError as error:
        if error.errno in (errno.ENOTSUP, errno.EOPNOTSUPP):
            return []
        raise MigrationError(f"cannot list extended attributes for {path}") from error
    attributes: list[list[str]] = []
    for name in sorted(names):
        try:
            value = os.getxattr(path, name, follow_symlinks=False)
        except OSError as error:
            raise MigrationError(
                f"cannot read extended attribute {name!r} for {path}"
            ) from error
        attributes.append([name, hashlib.sha256(value).hexdigest()])
    return attributes


def metadata_snapshot(info: os.stat_result) -> dict[str, int]:
    return {
        "mode": info.st_mode,
        "device": info.st_dev,
        "inode": info.st_ino,
        "size": info.st_size,
        "mtime_ns": info.st_mtime_ns,
        "ctime_ns": info.st_ctime_ns,
        "uid": info.st_uid,
        "gid": info.st_gid,
        "nlink": info.st_nlink,
    }


def reject_external_hardlinks(root: Path, entries: list[dict[str, object]]) -> None:
    internal_links: dict[tuple[int, int], int] = {}
    for entry in entries:
        mode = int(entry["mode"])
        if stat.S_ISDIR(mode):
            continue
        identity = (int(entry["device"]), int(entry["inode"]))
        internal_links[identity] = internal_links.get(identity, 0) + 1

    for entry in entries:
        mode = int(entry["mode"])
        if stat.S_ISDIR(mode):
            continue
        identity = (int(entry["device"]), int(entry["inode"]))
        inside = internal_links[identity]
        total = int(entry["nlink"])
        if inside == total:
            continue
        relative = str(entry["path"])
        path = root if relative == "." else root / relative
        if inside < total:
            raise MigrationError(
                f"ownership collision: external hardlink for {path} cannot be preserved "
                f"({inside} inside {root}, {total} total)"
            )
        raise MigrationError(f"hardlink topology changed while snapshotting {path}")


def snapshot_entries(root: Path) -> list[dict[str, object]]:
    mounted = mount_points()
    entries: list[dict[str, object]] = []
    pending = [root]
    while pending:
        path = pending.pop()
        info = path.lstat()
        if path != root and os.path.abspath(path) in mounted:
            raise MigrationError(f"ownership collision: {path} is a mountpoint")
        relative = "." if path == root else str(path.relative_to(root))
        payload = ""
        if stat.S_ISREG(info.st_mode):
            payload = regular_digest(path)
        elif stat.S_ISLNK(info.st_mode):
            payload = os.readlink(path)
        xattrs = xattr_snapshot(path)
        after = path.lstat()
        metadata = metadata_snapshot(info)
        if metadata != metadata_snapshot(after):
            raise MigrationError(f"configuration changed while snapshotting {path}")
        entries.append(
            {"path": relative, **metadata, "payload": payload, "xattrs": xattrs}
        )
        if stat.S_ISDIR(info.st_mode):
            children = sorted(path.iterdir(), reverse=True)
            pending.extend(children)
    entries = sorted(entries, key=lambda entry: str(entry["path"]))
    reject_external_hardlinks(root, entries)
    return entries


def xattr_snapshot_fd(node_fd: int, display: str) -> list[list[str]]:
    pinned_path = f"/proc/self/fd/{node_fd}"
    try:
        names = os.listxattr(pinned_path, follow_symlinks=True)
    except OSError as error:
        if error.errno in (errno.ENOTSUP, errno.EOPNOTSUPP):
            return []
        raise MigrationError(
            f"cannot list extended attributes for {display}"
        ) from error
    attributes: list[list[str]] = []
    for name in sorted(names):
        try:
            value = os.getxattr(pinned_path, name, follow_symlinks=True)
        except OSError as error:
            raise MigrationError(
                f"cannot read extended attribute {name!r} for {display}"
            ) from error
        attributes.append([name, hashlib.sha256(value).hexdigest()])
    return attributes


def regular_digest_fd(node_fd: int, display: str) -> str:
    flags = os.O_RDONLY | os.O_CLOEXEC
    try:
        content_fd = os.open(f"/proc/self/fd/{node_fd}", flags)
    except OSError as error:
        raise MigrationError(f"cannot open pinned regular file {display}") from error
    digest = hashlib.sha256()
    try:
        if not stat.S_ISREG(os.fstat(content_fd).st_mode):
            raise MigrationError(f"snapshot collision: {display} stopped being regular")
        while True:
            block = os.read(content_fd, 1024 * 1024)
            if not block:
                return digest.hexdigest()
            digest.update(block)
    finally:
        os.close(content_fd)


def snapshot_entries_fd(root_fd: int, display_root: Path) -> list[dict[str, object]]:
    entries: list[dict[str, object]] = []

    def visit(node_fd: int, relative: str) -> None:
        info = os.fstat(node_fd)
        mode = info.st_mode
        display = str(display_root if relative == "." else display_root / relative)
        payload = ""
        if stat.S_ISREG(mode):
            payload = regular_digest_fd(node_fd, display)
        elif stat.S_ISLNK(mode):
            try:
                payload = os.readlink("", dir_fd=node_fd)
            except OSError as error:
                raise MigrationError(f"cannot read pinned symlink {display}") from error
        xattrs = xattr_snapshot_fd(node_fd, display)
        after = os.fstat(node_fd)
        metadata = metadata_snapshot(info)
        if metadata != metadata_snapshot(after):
            raise MigrationError(f"configuration changed while snapshotting {display}")
        entries.append(
            {"path": relative, **metadata, "payload": payload, "xattrs": xattrs}
        )
        if not stat.S_ISDIR(mode):
            return

        directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC
        directory_fd = os.open(f"/proc/self/fd/{node_fd}", directory_flags)
        try:
            if (os.fstat(directory_fd).st_dev, os.fstat(directory_fd).st_ino) != (
                info.st_dev,
                info.st_ino,
            ):
                raise MigrationError(
                    f"directory identity changed while opening {display}"
                )
            for child_name in sorted(os.listdir(directory_fd)):
                if child_name in (".", "..") or "/" in child_name:
                    raise MigrationError(
                        f"invalid directory entry while snapshotting {display}"
                    )
                child_fd = os.open(
                    child_name,
                    os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC,
                    dir_fd=directory_fd,
                )
                try:
                    child_relative = (
                        child_name if relative == "." else relative + "/" + child_name
                    )
                    visit(child_fd, child_relative)
                finally:
                    os.close(child_fd)
        finally:
            os.close(directory_fd)

    root_node_fd = os.open(f"/proc/self/fd/{root_fd}", os.O_PATH | os.O_CLOEXEC)
    try:
        visit(root_node_fd, ".")
    finally:
        os.close(root_node_fd)
    entries = sorted(entries, key=lambda entry: str(entry["path"]))
    reject_external_hardlinks(display_root, entries)
    return entries


def equal_after_directory_exchange(
    actual: list[dict[str, object]], expected: list[dict[str, object]]
) -> bool:
    if len(actual) != len(expected):
        return False
    for actual_entry, expected_entry in zip(actual, expected, strict=True):
        if actual_entry.get("path") != expected_entry.get("path"):
            return False
        if actual_entry.get("path") != ".":
            if actual_entry != expected_entry:
                return False
            continue
        actual_root = dict(actual_entry)
        expected_root = dict(expected_entry)
        actual_root.pop("ctime_ns", None)
        expected_root.pop("ctime_ns", None)
        if actual_root != expected_root:
            return False
    return True


def write_snapshot(root: Path, output: Path) -> None:
    inspect_namespace(root)
    payload = json.dumps(
        {"root": str(root.resolve()), "entries": snapshot_entries(root)},
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    inherited = inherited_fd_number(output)
    if inherited is not None:
        fd = os.dup(inherited)
        os.set_inheritable(fd, False)
        os.ftruncate(fd, 0)
        os.lseek(fd, 0, os.SEEK_SET)
    else:
        flags = os.O_WRONLY | os.O_TRUNC | os.O_CLOEXEC
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        fd = os.open(output, flags)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise MigrationError(f"snapshot path is not regular: {output}")
        os.fchmod(fd, 0o600)
        view = memoryview(payload)
        while view:
            written = os.write(fd, view)
            if written == 0:
                raise MigrationError(f"short write while creating {output}")
            view = view[written:]
    finally:
        os.close(fd)


def verify_precommit(live: Path, stage: Path, snapshot: Path) -> None:
    inspect_namespace(live)
    validate_migrated(stage)
    expected = json.loads(read_regular(snapshot))
    if expected.get("root") != str(live.resolve()):
        raise MigrationError("namespace snapshot belongs to a different destination")
    if expected.get("entries") != snapshot_entries(live):
        raise MigrationError("live configuration changed during staged build")


def publish_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_NAMESPACE_PUBLISH_READY_FD")
    proceed = os.environ.get("WAHRWELT_TEST_NAMESPACE_PUBLISH_CONTINUE_FD")
    if ready is None and proceed is None:
        return
    if ready is None or proceed is None:
        raise MigrationError("incomplete namespace publish test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        raise MigrationError("namespace publish test barrier was not released")


def rollback_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_NAMESPACE_ROLLBACK_READY_FD")
    proceed = os.environ.get("WAHRWELT_TEST_NAMESPACE_ROLLBACK_CONTINUE_FD")
    if ready is None and proceed is None:
        return
    if ready is None or proceed is None:
        raise MigrationError("incomplete namespace rollback test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        raise MigrationError("namespace rollback test barrier was not released")


def post_rollback_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_NAMESPACE_POST_ROLLBACK_READY_FD")
    proceed = os.environ.get("WAHRWELT_TEST_NAMESPACE_POST_ROLLBACK_CONTINUE_FD")
    if ready is None and proceed is None:
        return
    if ready is None or proceed is None:
        raise MigrationError("incomplete namespace post-rollback test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        raise MigrationError("namespace post-rollback test barrier was not released")


def exchange_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_NAMESPACE_EXCHANGE_READY_FD")
    proceed = os.environ.get("WAHRWELT_TEST_NAMESPACE_EXCHANGE_CONTINUE_FD")
    if ready is None and proceed is None:
        return
    if ready is None or proceed is None:
        raise MigrationError("incomplete namespace exchange test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        raise MigrationError("namespace exchange test barrier was not released")


def directory_identity_at(parent_fd: int, name: str) -> tuple[int, int] | None:
    try:
        info = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    except FileNotFoundError:
        return None
    if not stat.S_ISDIR(info.st_mode):
        return None
    return info.st_dev, info.st_ino


def require_directory_identity_at(
    parent_fd: int, name: str, expected: tuple[int, int], stage: str
) -> None:
    if directory_identity_at(parent_fd, name) != expected:
        raise MigrationError(f"{stage}: directory identity changed for {name}")


def retained_directory_path(node_fd: int, fallback: str) -> str:
    try:
        return os.readlink(f"/proc/self/fd/{node_fd}")
    except OSError:
        return fallback


def rename_exchange(parent_fd: int, left: str, right: str) -> None:
    renameat2 = getattr(ctypes.CDLL(None, use_errno=True), "renameat2", None)
    if renameat2 is None:
        raise MigrationError("renameat2 is unavailable; cannot exchange atomically")
    renameat2.argtypes = [
        ctypes.c_int,
        ctypes.c_char_p,
        ctypes.c_int,
        ctypes.c_char_p,
        ctypes.c_uint,
    ]
    renameat2.restype = ctypes.c_int
    if renameat2(parent_fd, os.fsencode(left), parent_fd, os.fsencode(right), 2) != 0:
        error_number = ctypes.get_errno()
        raise OSError(error_number, os.strerror(error_number))


def open_expected_directory(
    parent_fd: int, name: str, expected: tuple[int, int], stage: str
) -> int:
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    try:
        node_fd = os.open(name, flags, dir_fd=parent_fd)
    except OSError as error:
        raise MigrationError(f"{stage}: could not pin {name}: {error}") from error
    if (os.fstat(node_fd).st_dev, os.fstat(node_fd).st_ino) != expected:
        os.close(node_fd)
        raise MigrationError(f"{stage}: directory identity changed for {name}")
    return node_fd


def atomic_publish(live: Path, candidate: Path, snapshot: Path) -> None:
    if (
        live.parent != candidate.parent
        or live.name in ("", ".", "..")
        or candidate.name in ("", ".", "..")
    ):
        raise MigrationError(
            "live and candidate must be distinct same-parent directory entries"
        )
    if live.name == candidate.name:
        raise MigrationError("live and candidate directory entries must differ")

    parent_info = lstat_optional(live.parent)
    if parent_info is None or not stat.S_ISDIR(parent_info.st_mode):
        raise MigrationError(
            f"ownership collision: {live.parent} must be an ordinary directory"
        )
    inspect_namespace(live)
    validate_migrated(candidate)
    live_info = live.lstat()
    candidate_info = candidate.lstat()
    if live_info.st_dev != candidate_info.st_dev:
        raise MigrationError("live and candidate must be on the same filesystem")
    if not stat.S_ISDIR(live_info.st_mode) or not stat.S_ISDIR(candidate_info.st_mode):
        raise MigrationError("live and candidate must remain ordinary directories")
    live_identity = (live_info.st_dev, live_info.st_ino)
    candidate_identity = (candidate_info.st_dev, candidate_info.st_ino)

    expected = json.loads(read_regular(snapshot))
    if expected.get("root") != str(live.resolve()):
        raise MigrationError("namespace snapshot belongs to a different destination")
    if expected.get("entries") != snapshot_entries(live):
        raise MigrationError("live configuration changed before atomic publication")
    canonical_entries = snapshot_entries(candidate)

    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    parent_fd = os.open(live.parent, flags)
    try:
        opened_parent = os.fstat(parent_fd)
        if (opened_parent.st_dev, opened_parent.st_ino) != (
            parent_info.st_dev,
            parent_info.st_ino,
        ):
            raise MigrationError("publication parent changed before it could be pinned")
        require_directory_identity_at(
            parent_fd, live.name, live_identity, "pre-publication revalidation"
        )
        require_directory_identity_at(
            parent_fd,
            candidate.name,
            candidate_identity,
            "pre-publication revalidation",
        )
        live_owner_fd = open_expected_directory(
            parent_fd, live.name, live_identity, "retain live owner"
        )
        candidate_owner_fd = open_expected_directory(
            parent_fd, candidate.name, candidate_identity, "retain canonical candidate"
        )
        try:
            publish_barrier()
            visible_parent = lstat_optional(live.parent)
            if visible_parent is None or (
                visible_parent.st_dev,
                visible_parent.st_ino,
            ) != (
                opened_parent.st_dev,
                opened_parent.st_ino,
            ):
                raise MigrationError(
                    "publication parent changed before pinned publication"
                )
            require_directory_identity_at(
                parent_fd, live.name, live_identity, "pre-exchange revalidation"
            )
            require_directory_identity_at(
                parent_fd,
                candidate.name,
                candidate_identity,
                "pre-exchange revalidation",
            )
            try:
                rename_exchange(parent_fd, live.name, candidate.name)
            except OSError as error:
                raise MigrationError(
                    "atomic publication exchange failed; "
                    f"live retained at {retained_directory_path(live_owner_fd, str(live))}; "
                    f"candidate retained at {retained_directory_path(candidate_owner_fd, str(candidate))}: {error}"
                ) from error
            exchange_barrier()

            displaced_entries: list[dict[str, object]] | None = None
            try:
                require_directory_identity_at(
                    parent_fd,
                    live.name,
                    candidate_identity,
                    "post-publication verification",
                )
                require_directory_identity_at(
                    parent_fd,
                    candidate.name,
                    live_identity,
                    "post-publication verification",
                )
                live_fd = open_expected_directory(
                    parent_fd,
                    live.name,
                    candidate_identity,
                    "published tree verification",
                )
                candidate_fd = open_expected_directory(
                    parent_fd,
                    candidate.name,
                    live_identity,
                    "displaced tree verification",
                )
                try:
                    published_entries = snapshot_entries_fd(live_fd, live)
                    displaced_entries = snapshot_entries_fd(candidate_fd, candidate)
                finally:
                    os.close(candidate_fd)
                    os.close(live_fd)
                if not equal_after_directory_exchange(
                    displaced_entries, expected.get("entries", [])
                ):
                    raise MigrationError(
                        "displaced live tree differs from the precommit snapshot"
                    )
                if not equal_after_directory_exchange(
                    published_entries, canonical_entries
                ):
                    raise MigrationError(
                        "published tree differs from the validated candidate"
                    )
                visible_parent = lstat_optional(live.parent)
                if visible_parent is None or (
                    visible_parent.st_dev,
                    visible_parent.st_ino,
                ) != (opened_parent.st_dev, opened_parent.st_ino):
                    raise MigrationError(
                        "publication parent changed after pinned publication"
                    )
            except (
                MigrationError,
                OSError,
                UnicodeError,
                ValueError,
            ) as publication_error:
                rollback_barrier()
                if (
                    directory_identity_at(parent_fd, live.name) != candidate_identity
                    or directory_identity_at(parent_fd, candidate.name) != live_identity
                ):
                    raise MigrationError(
                        "atomic publication failed and rollback was refused because a pinned name has a second owner; "
                        f"current entries preserved at {live} and {candidate}; "
                        f"original live retained at {retained_directory_path(live_owner_fd, str(live))}; "
                        f"canonical candidate retained at {retained_directory_path(candidate_owner_fd, str(candidate))}: "
                        f"{publication_error}"
                    ) from publication_error
                try:
                    rename_exchange(parent_fd, live.name, candidate.name)
                except OSError as rollback_error:
                    raise MigrationError(
                        "atomic publication failed and exact exchange rollback failed; "
                        f"recoveries retained at {retained_directory_path(live_owner_fd, str(live))} and "
                        f"{retained_directory_path(candidate_owner_fd, str(candidate))}: "
                        f"{publication_error}; {rollback_error}"
                    ) from publication_error
                try:
                    post_rollback_barrier()
                    require_directory_identity_at(
                        parent_fd,
                        live.name,
                        live_identity,
                        "post-rollback verification",
                    )
                    require_directory_identity_at(
                        parent_fd,
                        candidate.name,
                        candidate_identity,
                        "post-rollback verification",
                    )
                    rollback_live_fd = open_expected_directory(
                        parent_fd,
                        live.name,
                        live_identity,
                        "post-rollback live verification",
                    )
                    rollback_candidate_fd = open_expected_directory(
                        parent_fd,
                        candidate.name,
                        candidate_identity,
                        "post-rollback candidate verification",
                    )
                    try:
                        rollback_live = snapshot_entries_fd(rollback_live_fd, live)
                        rollback_candidate = snapshot_entries_fd(
                            rollback_candidate_fd, candidate
                        )
                    finally:
                        os.close(rollback_candidate_fd)
                        os.close(rollback_live_fd)
                    if not equal_after_directory_exchange(
                        rollback_candidate, canonical_entries
                    ) or (
                        displaced_entries is not None
                        and not equal_after_directory_exchange(
                            rollback_live, displaced_entries
                        )
                    ):
                        raise MigrationError(
                            "the pinned rollback had an uncertain content postcondition"
                        )
                except (
                    MigrationError,
                    OSError,
                    UnicodeError,
                    ValueError,
                ) as post_rollback_error:
                    raise MigrationError(
                        "atomic publication rollback completed but its postcondition changed; "
                        f"original live retained at {retained_directory_path(live_owner_fd, str(live))}; "
                        f"canonical candidate retained at {retained_directory_path(candidate_owner_fd, str(candidate))}: "
                        f"{publication_error}; {post_rollback_error}"
                    ) from post_rollback_error
                raise MigrationError(
                    f"atomic publication rejected and rolled back through exact pinned entries: {publication_error}"
                ) from publication_error
        finally:
            os.close(candidate_owner_fd)
            os.close(live_owner_fd)
    finally:
        os.close(parent_fd)


def main(arguments: list[str]) -> int:
    if len(arguments) < 2:
        raise MigrationError("usage: helper COMMAND ROOT [ARG ...]")
    command = arguments[0]

    if command == "create-owned-temp" and len(arguments) == 4:
        create_owned_temp(arguments[1], Path(arguments[2]), arguments[3])
        return 0
    if command == "find-owned-temp" and len(arguments) == 3:
        find_owned_temp(Path(arguments[1]), arguments[2])
        return 0

    root = Path(os.path.abspath(arguments[1]))

    if command == "completion-status" and len(arguments) == 2:
        print(inspect_completion(root))
        return 0
    if command == "publish-completion" and len(arguments) == 2:
        publish_completion(root)
        return 0
    if command == "needs-migration" and len(arguments) == 2:
        state = inspect_namespace(root)
        return (
            0
            if state["legacy_user"]
            or state["legacy_state"]
            or has_legacy_nix_path(root)
            else 1
        )
    if command == "migrate-stage" and len(arguments) == 2:
        migrate_stage(root)
        return 0
    if command == "validate-migrated" and len(arguments) == 2:
        validate_migrated(root)
        return 0
    if command == "snapshot-live" and len(arguments) == 3:
        write_snapshot(root, Path(arguments[2]))
        return 0
    if command == "precommit" and len(arguments) == 4:
        verify_precommit(
            root,
            Path(os.path.abspath(arguments[2])),
            Path(os.path.abspath(arguments[3])),
        )
        return 0
    if command == "publish" and len(arguments) == 4:
        atomic_publish(
            root,
            Path(os.path.abspath(arguments[2])),
            Path(os.path.abspath(arguments[3])),
        )
        return 0
    raise MigrationError(f"invalid helper invocation: {' '.join(arguments)}")


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except (MigrationError, OSError, UnicodeError, ValueError) as error:
        print(f"Wahrwelt user namespace migration: {error}", file=sys.stderr)
        raise SystemExit(2) from error
