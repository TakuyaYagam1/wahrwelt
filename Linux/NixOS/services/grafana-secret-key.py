"""Create Grafana's secret in one root-owned directory without following names."""

from __future__ import annotations

import ctypes
import errno
import grp
import os
import pwd
import re
import stat
import sys

AT_EMPTY_PATH = 0x1000
SECRET_NAME = "secret_key"
SECRET_PATTERN = re.compile(rb"^[0-9a-f]{64}\n$")
LIBC = ctypes.CDLL(None, use_errno=True)
LIBC.linkat.argtypes = [
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_int,
]
LIBC.linkat.restype = ctypes.c_int


def fail(message: str) -> None:
    raise SystemExit(f"Wahrwelt Grafana secret ownership collision: {message}")


def resolve_uid(value: str) -> int:
    if value.isdecimal():
        return int(value)
    return pwd.getpwnam(value).pw_uid


def resolve_gid(value: str) -> int:
    if value.isdecimal():
        return int(value)
    return grp.getgrnam(value).gr_gid


def identity(info: os.stat_result) -> tuple[int, ...]:
    return (
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


def read_existing(directory_fd: int, expected_uid: int, expected_gid: int) -> bool:
    try:
        visible = os.stat(SECRET_NAME, dir_fd=directory_fd, follow_symlinks=False)
    except FileNotFoundError:
        return False
    if (
        not stat.S_ISREG(visible.st_mode)
        or visible.st_nlink != 1
        or visible.st_uid != expected_uid
        or visible.st_gid != expected_gid
        or stat.S_IMODE(visible.st_mode) != 0o640
    ):
        fail("existing secret must be a single-link root:grafana 0640 regular file")
    flags = os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK
    descriptor = os.open(SECRET_NAME, flags, dir_fd=directory_fd)
    try:
        before = os.fstat(descriptor)
        if identity(before) != identity(visible):
            fail("existing secret changed while being opened")
        payload = os.pread(descriptor, 66, 0)
        after = os.fstat(descriptor)
        final_visible = os.stat(SECRET_NAME, dir_fd=directory_fd, follow_symlinks=False)
        if identity(before) != identity(after) or identity(after) != identity(
            final_visible
        ):
            fail("existing secret changed while being validated")
        if SECRET_PATTERN.fullmatch(payload) is None:
            fail("existing secret has unknown or incomplete content")
    finally:
        os.close(descriptor)
    return True


def write_all(descriptor: int, payload: bytes) -> None:
    offset = 0
    while offset < len(payload):
        written = os.write(descriptor, payload[offset:])
        if written <= 0:
            fail("short write while preparing secret")
        offset += written


def read_migration_source(path: str) -> bytes:
    if (
        not os.path.isabs(path)
        or path.startswith("//")
        or os.path.normpath(path) != path
    ):
        fail("migration source must be one canonical absolute path")
    parent_path = os.path.dirname(path)
    name = os.path.basename(path)
    parent_fd = os.open(
        parent_path,
        os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
    )
    try:
        opened_parent = os.fstat(parent_fd)
        visible_parent = os.lstat(parent_path)
        if not stat.S_ISDIR(opened_parent.st_mode) or identity(
            opened_parent
        ) != identity(visible_parent):
            fail("migration source parent changed while being opened")
        visible = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
        if (
            not stat.S_ISREG(visible.st_mode)
            or visible.st_nlink != 1
            or stat.S_IMODE(visible.st_mode) & 0o022
        ):
            fail("migration source must be a single-link non-writable regular file")
        source_fd = os.open(
            name,
            os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK,
            dir_fd=parent_fd,
        )
        try:
            before = os.fstat(source_fd)
            if identity(before) != identity(visible):
                fail("migration source changed while being opened")
            payload = os.pread(source_fd, 66, 0)
            after = os.fstat(source_fd)
            final_visible = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
            if identity(before) != identity(after) or identity(after) != identity(
                final_visible
            ):
                fail("migration source changed while being validated")
            if SECRET_PATTERN.fullmatch(payload) is None:
                fail("migration source has unknown or incomplete content")
            return payload
        finally:
            os.close(source_fd)
    finally:
        os.close(parent_fd)


def publish_secret(
    directory_fd: int,
    expected_uid: int,
    expected_gid: int,
    payload: bytes | None = None,
) -> None:
    if not hasattr(os, "O_TMPFILE"):
        fail("O_TMPFILE is unavailable")
    descriptor = os.open(
        ".",
        os.O_RDWR | os.O_TMPFILE | os.O_CLOEXEC,
        0o640,
        dir_fd=directory_fd,
    )
    try:
        if payload is None:
            payload = os.urandom(32).hex().encode("ascii") + b"\n"
        if SECRET_PATTERN.fullmatch(payload) is None:
            fail("prepared secret content is invalid")
        write_all(descriptor, payload)
        os.fchown(descriptor, expected_uid, expected_gid)
        os.fchmod(descriptor, 0o640)
        os.fsync(descriptor)
        prepared = os.fstat(descriptor)
        if (
            not stat.S_ISREG(prepared.st_mode)
            or prepared.st_nlink != 0
            or prepared.st_uid != expected_uid
            or prepared.st_gid != expected_gid
            or stat.S_IMODE(prepared.st_mode) != 0o640
            or prepared.st_size != len(payload)
        ):
            fail("prepared secret metadata changed")
        result = LIBC.linkat(
            descriptor,
            b"",
            directory_fd,
            os.fsencode(SECRET_NAME),
            AT_EMPTY_PATH,
        )
        if result != 0:
            error = ctypes.get_errno()
            if error == errno.EEXIST:
                fail("secret appeared during atomic publication and was preserved")
            raise OSError(error, os.strerror(error), SECRET_NAME)
        os.fsync(directory_fd)
        visible = os.stat(SECRET_NAME, dir_fd=directory_fd, follow_symlinks=False)
        published = os.fstat(descriptor)
        if (
            identity(visible) != identity(published)
            or published.st_nlink != 1
            or SECRET_PATTERN.fullmatch(os.pread(descriptor, 66, 0)) is None
        ):
            fail("published secret changed after atomic publication")
    finally:
        os.close(descriptor)


def main() -> None:
    if len(sys.argv) not in (4, 5):
        fail("usage: grafana-secret-key.py DIRECTORY OWNER GROUP [V1_SOURCE]")
    directory, owner, group = sys.argv[1:4]
    migration_source = sys.argv[4] if len(sys.argv) == 5 else ""
    if (
        not os.path.isabs(directory)
        or directory.startswith("//")
        or os.path.normpath(directory) != directory
    ):
        fail("secret directory must be one canonical absolute path")
    expected_uid = resolve_uid(owner)
    expected_gid = resolve_gid(group)
    descriptor = os.open(
        directory,
        os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
    )
    try:
        opened = os.fstat(descriptor)
        visible = os.lstat(directory)
        if (
            not stat.S_ISDIR(opened.st_mode)
            or identity(opened) != identity(visible)
            or opened.st_uid != expected_uid
            or opened.st_gid != expected_gid
            or stat.S_IMODE(opened.st_mode) != 0o750
        ):
            fail("secret directory must be an exact root:grafana 0750 directory")
        if not read_existing(descriptor, expected_uid, expected_gid):
            payload = (
                read_migration_source(migration_source) if migration_source else None
            )
            publish_secret(descriptor, expected_uid, expected_gid, payload)
    finally:
        os.close(descriptor)


if __name__ == "__main__":
    main()
