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
        "\\1property string wallpaperPath: Wallpapers.imageSourceFor(wallpaperSourcePath)\n"
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
        f"{indent}    z: 0\n"
        f"{indent}    path: bgRoot.wallpaperSourcePath\n"
        f"{indent}    fallbackPath: Wallpapers.fallbackFor(path)\n"
        f"{indent}    active: !bgRoot.wallpaperSafetyTriggered && (!bgRoot.wallpaperIsVideo || GlobalStates.screenLocked)\n"
        f"{indent}    visible: active && bgRoot.wallpaperIsLive\n"
        f"{indent}}}\n\n"
    )
    text = text[: match.start()] + live_surface + text[match.start() :]

    wallpaper_block = re.compile(r"(?m)^(\s+id: wallpaper\b[\s\S]*?^\s+visible:)([^\n]*)")
    text, count = wallpaper_block.subn(r"\1\2 && !bgRoot.wallpaperIsLive", text, count=1)
    if count != 1:
        fail(f"wallpaper image visibility anchor missing or ambiguous in {path}")

    if "property bool videoRevealed:" in text:
        startup_anchor = """            previousWallpaper.source = bgRoot.wallpaperSafetyTriggered ? "" : bgRoot.wallpaperPath
            wallpaper.source = bgRoot.wallpaperSafetyTriggered ? "" : bgRoot.wallpaperPath
            bgRoot.currentWallpaperSource = bgRoot.wallpaperPath
            bgRoot.previousWallpaperSource = ""
            bgRoot.transitionProgress = 1.0
"""
        startup_replacement = """            if (Wallpapers.isVideoPath(bgRoot.wallpaperSourcePath)) {
                previousWallpaper.source = ""
                wallpaper.source = ""
                bgRoot.currentWallpaperSource = ""
                bgRoot.previousWallpaperSource = ""
                bgRoot.transitionPending = false
                bgRoot.transitionProgress = 1.0
                bgRoot.videoRevealed = true
                return
            }
            previousWallpaper.source = bgRoot.wallpaperSafetyTriggered ? "" : bgRoot.wallpaperPath
            wallpaper.source = bgRoot.wallpaperSafetyTriggered ? "" : bgRoot.wallpaperPath
            bgRoot.currentWallpaperSource = bgRoot.wallpaperPath
            bgRoot.previousWallpaperSource = ""
            bgRoot.transitionProgress = 1.0
"""
        text = replace_once(
            text,
            startup_anchor,
            startup_replacement,
            f"video-safe startup in {path}",
        )

        wallpaper_change_anchor = """        onWallpaperPathChanged: {
            bgRoot.videoRevealed = false
"""
        wallpaper_change_replacement = """        onWallpaperPathChanged: {
            if (Wallpapers.isVideoPath(bgRoot.wallpaperSourcePath)) {
                previousWallpaper.source = ""
                wallpaper.source = ""
                bgRoot.currentWallpaperSource = ""
                bgRoot.previousWallpaperSource = ""
                bgRoot.transitionPending = false
                bgRoot.transitionProgress = 1.0
                bgRoot.videoRevealed = true
                return
            }
            bgRoot.videoRevealed = false
            if (bgRoot.currentWallpaperSource === "") {
                bgRoot.transitionPending = false
                wallpaper.source = wallpaperPath
                previousWallpaper.source = wallpaperPath
                bgRoot.currentWallpaperSource = wallpaperPath
                bgRoot.previousWallpaperSource = ""
                bgRoot.transitionProgress = 1.0
                return
            }
"""
        text = replace_once(
            text,
            wallpaper_change_anchor,
            wallpaper_change_replacement,
            f"video-safe wallpaper change in {path}",
        )

        legacy_previous_wallpaper = re.compile(
            r"(?m)^(\s+id: previousWallpaper\b[\s\S]*?^\s+visible:) true$"
        )
        compatible_previous_wallpaper = re.compile(
            r"(?m)^\s+id: previousWallpaper\b[\s\S]*?^\s+visible: !bgRoot\.videoRevealed$"
        )
        legacy_count = len(legacy_previous_wallpaper.findall(text))
        compatible_count = len(compatible_previous_wallpaper.findall(text))
        if legacy_count == 1 and compatible_count == 0:
            text = legacy_previous_wallpaper.sub(
                r"\1 !bgRoot.videoRevealed",
                text,
                count=1,
            )
        elif legacy_count != 0 or compatible_count != 1:
            fail(f"previous wallpaper video reveal anchor missing or ambiguous in {path}")
    path.write_text(text)


def patch_lock_dispatch(root: Path, variant: str) -> None:
    path = root / ("ii/modules/common/panels/lock/LockScreen.qml" if (root / "ii").is_dir() else "modules/common/panels/lock/LockScreen.qml")
    text = path.read_text()
    function_anchor = "    function lock() {\n"
    if variant == "pc":
        wallpaper_expression = """        const custom = Config.options.background.lockWall;
        return custom.length > 0 ? custom : Config.options.background.wallpaperPath;"""
    else:
        wallpaper_expression = "        return Config.options.background.wallpaperPath;"
    function_replacement = f"""    function lockWallpaperPath() {{
{wallpaper_expression}
    }}

    function lock() {{
"""
    text = replace_once(text, function_anchor, function_replacement, f"live lock path helper in {path}")
    text = replace_once(
        text,
        "        if (Config.options.lock.useHyprlock) {",
        "        if (Config.options.lock.useHyprlock && !Wallpapers.isLivePath(root.lockWallpaperPath())) {",
        f"live wallpaper native lock routing in {path}",
    )
    path.write_text(text)


def patch_session_lock(root: Path, variant: str) -> None:
    path = root / ("ii/modules/common/functions/Session.qml" if (root / "ii").is_dir() else "modules/common/functions/Session.qml")
    text = path.read_text()
    if variant == "pc":
        old = """    function lock() {
        if (WM.compositor === "niri") {
            Quickshell.execDetached(["qs", "-c", "end4-pC", "ipc", "call", "lock", "activate"]);
        } else {
            Quickshell.execDetached(["loginctl", "lock-session"]);
        }
    }
"""
        config_name = "end4-pC"
    else:
        old = """    function lock() {
        Quickshell.execDetached(["loginctl", "lock-session"]);
    }
"""
        config_name = "ii"
    new = f"""    function lock() {{
        Quickshell.execDetached(["qs", "-c", "{config_name}", "ipc", "call", "lock", "activate"]);
    }}
"""
    path.write_text(replace_once(text, old, new, f"session lock dispatcher in {path}"))


def patch_lock_setting(root: Path, variant: str) -> None:
    if variant == "pc":
        path = root / "modules/ii/settings/pages/InterfaceConfig.qml"
        old = """                ConfigSwitch {
                    buttonIcon: "water_drop"
                    text: Translation.tr("Use Hyprlock (instead of Quickshell)")
"""
        new = """                ConfigSwitch {
                    readonly property string lockWallpaperPath: Config.options.background.lockWall !== ""
                        ? Config.options.background.lockWall
                        : Config.options.background.wallpaperPath
                    readonly property bool liveWallpaperSelected: Wallpapers.isLivePath(lockWallpaperPath)
                    buttonIcon: "water_drop"
                    enabled: !liveWallpaperSelected
                    text: liveWallpaperSelected
                        ? Translation.tr("Hyprlock is unavailable for live wallpapers")
                        : Translation.tr("Use Hyprlock (instead of Quickshell)")
"""
    else:
        path = root / "ii/modules/settings/InterfaceConfig.qml"
        old = """        ConfigSwitch {
            buttonIcon: "water_drop"
            text: Translation.tr('Use Hyprlock (instead of Quickshell)')
"""
        new = """        ConfigSwitch {
            readonly property string lockWallpaperPath: Config.options.background.wallpaperPath
            readonly property bool liveWallpaperSelected: Wallpapers.isLivePath(lockWallpaperPath)
            buttonIcon: "water_drop"
            enabled: !liveWallpaperSelected
            text: liveWallpaperSelected
                ? Translation.tr("Hyprlock is unavailable for live wallpapers")
                : Translation.tr('Use Hyprlock (instead of Quickshell)')
"""
    text = path.read_text()
    path.write_text(replace_once(text, old, new, f"live wallpaper lock setting in {path}"))


def patch_waffle_lock(root: Path) -> None:
    path = root / ("ii/modules/waffle/lock/WaffleLock.qml" if (root / "ii").is_dir() else "modules/waffle/lock/WaffleLock.qml")
    if not path.is_file():
        return
    text = path.read_text()
    text = replace_once(
        text,
        "import qs.modules.common.panels.lock\n",
        "import qs.modules.common.panels.lock\nimport qs.modules.ii.background\n",
        f"live wallpaper import in {path}",
    )
    old = """        StyledImage {
            id: bg
            z: 0
            width: parent.width
            height: parent.height
            onStatusChanged: {
                if (status === Image.Ready) {
                    print("Lock wallpaper loaded");
                    print(lockSurfaceItem.height);
                    y = -lockSurfaceItem.height;
                    openAnim.restart();
                }
            }
            source: Config.options.background.wallpaperPath
            fillMode: Image.PreserveAspectCrop

            PropertyAnimation {
                id: openAnim
                target: bg
                property: "y"
                to: 0
                duration: 350
                easing.type: Easing.BezierSpline
                easing.bezierCurve: Looks.transition.easing.bezierCurve.easeIn
            }
        }
"""
    new = """        Item {
            id: bg
            z: 0
            width: parent.width
            height: parent.height
            property string wallpaperPath: Config.options.background.wallpaperPath

            Component.onCompleted: {
                y = -lockSurfaceItem.height;
                openAnim.restart();
            }

            StyledImage {
                anchors.fill: parent
                visible: !Wallpapers.isLivePath(bg.wallpaperPath)
                source: visible ? bg.wallpaperPath : ""
                fillMode: Image.PreserveAspectCrop
            }

            LiveWallpaperSurface {
                anchors.fill: parent
                path: bg.wallpaperPath
                fallbackPath: Wallpapers.fallbackFor(path)
                active: true
                visible: Wallpapers.isLivePath(bg.wallpaperPath)
            }

            PropertyAnimation {
                id: openAnim
                target: bg
                property: "y"
                to: 0
                duration: 350
                easing.type: Easing.BezierSpline
                easing.bezierCurve: Looks.transition.easing.bezierCurve.easeIn
            }
        }
"""
    text = replace_once(text, old, new, f"Waffle lock live wallpaper in {path}")
    path.write_text(text)


def patch_pc_lock_surface(root: Path) -> None:
    path = root / "modules/ii/lock/LockSurface.qml"
    text = path.read_text()
    text = replace_once(
        text,
        "import qs.modules.common.panels.lock\n",
        "import qs.modules.common.panels.lock\nimport qs.modules.ii.background\n",
        f"live wallpaper import in {path}",
    )
    text = replace_once(
        text,
        "    Loader {\n        anchors.fill: parent\n        z: -1\n        active: WM.compositor === \"niri\"\n",
        "    Loader {\n        id: lockBackgroundLoader\n        anchors.fill: parent\n        z: -1\n        active: WM.compositor === \"niri\"\n",
        f"pC lock wallpaper surface activation in {path}",
    )
    old = """        sourceComponent: Item {
            anchors.fill: parent

            Image {
                id: lockBgSource
                anchors.fill: parent
                source: Config.options.background.wallpaperPath
                fillMode: Image.PreserveAspectCrop
                asynchronous: true
                cache: true
                visible: false
            }
            FastBlur {
                anchors.fill: parent
                source: lockBgSource
                radius: 0 // fixme
            }
        }
"""
    new = """        sourceComponent: Item {
            id: niriBackground
            anchors.fill: parent
            property string wallpaperPath: Config.options.background.lockWall !== ""
                ? Config.options.background.lockWall
                : Config.options.background.wallpaperPath

            Image {
                id: lockBgSource
                anchors.fill: parent
                source: visible ? niriBackground.wallpaperPath : ""
                fillMode: Image.PreserveAspectCrop
                asynchronous: true
                cache: true
                visible: !Wallpapers.isLivePath(niriBackground.wallpaperPath)
            }
            FastBlur {
                anchors.fill: parent
                source: lockBgSource
                radius: 0 // fixme
                visible: lockBgSource.visible
            }
            LiveWallpaperSurface {
                anchors.fill: parent
                path: niriBackground.wallpaperPath
                fallbackPath: Wallpapers.fallbackFor(path)
                active: true
                visible: Wallpapers.isLivePath(niriBackground.wallpaperPath)
            }
        }
"""
    text = replace_once(text, old, new, f"pC lock live wallpaper in {path}")
    path.write_text(text)


def patch_image_consumers(root: Path, variant: str) -> None:
    tree = root / "ii" if (root / "ii").is_dir() else root

    def replace(relative: str, old: str, new: str, label: str) -> None:
        path = tree / relative
        text = path.read_text()
        path.write_text(replace_once(text, old, new, f"{label} in {path}"))

    abstract_widget = "modules/ii/background/widgets/AbstractBackgroundWidget.qml"
    abstract_path = tree / abstract_widget
    abstract_text = abstract_path.read_text()
    abstract_text = replace_once(
        abstract_text,
        "import qs\nimport qs.modules.common\n",
        "import qs\nimport qs.services\nimport qs.modules.common\n",
        f"wallpaper service import in {abstract_path}",
    )
    abstract_text = replace_once(
        abstract_text,
        '    property bool wallpaperIsVideo: Config.options.background.wallpaperPath.endsWith(".mp4") || Config.options.background.wallpaperPath.endsWith(".webm") || Config.options.background.wallpaperPath.endsWith(".mkv") || Config.options.background.wallpaperPath.endsWith(".avi") || Config.options.background.wallpaperPath.endsWith(".mov")\n    property string wallpaperPath: wallpaperIsVideo ? Config.options.background.thumbnailPath : Config.options.background.wallpaperPath',
        "    property bool wallpaperIsVideo: Wallpapers.isVideoPath(Config.options.background.wallpaperPath)\n    property string wallpaperPath: Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)",
        f"widget wallpaper fallback in {abstract_path}",
    )
    abstract_path.write_text(abstract_text)

    if variant == "pc":
        replace(
            "modules/ii/settings/pages/QuickConfig.qml",
            "                        source: /\\.(mp4|webm|mkv|avi|mov)$/i.test(Config.options.background.wallpaperPath)\n                            ? Config.options.background.thumbnailPath\n                            : Config.options.background.wallpaperPath",
            "                        source: Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)",
            "pC settings wallpaper preview fallback",
        )
        replace(
            "modules/ii/wallpaperSelector/WallpaperSelectorContent.qml",
            "                source: Config.options.background.wallpaperPath",
            "                source: Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)",
            "pC selector blur fallback",
        )
        replace(
            "modules/ii/background/NiriBackdrop.qml",
            "                source: backdrop.wallpaperPath",
            "                source: Wallpapers.imageSourceFor(backdrop.wallpaperPath)",
            "pC Niri backdrop fallback",
        )
        replace(
            "modules/ii/overview/NiriOverview.qml",
            "                            source: Config.options.background.wallpaperPath",
            "                            source: Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)",
            "pC overview fallback",
        )
        replace(
            "modules/ii/sidebarRight/SidebarRightContent.qml",
            "                                    source: Config.options.sidebar.bannerImage !== \"\" \n                                        ? Config.options.sidebar.bannerImage \n                                        : Config.options.background.wallpaperPath",
            "                                    source: Config.options.sidebar.bannerImage !== \"\" \n                                        ? Config.options.sidebar.bannerImage \n                                        : Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)",
            "pC sidebar banner fallback",
        )
        replace(
            "modules/ii/background/widgets/usercard/UserCardWidget.qml",
            '                    property string effectiveSource: "file://" + (GlobalStates.screenLocked && Config.options.background.lockWall !== ""\n                        ? Config.options.background.lockWall\n                        : Config.options.background.wallpaperPath)',
            '                    property string rawSource: GlobalStates.screenLocked && Config.options.background.lockWall !== ""\n                        ? Config.options.background.lockWall\n                        : Config.options.background.wallpaperPath\n                    property url effectiveSource: Qt.resolvedUrl(Wallpapers.imageSourceFor(rawSource))',
            "pC user card wallpaper fallback",
        )
        return

    replace(
        "modules/settings/QuickConfig.qml",
        "                    source: Config.options.background.wallpaperPath",
        "                    source: Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)",
        "official settings wallpaper preview fallback",
    )
    replace(
        "modules/waffle/polkit/WPolkitContent.qml",
        "        source: Config.options.background.wallpaperPath",
        "        source: Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)",
        "official polkit wallpaper fallback",
    )
    replace(
        "modules/waffle/taskView/TaskViewWorkspace.qml",
        "                    source: Config.options.background.wallpaperPath",
        "                    source: Wallpapers.imageSourceFor(Config.options.background.wallpaperPath)",
        "official task view wallpaper fallback",
    )

    waffle_path = tree / "modules/waffle/background/WaffleBackground.qml"
    waffle_text = waffle_path.read_text()
    waffle_text = replace_once(
        waffle_text,
        "import qs.modules.ii.background.widgets\n",
        "import qs.modules.ii.background\nimport qs.modules.ii.background.widgets\n",
        f"official Waffle live wallpaper import in {waffle_path}",
    )
    waffle_text = replace_once(
        waffle_text,
        "        color: \"transparent\"\n\n        StyledImage {\n            anchors.fill: parent\n            source: Config.options.background.wallpaperPath\n            fillMode: Image.PreserveAspectCrop\n        }",
        """        color: \"transparent\"
        property string wallpaperPath: Config.options.background.wallpaperPath

        StyledImage {
            anchors.fill: parent
            source: Wallpapers.imageSourceFor(panelRoot.wallpaperPath)
            visible: !Wallpapers.isLivePath(panelRoot.wallpaperPath)
            fillMode: Image.PreserveAspectCrop
        }

        LiveWallpaperSurface {
            anchors.fill: parent
            path: panelRoot.wallpaperPath
            fallbackPath: Wallpapers.fallbackFor(path)
            active: true
            visible: Wallpapers.isLivePath(path)
        }""",
        f"official Waffle live wallpaper surface in {waffle_path}",
    )
    waffle_path.write_text(waffle_text)


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


def qml_property_block(text: str, marker: str, label: str) -> tuple[int, int]:
    start = text.find(marker)
    if start < 0 or text.find(marker, start + len(marker)) >= 0:
        fail(f"{label}: expected exactly one {marker!r}")
    marker_brace = marker.rfind("{")
    brace = start + marker_brace if marker_brace >= 0 else text.find("{", start + len(marker))
    if brace < 0:
        fail(f"{label}: opening brace missing")
    depth = 0
    for index in range(brace, len(text)):
        if text[index] == "{":
            depth += 1
        elif text[index] == "}":
            depth -= 1
            if depth == 0:
                end = index + 1
                if end < len(text) and text[end] == "\n":
                    end += 1
                return start, end
    fail(f"{label}: closing brace missing")


def patch_official_pc_widget_compat(root: Path) -> None:
    path = root / "ii/modules/common/Config.qml"
    text = path.read_text()
    marker = "                property JsonObject widgets: JsonObject {"
    start, end = qml_property_block(text, marker, f"Official widget schema in {path}")
    fragment = Path(__file__).resolve().parent / "official-pc-widgets.qml"
    if not fragment.is_file():
        fail(f"missing vendored pC widget compatibility schema {fragment}")
    replacement = fragment.read_text()
    if not replacement.endswith("\n"):
        fail(f"pC widget compatibility schema must end with a newline: {fragment}")
    path.write_text(text[:start] + replacement + text[end:])


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
    patch_lock_dispatch(root, args.variant)
    patch_session_lock(root, args.variant)
    patch_lock_setting(root, args.variant)
    patch_waffle_lock(root)
    if args.variant == "pc":
        patch_pc_lock_surface(root)
    patch_image_consumers(root, args.variant)
    patch_appearance(root)
    if args.variant == "official":
        patch_official_pc_widget_compat(root)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
