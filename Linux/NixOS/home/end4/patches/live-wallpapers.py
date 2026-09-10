#!/usr/bin/env python3
"""Patch both End4 QuickShell trees with the shared live wallpaper contract."""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path
import re
import shutil
import subprocess
import sys


def fail(message: str) -> None:
    raise SystemExit(f"live-wallpapers patch failed: {message}")


def require_count(text: str, needle: str, count: int, label: str) -> None:
    actual = text.count(needle)
    if actual != count:
        fail(f"{label}: expected {count} matches, found {actual}")


def replace_once(text: str, old: str, new: str, label: str) -> str:
    require_count(text, old, 1, label)
    return text.replace(old, new, 1)


def apply_anchor_patch(root: Path, patch_file: Path, marker: str) -> None:
    result = subprocess.run(
        ["patch", "--fuzz=0", "--forward", "--batch", "-p1", "-i", str(patch_file)],
        cwd=root,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        fail(f"strict upstream anchor patch rejected: {result.stderr.strip() or result.stdout.strip()}")
    marker_path = root / ("ii/modules/ii/background/Background.qml" if (root / "ii").is_dir() else "modules/ii/background/Background.qml")
    text = marker_path.read_text()
    require_count(text, marker, 1, f"{marker_path} anchor marker")


def install_runtime_helpers(root: Path) -> None:
    scripts = root / ("ii/scripts/wallpapers" if (root / "ii").is_dir() else "scripts/wallpapers")
    scripts.mkdir(parents=True, exist_ok=True)
    source_dir = Path(__file__).resolve().parent
    for name in (
        "media-index.py",
        "first-frame-thumbnails.sh",
        "video-backend-reconcile.sh",
    ):
        source = source_dir / name
        target = scripts / name
        if not source.is_file():
            fail(f"missing vendored helper {source}")
        shutil.copy2(source, target)
        target.chmod(0o755)

    renderer_source = source_dir / "LiveWallpaperSurface.qml"
    renderer_target = root / (
        "ii/modules/ii/background/LiveWallpaperSurface.qml"
        if (root / "ii").is_dir()
        else "modules/ii/background/LiveWallpaperSurface.qml"
    )
    if not renderer_source.is_file():
        fail(f"missing vendored renderer {renderer_source}")
    renderer_target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(renderer_source, renderer_target)


SERVICE_SHA256 = {
    "official": "2fae0e588adb47ffbbf07b8ad49ea74125759080c2bb804a6d93c9f1c334b966",
    "pc": "8bb36d0ee14633c8bd25fc62741264e61f63d4f5e180389048562ea19fa205b6",
}


def patch_service(root: Path, variant: str) -> None:
    path = root / ("ii/services/Wallpapers.qml" if (root / "ii").is_dir() else "services/Wallpapers.qml")
    text = path.read_text()
    expected_hash = SERVICE_SHA256[variant]
    actual_hash = hashlib.sha256(path.read_bytes()).hexdigest()
    if actual_hash != expected_hash:
        fail(
            f"upstream Wallpapers.qml drifted in {path}: "
            f"expected sha256 {expected_hash}, found {actual_hash}"
        )
    for anchor, label in (
        ("pragma Singleton", "service singleton declaration"),
        ("FolderListModelWithHistory", "upstream folder model"),
        ("property string searchQuery", "upstream search property"),
    ):
        require_count(text, anchor, 1, f"{label} in {path}")
    template = Path(__file__).resolve().parent / "wallpapers-service.qml"
    path.write_text(template.read_text())


def patch_selector(root: Path) -> None:
    path = root / ("ii/modules/ii/wallpaperSelector/WallpaperSelectorContent.qml" if (root / "ii").is_dir() else "modules/ii/wallpaperSelector/WallpaperSelectorContent.qml")
    text = path.read_text()
    filter_bar = """
                RowLayout {
                    id: mediaFilterBar
                    Layout.fillWidth: true
                    Layout.margins: 4
                    spacing: 4

                    Repeater {
                        model: [
                            { id: \"all\", label: Translation.tr(\"All\") },
                            { id: \"static\", label: Translation.tr(\"Static\") },
                            { id: \"live\", label: Translation.tr(\"Live\") }
                        ]
                        delegate: RippleButton {
                            required property var modelData
                            implicitWidth: 76
                            implicitHeight: 32
                            toggled: Wallpapers.mediaFilter === modelData.id
                            onClicked: Wallpapers.setMediaFilter(modelData.id)
                            contentItem: StyledText {
                                text: modelData.label
                                horizontalAlignment: Text.AlignHCenter
                                color: parent.toggled ? Appearance.colors.colOnPrimary : Appearance.colors.colOnLayer1
                            }
                        }
                    }
                }

"""
    address_bar_anchor = "                AddressBar {"
    top_bar_anchor = "                Item {\n                    id: topBar"
    address_bar_count = text.count(address_bar_anchor)
    top_bar_count = text.count(top_bar_anchor)
    if address_bar_count == 1 and top_bar_count == 0:
        text = text.replace(address_bar_anchor, filter_bar + address_bar_anchor, 1)
    elif address_bar_count == 0 and top_bar_count == 1:
        text = text.replace(top_bar_anchor, filter_bar + top_bar_anchor, 1)
    else:
        fail(
            f"media filter insertion in {path}: expected one selector toolbar anchor, "
            f"found AddressBar={address_bar_count}, topBar={top_bar_count}"
        )
    if (root / "ii").is_dir():
        text = replace_once(text, "                        model: Wallpapers.folderModel", "                        model: Wallpapers.wallpaperModel", f"official wallpaper model in {path}")
    pc_generation = "        Wallpapers.setDirectory(`${Directories.pictures}/Wallpapers`);\n        Qt.callLater(() => Wallpapers.generateThumbnail(thumbnailSizeName));"
    official_generation = "        Wallpapers.generateThumbnail(thumbnailSizeName);"
    if text.count(pc_generation) == 1 and text.count(official_generation) == 0:
        text = text.replace(
            pc_generation,
            "        Wallpapers.generateThumbnail(thumbnailSizeName, Wallpapers.effectiveDirectory);",
            1,
        )
        update_function_end = "    }\n\n    function handleFilePasting(event) {"
        lifecycle = """    }

    Component.onCompleted: Qt.callLater(root.updateThumbnails)

    Connections {
        target: Wallpapers
        function onEffectiveDirectoryChanged() {
            Qt.callLater(() => root.updateThumbnails());
        }
    }

    function handleFilePasting(event) {"""
        text = replace_once(
            text,
            update_function_end,
            lifecycle,
            f"pC selector thumbnail lifecycle in {path}",
        )
    elif text.count(official_generation) == 1 and text.count(pc_generation) == 0:
        text = text.replace(
            official_generation,
            "        Wallpapers.generateThumbnail(thumbnailSizeName, Wallpapers.effectiveDirectory);",
            1,
        )
        official_lifecycle = "    }\n\n    Connections {\n        target: Wallpapers\n        function onDirectoryChanged() {"
        official_replacement = """    }

    Component.onCompleted: Qt.callLater(root.updateThumbnails)

    Connections {
        target: Wallpapers
        function onEffectiveDirectoryChanged() {"""
        text = replace_once(
            text,
            official_lifecycle,
            official_replacement,
            f"official selector thumbnail lifecycle in {path}",
        )
    else:
        fail(f"active directory thumbnail generation missing or ambiguous in {path}")
    path.write_text(text)


def patch_directory_item(root: Path) -> None:
    path = root / ("ii/modules/ii/wallpaperSelector/WallpaperDirectoryItem.qml" if (root / "ii").is_dir() else "modules/ii/wallpaperSelector/WallpaperDirectoryItem.qml")
    text = path.read_text()
    property_match = re.compile(r"(?m)^(\s*)property bool useThumbnail: [^\n]+$")
    text, count = property_match.subn(
        r'\1property bool useThumbnail: fileModelData && !isDirectory && fileModelData.mediaKind !== "unknown"',
        text,
        count=1,
    )
    if count != 1:
        fail(f"thumbnail-only tile anchor missing or ambiguous in {path}")
    if "generateThumbnail: false" not in text:
        fail(f"cached thumbnail anchor missing in {path}")
    path.write_text(text)


def patch_background(root: Path) -> None:
    relative = "ii/modules/ii/background/Background.qml" if (root / "ii").is_dir() else "modules/ii/background/Background.qml"
    path = root / relative
    text = path.read_text()
    property_block = re.compile(
        r"(?m)^(\s*)property bool wallpaperIsVideo: [^\n]+\n\s*property string wallpaperPath: [^\n]+\n"
    )
    source = "bgRoot.effectiveWallpaperPath" if "property string effectiveWallpaperPath:" in text else "Config.options.background.wallpaperPath"
    replacement = (
        "\\1property string wallpaperSourcePath: " + source + "\n"
        "\\1property bool wallpaperIsVideo: Wallpapers.isVideoPath(wallpaperSourcePath)\n"
        "\\1property bool wallpaperIsAnimated: Wallpapers.isAnimatedPath(wallpaperSourcePath)\n"
        "\\1property bool wallpaperIsLive: Wallpapers.isLivePath(wallpaperSourcePath)\n"
        "\\1property string wallpaperPath: wallpaperIsLive ? Wallpapers.fallbackFor(wallpaperSourcePath) : wallpaperSourcePath\n"
    )
    text, count = property_block.subn(replacement, text, count=1)
    if count != 1:
        fail(f"wallpaper source properties missing or ambiguous in {path}")

    item_anchor = re.compile(r"(?m)^(?P<indent>[ \t]*)Item \{\n(?P=indent)    anchors\.fill: parent\n")
    matches = list(item_anchor.finditer(text))
    if len(matches) != 1:
        fail(f"live renderer insertion in {path}: expected one wallpaper item anchor, found {len(matches)}")
    match = matches[0]
    indent = match.group("indent")
    live_surface = (
        f"{indent}LiveWallpaperSurface {{\n"
        f"{indent}    id: liveWallpaperSurface\n"
        f"{indent}    anchors.fill: parent\n"
        f"{indent}    z: 2\n"
        f"{indent}    path: Wallpapers.previewPath || bgRoot.wallpaperSourcePath\n"
        f"{indent}    fallbackPath: Wallpapers.fallbackFor(path)\n"
        f"{indent}    active: !bgRoot.wallpaperSafetyTriggered\n"
        f"{indent}    visible: active && bgRoot.wallpaperIsAnimated\n"
        f"{indent}}}\n\n"
    )
    text = text[: match.start()] + live_surface + text[match.start() :]

    wallpaper_block = re.compile(r"(?m)^(\s+id: wallpaper\b[\s\S]*?^\s+visible:)([^\n]*)")
    text, count = wallpaper_block.subn(r"\1\2 && !bgRoot.wallpaperIsLive", text, count=1)
    if count != 1:
        fail(f"wallpaper image visibility anchor missing or ambiguous in {path}")
    path.write_text(text)


def patch_appearance(root: Path) -> None:
    path = root / ("ii/modules/common/Appearance.qml" if (root / "ii").is_dir() else "modules/common/Appearance.qml")
    text = path.read_text()
    text = replace_once(text, "import qs.modules.common.functions\n", "import qs.modules.common.functions\nimport qs.services\n", f"wallpaper service import in {path}")
    text = replace_once(
        text,
        "        property bool wallpaperIsVideo: wallpaperPath.endsWith(\".mp4\") || wallpaperPath.endsWith(\".webm\") || wallpaperPath.endsWith(\".mkv\") || wallpaperPath.endsWith(\".avi\") || wallpaperPath.endsWith(\".mov\")\n        source: Qt.resolvedUrl(wallpaperIsVideo ? Config.options.background.thumbnailPath : Config.options.background.wallpaperPath)",
        "        property bool wallpaperIsVideo: Wallpapers.isVideoPath(wallpaperPath)\n        property bool wallpaperIsLive: Wallpapers.isLivePath(wallpaperPath)\n        source: Qt.resolvedUrl(wallpaperIsLive ? Wallpapers.fallbackFor(wallpaperPath) : Config.options.background.wallpaperPath)",
        f"wallpaper color fallback in {path}",
    )
    path.write_text(text)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("root", type=Path)
    parser.add_argument("--variant", choices=("official", "pc"), required=True)
    parser.add_argument("--patch", type=Path, required=True)
    args = parser.parse_args()
    root = args.root
    if not root.is_dir():
        fail(f"missing source root {root}")

    apply_anchor_patch(root, args.patch, "// End4 live wallpaper contract anchor.")
    install_runtime_helpers(root)
    patch_service(root, args.variant)
    patch_selector(root)
    patch_directory_item(root)
    patch_background(root)
    patch_appearance(root)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
