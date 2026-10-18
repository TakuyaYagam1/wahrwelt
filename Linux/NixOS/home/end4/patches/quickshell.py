"""end4 QuickShell config patcher.

Transforms the upstream end-4 illogical-impulse QuickShell sources so they
co-exist with our Hyprland config under ~/.config/hypr/end4 and ~/.config/quickshell/ii.
Each `replace_once` call asserts the upstream text shape so that the build
fails loudly if end-4 renames or reorders a snippet.

Invoked from Linux/NixOS/home/end4/patches/quickshell.nix inside a runCommand
derivation; it never runs at activation time.
"""

import sys
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if old not in text:
        raise SystemExit(f"{label} not found")
    return text.replace(old, new, 1)


def patch_hyprland_xkb(root: Path) -> None:
    path = root / "ii/services/HyprlandXkb.qml"
    text = path.read_text()

    text = replace_once(
        text,
        "    property bool needsLayoutRefresh: false\n",
        "    property bool needsLayoutRefresh: false\n"
        '    property string mainKeyboardName: ""\n',
        "HyprlandXkb needsLayoutRefresh property",
    )
    text = replace_once(
        text,
        '                const hyprlandKeyboard = parsedOutput["keyboards"].find(kb => kb.main === true);\n'
        '                root.layoutCodes = hyprlandKeyboard["layout"].split(",");\n'
        '                root.currentLayoutName = hyprlandKeyboard["active_keymap"];\n',
        '                const hyprlandKeyboard = parsedOutput["keyboards"].find(kb => kb.main === true);\n'
        '                root.mainKeyboardName = hyprlandKeyboard["name"];\n'
        '                root.layoutCodes = hyprlandKeyboard["layout"].split(",");\n'
        '                root.currentLayoutName = hyprlandKeyboard["active_keymap"];\n',
        "HyprlandXkb keyboard parse block",
    )

    old = """                // Update when layout might have changed
                const dataString = event.data;
                root.currentLayoutName = dataString.substring(dataString.indexOf(",") + 1);

                // Update layout for on-screen keyboard (osk)
                Config.options.osk.layout = root.currentLayoutName.split(" (")[0];"""

    new = """                // Update when layout might have changed
                const dataString = event.data;
                const keyboardName = dataString.substring(0, dataString.indexOf(","));
                if (root.mainKeyboardName !== "" && keyboardName !== root.mainKeyboardName) return;

                root.currentLayoutName = dataString.substring(dataString.indexOf(",") + 1);

                Quickshell.execDetached([
                    "notify-send",
                    Translation.tr("Keyboard layout"),
                    root.currentLayoutName,
                    "-a",
                    "Shell",
                    "-i",
                    "input-keyboard",
                    "-h",
                    "string:x-canonical-private-synchronous:keyboard-layout"
                ]);

                // Update layout for on-screen keyboard (osk)
                Config.options.osk.layout = root.currentLayoutName.split(" (")[0];"""

    if "x-canonical-private-synchronous:keyboard-layout" not in text:
        text = replace_once(text, old, new, "HyprlandXkb layout update block")

    path.write_text(text)


def patch_cheatsheet_keybinds(root: Path) -> None:
    path = root / "ii/modules/ii/cheatsheet/CheatsheetKeybinds.qml"
    text = path.read_text()

    old = "    readonly property var keybinds: HyprlandKeybinds.keybinds"
    if old not in text:
        # Newer end-4 reads Hyprland 0.55 Lua bind descriptions directly from
        # `hyprctl binds -j`, so the old static cheatsheet model no longer
        # exists or needs replacement.
        return

    new = r"""    readonly property var keybinds: ({
        "children": [
            {
                "children": [
                    {
                        "name": Translation.tr("Shell"),
                        "keybinds": [
                            {"mods": ["Super"], "key": "Super_L", "comment": Translation.tr("Search / launcher")},
                            {"mods": ["Super"], "key": "Slash", "comment": Translation.tr("Keybinds")},
                            {"mods": ["Super"], "key": "Tab", "comment": Translation.tr("Overview")},
                            {"mods": ["Super"], "key": "V", "comment": Translation.tr("Clipboard")},
                            {"mods": ["Super"], "key": "Period", "comment": Translation.tr("Emoji picker")},
                            {"mods": ["Super"], "key": "N", "comment": Translation.tr("Right sidebar")},
                            {"mods": ["Super", "Shift"], "key": "W", "comment": Translation.tr("Shell selector")}
                        ]
                    },
                    {
                        "name": Translation.tr("Window"),
                        "keybinds": [
                            {"mods": ["Super"], "key": "Q", "comment": Translation.tr("Close window")},
                            {"mods": ["Super"], "key": "Space", "comment": Translation.tr("Toggle floating")},
                            {"mods": ["Super"], "key": "Z", "comment": Translation.tr("Move window")},
                            {"mods": ["Super"], "key": "X", "comment": Translation.tr("Resize window")},
                            {"mods": ["Super"], "key": "P", "comment": Translation.tr("Pin window")},
                            {"mods": ["Ctrl", "Shift"], "key": "Return", "comment": Translation.tr("Fullscreen")},
                            {"mods": ["Super", "Ctrl"], "key": "Return", "comment": Translation.tr("Fullscreen")},
                            {"mods": ["Super", "Shift"], "key": "Return", "comment": Translation.tr("Fullscreen with borders")}
                        ]
                    }
                ]
            },
            {
                "children": [
                    {
                        "name": Translation.tr("Workspaces"),
                        "keybinds": [
                            {"mods": ["Super"], "key": "1-0", "comment": Translation.tr("Go to workspace")},
                            {"mods": ["Ctrl", "Super"], "key": "1-0", "comment": Translation.tr("Go to grouped workspace")},
                            {"mods": ["Ctrl", "Super"], "key": "Left/Right", "comment": Translation.tr("Previous / next workspace")},
                            {"mods": ["Super", "Shift"], "key": "1-0", "comment": Translation.tr("Move window to workspace")},
                            {"mods": ["Ctrl", "Super", "Alt"], "key": "1-0", "comment": Translation.tr("Move window to grouped workspace")}
                        ]
                    },
                    {
                        "name": Translation.tr("Apps"),
                        "keybinds": [
                            {"mods": ["Super"], "key": "Return", "comment": Translation.tr("Terminal")},
                            {"mods": ["Super"], "key": "E", "comment": Translation.tr("File manager")},
                            {"mods": ["Super", "Alt"], "key": "E", "comment": Translation.tr("Alternate file manager")},
                            {"mods": ["Super", "Shift"], "key": "F", "comment": Translation.tr("Zen Browser")},
                            {"mods": ["Super", "Shift"], "key": "C", "comment": Translation.tr("Cursor")},
                            {"mods": ["Super", "Shift"], "key": "V", "comment": Translation.tr("VS Code")},
                            {"mods": ["Super", "Shift"], "key": "Z", "comment": Translation.tr("Zed")},
                            {"mods": ["Ctrl", "Shift"], "key": "Escape", "comment": Translation.tr("btop")}
                        ]
                    }
                ]
            },
            {
                "children": [
                    {
                        "name": Translation.tr("Utilities"),
                        "keybinds": [
                            {"mods": ["Super"], "key": "S", "comment": Translation.tr("Screenshot to file + clipboard")},
                            {"mods": ["Super", "Shift"], "key": "S", "comment": Translation.tr("Region screenshot")},
                            {"mods": ["Super", "Shift", "Alt"], "key": "S", "comment": Translation.tr("Region screenshot to editor")},
                            {"mods": ["Super"], "key": "R", "comment": Translation.tr("Toggle screen recording")},
                            {"mods": ["Super"], "key": "C", "comment": Translation.tr("Color picker")},
                            {"mods": ["Ctrl", "Alt"], "key": "V", "comment": Translation.tr("Volume mixer")},
                            {"mods": ["Ctrl", "Alt"], "key": "Escape", "comment": Translation.tr("Process manager")}
                        ]
                    },
                    {
                        "name": Translation.tr("Session / media"),
                        "keybinds": [
                            {"mods": ["Super"], "key": "L", "comment": Translation.tr("Lock")},
                            {"mods": ["Super", "Shift"], "key": "L", "comment": Translation.tr("Sleep")},
                            {"mods": ["Ctrl", "Super"], "key": "Space", "comment": Translation.tr("Play / pause media")},
                            {"mods": ["Ctrl", "Super"], "key": "Equal", "comment": Translation.tr("Next track")},
                            {"mods": ["Ctrl", "Super"], "key": "Minus", "comment": Translation.tr("Previous track")}
                        ]
                    }
                ]
            }
        ]
    })"""

    path.write_text(replace_once(text, old, new, "cheatsheet keybinds property"))


def patch_cheatsheet_tabs(root: Path) -> None:
    path = root / "ii/modules/ii/cheatsheet/Cheatsheet.qml"
    text = path.read_text()

    old_tabs = """    property var tabButtonList: [
        {
            "icon": "keyboard",
            "name": Translation.tr("Keybinds")
        },
        {
            "icon": "experiment",
            "name": Translation.tr("Elements")
        },
    ]"""
    new_tabs = """    property var tabButtonList: [
        {
            "icon": "keyboard",
            "name": Translation.tr("Keybinds")
        },
    ]"""
    old_pages = """                        CheatsheetKeybinds {}
                        CheatsheetPeriodicTable {}"""
    new_pages = """                        CheatsheetKeybinds {}"""

    text = replace_once(text, old_tabs, new_tabs, "cheatsheet tab list")
    text = replace_once(text, old_pages, new_pages, "cheatsheet pages")
    text = text.replace(
        "currentIndex: Persistent.states.cheatsheet.tabIndex", "currentIndex: 0", 1
    )
    path.write_text(text)


def transparency_controls_qml(switch_block: str) -> str:
    qml = (
        """
        function applyWindowTransparency(transparency) {
            const inactiveOpacity = Math.max(0.05, Math.min(1.0, 1.0 - transparency)).toFixed(3);
            Quickshell.execDetached(["bash", "-c",
                `hyprctl keyword decoration:active_opacity 1; ` +
                `hyprctl keyword decoration:inactive_opacity ${inactiveOpacity}; ` +
                `hyprctl keyword decoration:fullscreen_opacity 1; ` +
                `hyprctl keyword windowrule "opacity 1 override ${inactiveOpacity} override 1 override, match:fullscreen false"`
            ]);
        }

"""
        + switch_block
        + """

        ConfigSlider {
            buttonIcon: "layers"
            text: Translation.tr("Base")
            textWidth: 72
            value: Config.options.appearance.transparency.backgroundTransparency * 100
            usePercentTooltip: false
            from: 0
            to: 90
            stopIndicatorValues: [20]
            onValueChanged: {
                Config.options.appearance.transparency.backgroundTransparency = value / 100;
                if (Config.options.appearance.transparency.enable) {
                    applyWindowTransparency(value / 100);
                }
            }
        }

        ConfigSlider {
            buttonIcon: "interests"
            text: Translation.tr("Content")
            textWidth: 72
            value: Config.options.appearance.transparency.contentTransparency * 100
            usePercentTooltip: false
            from: 0
            to: 90
            stopIndicatorValues: [20]
            onValueChanged: {
                Config.options.appearance.transparency.contentTransparency = value / 100;
            }
        }"""
    )
    return qml.replace(
        "Config.options.appearance.transparency.enable = checked;",
        "Config.options.appearance.transparency.enable = checked;\n"
        "                applyWindowTransparency(checked ? Config.options.appearance.transparency.backgroundTransparency : 0);",
        1,
    )


def patch_quick_config(root: Path) -> None:
    path = root / "ii/modules/settings/QuickConfig.qml"
    text = path.read_text()

    old = """        ConfigSwitch {
            buttonIcon: "ev_shadow"
            text: Translation.tr("Transparency")
            checked: Config.options.appearance.transparency.enable
            onCheckedChanged: {
                Config.options.appearance.transparency.enable = checked;
            }
        }"""
    new = transparency_controls_qml(old)

    if "applyWindowTransparency" not in text:
        text = replace_once(text, old, new, "transparency switch block")
        path.write_text(text)


def patch_vertical_bar(root: Path) -> None:
    path = root / "ii/modules/ii/verticalBar/VerticalBarContent.qml"
    text = path.read_text()

    if "Quickshell.Services.Pipewire" not in text:
        text = replace_once(
            text,
            "import Quickshell.Services.UPower\n",
            "import Quickshell.Services.UPower\nimport Quickshell.Services.Pipewire\nimport Quickshell.Hyprland\n",
            "vertical bar import block",
        )

    insert = r"""

            HorizontalBarSeparator {
                visible: Config.options.bar.verbose || Config.options.bar.weather.enable
            }

            ColumnLayout {
                visible: Config.options.bar.verbose
                Layout.alignment: Qt.AlignHCenter
                Layout.fillWidth: true
                Layout.fillHeight: false
                spacing: 4

                Loader {
                    active: Config.options.bar.utilButtons.showScreenSnip
                    visible: active
                    Layout.alignment: Qt.AlignHCenter
                    sourceComponent: Bar.CircleUtilButton {
                        onClicked: Quickshell.execDetached(["qs", "-p", Quickshell.shellPath(""), "ipc", "call", "region", "screenshot"])
                        MaterialSymbol {
                            horizontalAlignment: Qt.AlignHCenter
                            fill: 1
                            text: "screenshot_region"
                            iconSize: Appearance.font.pixelSize.large
                            color: Appearance.colors.colOnLayer2
                        }
                    }
                }

                Loader {
                    active: Config.options.bar.utilButtons.showScreenRecord
                    visible: active
                    Layout.alignment: Qt.AlignHCenter
                    sourceComponent: Bar.CircleUtilButton {
                        onClicked: Quickshell.execDetached([Directories.recordScriptPath])
                        MaterialSymbol {
                            horizontalAlignment: Qt.AlignHCenter
                            fill: 1
                            text: "videocam"
                            iconSize: Appearance.font.pixelSize.large
                            color: Appearance.colors.colOnLayer2
                        }
                    }
                }

                Loader {
                    active: Config.options.bar.utilButtons.showColorPicker
                    visible: active
                    Layout.alignment: Qt.AlignHCenter
                    sourceComponent: Bar.CircleUtilButton {
                        onClicked: Quickshell.execDetached(["hyprpicker", "-a"])
                        MaterialSymbol {
                            horizontalAlignment: Qt.AlignHCenter
                            fill: 1
                            text: "colorize"
                            iconSize: Appearance.font.pixelSize.large
                            color: Appearance.colors.colOnLayer2
                        }
                    }
                }

                Loader {
                    active: Config.options.bar.utilButtons.showKeyboardToggle
                    visible: active
                    Layout.alignment: Qt.AlignHCenter
                    sourceComponent: Bar.CircleUtilButton {
                        onClicked: GlobalStates.oskOpen = !GlobalStates.oskOpen
                        MaterialSymbol {
                            horizontalAlignment: Qt.AlignHCenter
                            fill: 0
                            text: "keyboard"
                            iconSize: Appearance.font.pixelSize.large
                            color: Appearance.colors.colOnLayer2
                        }
                    }
                }

                Loader {
                    active: Config.options.bar.utilButtons.showMicToggle
                    visible: active
                    Layout.alignment: Qt.AlignHCenter
                    sourceComponent: Bar.CircleUtilButton {
                        onClicked: Quickshell.execDetached(["wpctl", "set-mute", "@DEFAULT_SOURCE@", "toggle"])
                        MaterialSymbol {
                            horizontalAlignment: Qt.AlignHCenter
                            fill: 0
                            text: Pipewire.defaultAudioSource?.audio?.muted ? "mic_off" : "mic"
                            iconSize: Appearance.font.pixelSize.large
                            color: Appearance.colors.colOnLayer2
                        }
                    }
                }

                Loader {
                    active: Config.options.bar.utilButtons.showDarkModeToggle
                    visible: active
                    Layout.alignment: Qt.AlignHCenter
                    sourceComponent: Bar.CircleUtilButton {
                        onClicked: {
                            if (Appearance.m3colors.darkmode) {
                                Hyprland.dispatch(`exec ${Directories.wallpaperSwitchScriptPath} --mode light --noswitch`)
                            } else {
                                Hyprland.dispatch(`exec ${Directories.wallpaperSwitchScriptPath} --mode dark --noswitch`)
                            }
                        }
                        MaterialSymbol {
                            horizontalAlignment: Qt.AlignHCenter
                            fill: 0
                            text: Appearance.m3colors.darkmode ? "light_mode" : "dark_mode"
                            iconSize: Appearance.font.pixelSize.large
                            color: Appearance.colors.colOnLayer2
                        }
                    }
                }

                Loader {
                    active: Config.options.bar.utilButtons.showPerformanceProfileToggle
                    visible: active
                    Layout.alignment: Qt.AlignHCenter
                    sourceComponent: Bar.CircleUtilButton {
                        onClicked: {
                            if (PowerProfiles.hasPerformanceProfile) {
                                switch (PowerProfiles.profile) {
                                    case PowerProfile.PowerSaver: PowerProfiles.profile = PowerProfile.Balanced; break;
                                    case PowerProfile.Balanced: PowerProfiles.profile = PowerProfile.Performance; break;
                                    case PowerProfile.Performance: PowerProfiles.profile = PowerProfile.PowerSaver; break;
                                }
                            } else {
                                PowerProfiles.profile = PowerProfiles.profile == PowerProfile.Balanced ? PowerProfile.PowerSaver : PowerProfile.Balanced
                            }
                        }
                        MaterialSymbol {
                            horizontalAlignment: Qt.AlignHCenter
                            fill: 0
                            text: switch (PowerProfiles.profile) {
                                case PowerProfile.PowerSaver: return "energy_savings_leaf"
                                case PowerProfile.Balanced: return "airwave"
                                case PowerProfile.Performance: return "local_fire_department"
                            }
                            iconSize: Appearance.font.pixelSize.large
                            color: Appearance.colors.colOnLayer2
                        }
                    }
                }
            }

            Loader {
                active: Config.options.bar.weather.enable
                visible: active
                Layout.alignment: Qt.AlignHCenter
                Layout.fillWidth: true
                Layout.fillHeight: false
                sourceComponent: Bar.CircleUtilButton {
                    onClicked: Weather.getData()
                    Item {
                        implicitWidth: Math.max(weatherColumn.implicitWidth, 26)
                        implicitHeight: weatherColumn.implicitHeight

                        ColumnLayout {
                            id: weatherColumn
                            anchors.centerIn: parent
                            spacing: -2

                            MaterialSymbol {
                                Layout.alignment: Qt.AlignHCenter
                                fill: 0
                                text: Icons.getWeatherIcon(Weather.data.wCode) ?? "cloud"
                                iconSize: Appearance.font.pixelSize.large
                                color: Appearance.colors.colOnLayer2
                            }

                            StyledText {
                                Layout.alignment: Qt.AlignHCenter
                                visible: Config.options.bar.verbose
                                font.pixelSize: Appearance.font.pixelSize.smaller
                                color: Appearance.colors.colOnLayer2
                                text: Weather.data?.temp ?? "--°"
                            }
                        }
                    }
                }
            }
"""

    needle = """            BatteryIndicator {
                visible: Battery.available
                Layout.fillWidth: true
                Layout.fillHeight: false
            }
            """

    if "Config.options.bar.utilButtons.showScreenSnip" not in text:
        text = replace_once(
            text, needle, needle + insert, "vertical bar insertion point"
        )

    path.write_text(text)


def patch_managed_quickshell_lifecycle(root: Path) -> None:
    ipc_old = '["qs", "-p", Quickshell.shellPath(""), "ipc", "call"'
    ipc_new = '["qs", "-c", Quickshell.env("qsConfig"), "ipc", "call"'
    ipc_rewrites = 0
    for path in root.rglob("*.qml"):
        text = path.read_text()
        count = text.count(ipc_old)
        if count:
            path.write_text(text.replace(ipc_old, ipc_new))
            ipc_rewrites += count
    if ipc_rewrites != 12:
        raise SystemExit(
            f"expected 12 path-scoped QuickShell IPC calls, found {ipc_rewrites}"
        )

    replacements = [
        (
            root / "ii/services/FirstRunExperience.qml",
            '        Quickshell.execDetached(["bash", "-c", `qs -p \'${root.welcomeQmlPath}\'`])',
            '        Quickshell.execDetached(["notify-send", root.firstRunNotifSummary, root.firstRunNotifBody, "-a", "Shell"])',
            "first-run welcome lifecycle",
        ),
        (
            root / "ii/services/ConflictKiller.qml",
            '                    Quickshell.execDetached(["qs", "-p", root.killDialogQmlPath])',
            '                    Quickshell.execDetached(["notify-send", "Shell conflict", "Another notification daemon is already running", "-a", "Shell"])',
            "conflict dialog lifecycle",
        ),
        (
            root / "ii/modules/waffle/bar/StartButton.qml",
            '                    Quickshell.execDetached(["qs", "-p", Quickshell.shellPath("settings.qml")]);',
            '                    Quickshell.execDetached(["wahrwelt-end4-settings"]);',
            "waffle settings lifecycle",
        ),
        (
            root / "ii/modules/ii/sidebarRight/SidebarRightContent.qml",
            '                    Quickshell.execDetached(["qs", "-p", root.settingsQmlPath]);',
            '                    Quickshell.execDetached(["wahrwelt-end4-settings"]);',
            "sidebar settings lifecycle",
        ),
        (
            root / "ii/modules/waffle/actionCenter/mainPage/MainPageFooter.qml",
            '            Quickshell.execDetached(["qs", "-p", Quickshell.shellPath("settings.qml")]);',
            '            Quickshell.execDetached(["wahrwelt-end4-settings"]);',
            "action-center settings lifecycle",
        ),
    ]
    for path, old, new, label in replacements:
        text = path.read_text()
        path.write_text(replace_once(text, old, new, label))

    for path in root.rglob("*.qml"):
        text = path.read_text()
        if '["qs", "-p"' in text or "qs -p" in text:
            raise SystemExit(f"unmanaged QuickShell lifecycle remains in {path}")


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: quickshell.py <patched-quickshell-root>")

    root = Path(sys.argv[1])
    patch_hyprland_xkb(root)
    patch_cheatsheet_keybinds(root)
    patch_cheatsheet_tabs(root)
    patch_quick_config(root)
    patch_vertical_bar(root)
    patch_managed_quickshell_lifecycle(root)


if __name__ == "__main__":
    main()
