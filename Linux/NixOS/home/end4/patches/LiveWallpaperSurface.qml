import QtQuick
import qs.services
import qs.modules.common
import qs.modules.common.widgets
import qs.modules.common.functions

Item {
    id: root

    required property string path
    property string fallbackPath: ""
    readonly property string mediaKind: Wallpapers.mediaKind(root.path)
    property bool failed: false
    property bool active: true

    function stopLivePlayback() {
        animatedImage.playing = false;
        animatedImage.source = "";
    }

    function startPlayback() {
        root.failed = false;
        root.stopLivePlayback();
        if (!root.active || !root.path || root.path.length === 0) return;
        // switchwall.sh owns video playback through mpvpaper. Keeping video
        // out of this surface guarantees one active backend per selection.
        if (["animated", "animated_webp"].includes(root.mediaKind)) {
            animatedImage.source = root.path;
            animatedImage.playing = true;
        }
    }

    function showFallback() {
        root.failed = true;
        root.stopLivePlayback();
    }

    onPathChanged: root.startPlayback()
    onMediaKindChanged: root.startPlayback()
    onActiveChanged: root.active ? root.startPlayback() : root.stopLivePlayback()
    Component.onCompleted: root.startPlayback()
    Component.onDestruction: root.stopLivePlayback()

    Connections {
        target: Wallpapers
        function onSelectionChanged(path) {
            if (path !== root.path && !Wallpapers.isLivePath(path)) root.stopLivePlayback();
        }
    }

    Image {
        id: staticImage
        anchors.fill: parent
        visible: root.active && root.mediaKind === "static" && !root.failed
        source: root.mediaKind === "static" ? root.path : ""
        fillMode: Image.PreserveAspectCrop
        asynchronous: true
        cache: false
        onStatusChanged: if (status === Image.Error) root.showFallback()
    }

    AnimatedImage {
        id: animatedImage
        anchors.fill: parent
        visible: root.active && ["animated", "animated_webp"].includes(root.mediaKind) && !root.failed
        fillMode: Image.PreserveAspectCrop
        asynchronous: true
        cache: false
        playing: visible
        onStatusChanged: if (status === Image.Error) root.showFallback()
    }

    StyledImage {
        id: fallbackImage
        anchors.fill: parent
        visible: root.failed
        source: root.fallbackPath.length > 0 ? root.fallbackPath : `${FileUtils.trimFileProtocol(Directories.assetsPath)}/images/default_wallpaper.png`
        fillMode: Image.PreserveAspectCrop
        cache: true
    }
}
