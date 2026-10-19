"""Reconcile mutable defaults without claiming unknown user files.

Linux only: directory descriptors and renameat2 keep publication within the
validated HOME. The latest journal retains before/after bytes for manual
recovery; an interrupted transaction is never automatically replayed.
"""

import argparse
import base64
import binascii
import contextlib
import ctypes
import fcntl
import json
import os
import secrets
import stat
import subprocess
import sys
from dataclasses import dataclass

MAX_FILES = 1024
MAX_FILE = 1024 * 1024
MAX_TOTAL = 16 * 1024 * 1024
MAX_STATE = 64 * 1024 * 1024
DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
FILE_FLAGS = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC
LIBC = ctypes.CDLL(None, use_errno=True)
LIBC.renameat2.argtypes = [
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_int,
    ctypes.c_char_p,
    ctypes.c_uint,
]
LIBC.renameat2.restype = ctypes.c_int


class Refusal(Exception):
    pass


def require(condition, message):
    if not condition:
        raise Refusal(message)


def canonical(path):
    require(
        isinstance(path, str)
        and path.startswith("/")
        and not path.startswith("//")
        and os.path.normpath(path) == path
        and "\x00" not in path,
        f"non-canonical absolute path: {path!r}",
    )
    return path


def relative(path):
    require(
        isinstance(path, str)
        and path
        and not path.startswith("/")
        and all(part not in ("", ".", "..") for part in path.split("/"))
        and "\x00" not in path
        and len(os.fsencode(path)) <= 2048,
        f"unsafe relative config path: {path!r}",
    )
    return path


def within(path, root):
    return path.startswith(root + "/")


def identity(info):
    return info.st_dev, info.st_ino


def metadata(info):
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
class Snapshot:
    meta: tuple
    value: bytes

    @property
    def mode(self):
        return stat.S_IMODE(self.meta[2])


def after_rename(current, expected):
    return (
        current is not None
        and expected is not None
        and current.meta[:-1] == expected.meta[:-1]
        and current.value == expected.value
    )


def read_fd(fd, limit, owned=True, private=False):
    before = os.fstat(fd)
    require(stat.S_ISREG(before.st_mode), "expected an ordinary regular file")
    if owned:
        require(
            before.st_uid == os.getuid() and before.st_nlink == 1,
            "unsafe file ownership or hard link",
        )
    if private:
        require(
            stat.S_IMODE(before.st_mode) == 0o600, "state file must be private (0600)"
        )
    require(before.st_size <= limit, "file exceeds size limit")
    chunks = []
    size = 0
    while True:
        chunk = os.read(fd, min(65536, limit + 1 - size))
        if not chunk:
            break
        chunks.append(chunk)
        size += len(chunk)
        require(size <= limit, "file exceeds size limit")
    require(metadata(before) == metadata(os.fstat(fd)), "file changed while reading")
    return Snapshot(metadata(before), b"".join(chunks))


class SafePath:
    """Hold every ancestor open; all mutations use the final directory fd."""

    def __init__(self, path, home, private_dir=None, create=False):
        self.path, self.home, self.private_dir = path, home, private_dir
        self.parts = canonical(path).split("/")[1:]
        self.fds = [os.open("/", DIRECTORY_FLAGS)]
        self.names = []
        try:
            for index, name in enumerate(self.parts[:-1]):
                directory = "/" + "/".join(self.parts[: index + 1])
                try:
                    fd = os.open(name, DIRECTORY_FLAGS, dir_fd=self.fds[-1])
                except FileNotFoundError:
                    if not create:
                        break
                    self.validate()
                    os.mkdir(
                        name,
                        0o700 if directory == private_dir else 0o755,
                        dir_fd=self.fds[-1],
                    )
                    os.fsync(self.fds[-1])
                    fd = os.open(name, DIRECTORY_FLAGS, dir_fd=self.fds[-1])
                self.names.append(name)
                self.fds.append(fd)
                info = os.fstat(fd)
                if directory == home or within(directory, home):
                    require(
                        info.st_uid == os.getuid(),
                        f"directory has another owner: {directory}",
                    )
                if directory == private_dir:
                    require(
                        stat.S_IMODE(info.st_mode) == 0o700,
                        "state directory must be private (0700)",
                    )
            self.validate()
        except BaseException:
            self.close()
            raise

    @property
    def complete(self):
        return len(self.fds) == len(self.parts)

    @property
    def parent(self):
        require(self.complete, f"parent directory is absent: {self.path}")
        return self.fds[-1]

    @property
    def name(self):
        return self.parts[-1]

    def validate(self):
        for index, name in enumerate(self.names):
            visible = os.stat(name, dir_fd=self.fds[index], follow_symlinks=False)
            require(
                stat.S_ISDIR(visible.st_mode)
                and identity(visible) == identity(os.fstat(self.fds[index + 1])),
                f"parent directory changed: {self.path}",
            )

    def read(self, limit=MAX_FILE, private=False, name=None):
        self.validate()
        if not self.complete:
            return None
        name = self.name if name is None else name
        try:
            fd = os.open(name, FILE_FLAGS, dir_fd=self.parent)
        except FileNotFoundError:
            return None
        try:
            snapshot = read_fd(fd, limit, private=private)
            visible = os.stat(name, dir_fd=self.parent, follow_symlinks=False)
            require(metadata(visible) == snapshot.meta, f"file changed: {self.path}")
            self.validate()
            return snapshot
        finally:
            os.close(fd)

    def close(self):
        for fd in reversed(self.fds):
            os.close(fd)
        self.fds = []

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()


def read_source(path, limit=MAX_FILE):
    canonical(path)
    # Nix store sources can have symlinked parent directories, but the source
    # leaf must be an ordinary file. Sources never grant write authority.
    fd = os.open(path, FILE_FLAGS)
    try:
        return read_fd(fd, limit, owned=False).value
    finally:
        os.close(fd)


def parse_json(value):
    def unique(pairs):
        result = {}
        for key, entry in pairs:
            require(key not in result, f"duplicate JSON key: {key}")
            result[key] = entry
        return result

    def reject_constant(value):
        raise Refusal(f"invalid JSON number: {value}")

    return json.loads(value, object_pairs_hook=unique, parse_constant=reject_constant)


def encode(value):
    return base64.b64encode(value).decode("ascii")


def decode(value, limit=MAX_FILE):
    require(
        isinstance(value, str) and len(value) <= 4 * limit // 3 + 4,
        "invalid baseline byte encoding",
    )
    result = base64.b64decode(value, validate=True)
    require(len(result) <= limit, "baseline file exceeds size limit")
    return result


def json_bytes(value):
    result = (json.dumps(value, sort_keys=True, indent=2) + "\n").encode()
    require(len(result) <= MAX_STATE, "state exceeds size limit")
    return result


def load_manifest(path):
    manifest = parse_json(read_source(path, MAX_STATE))
    require(
        isinstance(manifest, dict)
        and manifest.get("version") == 1
        and set(manifest) == {"version", "files"},
        "invalid manifest version or fields",
    )
    files = manifest["files"]
    require(
        isinstance(files, dict) and len(files) <= MAX_FILES, "invalid manifest files"
    )
    total = 0
    for name, entry in files.items():
        relative(name)
        require(
            isinstance(entry, dict) and set(entry) == {"source", "policy", "validator"},
            f"invalid manifest entry: {name}",
        )
        require(
            entry["policy"] in ("merge", "seed")
            and entry["validator"] in ("lua", "json", "fish", "none"),
            f"invalid manifest policy or validator: {name}",
        )
        entry["value"] = read_source(entry["source"])
        total += len(entry["value"])
    require(total <= MAX_TOTAL, "manifest content exceeds total size limit")
    names = set(files)
    require(
        not any(
            "/".join(name.split("/")[:i]) in names
            for name in names
            for i in range(1, len(name.split("/")))
        ),
        "manifest contains overlapping file paths",
    )
    return files


def load_baseline(snapshot, root):
    if snapshot is None:
        return {}
    document = parse_json(snapshot.value)
    require(
        isinstance(document, dict)
        and set(document) == {"version", "root", "files"}
        and document["version"] == 1
        and document["root"] == root,
        "invalid baseline version, root, or fields",
    )
    files = document["files"]
    require(
        isinstance(files, dict) and len(files) <= MAX_FILES, "invalid baseline files"
    )
    total = 0
    for name, entry in files.items():
        relative(name)
        require(
            isinstance(entry, dict)
            and entry.get("policy") in ("merge", "seed")
            and entry.get("status") in ("managed", "unmanaged"),
            "invalid baseline entry",
        )
        if entry["status"] == "managed":
            require(
                set(entry) == {"policy", "status", "upstream"},
                "invalid baseline fields",
            )
            total += len(decode(entry["upstream"]))
        else:
            require(
                set(entry) == {"policy", "status"},
                "unmanaged entry must have no baseline",
            )
    require(total <= MAX_TOTAL, "baseline content exceeds total size limit")
    return files


@contextlib.contextmanager
def memory_files(values):
    """Anonymous files let check run parsers and git without filesystem writes."""
    fds = []
    try:
        for value in values:
            fd = os.memfd_create("dotfile-reconcile", os.MFD_CLOEXEC)
            fds.append(fd)
            write_all(fd, value)
            os.lseek(fd, 0, os.SEEK_SET)
        yield fds, [f"/proc/self/fd/{fd}" for fd in fds]
    finally:
        for fd in fds:
            os.close(fd)


def run_tool(command, fds=()):
    environment = {
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "LC_ALL": "C",
        "HOME": "/nonexistent",
        "GIT_CONFIG_NOSYSTEM": "1",
        "GIT_CONFIG_GLOBAL": "/dev/null",
    }
    result = subprocess.run(
        command,
        pass_fds=fds,
        capture_output=True,
        check=False,
        timeout=30,
        env=environment,
        cwd="/",
    )
    require(
        len(result.stdout) <= MAX_FILE and len(result.stderr) <= MAX_FILE,
        "tool output exceeds size limit",
    )
    return result


def merge(local, base, upstream, git, name):
    require(
        all(b"\x00" not in value for value in (local, base, upstream)),
        f"cannot merge binary config: {name}",
    )
    with memory_files([local, base, upstream]) as (fds, paths):
        result = run_tool([git, "merge-file", "--stdout", "--diff3", *paths], fds)
    require(result.returncode == 0, f"merge conflict: {name}; live files preserved")
    return result.stdout


def validate(value, validator, args, name):
    if validator == "none":
        return
    if validator == "json":
        parse_json(value)
        return
    with memory_files([value]) as (fds, paths):
        command = (
            [args.luac, "-p", paths[0]]
            if validator == "lua"
            else [args.fish, "--no-config", "--no-execute", paths[0]]
        )
        result = run_tool(command, fds)
    require(
        result.returncode == 0,
        f"{validator} syntax validation failed: {name}: "
        + result.stderr.decode(errors="replace")[:2048],
    )


@dataclass
class Change:
    name: str
    before: Snapshot | None
    after: bytes | None


def prepare(args, files, baseline):
    changes, observations, next_files = [], {}, {}
    total_current = total_proposed = 0
    for name in sorted(set(files) | set(baseline)):
        with SafePath(args.root + "/" + name, args.home) as context:
            current = context.read()
        observations[name] = current
        value = None if current is None else current.value
        total_current += len(value) if value is not None else 0
        require(total_current <= MAX_TOTAL, "local content exceeds total size limit")
        entry, previous = files.get(name), baseline.get(name)
        if entry is None:
            if previous["status"] == "managed" and previous["policy"] == "merge":
                if current is not None and value == decode(previous["upstream"]):
                    changes.append(Change(name, current, None))
                elif current is not None:
                    print(f"preserved modified upstream-removed file: {name}")
            continue
        upstream = entry["value"]
        policy = entry["policy"]
        if previous and previous["policy"] != policy:
            raise Refusal(
                f"policy changed for tracked file: {name}; manual migration required"
            )
        proposed = value
        if policy == "seed":
            if current is None:
                proposed = upstream
            next_files[name] = {
                "policy": "seed",
                "status": "managed",
                "upstream": encode(upstream),
            }
        elif previous and previous["status"] == "unmanaged" and value != upstream:
            next_files[name] = {"policy": policy, "status": "unmanaged"}
            print(f"unmanaged (no upstream baseline), preserved: {name}")
            continue
        elif previous and previous["status"] == "managed":
            base = decode(previous["upstream"])
            if current is None:
                require(
                    upstream == base,
                    f"delete/modify conflict: {name}; local deletion preserved",
                )
            elif value == base:
                proposed = upstream
            elif upstream != base and value != upstream:
                proposed = merge(value, base, upstream, args.git, name)
            next_files[name] = {
                "policy": policy,
                "status": "managed",
                "upstream": encode(upstream),
            }
        elif current is None:
            proposed = upstream
            next_files[name] = {
                "policy": policy,
                "status": "managed",
                "upstream": encode(upstream),
            }
        elif value == upstream:
            next_files[name] = {
                "policy": policy,
                "status": "managed",
                "upstream": encode(upstream),
            }
        else:
            next_files[name] = {"policy": policy, "status": "unmanaged"}
            print(f"unmanaged (no upstream baseline), preserved: {name}")
            continue
        if proposed != value:
            validate(proposed, entry["validator"], args, name)
            total_proposed += len(proposed)
            require(
                total_proposed <= MAX_TOTAL, "prepared content exceeds total size limit"
            )
            changes.append(Change(name, current, proposed))
    require(len(next_files) <= MAX_FILES, "too many retained baseline files")
    return (
        changes,
        observations,
        json_bytes({"version": 1, "root": args.root, "files": next_files}),
    )


def write_all(fd, value):
    offset = 0
    while offset < len(value):
        offset += os.write(fd, value[offset:])


def rename(parent, source, destination, flags):
    if LIBC.renameat2(
        parent, os.fsencode(source), parent, os.fsencode(destination), flags
    ):
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error), destination)


def candidate(context, value, mode):
    name = ".wahrwelt-reconcile-" + secrets.token_hex(16)
    fd = os.open(
        name,
        os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
        0o600,
        dir_fd=context.parent,
    )
    try:
        write_all(fd, value)
        os.fchmod(fd, mode)
        os.fsync(fd)
    except BaseException:
        try:
            context.validate()
            visible = os.stat(name, dir_fd=context.parent, follow_symlinks=False)
            require(
                metadata(visible) == metadata(os.fstat(fd)), "partial candidate changed"
            )
            os.unlink(name, dir_fd=context.parent)
        except (OSError, Refusal):
            print(
                f"partial recovery candidate retained: {os.path.dirname(context.path)}/{name}",
                file=sys.stderr,
            )
        raise
    finally:
        os.close(fd)
    return name, context.read(MAX_STATE, name=name)


@dataclass
class Undo:
    context: SafePath
    backup: str | None
    displaced: Snapshot | None
    installed: Snapshot | None


def publish(context, expected, value, undos, mode=0o644):
    """Append recovery information immediately after every atomic namespace change."""
    context.validate()
    require(context.read(MAX_STATE) == expected, f"intervening change: {context.path}")
    if value is None:
        backup = ".wahrwelt-reconcile-" + secrets.token_hex(16)
        rename(context.parent, context.name, backup, 1)  # RENAME_NOREPLACE
        undo = Undo(context, backup, expected, None)
        undos.append(undo)
    else:
        backup, staged = candidate(context, value, expected.mode if expected else mode)
        try:
            context.validate()
            require(
                context.read(MAX_STATE) == expected,
                f"intervening change: {context.path}",
            )
            rename(context.parent, backup, context.name, 2 if expected else 1)
        except BaseException:
            if after_rename(context.read(MAX_STATE, name=backup), staged):
                os.unlink(backup, dir_fd=context.parent)
            raise
        undo = Undo(context, backup if expected else None, expected, staged)
        undos.append(undo)
        require(
            after_rename(context.read(MAX_STATE), staged),
            f"published file changed: {context.path}",
        )
    if expected:
        moved = context.read(MAX_STATE, name=backup)
        undo.displaced = moved
        require(
            after_rename(moved, expected),
            f"intervening change during publication: {context.path}",
        )
    context.validate()
    os.fsync(context.parent)


def rollback(undos):
    incomplete = []
    for undo in reversed(undos):
        context = undo.context
        try:
            context.validate()
            visible = context.read(MAX_STATE)
            require(
                (visible is None and undo.installed is None)
                or after_rename(visible, undo.installed),
                "external edit preserved",
            )
            if undo.backup:
                require(
                    after_rename(
                        context.read(MAX_STATE, name=undo.backup), undo.displaced
                    ),
                    "recovery backup changed",
                )
                rename(context.parent, undo.backup, context.name, 2 if visible else 1)
                if visible:
                    moved = context.read(MAX_STATE, name=undo.backup)
                    if not after_rename(moved, undo.installed):
                        # An editor won the race after our comparison. Restore
                        # its inode, retaining our original recovery candidate.
                        rename(context.parent, undo.backup, context.name, 2)
                        raise Refusal("external edit during rollback preserved")
                    os.unlink(undo.backup, dir_fd=context.parent)
            else:
                held = ".wahrwelt-reconcile-" + secrets.token_hex(16)
                rename(context.parent, context.name, held, 1)
                if after_rename(context.read(MAX_STATE, name=held), undo.installed):
                    os.unlink(held, dir_fd=context.parent)
                else:
                    rename(context.parent, held, context.name, 1)
                    raise Refusal("external edit during rollback preserved")
            os.fsync(context.parent)
        except (OSError, Refusal) as error:
            backup = (
                os.path.dirname(context.path) + "/" + undo.backup
                if undo.backup
                else "journal before/after bytes"
            )
            incomplete.append(f"{context.path}: {error}; recovery candidate: {backup}")
    return incomplete


def discard_backups(undos):
    for undo in undos:
        if undo.backup:
            undo.context.validate()
            require(
                after_rename(
                    undo.context.read(MAX_STATE, name=undo.backup), undo.displaced
                ),
                "recovery backup changed; retained",
            )
            os.unlink(undo.backup, dir_fd=undo.context.parent)
            os.fsync(undo.context.parent)


def state_write(context, expected, value):
    undo = []
    try:
        publish(context, expected, value, undo, 0o600)
    except BaseException:
        rollback(undo)
        raise
    discard_backups(undo)
    return context.read(MAX_STATE, private=True)


def inspect_state(args):
    values = []
    for name in ("baseline.json", "journal.json"):
        with SafePath(args.state + "/" + name, args.home, args.state) as context:
            if context.complete:
                require(
                    set(os.listdir(context.parent))
                    <= {"baseline.json", "journal.json"},
                    "unexpected state entries; manual recovery required",
                )
            values.append(context.read(MAX_STATE, private=True))
    if values[1]:
        journal = parse_json(values[1].value)
        require(
            isinstance(journal, dict)
            and journal.get("version") == 1
            and journal.get("root") == args.root
            and journal.get("status") in ("committed", "rolled_back"),
            f"manual recovery required: inspect {args.state}/journal.json; "
            "restore or retain its before/after files, then archive the journal before retrying",
        )
        required = {
            "version",
            "root",
            "status",
            "baseline_before",
            "baseline_after",
            "files",
        }
        require(
            set(journal) in (required, required | {"recovery"}),
            "invalid journal fields",
        )
        for field in ("baseline_before", "baseline_after"):
            value = journal[field]
            if value is None:
                require(field == "baseline_before", "missing journal baseline")
            else:
                load_baseline(Snapshot((), decode(value, MAX_STATE)), args.root)
        entries = journal["files"]
        require(
            isinstance(entries, dict) and len(entries) <= 2 * MAX_FILES,
            "invalid journal files",
        )
        total = 0
        for name, entry in entries.items():
            relative(name)
            require(
                isinstance(entry, dict)
                and set(entry) == {"before", "after", "mode"}
                and type(entry["mode"]) is int
                and 0 <= entry["mode"] <= 0o7777,
                "invalid journal file entry",
            )
            for field in ("before", "after"):
                if entry[field] is not None:
                    total += len(decode(entry[field]))
        require(total <= 2 * MAX_TOTAL, "journal content exceeds total size limit")
        require(
            "recovery" not in journal or journal["recovery"] == [],
            "journal has unresolved recovery entries",
        )
    return values


def apply(args, changes, observations, next_baseline, old_state):
    with contextlib.ExitStack() as stack:
        state = stack.enter_context(
            SafePath(args.state + "/baseline.json", args.home, args.state, create=True)
        )
        journal_path = stack.enter_context(
            SafePath(args.state + "/journal.json", args.home, args.state)
        )
        fcntl.flock(state.parent, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(inspect_state(args) == old_state, "state changed since preparation")
        for name, snapshot in observations.items():
            with SafePath(args.root + "/" + name, args.home) as context:
                require(context.read() == snapshot, f"intervening change: {name}")
        contexts = {}
        for change in changes:
            contexts[change.name] = stack.enter_context(
                SafePath(args.root + "/" + change.name, args.home, create=True)
            )
        journal = {
            "version": 1,
            "root": args.root,
            "status": "publishing",
            "baseline_before": None
            if old_state[0] is None
            else encode(old_state[0].value),
            "baseline_after": encode(next_baseline),
            "files": {
                c.name: {
                    "before": None if c.before is None else encode(c.before.value),
                    "after": None if c.after is None else encode(c.after),
                    "mode": c.before.mode if c.before else 0o644,
                }
                for c in changes
            },
        }
        current_journal = state_write(journal_path, old_state[1], json_bytes(journal))
        undos = []
        try:
            for change in changes:
                publish(contexts[change.name], change.before, change.after, undos)
            for name, snapshot in observations.items():
                if name not in contexts:
                    with SafePath(args.root + "/" + name, args.home) as context:
                        require(
                            context.read() == snapshot, f"intervening change: {name}"
                        )
            for undo in undos:
                current = undo.context.read()
                require(
                    (current is None and undo.installed is None)
                    or after_rename(current, undo.installed),
                    f"intervening change: {undo.context.path}",
                )
            if old_state[0] is None or old_state[0].value != next_baseline:
                publish(state, old_state[0], next_baseline, undos, 0o600)
            journal["status"] = "committed"
            current_journal = state_write(
                journal_path, current_journal, json_bytes(journal)
            )
        except BaseException:
            failures = rollback(undos)
            journal["status"] = "recovery_required" if failures else "rolled_back"
            journal["recovery"] = failures
            try:
                state_write(journal_path, current_journal, json_bytes(journal))
            except (OSError, Refusal) as error:
                print(
                    f"journal update failed; recovery retained: {error}",
                    file=sys.stderr,
                )
            if failures:
                print(
                    "manual recovery required: " + "; ".join(failures), file=sys.stderr
                )
            raise
        discard_backups(undos)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("check", "apply"))
    for name in ("home", "root", "state", "manifest"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--git", default="git")
    parser.add_argument("--luac", default="luac")
    parser.add_argument("--fish", default="fish")
    args = parser.parse_args(argv)
    try:
        require(
            os.getuid() != 0 and os.geteuid() == os.getuid(),
            "run reconciliation as the owning user, never root",
        )
        for path in (args.home, args.root, args.state):
            canonical(path)
        require(
            within(args.root, args.home) and within(args.state, args.home),
            "config root and state must be strictly inside HOME",
        )
        require(
            args.root != args.state
            and not within(args.root, args.state)
            and not within(args.state, args.root),
            "config root and state must be separate",
        )
        with SafePath(args.home + "/.ownership-check", args.home) as home:
            require(home.complete, "HOME must exist")
        files = load_manifest(args.manifest)
        old_state = inspect_state(args)
        baseline = load_baseline(old_state[0], args.root)
        changes, observations, next_baseline = prepare(args, files, baseline)
        if args.mode == "apply" and (
            changes or old_state[0] is None or old_state[0].value != next_baseline
        ):
            apply(args, changes, observations, next_baseline, old_state)
        print(f"dotfile reconciliation {args.mode}: {len(changes)} file change(s)")
        return 0
    except (
        Refusal,
        OSError,
        ValueError,
        binascii.Error,
        subprocess.SubprocessError,
    ) as error:
        print(f"dotfile reconciliation refused: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
