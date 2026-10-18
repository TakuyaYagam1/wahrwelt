import ctypes
import errno
import hashlib
import os
import re
import secrets
import stat
import sys
from contextlib import contextmanager

RENAME_NOREPLACE = 1
STORE_MANAGED_PATHS = {
    ".config/hypr/lib/mysetup.lua",
    ".config/quickshell/mysetup-shell-selector",
}
STALE_END4_LINKS = {
    ".config/hypr/monitors.conf": (
        "hypr/monitors.conf",
        "hypr/end4/monitors.conf",
    ),
    ".config/hypr/workspaces.conf": (
        "hypr/workspaces.conf",
        "hypr/end4/workspaces.conf",
    ),
}
END4_APP_LINK_TYPES = {
    ".config/kitty": "directory",
    ".config/fuzzel": "directory",
    ".config/kdeglobals": "regular",
    ".local/share/konsole/Profile 1.profile": "regular",
}
SUPPORTED_PATHS = (
    STORE_MANAGED_PATHS | STALE_END4_LINKS.keys() | END4_APP_LINK_TYPES.keys()
)
HISTORICAL_STORE = re.compile(
    r"^/nix/store/[0-9abcdfghijklmnpqrsvwxyz]{32}-home-manager-files/(.+)$"
)
LIBC = ctypes.CDLL(None, use_errno=True)


@contextmanager
def closing_fd(fd: int):
    try:
        yield fd
    finally:
        os.close(fd)


def fail(message: str, code: int = 1) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(code)


def ownership_collision(target: str) -> None:
    fail(f"Wahrwelt legacy link ownership collision: {target}")


def inode_id(info: os.stat_result) -> tuple[int, int]:
    return info.st_dev, info.st_ino


def format_id(info: os.stat_result) -> str:
    return f"{info.st_dev}:{info.st_ino}"


def parse_id(value: str) -> tuple[int, int]:
    fields = value.split(":")
    if len(fields) != 2:
        raise ValueError(value)
    return int(fields[0]), int(fields[1])


def target_digest(target: str) -> str:
    return hashlib.sha256(os.fsencode(target)).hexdigest()


def rename_noreplace(source_fd: int, source: str, target_fd: int, target: str) -> None:
    result = LIBC.renameat2(
        ctypes.c_int(source_fd),
        ctypes.c_char_p(os.fsencode(source)),
        ctypes.c_int(target_fd),
        ctypes.c_char_p(os.fsencode(target)),
        ctypes.c_uint(RENAME_NOREPLACE),
    )
    if result != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error), target)


def normalized_absolute(path: str) -> str | None:
    if not os.path.isabs(path):
        return None
    return os.path.normpath(path)


def generation_candidates(generation: str, expected_relative: str) -> set[str]:
    if not generation:
        return set()
    candidates = {
        os.path.normpath(os.path.join(generation, "home-files", expected_relative))
    }
    if os.path.exists(generation):
        resolved = os.path.realpath(generation)
        candidates.add(
            os.path.normpath(os.path.join(resolved, "home-files", expected_relative))
        )
    home_files = os.path.join(generation, "home-files")
    if os.path.exists(home_files):
        candidates.add(
            os.path.normpath(
                os.path.join(os.path.realpath(home_files), expected_relative)
            )
        )
    return candidates


def is_managed_target(
    raw_target: str,
    display_path: str,
    expected_relative: str,
    generations: tuple[str, str],
    recovery_parent: str,
) -> bool:
    stale_end4 = STALE_END4_LINKS.get(expected_relative)
    if stale_end4 is not None:
        return raw_target == os.path.join(
            os.path.normpath(recovery_parent), stale_end4[1]
        )
    clean = normalized_absolute(raw_target)
    if clean is None:
        clean = normalized_absolute(
            os.path.join(os.path.dirname(display_path), raw_target)
        )
    if clean is None:
        return False
    historical = HISTORICAL_STORE.fullmatch(clean)
    expected_type = END4_APP_LINK_TYPES.get(expected_relative)
    if expected_type is not None:
        if historical is None or historical.group(1) != expected_relative:
            return False
        generation = clean[: -(len(expected_relative) + 1)]
        try:
            asset = os.stat(clean)
            quickshell_marker = os.stat(
                os.path.join(generation, ".config/quickshell/ii/shell.qml")
            )
            hypr_marker = os.stat(
                os.path.join(generation, ".config/hypr/end4/hyprland.conf")
            )
        except OSError:
            return False
        if not stat.S_ISREG(quickshell_marker.st_mode) or not stat.S_ISREG(
            hypr_marker.st_mode
        ):
            return False
        if expected_type == "directory":
            return stat.S_ISDIR(asset.st_mode)
        return stat.S_ISREG(asset.st_mode)
    if historical is not None and historical.group(1) == expected_relative:
        return True
    return any(
        clean in generation_candidates(generation, expected_relative)
        for generation in generations
    )


def is_expected_mutable_target(info: os.stat_result, expected_relative: str) -> bool:
    expected_type = END4_APP_LINK_TYPES.get(expected_relative)
    if expected_type == "directory":
        return stat.S_ISDIR(info.st_mode)
    if expected_type == "regular":
        return stat.S_ISREG(info.st_mode)
    return False


def classify_public_target(
    target: str,
    expected_relative: str,
    generations: tuple[str, str],
    recovery_parent: str,
) -> bool:
    try:
        info = os.lstat(target)
    except FileNotFoundError:
        return False
    if not stat.S_ISLNK(info.st_mode):
        ownership_collision(target)
    if not is_managed_target(
        os.readlink(target), target, expected_relative, generations, recovery_parent
    ):
        ownership_collision(target)
    return True


def open_ordinary_directory(path: str) -> int:
    return os.open(path, os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)


def open_directory_beneath(root_fd: int, relative: str) -> int:
    components = [] if relative in ("", ".") else relative.split(os.sep)
    if any(component in ("", ".", "..") for component in components):
        raise OSError(errno.EINVAL, "invalid relative directory", relative)
    current = os.dup(root_fd)
    try:
        for component in components:
            following = os.open(
                component,
                os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                dir_fd=current,
            )
            os.close(current)
            current = following
        return current
    except BaseException:
        os.close(current)
        raise


def pin_nearest_existing_ancestor(
    path: str, target: str
) -> tuple[int, os.stat_result, str, str]:
    missing: list[str] = []
    ancestor = path
    while not os.path.lexists(ancestor):
        next_ancestor = os.path.dirname(ancestor)
        name = os.path.basename(ancestor)
        if not name or next_ancestor == ancestor:
            ownership_collision(target)
        missing.append(name)
        ancestor = next_ancestor
    if "|" in ancestor or any("|" in component for component in missing):
        ownership_collision(target)
    try:
        fd = open_ordinary_directory(ancestor)
    except OSError:
        ownership_collision(target)
    info = os.fstat(fd)
    if not visible_directory_matches(ancestor, inode_id(info)):
        os.close(fd)
        ownership_collision(target)
    relative = os.path.join(*reversed(missing))
    return fd, info, ancestor, relative


def verify_absent_root(target: str, recovery_parent: str, fields: list[str]) -> None:
    if len(fields) != 4 or fields[0] != "absent-root":
        fail("Wahrwelt legacy link guard received an invalid absent-root token", 2)
    try:
        expected_ancestor = parse_id(fields[1])
    except (ValueError, OverflowError):
        fail("Wahrwelt legacy link guard received an invalid absent-root token", 2)
    ancestor = os.path.normpath(fields[2])
    relative = fields[3]
    if (
        not os.path.isabs(ancestor)
        or "|" in ancestor
        or "|" in relative
        or os.path.normpath(os.path.join(ancestor, relative)) != recovery_parent
    ):
        fail("Wahrwelt legacy link guard received an invalid absent-root token", 2)
    try:
        ancestor_fd = open_ordinary_directory(ancestor)
    except OSError:
        ownership_collision(target)
    with closing_fd(ancestor_fd):
        if inode_id(
            os.fstat(ancestor_fd)
        ) != expected_ancestor or not visible_directory_matches(
            ancestor, expected_ancestor
        ):
            ownership_collision(target)
        test_barrier()
        try:
            root_fd = open_directory_beneath(ancestor_fd, relative)
        except FileNotFoundError:
            if not visible_directory_matches(ancestor, expected_ancestor):
                ownership_collision(target)
            return
        except OSError:
            ownership_collision(target)
        else:
            os.close(root_fd)
            ownership_collision(target)


def visible_directory_matches(path: str, expected: tuple[int, int]) -> bool:
    try:
        info = os.lstat(path)
    except OSError:
        return False
    return stat.S_ISDIR(info.st_mode) and inode_id(info) == expected


def test_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_LINK_GUARD_READY_FD", "")
    proceed = os.environ.get("WAHRWELT_TEST_LINK_GUARD_CONTINUE_FD", "")
    if not ready and not proceed:
        return
    if not ready or not proceed:
        fail("incomplete Wahrwelt legacy link guard test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        fail("closed Wahrwelt legacy link guard test barrier")


def move_test_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_LINK_MOVE_READY_FD", "")
    proceed = os.environ.get("WAHRWELT_TEST_LINK_MOVE_CONTINUE_FD", "")
    if not ready and not proceed:
        return
    if not ready or not proceed:
        fail("incomplete Wahrwelt legacy link move test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        fail("closed Wahrwelt legacy link move test barrier")


def pinned_target(
    parent_fd: int,
    name: str,
    display_path: str,
    expected_relative: str,
    generations: tuple[str, str],
    recovery_parent: str,
) -> tuple[os.stat_result, str]:
    try:
        info = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    except FileNotFoundError:
        ownership_collision(display_path)
    if not stat.S_ISLNK(info.st_mode):
        ownership_collision(display_path)
    raw_target = os.readlink(name, dir_fd=parent_fd)
    if not is_managed_target(
        raw_target,
        display_path,
        expected_relative,
        generations,
        recovery_parent,
    ):
        ownership_collision(display_path)
    return info, raw_target


def target_coordinates(target: str, recovery_parent: str) -> tuple[str, str, str, str]:
    root_path = os.path.normpath(recovery_parent)
    target_path = os.path.normpath(target)
    try:
        relative = os.path.relpath(target_path, root_path)
    except ValueError:
        ownership_collision(target)
    if (
        relative == ".."
        or relative.startswith(".." + os.sep)
        or os.path.isabs(relative)
    ):
        ownership_collision(target)
    target_name = os.path.basename(relative)
    relative_parent = os.path.dirname(relative)
    if target_name in ("", ".", ".."):
        ownership_collision(target)
    return root_path, target_path, relative_parent, target_name


def check_with_token(
    target: str,
    expected_relative: str,
    generations: tuple[str, str],
    recovery_parent: str,
) -> None:
    root_path, _, relative_parent, target_name = target_coordinates(
        target, recovery_parent
    )
    if not os.path.lexists(root_path):
        if os.path.lexists(target):
            ownership_collision(target)
        ancestor_fd, ancestor_info, ancestor, relative = pin_nearest_existing_ancestor(
            root_path, target
        )
        with closing_fd(ancestor_fd):
            if os.path.lexists(root_path) or not visible_directory_matches(
                ancestor, inode_id(ancestor_info)
            ):
                ownership_collision(target)
            print(
                "absent-root|"
                + format_id(ancestor_info)
                + "|"
                + ancestor
                + "|"
                + relative
            )
            return
    try:
        root_fd = open_ordinary_directory(root_path)
    except OSError:
        fail(
            f"Wahrwelt legacy link recovery parent is not an ordinary directory: {root_path}"
        )
    with closing_fd(root_fd):
        root_info = os.fstat(root_fd)
        root_id = inode_id(root_info)
        if not visible_directory_matches(root_path, root_id):
            fail(f"Wahrwelt legacy link recovery parent changed: {root_path}")
        try:
            parent_fd = open_directory_beneath(root_fd, relative_parent)
        except FileNotFoundError:
            if os.path.lexists(target):
                ownership_collision(target)
            print(f"absent-parent|{format_id(root_info)}")
            return
        except OSError:
            ownership_collision(target)
        with closing_fd(parent_fd):
            parent_info = os.fstat(parent_fd)
            try:
                link_info = os.stat(
                    target_name, dir_fd=parent_fd, follow_symlinks=False
                )
            except FileNotFoundError:
                if not visible_directory_matches(root_path, root_id):
                    fail(f"Wahrwelt legacy link recovery parent changed: {root_path}")
                print(f"absent|{format_id(root_info)}|{format_id(parent_info)}")
                return
            if is_expected_mutable_target(link_info, expected_relative):
                if not visible_directory_matches(root_path, root_id):
                    fail(f"Wahrwelt legacy link recovery parent changed: {root_path}")
                print(
                    "preserved|"
                    + format_id(root_info)
                    + "|"
                    + format_id(parent_info)
                    + "|"
                    + format_id(link_info)
                )
                return
            link_info, raw_target = pinned_target(
                parent_fd,
                target_name,
                target,
                expected_relative,
                generations,
                recovery_parent,
            )
            if not visible_directory_matches(root_path, root_id):
                fail(f"Wahrwelt legacy link recovery parent changed: {root_path}")
            print(
                "present|"
                + format_id(root_info)
                + "|"
                + format_id(parent_info)
                + "|"
                + format_id(link_info)
                + "|"
                + target_digest(raw_target)
            )


def quarantine(
    target: str,
    expected_relative: str,
    generations: tuple[str, str],
    recovery_parent: str,
    token: str,
) -> None:
    root_path, _, relative_parent, target_name = target_coordinates(
        target, recovery_parent
    )
    fields = token.split("|")
    if fields and fields[0] == "absent-root":
        verify_absent_root(target, root_path, fields)
        return
    if not fields or fields[0] not in (
        "absent-parent",
        "absent",
        "preserved",
        "present",
    ):
        fail("Wahrwelt legacy link guard received an invalid preflight token", 2)
    try:
        expected_root = parse_id(fields[1])
        expected_parent = (
            parse_id(fields[2])
            if fields[0] in ("absent", "preserved", "present")
            else None
        )
        expected_link = (
            parse_id(fields[3]) if fields[0] in ("preserved", "present") else None
        )
        expected_target_digest = fields[4] if fields[0] == "present" else ""
    except (IndexError, ValueError, OverflowError):
        fail("Wahrwelt legacy link guard received an invalid preflight token", 2)
    expected_length = {
        "absent-parent": 2,
        "absent": 3,
        "preserved": 4,
        "present": 5,
    }[fields[0]]
    if len(fields) != expected_length:
        fail("Wahrwelt legacy link guard received an invalid preflight token", 2)

    try:
        root_fd = open_ordinary_directory(root_path)
    except OSError:
        fail(
            f"Wahrwelt legacy link recovery parent is not an ordinary directory: {root_path}"
        )
    with closing_fd(root_fd):
        root_id = inode_id(os.fstat(root_fd))
        if root_id != expected_root or not visible_directory_matches(
            root_path, root_id
        ):
            fail(
                f"Wahrwelt legacy link recovery parent changed after preflight: {root_path}"
            )
        if fields[0] == "absent-parent":
            try:
                unexpected_parent = open_directory_beneath(root_fd, relative_parent)
            except FileNotFoundError:
                return
            except OSError:
                ownership_collision(target)
            else:
                os.close(unexpected_parent)
                ownership_collision(target)
        test_barrier()
        try:
            parent_fd = open_directory_beneath(root_fd, relative_parent)
        except OSError:
            ownership_collision(target)
        with closing_fd(parent_fd):
            parent_id = inode_id(os.fstat(parent_fd))
            if parent_id != expected_parent:
                ownership_collision(target)
            if fields[0] == "absent":
                try:
                    os.stat(target_name, dir_fd=parent_fd, follow_symlinks=False)
                except FileNotFoundError:
                    return
                ownership_collision(target)
            if fields[0] == "preserved":
                try:
                    preserved_info = os.stat(
                        target_name,
                        dir_fd=parent_fd,
                        follow_symlinks=False,
                    )
                except FileNotFoundError:
                    ownership_collision(target)
                if (
                    inode_id(preserved_info) != expected_link
                    or not is_expected_mutable_target(
                        preserved_info,
                        expected_relative,
                    )
                    or not visible_directory_matches(root_path, root_id)
                ):
                    ownership_collision(target)
                return
            before_info, before_target = pinned_target(
                parent_fd,
                target_name,
                target,
                expected_relative,
                generations,
                recovery_parent,
            )
            before_id = inode_id(before_info)
            if (
                before_id != expected_link
                or target_digest(before_target) != expected_target_digest
            ):
                ownership_collision(target)

            for _ in range(128):
                recovery_name = (
                    ".wahrwelt-migration-recovery-links-" + secrets.token_hex(12)
                )
                try:
                    os.mkdir(recovery_name, 0o700, dir_fd=root_fd)
                    break
                except FileExistsError:
                    continue
            else:
                fail(
                    f"Wahrwelt could not allocate legacy link recovery beneath {root_path}"
                )
            recovery_path = os.path.join(root_path, recovery_name)
            recovery_fd = os.open(
                recovery_name,
                os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                dir_fd=root_fd,
            )
            with closing_fd(recovery_fd):
                recovery_id = inode_id(os.fstat(recovery_fd))
                move_test_barrier()
                try:
                    rename_noreplace(parent_fd, target_name, recovery_fd, "legacy-link")
                except OSError:
                    fail(f"Wahrwelt legacy link changed before quarantine: {target}")

                moved_ok = False
                moved_info = None
                try:
                    moved_info = os.stat(
                        "legacy-link", dir_fd=recovery_fd, follow_symlinks=False
                    )
                    moved_target = os.readlink("legacy-link", dir_fd=recovery_fd)
                    moved_ok = (
                        stat.S_ISLNK(moved_info.st_mode)
                        and inode_id(moved_info) == before_id
                        and moved_target == before_target
                    )
                except OSError:
                    pass

                parent_visible = False
                try:
                    fresh_parent = open_directory_beneath(root_fd, relative_parent)
                    try:
                        parent_visible = inode_id(os.fstat(fresh_parent)) == parent_id
                    finally:
                        os.close(fresh_parent)
                except OSError:
                    pass
                try:
                    recovery_visible = (
                        inode_id(
                            os.stat(
                                recovery_name, dir_fd=root_fd, follow_symlinks=False
                            )
                        )
                        == recovery_id
                    )
                except OSError:
                    recovery_visible = False
                publication_ok = (
                    moved_ok
                    and visible_directory_matches(root_path, root_id)
                    and parent_visible
                    and recovery_visible
                )
                if publication_ok:
                    print(recovery_path)
                    return

                restored = False
                try:
                    os.stat(target_name, dir_fd=parent_fd, follow_symlinks=False)
                except FileNotFoundError:
                    if moved_info is not None:
                        try:
                            rename_noreplace(
                                recovery_fd, "legacy-link", parent_fd, target_name
                            )
                            restored_info = os.stat(
                                target_name,
                                dir_fd=parent_fd,
                                follow_symlinks=False,
                            )
                            try:
                                os.stat(
                                    "legacy-link",
                                    dir_fd=recovery_fd,
                                    follow_symlinks=False,
                                )
                                recovery_absent = False
                            except FileNotFoundError:
                                recovery_absent = True
                            restored = (
                                inode_id(restored_info) == inode_id(moved_info)
                                and recovery_absent
                            )
                        except OSError:
                            restored = False
                if restored:
                    if moved_ok:
                        fail(
                            f"Wahrwelt legacy link parent changed; original link restored in pinned parent: {target}"
                        )
                    fail(
                        f"Wahrwelt legacy link changed during quarantine; concurrent replacement restored: {target}"
                    )
                fail(
                    f"Wahrwelt legacy link parent changed; recovery retained in pinned inode at {recovery_path}"
                )


def main() -> None:
    if len(sys.argv) not in (7, 8):
        fail(
            f"usage: {sys.argv[0]} check TARGET EXPECTED_RELATIVE OLD_GENERATION CURRENT_GENERATION RECOVERY_PARENT | quarantine TARGET EXPECTED_RELATIVE OLD_GENERATION CURRENT_GENERATION RECOVERY_PARENT TOKEN",
            2,
        )
    command_name, target, expected_relative, old_generation, current_generation = (
        sys.argv[1:6]
    )
    if expected_relative not in SUPPORTED_PATHS:
        fail(
            f"Wahrwelt legacy link guard received an unsupported managed path: {expected_relative}",
            2,
        )
    stale_end4 = STALE_END4_LINKS.get(expected_relative)
    if stale_end4 is not None:
        expected_public = os.path.join(os.path.normpath(sys.argv[6]), stale_end4[0])
        if target != expected_public:
            ownership_collision(target)
    generations = old_generation, current_generation
    if command_name == "check" and len(sys.argv) == 7 and sys.argv[6]:
        check_with_token(target, expected_relative, generations, sys.argv[6])
        return
    if (
        command_name != "quarantine"
        or len(sys.argv) != 8
        or not sys.argv[6]
        or not sys.argv[7]
    ):
        fail("Wahrwelt legacy link guard received an invalid command", 2)
    quarantine(target, expected_relative, generations, sys.argv[6], sys.argv[7])


if __name__ == "__main__":
    main()
