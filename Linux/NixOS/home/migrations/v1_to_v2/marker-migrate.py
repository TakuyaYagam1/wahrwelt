import ctypes
import json
import os
import secrets
import stat
import sys

AT_EMPTY_PATH = 0x1000
RENAME_NOREPLACE = 1
LIBC = ctypes.CDLL(None, use_errno=True)
MARKER_VERSION = 2


class MarkerRemovalError(RuntimeError):
    pass


def fail(message: str, code: int = 1) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(code)


def collision(old: str, new: str, detail: str) -> None:
    fail(
        f"Wahrwelt migration conflict: marker ownership changed for {old} -> {new}: {detail}"
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


def split_paths(old: str, new: str) -> tuple[str, str, str]:
    old = os.path.normpath(old)
    new = os.path.normpath(new)
    parent = os.path.dirname(old)
    if os.path.dirname(new) != parent:
        collision(old, new, "paths do not share one parent")
    old_name = os.path.basename(old)
    new_name = os.path.basename(new)
    if old_name != ".mysetup-managed.json" or new_name != ".wahrwelt-managed.json":
        collision(old, new, "unsupported marker names")
    return parent, old_name, new_name


def expected_kind_for(
    parent: str, config_home: str, home: str, old: str, new: str
) -> str:
    config_home = os.path.normpath(config_home)
    home = os.path.normpath(home)
    if not all(os.path.isabs(path) for path in (parent, config_home, home)):
        collision(old, new, "marker paths, config home, and home must be absolute")
    config_relative = os.path.relpath(parent, config_home).split(os.sep)
    if config_relative == ["hypr"]:
        return "hypr"
    if config_relative == ["nvim"]:
        return "nvim"
    zen_relative = os.path.relpath(parent, os.path.join(home, ".zen")).split(os.sep)
    if (
        len(zen_relative) == 2
        and zen_relative[0] not in ("", ".", "..")
        and zen_relative[1] == "chrome"
    ):
        return "zen-chrome"
    collision(old, new, "unsupported managed marker path")


def marker_matches(payload: dict, manager: str, expected_kind: str) -> bool:
    return (
        payload.get("manager") == manager
        and type(payload.get("version")) is int
        and payload["version"] == MARKER_VERSION
        and payload.get("kind") == expected_kind
    )


def require_marker_schema(
    payload: dict,
    manager: str,
    expected_kind: str,
    old: str,
    new: str,
    role: str,
) -> None:
    if payload.get("manager") != manager:
        collision(old, new, f"{role} marker manager is not exactly {manager}")
    if type(payload.get("version")) is not int or payload["version"] != MARKER_VERSION:
        collision(old, new, f"{role} marker version is not exactly {MARKER_VERSION}")
    if payload.get("kind") != expected_kind:
        collision(old, new, f"{role} marker kind is not exactly {expected_kind}")


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


def lstat_at(parent_fd: int, name: str) -> os.stat_result | None:
    try:
        return os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    except FileNotFoundError:
        return None


def read_marker(parent_fd: int, name: str, display: str) -> tuple[os.stat_result, dict]:
    info = lstat_at(parent_fd, name)
    if info is None or not stat.S_ISREG(info.st_mode):
        collision(display, display, "marker is not an ordinary regular file")
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent_fd)
    except OSError:
        collision(display, display, "marker changed while opening")
    try:
        opened = os.fstat(fd)
        if inode_id(opened) != inode_id(info) or not stat.S_ISREG(opened.st_mode):
            collision(display, display, "marker changed while opening")
        chunks = []
        while True:
            chunk = os.read(fd, 65536)
            if not chunk:
                break
            chunks.append(chunk)
    finally:
        os.close(fd)
    try:
        payload = json.loads(b"".join(chunks))
    except (UnicodeDecodeError, json.JSONDecodeError):
        collision(display, display, "marker is not valid JSON")
    if not isinstance(payload, dict):
        collision(display, display, "marker JSON is not an object")
    return info, payload


def link_anonymous(candidate_fd: int, parent_fd: int, target_name: str) -> None:
    result = LIBC.linkat(
        ctypes.c_int(candidate_fd),
        ctypes.c_char_p(b""),
        ctypes.c_int(parent_fd),
        ctypes.c_char_p(os.fsencode(target_name)),
        ctypes.c_int(AT_EMPTY_PATH),
    )
    if result != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error), target_name)


def rename_noreplace(
    source_fd: int, source_name: str, target_fd: int, target_name: str
) -> None:
    renameat2 = getattr(LIBC, "renameat2", None)
    if renameat2 is None:
        raise MarkerRemovalError("renameat2 is unavailable")
    result = renameat2(
        ctypes.c_int(source_fd),
        ctypes.c_char_p(os.fsencode(source_name)),
        ctypes.c_int(target_fd),
        ctypes.c_char_p(os.fsencode(target_name)),
        ctypes.c_uint(RENAME_NOREPLACE),
    )
    if result != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error), f"{source_name} -> {target_name}")


def test_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_MARKER_READY_FD", "")
    proceed = os.environ.get("WAHRWELT_TEST_MARKER_CONTINUE_FD", "")
    if not ready and not proceed:
        return
    if not ready or not proceed:
        fail("incomplete Wahrwelt marker migration test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        fail("closed Wahrwelt marker migration test barrier")


def removal_test_barrier() -> None:
    ready = os.environ.get("WAHRWELT_TEST_MARKER_REMOVE_READY_FD", "")
    proceed = os.environ.get("WAHRWELT_TEST_MARKER_REMOVE_CONTINUE_FD", "")
    if not ready and not proceed:
        return
    if not ready or not proceed:
        raise MarkerRemovalError("incomplete Wahrwelt marker removal test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        raise MarkerRemovalError("closed Wahrwelt marker removal test barrier")


def remove_exact_legacy_marker(
    parent_fd: int,
    parent: str,
    expected_parent: tuple[int, int],
    old_name: str,
    old_display: str,
    expected_old: tuple[int, int],
    expected_kind: str,
) -> str:
    for _ in range(128):
        recovery_name = ".wahrwelt-migration-recovery-marker-" + secrets.token_hex(12)
        try:
            os.mkdir(recovery_name, 0o700, dir_fd=parent_fd)
            break
        except FileExistsError:
            continue
    else:
        raise MarkerRemovalError("could not allocate marker recovery")
    recovery_path = os.path.join(parent, recovery_name)
    recovery_fd = os.open(
        recovery_name,
        os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
        dir_fd=parent_fd,
    )
    try:
        inode_id(os.fstat(recovery_fd))
        try:
            current_old, current_payload = read_marker(parent_fd, old_name, old_display)
        except SystemExit as error:
            raise MarkerRemovalError(
                "legacy marker changed before exact removal"
            ) from error
        if inode_id(current_old) != expected_old or not marker_matches(
            current_payload, "mysetup", expected_kind
        ):
            raise MarkerRemovalError("legacy marker changed before exact removal")
        if not visible_parent_matches(parent, expected_parent):
            raise MarkerRemovalError("marker parent changed before exact removal")
        removal_test_barrier()
        try:
            rename_noreplace(
                parent_fd,
                old_name,
                recovery_fd,
                "legacy-marker",
            )
        except OSError as error:
            raise MarkerRemovalError(
                f"could not move legacy marker to recovery: {error}"
            ) from error

        recovered = lstat_at(recovery_fd, "legacy-marker")
        moved_expected = recovered is not None and inode_id(recovered) == expected_old
        if moved_expected:
            try:
                _, recovered_payload = read_marker(
                    recovery_fd, "legacy-marker", recovery_path + "/legacy-marker"
                )
                moved_expected = marker_matches(
                    recovered_payload, "mysetup", expected_kind
                )
            except SystemExit:
                moved_expected = False
        if not moved_expected:
            try:
                rename_noreplace(recovery_fd, "legacy-marker", parent_fd, old_name)
            except OSError as error:
                raise MarkerRemovalError(
                    f"unexpected marker moved during exact removal; recovery retained at {recovery_path}: {error}"
                ) from error
            restored = lstat_at(parent_fd, old_name)
            if (
                recovered is None
                or restored is None
                or inode_id(restored) != inode_id(recovered)
            ):
                raise MarkerRemovalError(
                    f"unexpected marker restore had an uncertain postcondition; inspect {recovery_path}"
                )
            raise MarkerRemovalError(
                "legacy marker changed during exact removal; concurrent replacement restored"
            )

        if lstat_at(parent_fd, old_name) is not None:
            raise MarkerRemovalError("another owner appeared at the legacy marker path")
        recovered_after = lstat_at(recovery_fd, "legacy-marker")
        if recovered_after is None or inode_id(recovered_after) != expected_old:
            raise MarkerRemovalError("legacy marker recovery identity changed")
        if not visible_parent_matches(parent, expected_parent):
            try:
                rename_noreplace(
                    recovery_fd,
                    "legacy-marker",
                    parent_fd,
                    old_name,
                )
            except OSError as error:
                raise MarkerRemovalError(
                    f"marker parent changed; recovery retained at {recovery_path}: {error}"
                ) from error
            raise MarkerRemovalError(
                "marker parent changed; legacy marker restored through pinned parent"
            )
        return recovery_path
    finally:
        os.close(recovery_fd)


def retain_exact_marker(
    parent_fd: int,
    parent: str,
    name: str,
    expected: tuple[int, int],
    manager: str,
    expected_kind: str,
    recovery_entry: str,
) -> str:
    for _ in range(128):
        recovery_name = ".wahrwelt-migration-recovery-marker-" + secrets.token_hex(12)
        try:
            os.mkdir(recovery_name, 0o700, dir_fd=parent_fd)
            break
        except FileExistsError:
            continue
    else:
        raise MarkerRemovalError("could not allocate published marker recovery")
    recovery_path = os.path.join(parent, recovery_name)
    recovery_fd = os.open(
        recovery_name,
        os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
        dir_fd=parent_fd,
    )
    try:
        try:
            rename_noreplace(parent_fd, name, recovery_fd, recovery_entry)
        except OSError as error:
            raise MarkerRemovalError(
                f"could not move generated marker to recovery: {error}"
            ) from error
        moved = lstat_at(recovery_fd, recovery_entry)
        moved_expected = moved is not None and inode_id(moved) == expected
        if moved_expected:
            try:
                _, payload = read_marker(
                    recovery_fd, recovery_entry, recovery_path + "/" + recovery_entry
                )
                moved_expected = marker_matches(payload, manager, expected_kind)
            except SystemExit:
                moved_expected = False
        if not moved_expected:
            try:
                rename_noreplace(recovery_fd, recovery_entry, parent_fd, name)
            except OSError as error:
                raise MarkerRemovalError(
                    f"unexpected marker moved while retaining generated marker; recovery kept at {recovery_path}: {error}"
                ) from error
            restored = lstat_at(parent_fd, name)
            if (
                moved is None
                or restored is None
                or inode_id(restored) != inode_id(moved)
            ):
                raise MarkerRemovalError(
                    f"generated marker restore had an uncertain postcondition; inspect {recovery_path}"
                )
            raise MarkerRemovalError(
                "generated marker changed; concurrent replacement restored"
            )
        return recovery_path
    finally:
        os.close(recovery_fd)


def check(old: str, new: str, config_home: str, home: str) -> None:
    parent, old_name, new_name = split_paths(old, new)
    expected_kind = expected_kind_for(parent, config_home, home, old, new)
    parent_fd, parent_info = pin_parent(parent, old, new)
    try:
        old_info, old_payload = read_marker(parent_fd, old_name, old)
        require_marker_schema(old_payload, "mysetup", expected_kind, old, new, "legacy")
        new_info = lstat_at(parent_fd, new_name)
        if new_info is None:
            kind = "publish"
            suffix = ""
        else:
            if not stat.S_ISREG(new_info.st_mode):
                collision(old, new, "canonical marker is not an ordinary regular file")
            opened_new, new_payload = read_marker(parent_fd, new_name, new)
            require_marker_schema(
                new_payload, "wahrwelt", expected_kind, old, new, "canonical"
            )
            kind = "compatible"
            suffix = "|" + format_id(opened_new)
        if not visible_parent_matches(parent, inode_id(parent_info)):
            collision(old, new, "parent changed during preflight")
        print(f"{kind}|{format_id(parent_info)}|{format_id(old_info)}{suffix}")
    finally:
        os.close(parent_fd)


def migrate(old: str, new: str, config_home: str, home: str, token: str) -> None:
    parent, old_name, new_name = split_paths(old, new)
    expected_kind = expected_kind_for(parent, config_home, home, old, new)
    fields = token.split("|")
    if len(fields) not in (3, 4) or fields[0] not in ("publish", "compatible"):
        fail("Wahrwelt marker migration received an invalid preflight token", 2)
    if (fields[0] == "publish" and len(fields) != 3) or (
        fields[0] == "compatible" and len(fields) != 4
    ):
        fail("Wahrwelt marker migration received an invalid preflight token", 2)
    try:
        expected_parent = parse_id(fields[1])
        expected_old = parse_id(fields[2])
        expected_new = parse_id(fields[3]) if fields[0] == "compatible" else None
    except (ValueError, OverflowError):
        fail("Wahrwelt marker migration received an invalid preflight token", 2)

    parent_fd, parent_info = pin_parent(parent, old, new)
    try:
        if inode_id(parent_info) != expected_parent:
            collision(old, new, "parent changed after preflight")
        old_info, old_payload = read_marker(parent_fd, old_name, old)
        if inode_id(old_info) != expected_old or not marker_matches(
            old_payload, "mysetup", expected_kind
        ):
            collision(old, new, "legacy marker changed after preflight")
        new_info = lstat_at(parent_fd, new_name)
        if expected_new is not None:
            if new_info is None or inode_id(new_info) != expected_new:
                collision(old, new, "canonical marker changed after preflight")
            _, new_payload = read_marker(parent_fd, new_name, new)
            if not marker_matches(new_payload, "wahrwelt", expected_kind):
                collision(old, new, "canonical marker changed after preflight")
            if not visible_parent_matches(parent, expected_parent):
                collision(old, new, "parent changed after preflight")
            try:
                recovery_path = remove_exact_legacy_marker(
                    parent_fd,
                    parent,
                    expected_parent,
                    old_name,
                    old,
                    expected_old,
                    expected_kind,
                )
            except MarkerRemovalError as error:
                collision(old, new, str(error))
            print(f"Wahrwelt marker recovery retained at {recovery_path}")
            return
        if new_info is not None:
            collision(old, new, "canonical marker appeared after preflight")
        if not visible_parent_matches(parent, expected_parent):
            collision(old, new, "parent changed before publication")

        candidate_fd = os.open(
            ".",
            os.O_TMPFILE | os.O_RDWR | os.O_CLOEXEC,
            stat.S_IMODE(old_info.st_mode),
            dir_fd=parent_fd,
        )
        try:
            candidate_payload = dict(old_payload)
            candidate_payload["manager"] = "wahrwelt"
            candidate = (
                json.dumps(candidate_payload, indent=2, sort_keys=True) + "\n"
            ).encode()
            offset = 0
            while offset < len(candidate):
                offset += os.write(candidate_fd, candidate[offset:])
            os.fchmod(candidate_fd, stat.S_IMODE(old_info.st_mode))
            os.fsync(candidate_fd)
            candidate_info = os.fstat(candidate_fd)
            candidate_id = inode_id(candidate_info)
            os.lseek(candidate_fd, 0, os.SEEK_SET)
            candidate_check = json.loads(os.read(candidate_fd, len(candidate) + 1))
            if not marker_matches(candidate_check, "wahrwelt", expected_kind):
                fail("Wahrwelt marker candidate verification failed")
            current_old, current_payload = read_marker(parent_fd, old_name, old)
            if inode_id(current_old) != expected_old or not marker_matches(
                current_payload, "mysetup", expected_kind
            ):
                collision(old, new, "legacy marker changed before publication")
            if lstat_at(parent_fd, new_name) is not None:
                collision(old, new, "canonical marker appeared before publication")
            if not visible_parent_matches(parent, expected_parent):
                collision(old, new, "parent changed before publication")
            test_barrier()
            try:
                link_anonymous(candidate_fd, parent_fd, new_name)
            except OSError:
                collision(old, new, "canonical marker appeared during publication")

            published = lstat_at(parent_fd, new_name)
            anchored_ok = (
                published is not None
                and stat.S_ISREG(published.st_mode)
                and inode_id(published) == candidate_id
            )
            old_after, old_payload_after = read_marker(parent_fd, old_name, old)
            anchored_ok = (
                anchored_ok
                and inode_id(old_after) == expected_old
                and marker_matches(old_payload_after, "mysetup", expected_kind)
            )
            visible_ok = False
            if visible_parent_matches(parent, expected_parent):
                try:
                    visible_new = os.lstat(new)
                    visible_ok = (
                        stat.S_ISREG(visible_new.st_mode)
                        and inode_id(visible_new) == candidate_id
                    )
                except OSError:
                    pass
            if anchored_ok and visible_ok:
                try:
                    recovery_path = remove_exact_legacy_marker(
                        parent_fd,
                        parent,
                        expected_parent,
                        old_name,
                        old,
                        expected_old,
                        expected_kind,
                    )
                except MarkerRemovalError as error:
                    try:
                        candidate_recovery = retain_exact_marker(
                            parent_fd,
                            parent,
                            new_name,
                            candidate_id,
                            "wahrwelt",
                            expected_kind,
                            "canonical-marker",
                        )
                    except MarkerRemovalError as rollback_error:
                        collision(
                            old,
                            new,
                            f"legacy marker removal failed: {error}; generated marker rollback failed: {rollback_error}",
                        )
                    collision(
                        old,
                        new,
                        f"legacy marker removal failed: {error}; generated marker retained at {candidate_recovery}",
                    )
                print(f"Wahrwelt marker recovery retained at {recovery_path}")
                return

            try:
                candidate_recovery = retain_exact_marker(
                    parent_fd,
                    parent,
                    new_name,
                    candidate_id,
                    "wahrwelt",
                    expected_kind,
                    "canonical-marker",
                )
            except MarkerRemovalError as rollback_error:
                collision(
                    old,
                    new,
                    f"publication postcondition failed; rollback failed: {rollback_error}",
                )
            collision(
                old,
                new,
                f"publication postcondition failed; generated marker retained at {candidate_recovery}",
            )
        finally:
            os.close(candidate_fd)
    finally:
        os.close(parent_fd)


def main() -> None:
    if len(sys.argv) not in (6, 7):
        fail(
            f"usage: {sys.argv[0]} check OLD NEW CONFIG_HOME HOME | "
            "migrate OLD NEW CONFIG_HOME HOME TOKEN",
            2,
        )
    command_name, old, new = sys.argv[1:4]
    config_home = sys.argv[4]
    home = sys.argv[5]
    if command_name == "check" and len(sys.argv) == 6:
        check(old, new, config_home, home)
        return
    if command_name == "migrate" and len(sys.argv) == 7:
        migrate(old, new, config_home, home, sys.argv[6])
        return
    fail("Wahrwelt marker migration received an invalid command", 2)


if __name__ == "__main__":
    main()
