"""Connect Home Manager's default profile to the existing Zen profile."""

import configparser
import sys
from pathlib import Path


def prepare(home: Path, root: Path, *, check_only: bool = False) -> None:
    if not root.is_absolute():
        root = home / root
    legacy = home / ".zen"
    registry = root / "profiles.ini"
    legacy_registry = legacy / "profiles.ini"
    use_legacy = not registry.exists() and legacy_registry.exists()

    if use_legacy:
        if root.is_symlink() or (root.exists() and any(root.iterdir())):
            raise ValueError(f"Zen profile roots conflict: {root} and {legacy}")
        source_root = legacy
    else:
        source_root = root
        if (
            registry.exists()
            and legacy_registry.exists()
            and root.resolve() != legacy.resolve()
        ):
            raise ValueError(f"Zen profile roots conflict: {root} and {legacy}")

    source_registry = source_root / "profiles.ini"
    target = source_root / "default"
    if source_registry.exists():
        ini = configparser.ConfigParser(interpolation=None)
        ini.read(source_registry)
        profiles = [
            ini[s]
            for s in ini.sections()
            if s.startswith("Profile") and "Path" in ini[s]
        ]
        installs = {
            ini[s]["Default"]
            for s in ini.sections()
            if s.startswith("Install") and "Default" in ini[s]
        }
        defaults = [p for p in profiles if p.get("Default") == "1"]
        if len(defaults) == 1:
            selected = defaults[0]
        elif not defaults and len(profiles) == 1:
            selected = profiles[0]
        else:
            raise ValueError(
                f"Select one default profile in {source_registry} before activating Zen"
            )
        if installs and installs != {selected["Path"]}:
            raise ValueError(
                f"Zen has conflicting installation defaults in {source_registry}"
            )
        target = Path(selected["Path"])
        if selected.get("IsRelative", "1") == "1":
            target = source_root / target
        if not target.is_absolute() or not target.is_dir():
            raise ValueError(f"Zen profile does not exist: {target}")

    alias = source_root / "default"
    if (alias.is_symlink() or alias.exists()) and (
        alias.resolve() != target.resolve() or not alias.is_dir()
    ):
        raise ValueError(f"Zen profile path is already occupied: {alias}")

    if check_only:
        return

    # Validate all paths before changing anything. Never move profile data or
    # replace the browser's registry; an alias lets the upstream module use it.
    if use_legacy:
        root.parent.mkdir(parents=True, exist_ok=True)
        if root.exists():
            root.rmdir()  # Only an empty directory passed validation above.
        root.symlink_to(legacy, target_is_directory=True)
    else:
        root.mkdir(parents=True, exist_ok=True)
    if not source_registry.exists():
        target.mkdir(exist_ok=True)
        with source_registry.open("x") as stream:
            stream.write(
                "[Profile0]\nName=default\nIsRelative=1\nPath=default\nDefault=1\n\n"
                "[General]\nStartWithLastProfile=1\nVersion=2\n"
            )
    elif alias != target and not alias.is_symlink():
        alias.symlink_to(target.resolve(), target_is_directory=True)


if __name__ == "__main__":
    try:
        prepare(
            Path(sys.argv[1]), Path(sys.argv[2]), check_only="--check" in sys.argv[3:]
        )
    except (OSError, ValueError, configparser.Error) as error:
        sys.exit(f"zen-profile: {error}")
