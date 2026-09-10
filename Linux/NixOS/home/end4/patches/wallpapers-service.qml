import qs.modules.common
import qs.modules.common.models
import qs.modules.common.functions
import QtQuick
import Qt.labs.folderlistmodel
import Quickshell
import Quickshell.Io
pragma Singleton
pragma ComponentBehavior: Bound

/**
 * Shared End4 wallpaper model. The model keeps directories for navigation,
 * indexes media through the build-installed helper, and leaves playback to
 * the background surface for the selected path only.
 */
Singleton {
    id: root

    readonly property list<string> supportedFormats: [
        "jpg", "jpeg", "png", "webp", "gif",
        "mp4", "webm", "mkv", "mov", "avi"
    ]
    readonly property list<string> staticFormats: ["jpg", "jpeg", "png", "webp"]
    readonly property list<string> liveFormats: ["gif", "mp4", "webm", "mkv", "mov", "avi"]
    readonly property string mediaIndexScriptPath: `${FileUtils.trimFileProtocol(Directories.scriptPath)}/wallpapers/media-index.py`
    readonly property string firstFrameScriptPath: `${FileUtils.trimFileProtocol(Directories.scriptPath)}/wallpapers/first-frame-thumbnails.sh`
    readonly property string reconcileVideoScriptPath: `${FileUtils.trimFileProtocol(Directories.scriptPath)}/wallpapers/video-backend-reconcile.sh`
    property string thumbgenScriptPath: `${FileUtils.trimFileProtocol(Directories.scriptPath)}/thumbnails/thumbgen-venv.sh`
    property string generateThumbnailsMagickScriptPath: `${FileUtils.trimFileProtocol(Directories.scriptPath)}/thumbnails/generate-thumbnails-magick.sh`
    property alias directory: folderModel.folder
    readonly property string effectiveDirectory: FileUtils.trimFileProtocol(folderModel.folder.toString())
    property url defaultFolder: Qt.resolvedUrl(`${Directories.pictures}/Wallpapers`)
    property alias folderModel: folderModel
    property alias wallpaperModel: wallpaperModel
    property string searchQuery: ""
    property string mediaFilter: "all"
    property var metadata: ({})
    property list<string> wallpapers: []
    readonly property bool thumbnailGenerationRunning: thumbgenProc.running
    property real thumbnailGenerationProgress: 0
    readonly property string selectedPath: Config.options.background.wallpaperPath || ""
    property string previewPath: ""
    property string confirmedPath: ""
    property bool orderLoaded: false
    property string sortMode: Config.options.wallpaperSelector?.sortMode || "custom"
    property var orderMap: ({})
    readonly property list<string> sortModes: ["custom", "time", "time_rev", "name", "name_rev", "size", "size_rev"]
    readonly property string orderFilePath: `${Directories.shellConfig}/wallpaper_order.json`

    signal changed()
    signal selectionChanged(path: string)
    signal thumbnailGenerated(directory: string)
    signal thumbnailGeneratedFile(filePath: string)

    function cleanPath(path) {
        return FileUtils.trimFileProtocol(path || "").replace(/\/+$/, "");
    }

    function customOrderForDirectory() {
        const cleanDirectory = root.cleanPath(root.effectiveDirectory);
        const saved = root.orderMap[cleanDirectory] || root.orderMap[`${cleanDirectory}/`] || [];
        return Array.isArray(saved) ? saved : [];
    }

    function setSortMode(mode) {
        if (!root.sortModes.includes(mode)) return;
        root.sortMode = mode;
        if (Config.options.wallpaperSelector) Config.options.wallpaperSelector.sortMode = mode;
        if (typeof Config.setNestedValue === "function") Config.setNestedValue("wallpaperSelector.sortMode", mode);
        root.rebuildWallpaperModel();
    }

    function mediaKind(path) {
        const clean = cleanPath(path);
        const indexed = root.metadata[clean];
        // WebP animation is classified from file content by media-index.py.
        if (indexed && indexed.mediaKind) return indexed.mediaKind;
        const suffix = clean.toLowerCase().split(".").pop();
        if (suffix === "gif") return "animated";
        if (["mp4", "webm", "mkv", "mov", "avi"].includes(suffix)) return "video";
        if (["jpg", "jpeg", "png", "webp"].includes(suffix)) return "static";
        return "unknown";
    }

    function mediaKindForEntry(entry) {
        if (entry && entry.mediaKind) return entry.mediaKind;
        return root.mediaKind(entry ? entry.filePath : "");
    }

    function isVideoPath(path) {
        return mediaKind(path) === "video";
    }

    function isAnimatedPath(path) {
        return ["animated", "animated_webp"].includes(mediaKind(path));
    }

    function isLivePath(path) {
        return isVideoPath(path) || isAnimatedPath(path);
    }

    function fallbackFor(path) {
        const clean = cleanPath(path);
        const indexed = root.metadata[clean];
        if (indexed && indexed.thumbnailPath) return indexed.thumbnailPath;
        if (Config.options.background.thumbnailPath) return Config.options.background.thumbnailPath;
        return `${FileUtils.trimFileProtocol(Directories.assetsPath)}/images/default_wallpaper.png`;
    }

    function setMediaFilter(filter) {
        if (!["all", "static", "live"].includes(filter)) return;
        root.mediaFilter = filter;
        rebuildWallpaperModel();
    }

    function matchesFilter(item) {
        if (item.fileIsDir || root.mediaFilter === "all") return true;
        const kind = root.mediaKindForEntry(item);
        const live = ["video", "animated", "animated_webp"].includes(kind);
        if (root.mediaFilter === "live") return live;
        return !live && kind !== "unknown";
    }

    function load() {}

    function openFallbackPicker(darkMode = Appearance.m3colors.darkmode, startDir = "") {
        const args = [Directories.wallpaperSwitchScriptPath, "--mode", darkMode ? "dark" : "light"];
        if (startDir.length > 0) args.push("--start-dir", startDir);
        Quickshell.execDetached(args);
    }

    function apply(path, darkMode = Appearance.m3colors.darkmode) {
        if (!path || path.length === 0) return;
        root.confirmedPath = path;
        root.selectionChanged(path);
        Quickshell.execDetached([
            Directories.wallpaperSwitchScriptPath,
            "--mode", darkMode ? "dark" : "light",
            "--image", path
        ]);
        root.changed();
    }

    function select(path, darkMode = Appearance.m3colors.darkmode, onFileSelected = null) {
        selectProc.select(path, darkMode, onFileSelected);
    }

    function randomFromCurrentFolder(darkMode = Appearance.m3colors.darkmode) {
        if (wallpaperModel.count === 0) return;
        const index = Math.floor(Math.random() * wallpaperModel.count);
        const item = wallpaperModel.get(index);
        if (item && item.filePath) root.select(item.filePath, darkMode);
    }

    function startPreview(path) {
        if (path && path.length > 0) root.previewPath = path;
    }

    function stopPreview() {
        root.previewPath = "";
    }

    function getRandomWallpaperPath(excludePath = "") {
        const excluded = cleanPath(excludePath);
        const candidates = [];
        for (let i = 0; i < wallpaperModel.count; i++) {
            const item = wallpaperModel.get(i);
            if (item.filePath && cleanPath(item.filePath) !== excluded) candidates.push(item.filePath);
        }
        if (candidates.length === 0) return "";
        return candidates[Math.floor(Math.random() * candidates.length)];
    }

    Process {
        id: selectProc
        property string filePath: ""
        property bool darkMode: Appearance.m3colors.darkmode
        property var onFileSelected: null

        function select(path, mode, callback) {
            selectProc.filePath = path;
            selectProc.darkMode = mode;
            selectProc.onFileSelected = callback;
            selectProc.exec(["test", "-d", FileUtils.trimFileProtocol(path)]);
        }

        onExited: (exitCode, exitStatus) => {
            if (exitCode === 0) {
                root.setDirectory(selectProc.filePath);
            } else if (selectProc.onFileSelected) {
                selectProc.onFileSelected(selectProc.filePath);
            } else {
                root.apply(selectProc.filePath, selectProc.darkMode);
            }
        }
    }

    Process {
        id: validateDirProc
        property string nicePath: ""

        function setDirectoryIfValid(path) {
            validateDirProc.nicePath = FileUtils.trimFileProtocol(path).replace(/\/+$/, "");
            if (/^\/*$/.test(validateDirProc.nicePath)) validateDirProc.nicePath = "/";
            validateDirProc.exec([
                "bash", "-c",
                "if [ -d \"$1\" ]; then echo dir; elif [ -f \"$1\" ]; then echo file; else echo invalid; fi",
                "end4-validate-directory",
                validateDirProc.nicePath
            ]);
        }

        stdout: StdioCollector {
            onStreamFinished: {
                const result = text.trim();
                if (result === "dir") root.directory = Qt.resolvedUrl(validateDirProc.nicePath);
                else if (result === "file") root.directory = Qt.resolvedUrl(FileUtils.parentDirectory(validateDirProc.nicePath));
            }
        }
    }

    function setDirectory(path) {
        validateDirProc.setDirectoryIfValid(path);
    }

    function navigateUp() { folderModel.navigateUp(); }
    function navigateBack() { folderModel.navigateBack(); }
    function navigateForward() { folderModel.navigateForward(); }

    FolderListModelWithHistory {
        id: folderModel
        folder: Qt.resolvedUrl(root.defaultFolder)
        caseSensitive: false
        nameFilters: ["*"]
        showDirs: true
        showDotAndDotDot: false
        showOnlyReadable: true
        sortField: FolderListModel.Time
        sortReversed: false
        onCountChanged: rebuildIndexProc.restartIndex()
        onFolderChanged: rebuildIndexProc.restartIndex()
    }

    ListModel {
        id: wallpaperModel
    }

    FileView {
        id: orderFileView
        path: root.orderFilePath
        watchChanges: false
        onLoaded: {
            try {
                const loaded = JSON.parse(orderFileView.text() || "{}");
                root.orderMap = loaded && typeof loaded === "object" ? loaded : {};
            } catch (error) {
                console.log("[Wallpapers] wallpaper order unavailable:", error);
                root.orderMap = {};
            }
            root.orderLoaded = true;
            root.rebuildWallpaperModel();
        }
        onLoadFailed: {
            root.orderMap = {};
            root.orderLoaded = true;
            root.rebuildWallpaperModel();
        }
    }

    Process {
        id: rebuildIndexProc
        property string directory: ""
        property string query: ""
        property string customOrderJson: "[]"

        function restartIndex() {
            rebuildIndexProc.directory = root.effectiveDirectory;
            rebuildIndexProc.query = root.searchQuery;
            rebuildIndexProc.customOrderJson = JSON.stringify(root.customOrderForDirectory());
            rebuildIndexProc.running = false;
            rebuildIndexProc.command = [
                root.mediaIndexScriptPath,
                "--directory", rebuildIndexProc.directory,
                "--sort", root.sortMode,
                "--custom-order", rebuildIndexProc.customOrderJson,
                "--",
                rebuildIndexProc.query
            ];
            rebuildIndexProc.running = true;
        }

        stdout: StdioCollector {
            onStreamFinished: {
                try {
                    const entries = JSON.parse(text || "[]");
                    const nextMetadata = Object.assign({}, root.metadata);
                    wallpaperModel.clear();
                    const paths = [];
                    entries.forEach(entry => {
                        nextMetadata[root.cleanPath(entry.filePath)] = entry;
                        if (root.matchesFilter(entry)) {
                            wallpaperModel.append(entry);
                            if (entry.filePath && !entry.fileIsDir) paths.push(entry.filePath);
                        }
                    });
                    root.metadata = nextMetadata;
                    root.wallpapers = paths;
                } catch (error) {
                    // Keep the last good model when a corrupt directory entry
                    // or a partial helper response cannot be decoded.
                    console.log("[Wallpapers] media index unavailable:", error);
                }
            }
        }

        onExited: (exitCode, exitStatus) => {
            if (exitCode !== 0) console.log("[Wallpapers] media index failed:", exitCode);
        }
    }

    Process {
        id: selectedKindProc
        property string path: ""
        command: [root.mediaIndexScriptPath, "--file", path]
        stdout: StdioCollector {
            onStreamFinished: {
                try {
                    const entry = JSON.parse(text || "{}");
                    if (!entry.filePath) return;
                    const nextMetadata = Object.assign({}, root.metadata);
                    nextMetadata[root.cleanPath(entry.filePath)] = entry;
                    root.metadata = nextMetadata;
                } catch (error) {
                    console.log("[Wallpapers] selected media classification unavailable:", error);
                }
            }
        }
    }

    Process {
        id: reconcileVideoProc

        function reconcile() {
            reconcileVideoProc.running = false;
            reconcileVideoProc.command = [root.reconcileVideoScriptPath];
            reconcileVideoProc.running = true;
        }

        onExited: (exitCode, exitStatus) => {
            if (exitCode !== 0) console.log("[Wallpapers] video backend reconciliation failed:", exitCode);
        }
    }

    onSelectedPathChanged: {
        if (root.selectedPath.length > 0) {
            selectedKindProc.path = root.selectedPath;
            selectedKindProc.running = true;
        }
    }
    onSearchQueryChanged: rebuildIndexProc.restartIndex()
    onMediaFilterChanged: rebuildWallpaperModel()
    onSortModeChanged: rebuildWallpaperModel()

    Connections {
        target: Quickshell
        function onScreensChanged() {
            root.reconcileVideoBackend();
        }
    }

    Connections {
        target: Config
        function onReadyChanged() {
            if (Config.ready) Qt.callLater(root.reconcileVideoBackend);
        }
    }

    Component.onCompleted: {
        if (Config.ready) Qt.callLater(root.reconcileVideoBackend);
    }

    function rebuildWallpaperModel() {
        rebuildIndexProc.restartIndex();
    }

    function reconcileVideoBackend() {
        reconcileVideoProc.reconcile();
    }

    function generateThumbnail(size, directory = root.effectiveDirectory) {
        if (!["normal", "large", "x-large", "xx-large"].includes(size)) throw new Error("Invalid thumbnail size");
        const cleanDirectory = root.cleanPath(directory);
        if (cleanDirectory.length === 0) return;
        thumbgenProc.running = false;
        thumbgenProc.directory = cleanDirectory;
        thumbgenProc.command = [
            "bash", "-c",
            "\"$1\" --size \"$2\" --machine_progress -d \"$3\" || true; \"$4\" --size \"$2\" -d \"$3\" || true; \"$5\" --size \"$2\" --directory \"$3\" || true",
            "end4-thumbnail-generation",
            root.thumbgenScriptPath,
            size,
            cleanDirectory,
            root.generateThumbnailsMagickScriptPath,
            root.firstFrameScriptPath
        ];
        root.thumbnailGenerationProgress = 0;
        thumbgenProc.running = true;
    }

    Process {
        id: thumbgenProc
        property string directory: ""
        stdout: SplitParser {
            onRead: data => {
                const progress = data.match(/PROGRESS (\d+)\/(\d+)/);
                if (progress) root.thumbnailGenerationProgress = parseInt(progress[1]) / parseInt(progress[2]);
                const file = data.match(/FILE (.+)/);
                if (file) root.thumbnailGeneratedFile(file[1]);
            }
        }
        onExited: root.thumbnailGenerated(thumbgenProc.directory)
    }

    function saveCustomOrder() {
        if (!orderFileView) return;
        try {
            orderFileView.setText(JSON.stringify(root.orderMap, null, 2));
        } catch (error) {
            console.log("[Wallpapers] failed to save wallpaper order:", error);
        }
    }

    function moveWallpaper(fromIndex, toIndex) {
        if (fromIndex < 0 || toIndex < 0 || fromIndex >= wallpaperModel.count || toIndex >= wallpaperModel.count || fromIndex === toIndex) return;
        wallpaperModel.move(fromIndex, toIndex, 1);
        const cleanDirectory = root.cleanPath(root.effectiveDirectory);
        const nextOrder = [];
        const nextPaths = [];
        for (let index = 0; index < wallpaperModel.count; index++) {
            const item = wallpaperModel.get(index);
            if (item.fileName) nextOrder.push(item.fileName);
            if (item.filePath) nextPaths.push(item.filePath);
        }
        const nextMap = Object.assign({}, root.orderMap);
        nextMap[cleanDirectory] = nextOrder;
        root.orderMap = nextMap;
        root.wallpapers = nextPaths;
        root.saveCustomOrder();
        root.setSortMode("custom");
    }
    function moveToTop(index) { moveWallpaper(index, 0); }
    function moveToBottom(index) { moveWallpaper(index, wallpaperModel.count - 1); }

    IpcHandler {
        target: "wallpapers"
        function apply(path: string): void { root.apply(path); }
        function setMediaFilter(filter: string): void { root.setMediaFilter(filter); }
        function setSortMode(mode: string): void { root.setSortMode(mode); }
        function moveWallpaper(fromIndex: int, toIndex: int): void { root.moveWallpaper(fromIndex, toIndex); }
    }
}
