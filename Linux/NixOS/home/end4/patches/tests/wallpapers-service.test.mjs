import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const source = fs.readFileSync(new URL("../wallpapers-service.qml", import.meta.url), "utf8");
const collector = source.match(/id: rebuildIndexProc[\s\S]*?onStreamFinished: \{\n([\s\S]*?)\n            \}/);
assert.ok(collector, "media index collector must be available");

function createService() {
  const entries = [];
  const notifications = [];
  const diagnostics = [];
  const root = {
    metadata: {}, wallpapers: [], searchQuery: "", mediaFilter: "all",
    effectiveDirectory: "/walls", orderMap: {},
    mediaKindForEntry: entry => entry.mediaKind,
    saveCustomOrder() { throw new Error("search must not overwrite saved order"); },
    setSortMode() { throw new Error("search must not change sort mode"); },
  };
  const wallpaperModel = {
    get count() { return entries.length; },
    clear() { entries.length = 0; },
    append(entry) { entries.push(entry); },
    get(index) { return entries[index]; },
    move(from, to) { entries.splice(to, 0, ...entries.splice(from, 1)); },
  };
  for (const [, name] of source.matchAll(/^    signal (\w+)\(/gm)) {
    root[name] = () => {
      if (name === "resultsUpdated") {
        notifications.push({ paths: [...root.wallpapers], count: entries.length });
      }
    };
  }
  const context = vm.createContext({ root, wallpaperModel,
    console: { log: message => diagnostics.push(message) }, FileUtils: {
    trimFileProtocol: path => path.replace(/^file:\/\//, ""),
  } });
  for (const name of ["cleanPath", "matchesFilter", "moveWallpaper"]) {
    const fn = source.match(new RegExp(`^    function ${name}\\([^\\n]*\\) \\{\\n[\\s\\S]*?^    \\}`, "m"));
    assert.ok(fn, `${name} must be available`);
    root[name] = vm.runInContext(`(${fn[0].trim()})`, context);
  }
  return {
    root, entries, notifications, diagnostics,
    accept(text) {
      context.text = text;
      vm.runInContext(`(() => {${collector[1]}\n})()`, context);
    },
  };
}

const directory = { filePath: "/walls/folder", fileName: "folder", fileIsDir: true, mediaKind: "directory" };
const video = { filePath: "/walls/live.mp4", fileName: "live.mp4", fileIsDir: false, mediaKind: "video" };
const image = { filePath: "/walls/static.png", fileName: "static.png", fileIsDir: false, mediaKind: "static" };

{
  const service = createService();
  service.accept(JSON.stringify([directory, video, image]));
  assert.deepEqual(service.notifications, [{ paths: ["/walls/live.mp4", "/walls/static.png"], count: 3 }],
    "selector must receive resultsUpdated after model and wallpaper paths are committed");
  assert.equal(service.root.metadata["/walls/live.mp4"].mediaKind, "video");
  service.accept("invalid JSON");
  assert.equal(service.notifications.length, 1, "invalid responses must not notify a successful update");
  assert.equal(service.entries.length, 3, "invalid responses must retain the last good model");
  assert.deepEqual(Array.from(service.root.wallpapers), ["/walls/live.mp4", "/walls/static.png"]);
  assert.deepEqual(service.diagnostics, ["[Wallpapers] media index unavailable:"],
    "invalid responses must report the error without replacing the last good results");
  service.accept("[]");
  assert.deepEqual(service.notifications[1], { paths: [], count: 0 },
    "empty search results must also notify the selector");
}

{
  const service = createService();
  service.root.mediaFilter = "live";
  service.accept(JSON.stringify([directory, video, image]));
  assert.deepEqual(service.notifications, [{ paths: ["/walls/live.mp4"], count: 2 }],
    "live filtering must retain navigation and notify with only playable wallpaper paths");
}

{
  const service = createService();
  service.accept(JSON.stringify([video, image]));
  service.root.searchQuery = "live";
  service.root.moveWallpaper(0, 1);
  assert.deepEqual(service.entries.map(entry => entry.filePath), ["/walls/live.mp4", "/walls/static.png"],
    "reordering filtered search results must not corrupt the full directory's custom order");
  assert.deepEqual(service.root.orderMap, {});
}

console.log("OK End4 wallpaper selector results notification and filtered-order safety");
