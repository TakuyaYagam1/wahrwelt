import contextlib
import ctypes
import errno
import os
import re
import secrets
import stat
import sys

RENAME_NOREPLACE = 1
LIBC = ctypes.CDLL(None, use_errno=True)


def fail(message: str, code: int = 1) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(code)


def collision(old: str, new: str, detail: str = "") -> None:
    suffix = f": {detail}" if detail else ""
    fail(
        f"Wahrwelt migration conflict: namespace ownership changed for {old} -> {new}{suffix}"
    )


def inode_id(info: os.stat_result) -> tuple[int, int]:
    return info.st_dev, info.st_ino


def format_id(info: os.stat_result) -> str:
    return f"{info.st_dev}:{info.st_ino}"


def parse_id(value: str) -> tuple[int, int]:
    fields = value.split(":")
    if len(fields) != 2:
        raise ValueError(value)
    return int(fields[0]), int(fields[1])


def rename_noreplace(parent_fd: int, source: str, target: str) -> None:
    result = LIBC.renameat2(
        ctypes.c_int(parent_fd),
        ctypes.c_char_p(os.fsencode(source)),
        ctypes.c_int(parent_fd),
        ctypes.c_char_p(os.fsencode(target)),
        ctypes.c_uint(RENAME_NOREPLACE),
    )
    if result != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error), target)


def split_paths(old: str, new: str) -> tuple[str, str, str]:
    old = os.path.normpath(old)
    new = os.path.normpath(new)
    parent = os.path.dirname(old)
    if os.path.dirname(new) != parent:
        collision(old, new, "paths do not share one parent")
    old_name = os.path.basename(old)
    new_name = os.path.basename(new)
    if (
        old_name in ("", ".", "..")
        or new_name in ("", ".", "..")
        or old_name == new_name
    ):
        collision(old, new, "invalid namespace names")
    return parent, old_name, new_name


def visible_parent_matches(parent: str, expected: tuple[int, int]) -> bool:
    try:
        info = os.lstat(parent)
    except OSError:
        return False
    return stat.S_ISDIR(info.st_mode) and inode_id(info) == expected


def pin_parent(parent: str, old: str, new: str) -> tuple[int, os.stat_result]:
    try:
        fd = os.open(parent, os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    except OSError:
        collision(old, new, "parent is not an ordinary directory")
    info = os.fstat(fd)
    if not visible_parent_matches(parent, inode_id(info)):
        os.close(fd)
        collision(old, new, "parent changed while pinning")
    return fd, info


def pin_nearest_existing_ancestor(
    parent: str, old: str, new: str
) -> tuple[int, os.stat_result, str, str]:
    missing: list[str] = []
    ancestor = parent
    while not os.path.lexists(ancestor):
        next_ancestor = os.path.dirname(ancestor)
        name = os.path.basename(ancestor)
        if not name or next_ancestor == ancestor:
            collision(old, new, "cannot anchor absent parent")
        missing.append(name)
        ancestor = next_ancestor
    if "|" in ancestor or any("|" in component for component in missing):
        collision(old, new, "namespace path cannot contain token delimiters")
    try:
        fd = os.open(
            ancestor, os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
        )
    except OSError:
        collision(old, new, "nearest existing ancestor is not an ordinary directory")
    info = os.fstat(fd)
    if not visible_parent_matches(ancestor, inode_id(info)):
        os.close(fd)
        collision(old, new, "nearest existing ancestor changed while pinning")
    relative = os.path.join(*reversed(missing)) if missing else "."
    return fd, info, ancestor, relative


def open_optional_directory_beneath(
    ancestor_fd: int, relative: str, old: str, new: str
) -> int | None:
    components = [] if relative in ("", ".") else relative.split(os.sep)
    if any(component in ("", ".", "..") for component in components):
        collision(old, new, "invalid absent-parent token path")
    current = os.dup(ancestor_fd)
    try:
        for component in components:
            try:
                following = os.open(
                    component,
                    os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                    dir_fd=current,
                )
            except FileNotFoundError:
                os.close(current)
                return None
            except OSError:
                collision(old, new, "absent parent path became non-directory")
            os.close(current)
            current = following
        return current
    except BaseException:
        with contextlib.suppress(OSError):
            os.close(current)
        raise


def verify_absent_parent(
    old: str,
    new: str,
    fields: list[str],
    with_barrier: bool,
) -> None:
    parent, old_name, _ = split_paths(old, new)
    if len(fields) != 4 or fields[0] != "absent-parent":
        fail("Wahrwelt namespace move received an invalid absent-parent token", 2)
    try:
        expected_ancestor = parse_id(fields[1])
    except (ValueError, OverflowError):
        fail("Wahrwelt namespace move received an invalid absent-parent token", 2)
    ancestor = os.path.normpath(fields[2])
    relative = fields[3]
    if (
        not os.path.isabs(ancestor)
        or "|" in ancestor
        or "|" in relative
        or os.path.normpath(os.path.join(ancestor, relative)) != parent
    ):
        fail("Wahrwelt namespace move received an invalid absent-parent token", 2)
    try:
        ancestor_fd = os.open(
            ancestor,
            os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
        )
    except OSError:
        collision(old, new, "anchored ancestor changed after preflight")
    try:
        if inode_id(
            os.fstat(ancestor_fd)
        ) != expected_ancestor or not visible_parent_matches(
            ancestor, expected_ancestor
        ):
            collision(old, new, "anchored ancestor changed after preflight")
        if with_barrier:
            verify_test_barrier()
        parent_fd = open_optional_directory_beneath(ancestor_fd, relative, old, new)
        if parent_fd is None:
            if not visible_parent_matches(ancestor, expected_ancestor):
                collision(old, new, "anchored ancestor changed during verification")
            return
        try:
            if lstat_at(parent_fd, old_name) is not None:
                collision(
                    old, new, "legacy namespace appeared after absent-parent preflight"
                )
        finally:
            os.close(parent_fd)
        if not visible_parent_matches(ancestor, expected_ancestor):
            collision(old, new, "anchored ancestor changed during verification")
    finally:
        os.close(ancestor_fd)


def lstat_at(parent_fd: int, name: str) -> os.stat_result | None:
    try:
        return os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    except FileNotFoundError:
        return None


def create_owned_directory(parent: str, prefix: str) -> None:
    if not prefix or "/" in prefix or prefix in (".", ".."):
        fail("Wahrwelt temporary directory creator received an invalid prefix", 2)
    inherited = re.fullmatch(r"/proc/self/fd/([0-9]+)", parent)
    try:
        if inherited is not None:
            parent_fd = os.dup(int(inherited.group(1)))
            os.set_inheritable(parent_fd, False)
        else:
            parent_fd = os.open(
                parent,
                os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
            )
        if not stat.S_ISDIR(os.fstat(parent_fd).st_mode):
            raise OSError(errno.ENOTDIR, "not a directory")
    except OSError:
        fail(f"Wahrwelt temporary directory creator cannot pin parent: {parent}")
    try:
        for _ in range(128):
            name = prefix + secrets.token_hex(12)
            try:
                os.mkdir(name, 0o700, dir_fd=parent_fd)
            except FileExistsError:
                continue
            try:
                created_fd = os.open(
                    name,
                    os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                    dir_fd=parent_fd,
                )
            except OSError:
                fail(
                    "Wahrwelt temporary directory changed before creator pin: "
                    + os.path.join(parent, name)
                )
            try:
                created = os.fstat(created_fd)
                visible = lstat_at(parent_fd, name)
                if (
                    visible is None
                    or not stat.S_ISDIR(created.st_mode)
                    or not stat.S_ISDIR(visible.st_mode)
                    or inode_id(created) != inode_id(visible)
                ):
                    fail(
                        "Wahrwelt temporary directory changed before creator handoff: "
                        + os.path.join(parent, name)
                    )
                os.fchmod(created_fd, 0o700)
                print(f"{name}|{format_id(created)}")
                return
            finally:
                os.close(created_fd)
        fail(f"Wahrwelt temporary directory creator exhausted names beneath {parent}")
    finally:
        os.close(parent_fd)


def test_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_NAMESPACE_MOVE_READY_FD", "")
    proceed = os.environ.get("WAHRWELT_TEST_NAMESPACE_MOVE_CONTINUE_FD", "")
    if not ready and not proceed:
        return
    if not ready or not proceed:
        fail("incomplete Wahrwelt namespace move test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        fail("closed Wahrwelt namespace move test barrier")


def verify_test_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_NAMESPACE_VERIFY_READY_FD", "")
    proceed = os.environ.get("WAHRWELT_TEST_NAMESPACE_VERIFY_CONTINUE_FD", "")
    if not ready and not proceed:
        return
    if not ready or not proceed:
        fail("incomplete Wahrwelt namespace verification test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        fail("closed Wahrwelt namespace verification test barrier")


def check(old: str, new: str) -> None:
    parent, old_name, new_name = split_paths(old, new)
    if not os.path.lexists(parent):
        if os.path.lexists(old):
            collision(old, new, "legacy namespace has no ordinary parent")
        ancestor_fd, ancestor_info, ancestor, relative = pin_nearest_existing_ancestor(
            parent, old, new
        )
        try:
            if os.path.lexists(old) or not visible_parent_matches(
                ancestor, inode_id(ancestor_info)
            ):
                collision(old, new, "absent parent changed during preflight")
            print(
                "absent-parent|"
                + format_id(ancestor_info)
                + "|"
                + ancestor
                + "|"
                + relative
            )
            return
        finally:
            os.close(ancestor_fd)
    parent_fd, parent_info = pin_parent(parent, old, new)
    try:
        source = lstat_at(parent_fd, old_name)
        target = lstat_at(parent_fd, new_name)
        parent_id = inode_id(parent_info)
        if source is None:
            if not visible_parent_matches(parent, parent_id):
                collision(old, new, "parent changed during preflight")
            print(f"absent|{format_id(parent_info)}")
            return
        if not stat.S_ISDIR(source.st_mode):
            collision(old, new, "source is not an ordinary directory")
        if target is not None:
            collision(old, new, "legacy and canonical namespaces coexist")
        if not visible_parent_matches(parent, parent_id):
            collision(old, new, "parent changed during preflight")
        print(f"present|{format_id(parent_info)}|{format_id(source)}")
    finally:
        os.close(parent_fd)


def move(old: str, new: str, token: str) -> None:
    parent, old_name, new_name = split_paths(old, new)
    fields = token.split("|")
    if fields[0] == "absent-parent":
        verify_absent_parent(old, new, fields, False)
        return
    if len(fields) not in (2, 3) or fields[0] not in ("absent", "present"):
        fail("Wahrwelt namespace move received an invalid preflight token", 2)
    try:
        expected_parent = parse_id(fields[1])
        expected_source = parse_id(fields[2]) if fields[0] == "present" else None
    except (ValueError, OverflowError):
        fail("Wahrwelt namespace move received an invalid preflight token", 2)
    parent_fd, parent_info = pin_parent(parent, old, new)
    try:
        if inode_id(parent_info) != expected_parent:
            collision(old, new, "parent changed after preflight")
        source = lstat_at(parent_fd, old_name)
        if expected_source is None:
            if source is not None:
                collision(old, new, "legacy namespace appeared after preflight")
            if not visible_parent_matches(parent, expected_parent):
                collision(old, new, "parent changed after preflight")
            return
        target = lstat_at(parent_fd, new_name)
        if (
            source is None
            or not stat.S_ISDIR(source.st_mode)
            or inode_id(source) != expected_source
        ):
            collision(old, new, "source changed after preflight")
        if target is not None:
            collision(old, new, "canonical namespace appeared after preflight")
        if not visible_parent_matches(parent, expected_parent):
            collision(old, new, "parent changed before commit")
        test_barrier()
        try:
            rename_noreplace(parent_fd, old_name, new_name)
        except OSError:
            collision(old, new, "canonical namespace appeared during commit")

        moved = lstat_at(parent_fd, new_name)
        anchored_ok = (
            lstat_at(parent_fd, old_name) is None
            and moved is not None
            and stat.S_ISDIR(moved.st_mode)
            and inode_id(moved) == expected_source
        )
        visible_ok = False
        if visible_parent_matches(parent, expected_parent):
            try:
                visible_target = os.lstat(new)
                visible_ok = (
                    stat.S_ISDIR(visible_target.st_mode)
                    and inode_id(visible_target) == expected_source
                )
            except OSError:
                pass
        if anchored_ok and visible_ok:
            return

        restored = False
        moved_id = inode_id(moved) if moved is not None else None
        if moved_id is not None and lstat_at(parent_fd, old_name) is None:
            try:
                rename_noreplace(parent_fd, new_name, old_name)
                restored_source = lstat_at(parent_fd, old_name)
                restored = (
                    restored_source is not None
                    and inode_id(restored_source) == moved_id
                    and lstat_at(parent_fd, new_name) is None
                )
            except OSError:
                pass
        if restored:
            if moved_id == expected_source:
                collision(
                    old,
                    new,
                    "commit parent changed; move rolled back through pinned parent",
                )
            collision(
                old,
                new,
                "source changed during commit; concurrent replacement restored",
            )
        try:
            pinned_parent = os.readlink(f"/proc/self/fd/{parent_fd}")
        except OSError:
            pinned_parent = parent
        fail(
            "Wahrwelt namespace move rollback incomplete; recovery retained at "
            + os.path.join(pinned_parent, new_name)
        )
    finally:
        os.close(parent_fd)


def verify(old: str, new: str, token: str) -> None:
    parent, old_name, new_name = split_paths(old, new)
    fields = token.split("|")
    if fields[0] == "absent-parent":
        verify_absent_parent(old, new, fields, True)
        return
    if len(fields) not in (2, 3) or fields[0] not in ("absent", "present"):
        fail("Wahrwelt namespace verification received an invalid preflight token", 2)
    try:
        expected_parent = parse_id(fields[1])
        expected_source = parse_id(fields[2]) if fields[0] == "present" else None
    except (ValueError, OverflowError):
        fail("Wahrwelt namespace verification received an invalid preflight token", 2)

    parent_fd, parent_info = pin_parent(parent, old, new)
    try:
        if inode_id(parent_info) != expected_parent:
            collision(old, new, "parent changed before final verification")
        verify_test_barrier()
        source = lstat_at(parent_fd, old_name)
        if source is not None:
            collision(old, new, "legacy namespace remains after migration")
        if expected_source is not None:
            target = lstat_at(parent_fd, new_name)
            if (
                target is None
                or not stat.S_ISDIR(target.st_mode)
                or inode_id(target) != expected_source
            ):
                collision(old, new, "published namespace identity changed")
        if not visible_parent_matches(parent, expected_parent):
            collision(old, new, "parent changed during final verification")
    finally:
        os.close(parent_fd)


def verify_no_legacy_markers(root: str, excluded_roots: list[str]) -> None:
    root = os.path.normpath(root)
    excluded = [os.path.normpath(path) for path in excluded_roots]
    try:
        root_info = os.lstat(root)
    except FileNotFoundError:
        return
    except OSError:
        fail(f"Wahrwelt migration conflict: marker root is unavailable: {root}")
    if not stat.S_ISDIR(root_info.st_mode):
        fail(
            f"Wahrwelt migration conflict: marker root is not an ordinary directory: {root}"
        )
    for current, directories, files in os.walk(root, followlinks=False):
        for name in directories + files:
            if name != ".mysetup-managed.json":
                continue
            marker = os.path.join(current, name)
            if any(
                marker == excluded_root or marker.startswith(excluded_root + os.sep)
                for excluded_root in excluded
            ):
                continue
            fail(
                "Wahrwelt migration conflict: legacy marker appeared after preflight: "
                + marker
            )


def main() -> None:
    if len(sys.argv) >= 4 and sys.argv[1] == "verify-markers":
        verify_no_legacy_markers(sys.argv[2], sys.argv[3:])
        return
    if len(sys.argv) not in (4, 5):
        fail(f"usage: {sys.argv[0]} check OLD NEW | move|verify OLD NEW TOKEN", 2)
    if sys.argv[1] == "create-directory" and len(sys.argv) == 4:
        create_owned_directory(sys.argv[2], sys.argv[3])
        return
    command_name, old, new = sys.argv[1:4]
    if command_name == "check" and len(sys.argv) == 4:
        check(old, new)
        return
    if command_name == "move" and len(sys.argv) == 5:
        move(old, new, sys.argv[4])
        return
    if command_name == "verify" and len(sys.argv) == 5:
        verify(old, new, sys.argv[4])
        return
    fail("Wahrwelt namespace move received an invalid command", 2)


if __name__ == "__main__":
    main()
