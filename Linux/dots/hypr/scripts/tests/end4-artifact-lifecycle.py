"""Reject QuickShell lifecycle ownership outside the managed launcher."""

from __future__ import annotations

import re
import stat
import sys
from pathlib import Path

SHELL_KILL = re.compile(r"(?<![A-Za-z0-9_])(?:kill|pkill|killall)\b([^;\n]*)")
SHELL_EXEC = re.compile(
    r"(?:^|(?<=[ \t;|&(\[\"'`]))(?:[^\s;'\"`()\[\],]+/)?(?:qs|quickshell)"
    r"(?=(?:[ \t]+(?:-|[;|&<>)]))|[;|&<>\n)]|$)",
    re.MULTILINE,
)
SHELL_IPC_TAIL = re.compile(
    r"^\s+-c\s+(?:[^\s\"();&|`]+|\"[^\"]+\"|'[^']+')\s+ipc\s+call"
    r"(?:\s|[;|&\"'`)]|$)"
)
ARRAY_EXEC = re.compile(
    r"\[\s*['\"](?:qs|quickshell)['\"]\s*,(?P<tail>.*?)\]",
    re.DOTALL,
)
ARRAY_IPC_TAIL = re.compile(
    r"^\s*['\"]-c['\"]\s*,\s*[^,\[\]]+\s*,\s*"
    r"['\"]ipc['\"]\s*,\s*['\"]call['\"](?:\s*,|\s*$)",
    re.DOTALL,
)
ARRAY_KILL = re.compile(
    r"\[\s*['\"](?:kill|pkill|killall)['\"]\s*,(?P<tail>.*?)\]",
    re.DOTALL,
)
HYPRIDLE_EXEC = re.compile(
    r"(?:^|(?<=[ \t=;|&(\[\"'`]))(?:[^\s;'\"`()\[\],]+/)?hypridle"
    r"(?=(?:[ \t]+(?:-|[;|&<>)]))|[;|&<>\n)]|$)",
    re.MULTILINE,
)
HYPRIDLE_ARRAY_EXEC = re.compile(
    r"\[\s*['\"](?:[^'\"\s,]+/)?hypridle['\"](?:\s*,|\s*\])",
    re.DOTALL,
)
QUOTED_EXACT_EXEC = re.compile(
    r"(?:exec(?:_cmd|Cmd|Detached)?|spawn|system|popen|run)\s*\(\s*"
    r"(?P<quote>['\"])\s*(?:[^'\"\s(),]+/)*"
    r"(?P<command>qs|quickshell|hypridle)\s*(?P=quote)",
    re.DOTALL,
)
LINE_QUOTED_EXACT_EXEC = re.compile(
    r"^[ \t]*(?P<quote>['\"])(?:[^'\"\s(),]+/)*"
    r"(?P<command>qs|quickshell|hypridle)(?P=quote)"
    r"[ \t]*(?:[;|&<>]|$)",
    re.MULTILINE,
)
ARRAY_BARE_EXEC = re.compile(
    r"\[\s*['\"](?P<command>qs|quickshell)['\"]\s*\]",
    re.DOTALL,
)
WRAPPED_EXEC_ARRAY = re.compile(
    r"(?:exec(?:_cmd|Cmd|Detached)?|spawn|system|popen|run)\s*\(\s*"
    r"\[(?P<body>.*?)\]\s*\)",
    re.DOTALL,
)
QUOTED_LIFECYCLE_ARG = re.compile(
    r"(?P<quote>['\"])(?:[^'\"\s(),]+/)*"
    r"(?P<command>qs|quickshell|hypridle)(?P=quote)"
)
QUICKSHELL_WORD = re.compile(r"(?<![A-Za-z0-9_-])(?:qs|quickshell)(?![A-Za-z0-9_-])")

TEXT_SUFFIXES = {
    ".bash",
    ".conf",
    ".desktop",
    ".fish",
    ".js",
    ".lua",
    ".mjs",
    ".py",
    ".qml",
    ".service",
    ".sh",
    ".ts",
    ".zsh",
}


def fail(path: Path, line_number: int, message: str) -> None:
    raise SystemExit(f"FAIL: {message}: {path}:{line_number}")


def line_number(text: str, offset: int) -> int:
    return text.count("\n", 0, offset) + 1


def starts_in_comment(text: str, offset: int) -> bool:
    line_start = text.rfind("\n", 0, offset) + 1
    prefix = text[line_start:offset].lstrip()
    return prefix.startswith(("#", "--", "//"))


def starts_in_qml_import(path: Path, text: str, offset: int) -> bool:
    if path.suffix != ".qml":
        return False
    line_start = text.rfind("\n", 0, offset) + 1
    return text[line_start:offset].strip() == "import"


def verify_text(path: Path, text: str) -> None:
    normalized = text.replace("\\\n", " ")

    for match in WRAPPED_EXEC_ARRAY.finditer(normalized):
        if starts_in_comment(normalized, match.start()):
            continue
        body = match.group("body")
        lifecycle_arg = QUOTED_LIFECYCLE_ARG.search(body)
        if lifecycle_arg is None:
            continue
        command = lifecycle_arg.group("command")
        first_quickshell = re.match(
            r"\s*['\"](?:qs|quickshell)['\"]\s*,(?P<tail>.*)",
            body,
            re.DOTALL,
        )
        if (
            command != "hypridle"
            and first_quickshell is not None
            and ARRAY_IPC_TAIL.match(first_quickshell.group("tail"))
        ):
            continue
        if command == "hypridle":
            message = (
                "realized End4 artifact contains a direct hypridle lifecycle launch "
                "outside start-shell.sh"
            )
        else:
            message = (
                "realized End4 artifact contains a direct QuickShell lifecycle launch "
                "outside start-shell.sh"
            )
        fail(path, line_number(normalized, match.start()), message)

    for pattern in (QUOTED_EXACT_EXEC, LINE_QUOTED_EXACT_EXEC, ARRAY_BARE_EXEC):
        for match in pattern.finditer(normalized):
            if starts_in_comment(normalized, match.start()):
                continue
            command = match.group("command")
            if command == "hypridle":
                message = (
                    "realized End4 artifact contains a direct hypridle lifecycle launch "
                    "outside start-shell.sh"
                )
            else:
                message = (
                    "realized End4 artifact contains a direct QuickShell lifecycle launch "
                    "outside start-shell.sh"
                )
            fail(path, line_number(normalized, match.start()), message)

    for match in SHELL_KILL.finditer(normalized):
        if starts_in_comment(normalized, match.start()):
            continue
        if not QUICKSHELL_WORD.search(match.group(1)):
            continue
        fail(
            path,
            line_number(normalized, match.start()),
            "realized End4 artifact contains raw QuickShell process control outside start-shell.sh",
        )

    for match in ARRAY_KILL.finditer(normalized):
        if starts_in_comment(normalized, match.start()):
            continue
        if not QUICKSHELL_WORD.search(match.group("tail")):
            continue
        fail(
            path,
            line_number(normalized, match.start()),
            "realized End4 artifact contains raw QuickShell process control outside start-shell.sh",
        )

    for pattern in (HYPRIDLE_EXEC, HYPRIDLE_ARRAY_EXEC):
        for match in pattern.finditer(normalized):
            if starts_in_comment(normalized, match.start()):
                continue
            fail(
                path,
                line_number(normalized, match.start()),
                "realized End4 artifact contains a direct hypridle lifecycle launch outside start-shell.sh",
            )

    for match in ARRAY_EXEC.finditer(normalized):
        if ARRAY_IPC_TAIL.match(match.group("tail")):
            continue
        fail(
            path,
            line_number(normalized, match.start()),
            "realized End4 artifact contains a direct QuickShell lifecycle launch outside start-shell.sh",
        )

    for match in SHELL_EXEC.finditer(normalized):
        if starts_in_comment(normalized, match.start()) or starts_in_qml_import(
            path, normalized, match.start()
        ):
            continue
        tail = normalized[match.end() :]
        if SHELL_IPC_TAIL.match(tail):
            continue
        fail(
            path,
            line_number(normalized, match.start()),
            "realized End4 artifact contains a direct QuickShell lifecycle launch outside start-shell.sh",
        )


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit(f"usage: {sys.argv[0]} END4_ARTIFACT")
    artifact = Path(sys.argv[1])
    for path in sorted(artifact.rglob("*")):
        if not path.is_file():
            continue
        try:
            mode = path.stat().st_mode
        except OSError as error:
            raise SystemExit(
                f"FAIL: cannot stat realized End4 artifact file {path}: {error}"
            ) from error
        if path.suffix.lower() not in TEXT_SUFFIXES and not (
            not path.suffix and mode & (stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
        ):
            continue
        try:
            text = path.read_text(errors="replace")
        except OSError as error:
            raise SystemExit(
                f"FAIL: cannot read realized End4 artifact file {path}: {error}"
            ) from error
        verify_text(path, text)


if __name__ == "__main__":
    main()
