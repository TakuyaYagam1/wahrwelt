import contextlib
import ctypes
import errno
import hashlib
import json
import os
import secrets
import stat
import subprocess
import sys
from dataclasses import dataclass

RENAME_NOREPLACE = 1
MAX_JSON_BYTES = 64 * 1024 * 1024
LIBC = ctypes.CDLL(None, use_errno=True)
LIBC.renameat2.argtypes = [
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_uint,
]
LIBC.renameat2.restype = ctypes.c_int


def fail(message: str, code: int = 1) -> None:
    print(f"Wahrwelt mutable seed ownership collision: {message}", file=sys.stderr)
    raise SystemExit(code)


def inode_id(info: os.stat_result) -> tuple[int, int]:
    return info.st_dev, info.st_ino


def stat_state(info: os.stat_result) -> tuple[int, ...]:
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


@dataclass(frozen=True)
class RegularSnapshot:
    state: tuple[int, ...]
    digest: bytes


def equivalent_after_noreplace(
    current: RegularSnapshot,
    staged: RegularSnapshot,
) -> bool:
    # Linux updates ctime when the private candidate is published. Every other
    # inode field and the exact bytes must remain unchanged.
    return current.state[:8] == staged.state[:8] and current.digest == staged.digest


def read_regular_snapshot(
    fd: int,
    require_single_link: bool = True,
) -> tuple[RegularSnapshot, bytes]:
    before = os.fstat(fd)
    if (
        not stat.S_ISREG(before.st_mode)
        or before.st_uid != os.getuid()
        or (require_single_link and before.st_nlink != 1)
    ):
        fail("regular file changed ownership while being read")
    chunks: list[bytes] = []
    offset = 0
    while True:
        chunk = os.pread(fd, min(1024 * 1024, MAX_JSON_BYTES + 1 - offset), offset)
        if not chunk:
            break
        chunks.append(chunk)
        offset += len(chunk)
        if offset > MAX_JSON_BYTES:
            fail("mutable seed file exceeds the 64 MiB safety limit")
    value = b"".join(chunks)
    after = os.fstat(fd)
    if stat_state(before) != stat_state(after):
        fail("regular file changed while being read")
    return RegularSnapshot(stat_state(after), hashlib.sha256(value).digest()), value


def visible_regular_matches(
    parent_fd: int,
    name: str,
    expected: RegularSnapshot,
) -> bool:
    visible = lstat_at(parent_fd, name)
    return (
        visible is not None
        and stat.S_ISREG(visible.st_mode)
        and stat_state(visible) == expected.state
    )


def renameat2(
    source_fd: int,
    source: str,
    target_fd: int,
    target: str,
    flags: int,
) -> None:
    result = LIBC.renameat2(
        source_fd,
        os.fsencode(source),
        target_fd,
        os.fsencode(target),
        flags,
    )
    if result != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error), target)


def named_test_barrier(ready_name: str, proceed_name: str) -> None:
    ready = os.environ.get(ready_name, "")
    proceed = os.environ.get(proceed_name, "")
    if not ready and not proceed:
        return
    if not ready or not proceed:
        fail("incomplete mutable seed test barrier")
    os.write(int(ready), b"ready\n")
    if os.read(int(proceed), 1) != b"1":
        fail("closed mutable seed test barrier")


def test_barrier() -> None:
    named_test_barrier(
        "WAHRWELT_TEST_MUTABLE_SEED_READY_FD",
        "WAHRWELT_TEST_MUTABLE_SEED_CONTINUE_FD",
    )


def ordinary_directory_fd(path: str) -> int:
    return os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)


class TargetContext:
    def __init__(self, root: str, target: str, create_parents: bool = False):
        if (
            not os.path.isabs(root)
            or not os.path.isabs(target)
            or root.startswith("//")
            or target.startswith("//")
            or os.path.normpath(root) != root
            or os.path.normpath(target) != target
        ):
            fail(f"non-canonical seed path: {target}")
        relative = os.path.relpath(target, root)
        if relative in ("", ".") or relative == ".." or relative.startswith("../"):
            fail(f"seed target is outside HOME: {target}")
        self.root = root
        self.target = target
        self.components = relative.split(os.sep)
        if any(component in ("", ".", "..") for component in self.components):
            fail(f"invalid seed target: {target}")
        self.fds: list[int] = []
        self.identities: list[tuple[int, int]] = []
        self.parent_components: list[str] = []
        try:
            root_fd = ordinary_directory_fd(root)
        except OSError as error:
            fail(f"HOME is not an ordinary directory: {root}: {error}")
        root_info = os.fstat(root_fd)
        try:
            visible = os.lstat(root)
        except OSError as error:
            os.close(root_fd)
            fail(f"HOME disappeared during validation: {root}: {error}")
        if (
            not stat.S_ISDIR(root_info.st_mode)
            or root_info.st_uid != os.getuid()
            or inode_id(root_info) != inode_id(visible)
        ):
            os.close(root_fd)
            fail(f"HOME ownership changed during validation: {root}")
        self.fds.append(root_fd)
        self.identities.append(inode_id(root_info))
        current = root_fd
        for component in self.components[:-1]:
            try:
                following = ordinary_directory_at(current, component)
            except FileNotFoundError:
                if not create_parents:
                    self.close()
                    fail(f"seed parent is absent: {target}")
                following = publish_empty_directory(
                    self,
                    current,
                    component,
                    0o755,
                )
            except OSError as error:
                self.close()
                fail(f"seed parent is not an ordinary directory: {target}: {error}")
            self.parent_components.append(component)
            self.fds.append(following)
            self.identities.append(inode_id(os.fstat(following)))
            current = following
        self.parent_fd = current
        self.name = self.components[-1]
        self.validate_chain()

    def validate_chain(self) -> None:
        try:
            visible_root = os.lstat(self.root)
        except OSError:
            fail(f"HOME changed during seed transaction: {self.root}")
        if (
            not stat.S_ISDIR(visible_root.st_mode)
            or inode_id(visible_root) != self.identities[0]
        ):
            fail(f"HOME changed during seed transaction: {self.root}")
        current = os.dup(self.fds[0])
        try:
            for index, component in enumerate(self.parent_components, start=1):
                following = ordinary_directory_at(current, component)
                os.close(current)
                current = following
                if inode_id(os.fstat(current)) != self.identities[index]:
                    fail(f"seed parent changed during transaction: {self.target}")
        except OSError as error:
            fail(f"seed parent changed during transaction: {self.target}: {error}")
        finally:
            os.close(current)

    def close(self) -> None:
        while self.fds:
            os.close(self.fds.pop())

    def __enter__(self):
        return self

    def __exit__(self, _type, _value, _traceback):
        self.close()


def ordinary_directory_at(parent_fd: int, name: str) -> int:
    return os.open(
        name,
        os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
        dir_fd=parent_fd,
    )


def lstat_at(parent_fd: int, name: str) -> os.stat_result | None:
    try:
        return os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    except FileNotFoundError:
        return None


def random_name(prefix: str) -> str:
    return prefix + secrets.token_hex(16)


def allocate_file(parent_fd: int) -> tuple[str, int]:
    for _ in range(128):
        name = random_name(".wahrwelt-seed-file-")
        try:
            fd = os.open(
                name,
                os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                0o600,
                dir_fd=parent_fd,
            )
            return name, fd
        except FileExistsError:
            continue
    fail("could not allocate a private file candidate")


def allocate_directory(parent_fd: int) -> tuple[str, int]:
    for _ in range(128):
        name = random_name(".wahrwelt-seed-tree-")
        try:
            os.mkdir(name, 0o700, dir_fd=parent_fd)
            fd = ordinary_directory_at(parent_fd, name)
            return name, fd
        except FileExistsError:
            continue
    fail("could not allocate a private directory candidate")


def retain_file_candidate(
    parent_fd: int,
    name: str,
    expected: RegularSnapshot,
) -> None:
    info = lstat_at(parent_fd, name)
    if info is None:
        return
    if not stat.S_ISREG(info.st_mode) or stat_state(info) != expected.state:
        fail(f"private file recovery retained after candidate collision: {name}")
    print(
        f"Wahrwelt mutable seed recovery retained: {name}",
        file=sys.stderr,
    )


def retain_directory_candidate(
    parent_fd: int, name: str, expected: tuple[int, int]
) -> None:
    info = lstat_at(parent_fd, name)
    if info is None:
        return
    if not stat.S_ISDIR(info.st_mode) or inode_id(info) != expected:
        fail(f"private directory recovery retained after candidate collision: {name}")
    directory_fd = ordinary_directory_at(parent_fd, name)
    try:
        if inode_id(os.fstat(directory_fd)) != expected:
            fail(f"private directory recovery changed before retention: {name}")
    finally:
        os.close(directory_fd)
    print(
        f"Wahrwelt mutable seed recovery retained: {name}",
        file=sys.stderr,
    )


def classify_existing(context: TargetContext, expected: str) -> bool:
    info = lstat_at(context.parent_fd, context.name)
    if info is None:
        return False
    context.validate_chain()
    if expected == "regular" and stat.S_ISREG(info.st_mode):
        return True
    if expected == "directory" and stat.S_ISDIR(info.st_mode):
        return True
    fail(f"expected {expected} or absent path: {context.target}")


def publish_empty_directory(
    context: TargetContext,
    parent_fd: int,
    name: str,
    mode: int,
) -> int:
    candidate, candidate_fd = allocate_directory(parent_fd)
    candidate_id = inode_id(os.fstat(candidate_fd))
    try:
        test_barrier()
        context.validate_chain()
        try:
            renameat2(parent_fd, candidate, parent_fd, name, RENAME_NOREPLACE)
        except OSError as error:
            retain_directory_candidate(parent_fd, candidate, candidate_id)
            if error.errno == errno.EEXIST:
                fail(
                    f"concurrent winner appeared while creating directory: {context.target}"
                )
            raise
        published = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
        if not stat.S_ISDIR(published.st_mode) or inode_id(published) != candidate_id:
            fail(f"published directory identity changed: {context.target}")
        os.fchmod(candidate_fd, mode)
        os.fsync(candidate_fd)
        os.fsync(parent_fd)
        return candidate_fd
    except BaseException:
        os.close(candidate_fd)
        raise


def ensure_directory(root: str, target: str) -> None:
    with TargetContext(root, target, create_parents=True) as context:
        if classify_existing(context, "directory"):
            return
        directory_fd = publish_empty_directory(
            context, context.parent_fd, context.name, 0o755
        )
        os.close(directory_fd)
        context.validate_chain()


def source_file_bytes(
    source: str,
    replacement: str,
    line_prefix: str = "",
    line_value: str = "",
) -> bytes:
    fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode):
            fail(f"seed source is not a regular file: {source}")
        chunks: list[bytes] = []
        total = 0
        while True:
            chunk = os.read(fd, 1024 * 1024)
            if not chunk:
                break
            total += len(chunk)
            if total > MAX_JSON_BYTES:
                fail(f"seed source is too large: {source}")
            chunks.append(chunk)
        value = b"".join(chunks)
        if replacement:
            value = value.replace(b"@HOME@", os.fsencode(replacement))
        if line_prefix:
            if "\n" in line_prefix or "\r" in line_prefix:
                fail("seed line prefix contains a newline")
            if "\n" in line_value or "\r" in line_value:
                fail("seed replacement line contains a newline")
            prefix = os.fsencode(line_prefix)
            replacement_line = os.fsencode(line_value)
            replaced = 0
            transformed: list[bytes] = []
            for line in value.splitlines(keepends=True):
                ending = b""
                body = line
                if line.endswith(b"\r\n"):
                    body, ending = line[:-2], b"\r\n"
                elif line.endswith((b"\n", b"\r")):
                    body, ending = line[:-1], line[-1:]
                if body.startswith(prefix):
                    transformed.append(replacement_line + ending)
                    replaced += 1
                else:
                    transformed.append(line)
            if replaced == 0:
                fail(f"seed source has no line starting with {line_prefix!r}: {source}")
            value = b"".join(transformed)
        return value
    finally:
        os.close(fd)


def write_all(fd: int, value: bytes) -> None:
    offset = 0
    while offset < len(value):
        offset += os.write(fd, value[offset:])


def prepared_file(
    parent_fd: int,
    value: bytes,
    mode: int,
) -> tuple[str, int, RegularSnapshot]:
    candidate, candidate_fd = allocate_file(parent_fd)
    try:
        write_all(candidate_fd, value)
        os.fchmod(candidate_fd, mode)
        os.fsync(candidate_fd)
        candidate_snapshot, _ = read_regular_snapshot(candidate_fd)
        return candidate, candidate_fd, candidate_snapshot
    except BaseException:
        try:
            candidate_snapshot, _ = read_regular_snapshot(candidate_fd)
        except BaseException:
            os.close(candidate_fd)
            print(
                f"Wahrwelt mutable seed recovery retained: {candidate}",
                file=sys.stderr,
            )
            raise
        retain_file_candidate(parent_fd, candidate, candidate_snapshot)
        os.close(candidate_fd)
        raise


def publish_file_if_absent(
    context: TargetContext,
    candidate: str,
    candidate_fd: int,
    candidate_snapshot: RegularSnapshot,
) -> None:
    test_barrier()
    context.validate_chain()
    try:
        renameat2(
            context.parent_fd,
            candidate,
            context.parent_fd,
            context.name,
            RENAME_NOREPLACE,
        )
    except OSError as error:
        retain_file_candidate(context.parent_fd, candidate, candidate_snapshot)
        if error.errno == errno.EEXIST:
            fail(f"concurrent winner appeared while seeding file: {context.target}")
        raise
    published = os.stat(context.name, dir_fd=context.parent_fd, follow_symlinks=False)
    current_candidate, _ = read_regular_snapshot(candidate_fd)
    if (
        not stat.S_ISREG(published.st_mode)
        or stat_state(published) != current_candidate.state
        or not equivalent_after_noreplace(current_candidate, candidate_snapshot)
    ):
        fail(f"published file identity changed: {context.target}")
    context.validate_chain()
    os.fsync(context.parent_fd)
    os.close(candidate_fd)


def seed_file(root: str, target: str, source: str, replacement: str) -> None:
    with TargetContext(root, target) as context:
        if classify_existing(context, "regular"):
            return
        candidate, candidate_fd, candidate_snapshot = prepared_file(
            context.parent_fd,
            source_file_bytes(source, replacement),
            0o644,
        )
        try:
            publish_file_if_absent(
                context,
                candidate,
                candidate_fd,
                candidate_snapshot,
            )
        except BaseException:
            with contextlib.suppress(OSError):
                os.close(candidate_fd)
            raise


def seed_file_with_line(
    root: str,
    target: str,
    source: str,
    line_prefix: str,
    line_value: str,
) -> None:
    with TargetContext(root, target) as context:
        if classify_existing(context, "regular"):
            return
        candidate, candidate_fd, candidate_snapshot = prepared_file(
            context.parent_fd,
            source_file_bytes(source, "", line_prefix, line_value),
            0o644,
        )
        try:
            publish_file_if_absent(
                context,
                candidate,
                candidate_fd,
                candidate_snapshot,
            )
        except BaseException:
            with contextlib.suppress(OSError):
                os.close(candidate_fd)
            raise


def copy_regular(source_fd: int, destination_fd: int, name: str, mode: int) -> None:
    input_fd = os.open(
        name,
        os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
        dir_fd=source_fd,
    )
    output_fd = os.open(
        name,
        os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
        0o600,
        dir_fd=destination_fd,
    )
    try:
        while True:
            chunk = os.read(input_fd, 1024 * 1024)
            if not chunk:
                break
            write_all(output_fd, chunk)
        os.fchmod(output_fd, 0o755 if mode & 0o111 else 0o644)
        os.fsync(output_fd)
    finally:
        os.close(output_fd)
        os.close(input_fd)


def copy_tree(source_fd: int, destination_fd: int) -> None:
    for name in os.listdir(source_fd):
        if name in ("", ".", "..") or "/" in name:
            fail("invalid seed tree entry")
        info = os.stat(name, dir_fd=source_fd, follow_symlinks=False)
        if stat.S_ISREG(info.st_mode):
            copy_regular(source_fd, destination_fd, name, stat.S_IMODE(info.st_mode))
        elif stat.S_ISDIR(info.st_mode):
            os.mkdir(name, 0o700, dir_fd=destination_fd)
            source_child = ordinary_directory_at(source_fd, name)
            destination_child = ordinary_directory_at(destination_fd, name)
            try:
                copy_tree(source_child, destination_child)
                os.fchmod(destination_child, 0o755)
                os.fsync(destination_child)
            finally:
                os.close(destination_child)
                os.close(source_child)
        elif stat.S_ISLNK(info.st_mode):
            os.symlink(
                os.readlink(name, dir_fd=source_fd),
                name,
                dir_fd=destination_fd,
            )
        else:
            fail(f"unsupported seed tree entry: {name}")


def seed_tree(root: str, target: str, source: str) -> None:
    with TargetContext(root, target) as context:
        if classify_existing(context, "directory"):
            return
        source_fd = ordinary_directory_fd(source)
        candidate, candidate_fd = allocate_directory(context.parent_fd)
        candidate_id = inode_id(os.fstat(candidate_fd))
        try:
            copy_tree(source_fd, candidate_fd)
            os.fsync(candidate_fd)
            test_barrier()
            context.validate_chain()
            try:
                renameat2(
                    context.parent_fd,
                    candidate,
                    context.parent_fd,
                    context.name,
                    RENAME_NOREPLACE,
                )
            except OSError as error:
                retain_directory_candidate(context.parent_fd, candidate, candidate_id)
                if error.errno == errno.EEXIST:
                    fail(
                        f"concurrent winner appeared while seeding directory: {target}"
                    )
                raise
            published = os.stat(
                context.name,
                dir_fd=context.parent_fd,
                follow_symlinks=False,
            )
            if (
                not stat.S_ISDIR(published.st_mode)
                or inode_id(published) != candidate_id
            ):
                fail(f"published directory identity changed: {target}")
            os.fchmod(candidate_fd, 0o755)
            os.fsync(candidate_fd)
            os.fsync(context.parent_fd)
            context.validate_chain()
        except BaseException:
            if lstat_at(context.parent_fd, candidate) is not None:
                retain_directory_candidate(context.parent_fd, candidate, candidate_id)
            raise
        finally:
            os.close(candidate_fd)
            os.close(source_fd)


def parse_json_object(value: bytes, label: str) -> dict:
    try:
        parsed = json.loads(value)
    except (json.JSONDecodeError, UnicodeDecodeError) as error:
        fail(f"{label} is invalid JSON: {error}")
    if not isinstance(parsed, dict):
        fail(f"{label} is not a JSON object")
    return parsed


def run_jq_transform(
    value: bytes,
    jq_path: str,
    jq_filter: str,
    jq_arguments: list[str],
) -> tuple[bytes, dict]:
    if not jq_filter:
        return value, parse_json_object(value, "mutable seed JSON")
    result = subprocess.run(
        [jq_path, *jq_arguments, jq_filter],
        input=value,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        if result.stderr:
            sys.stderr.buffer.write(result.stderr)
        raise SystemExit(result.returncode)
    if len(result.stdout) > MAX_JSON_BYTES:
        fail("jq output exceeds the 64 MiB safety limit")
    return result.stdout, parse_json_object(result.stdout, "jq output")


def read_existing_json(
    context: TargetContext,
) -> tuple[RegularSnapshot, bytes, dict]:
    try:
        fd = os.open(
            context.name,
            os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK,
            dir_fd=context.parent_fd,
        )
    except OSError as error:
        fail(f"expected an ordinary JSON file: {context.target}: {error}")
    try:
        info = os.fstat(fd)
        visible = lstat_at(context.parent_fd, context.name)
        if (
            visible is None
            or not stat.S_ISREG(info.st_mode)
            or not stat.S_ISREG(visible.st_mode)
            or info.st_uid != os.getuid()
            or stat_state(info) != stat_state(visible)
        ):
            fail(f"JSON file ownership is unsafe: {context.target}")
        snapshot, value = read_regular_snapshot(fd, require_single_link=False)
        context.validate_chain()
        if not visible_regular_matches(context.parent_fd, context.name, snapshot):
            fail(f"JSON file changed while being validated: {context.target}")
        parsed = parse_json_object(value, f"existing JSON file {context.target}")
        final_snapshot, final_value = read_regular_snapshot(
            fd, require_single_link=False
        )
        if final_snapshot != snapshot or final_value != value:
            fail(f"JSON file changed while being validated: {context.target}")
        return snapshot, value, parsed
    finally:
        os.close(fd)


def seed_json_object(
    root: str,
    target: str,
    source: str,
    replacement: str,
    jq_path: str,
    jq_filter: str,
    jq_arguments: list[str],
) -> None:
    with TargetContext(root, target) as context:
        existing = lstat_at(context.parent_fd, context.name)
        if existing is not None:
            existing_snapshot, existing_value, existing_json = read_existing_json(
                context
            )
            if jq_filter:
                _, transformed_json = run_jq_transform(
                    existing_value,
                    jq_path,
                    jq_filter,
                    jq_arguments,
                )
                if transformed_json != existing_json:
                    fail(
                        "existing JSON requires a managed transform and was preserved; "
                        f"merge defaults manually: {target}"
                    )
            test_barrier()
            final_snapshot, final_value, _ = read_existing_json(context)
            if final_snapshot != existing_snapshot or final_value != existing_value:
                fail(
                    f"existing JSON changed during validation and was preserved: {target}"
                )
            return

        source_value = source_file_bytes(source, replacement)
        parse_json_object(source_value, f"canonical JSON seed {source}")
        transformed, _ = run_jq_transform(
            source_value,
            jq_path,
            jq_filter,
            jq_arguments,
        )
        candidate, candidate_fd, candidate_snapshot = prepared_file(
            context.parent_fd,
            transformed,
            0o644,
        )
        try:
            publish_file_if_absent(
                context,
                candidate,
                candidate_fd,
                candidate_snapshot,
            )
        except BaseException:
            with contextlib.suppress(OSError):
                os.close(candidate_fd)
            raise


def classify_file(root: str, target: str) -> None:
    with TargetContext(root, target) as context:
        classify_existing(context, "regular")


def usage() -> None:
    fail(
        "usage: mutable-seed.py ensure-dir ROOT TARGET | "
        "seed-file ROOT TARGET SOURCE HOME_REPLACEMENT | "
        "seed-file-replace-line ROOT TARGET SOURCE PREFIX LINE | "
        "seed-tree ROOT TARGET SOURCE | "
        "seed-json-object ROOT TARGET SOURCE HOME_REPLACEMENT JQ FILTER [JQ_ARGS...] | "
        "classify-file ROOT TARGET",
        2,
    )


def main() -> None:
    if len(sys.argv) < 4:
        usage()
    operation = sys.argv[1]
    if operation == "ensure-dir" and len(sys.argv) == 4:
        ensure_directory(sys.argv[2], sys.argv[3])
    elif operation == "seed-file" and len(sys.argv) == 6:
        seed_file(sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5])
    elif operation == "seed-file-replace-line" and len(sys.argv) == 7:
        seed_file_with_line(
            sys.argv[2],
            sys.argv[3],
            sys.argv[4],
            sys.argv[5],
            sys.argv[6],
        )
    elif operation == "seed-tree" and len(sys.argv) == 5:
        seed_tree(sys.argv[2], sys.argv[3], sys.argv[4])
    elif operation == "seed-json-object" and len(sys.argv) >= 8:
        seed_json_object(
            sys.argv[2],
            sys.argv[3],
            sys.argv[4],
            sys.argv[5],
            sys.argv[6],
            sys.argv[7],
            sys.argv[8:],
        )
    elif operation == "classify-file" and len(sys.argv) == 4:
        classify_file(sys.argv[2], sys.argv[3])
    else:
        usage()


if __name__ == "__main__":
    main()
