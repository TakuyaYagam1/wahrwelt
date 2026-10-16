#include "config/config_service.h"
#include "core/deferred_call.h"
#include "core/process/process.h"
#include "net/http_client.h"
#include "shell/wallpaper/panel/wallpaper_scanner.h"
#include "tests/test_check.h"
#include "theme/custom_palettes.h"
#include "theme/image_loader.h"
#include "theme/scheme.h"
#include "theme/theme_service.h"

#include <algorithm>
#include <chrono>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <optional>
#include <poll.h>
#include <print>
#include <string>
#include <vector>

namespace {

  namespace fs = std::filesystem;

  const WallpaperScanResult* scanBlocking(WallpaperScanner& scanner, const fs::path& dir, bool flatten) {
    if (scanner.requestScan(dir, flatten)) {
      return scanner.cached(dir, flatten);
    }
    const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(20);
    while (std::chrono::steady_clock::now() < deadline) {
      std::vector<pollfd> fds;
      const auto start = scanner.addPollFds(fds);
      if (fds.empty() || ::poll(fds.data(), fds.size(), 100) < 0) {
        return nullptr;
      }
      scanner.dispatch(fds, start);
      if (const auto* result = scanner.cached(dir, flatten)) {
        return result;
      }
    }
    return nullptr;
  }

  void createMedia(const fs::path& path, const std::string& codec) {
    const auto result = process::runSync(
        std::vector<std::string>{
            "ffmpeg",
            "-hide_banner",
            "-loglevel",
            "error",
            "-nostdin",
            "-y",
            "-f",
            "lavfi",
            "-i",
            "color=c=red:s=32x32:d=0.1",
            "-frames:v",
            "1",
            "-threads",
            "1",
            "-c:v",
            codec,
            path.string(),
        }
    );
    if (!result) {
      std::println(stderr, "fixture creation failed: {}", result.err);
    }
    TEST_CHECK(result);
  }

  void checkVideoPalette(const fs::path& video) {
    ConfigService config;
    config.setWallpaperPath(std::nullopt, video.string());
    TEST_CHECK(config.setThemeColorScheme(PaletteSource::Wallpaper, "m3-content"));
    TEST_CHECK(config.getPaletteWallpaperPath() == video.string());
    HttpClient http;
    noctalia::theme::ThemeService theme(config, http);
    bool resolved = false;
    theme.setResolvedCallback([&resolved](const noctalia::theme::GeneratedPalette&, std::string_view) {
      resolved = true;
    });
    theme.apply();
    for (auto& call : DeferredCall::takePending()) {
      call();
    }
    TEST_CHECK(resolved);

    // A builtin fallback also fires the callback. Saving must decode the actual
    // video palette, so removing the theme thumbnail conversion fails here.
    std::string name;
    std::string error;
    const bool saved = theme.saveWallpaperPaletteAsCustom(&name, &error);
    if (!saved) {
      std::println(stderr, "video palette generation failed: {}", error);
    }
    TEST_CHECK(saved);
    TEST_CHECK(!name.empty());
    TEST_CHECK(fs::is_regular_file(noctalia::theme::customPalettePath(name)));
  }

  void checkScan(const fs::path& directory, const fs::path& video, const fs::path& still, bool flatten) {
    WallpaperScanner scanner;
    const auto* result = scanBlocking(scanner, directory, flatten);
    TEST_CHECK(result != nullptr);
    TEST_CHECK(result->entries.size() == 3);
    const auto findPath = [&](const fs::path& path) {
      return std::ranges::find_if(result->entries, [&](const WallpaperEntry& entry) { return entry.absPath == path; });
    };
    const auto live = findPath(video);
    TEST_CHECK(live != result->entries.end());
    TEST_CHECK(live->kind == noctalia::wallpaper::MediaKind::Live);
    TEST_CHECK(live->thumbnailPath != video);
    TEST_CHECK(fs::is_regular_file(live->thumbnailPath));
    TEST_CHECK(noctalia::theme::loadAndResize(live->thumbnailPath.string(), noctalia::theme::Scheme::Content));

    const auto image = findPath(still);
    TEST_CHECK(image != result->entries.end());
    TEST_CHECK(image->kind == noctalia::wallpaper::MediaKind::Static);
    TEST_CHECK(image->thumbnailPath == still);
    TEST_CHECK(noctalia::theme::loadAndResize(image->thumbnailPath.string(), noctalia::theme::Scheme::Content));
    TEST_CHECK((findPath(directory / "nested" / "still.png") != result->entries.end()) == flatten);
  }

} // namespace

int main() {
  std::string pattern = (fs::temp_directory_path() / "noctalia-wallpaper-integration-XXXXXX").string();
  const char* created = ::mkdtemp(pattern.data());
  TEST_CHECK(created != nullptr);
  const fs::path root(created);
  fs::create_directories(root / "config" / "noctalia");
  fs::create_directories(root / "state" / "noctalia");
  fs::create_directories(root / "data");
  fs::create_directories(root / "cache");
  fs::create_directories(root / "wallpapers" / "nested");
  TEST_CHECK(::setenv("NOCTALIA_CONFIG_HOME", (root / "config").c_str(), 1) == 0);
  TEST_CHECK(::setenv("NOCTALIA_STATE_HOME", (root / "state").c_str(), 1) == 0);
  TEST_CHECK(::setenv("NOCTALIA_DATA_HOME", (root / "data").c_str(), 1) == 0);
  TEST_CHECK(::setenv("XDG_CACHE_HOME", (root / "cache").c_str(), 1) == 0);

  const fs::path directory = root / "wallpapers";
  const fs::path video = directory / "red scene.mp4";
  const fs::path still = directory / "still.png";
  createMedia(video, "mpeg4");
  createMedia(still, "png");
  fs::copy_file(still, directory / "nested" / "still.png");
  std::ofstream(directory / "notes.txt") << "not a wallpaper";

  // Exercise theme extraction before scanning so the thumbnail is not prewarmed.
  checkVideoPalette(video);
  checkScan(directory, video, still, false);
  checkScan(directory, video, still, true);

  fs::remove_all(root);
  std::println("wallpaper_integration_test: ok");
  return 0;
}
