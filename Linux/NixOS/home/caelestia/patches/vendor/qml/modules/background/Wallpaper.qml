pragma ComponentBehavior: Bound

import QtMultimedia
import QtQuick
import Caelestia.Config
import qs.components
import qs.components.filedialog
import qs.components.images
import qs.services
import qs.utils

Item {
    id: root

    property string source: Wallpapers.current
    property bool completed
    property bool liveError
    readonly property bool liveSource: source !== "" && Wallpapers.isLivePath(source)
    readonly property bool videoSource: source !== "" && Wallpapers.isVideoPath(source)
    readonly property string lastGoodFrame: `${Paths.state}/wallpaper/thumbnail.jpg`

    function stopLivePlayers(): void {
        if (videoLoader.item && videoLoader.item.stopPlayback)
            videoLoader.item.stopPlayback();
        if (animatedLoader.item && animatedLoader.item.stopPlayback)
            animatedLoader.item.stopPlayback();
    }

    function restartLivePlayers(): void {
        if (root.videoSource && videoLoader.item && videoLoader.item.startPlayback)
            videoLoader.item.startPlayback();
        if (root.liveSource && !root.videoSource && animatedLoader.item && animatedLoader.item.startPlayback)
            animatedLoader.item.startPlayback();
    }

    onSourceChanged: {
        stopLivePlayers();
        liveError = false;
        Qt.callLater(() => {
            restartLivePlayers();
        });
    }

    Component.onCompleted: completed = true

    Loader {
        id: staticLoader

        anchors.fill: parent
        active: root.completed && !!root.source && !root.liveSource
        asynchronous: true
        sourceComponent: staticComponent
    }

    Loader {
        id: animatedLoader

        anchors.fill: parent
        active: root.completed && root.liveSource && !root.videoSource
        asynchronous: true
        sourceComponent: animatedComponent
    }

    Loader {
        id: videoLoader

        anchors.fill: parent
        active: root.completed && root.videoSource
        asynchronous: true
        sourceComponent: videoComponent
    }

    Loader {
        anchors.fill: parent
        active: root.liveError
        asynchronous: true
        sourceComponent: CachingImage {
            path: root.lastGoodFrame
            fillMode: Image.PreserveAspectCrop
        }
    }

    Loader {
        anchors.fill: parent
        active: root.completed && !root.source
        asynchronous: true
        sourceComponent: StyledRect {
            color: Colours.palette.m3surfaceContainer

            Row {
                anchors.centerIn: parent
                spacing: Tokens.spacing.largeIncreased

                MaterialIcon {
                    text: "sentiment_stressed"
                    color: Colours.palette.m3onSurfaceVariant
                    fontStyle: Tokens.font.icon.builders.extraLarge.scale(5).build()
                }

                Column {
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: Tokens.spacing.small

                    StyledText {
                        text: qsTr("Wallpaper missing?")
                        color: Colours.palette.m3onSurfaceVariant
                        font: Tokens.font.body.builders.large.size(28 * 2).weight(Font.Bold).build()
                    }

                    StyledRect {
                        implicitWidth: selectWallText.implicitWidth + Tokens.padding.extraLargeIncreased
                        implicitHeight: selectWallText.implicitHeight + Tokens.padding.small

                        radius: Tokens.rounding.full
                        color: Colours.palette.m3primary

                        FileDialog {
                            id: dialog

                            title: qsTr("Select a wallpaper")
                            filterLabel: qsTr("Image files")
                            filters: Images.validImageExtensions
                            onAccepted: path => Wallpapers.setWallpaper(path)
                        }

                        StateLayer {
                            radius: parent.radius
                            color: Colours.palette.m3onPrimary
                            onClicked: dialog.open()
                        }

                        StyledText {
                            id: selectWallText

                            anchors.centerIn: parent
                            text: qsTr("Set it now!")
                            color: Colours.palette.m3onPrimary
                            font: Tokens.font.body.large
                        }
                    }
                }
            }
        }
    }

    Component {
        id: staticComponent

        CachingImage {
            anchors.fill: parent
            path: root.source
            fillMode: Image.PreserveAspectCrop
        }
    }

    Component {
        id: animatedComponent

        AnimatedImage {
            anchors.fill: parent
            source: root.source
            fillMode: Image.PreserveAspectCrop
            cache: true

            Component.onCompleted: startPlayback()

            function stopPlayback(): void {
                playing = false;
            }

            function startPlayback(): void {
                playing = true;
            }

            onStatusChanged: {
                if (status === Image.Error) {
                    stopPlayback();
                    root.liveError = true;
                }
            }
        }
    }

    Component {
        id: videoComponent

        Item {
            anchors.fill: parent

            function stopPlayback(): void {
                player.stop();
            }

            MediaPlayer {
                id: player

                source: root.source
                loops: MediaPlayer.Infinite
                videoOutput: output
                audioOutput: AudioOutput {
                    muted: true
                    volume: 0
                }

                Component.onCompleted: startPlayback()
                onErrorOccurred: {
                    stopPlayback();
                    root.liveError = true;
                }
            }

            VideoOutput {
                id: output

                anchors.fill: parent
                fillMode: VideoOutput.PreserveAspectCrop
            }

            function startPlayback(): void {
                player.play();
            }
        }
    }
}
