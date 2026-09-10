#include "shell/wallpaper/live_wallpaper_controller.h"
#include "shell/wallpaper/wallpaper_media.h"

#include <cassert>
#include <memory>
#include <string>
#include <utility>
#include <vector>

namespace {

  struct ProcessState {
    bool running = true;
    bool terminated = false;

    void simulateExit() { running = false; }
    void simulateCrash() { running = false; }
  };

  class FakeProcess final : public noctalia::wallpaper::ProcessHandle {
  public:
    explicit FakeProcess(std::shared_ptr<ProcessState> state) : m_state(std::move(state)) {}

    [[nodiscard]] bool isRunning() const override { return m_state->running; }
    void terminate() override {
      m_state->running = false;
      m_state->terminated = true;
    }

  private:
    std::shared_ptr<ProcessState> m_state;
  };

} // namespace

int main() {
  using noctalia::wallpaper::MediaKind;
  using noctalia::wallpaper::WallpaperMedia;

  assert(WallpaperMedia::kindForPath("scene.gif") == MediaKind::Live);
  assert(WallpaperMedia::kindForPath("scene.mp4") == MediaKind::Live);
  assert(WallpaperMedia::kindForPath("scene.webm") == MediaKind::Live);
  assert(WallpaperMedia::kindForPath("scene.mkv") == MediaKind::Live);
  assert(WallpaperMedia::kindForPath("scene.mov") == MediaKind::Live);
  assert(WallpaperMedia::kindForPath("scene.avi") == MediaKind::Live);
  assert(WallpaperMedia::kindForPath("still.png") == MediaKind::Static);

  std::vector<std::vector<std::string>> launched;
  std::vector<std::shared_ptr<ProcessState>> processes;
  std::size_t launchAttempts = 0;
  bool failNextLaunch = false;
  bool failAllLaunches = false;
  noctalia::wallpaper::LiveWallpaperController controller(
      [&launched, &processes, &launchAttempts, &failNextLaunch, &failAllLaunches](const std::vector<std::string>& command) {
        ++launchAttempts;
        if (failAllLaunches || failNextLaunch) {
          failNextLaunch = false;
          return std::unique_ptr<noctalia::wallpaper::ProcessHandle>{};
        }
        launched.push_back(command);
        auto state = std::make_shared<ProcessState>();
        processes.push_back(state);
        return std::unique_ptr<noctalia::wallpaper::ProcessHandle>(
            std::make_unique<FakeProcess>(std::move(state))
        );
      }
  );

  const auto livePath = "/home/test/Pictures/Wallpapers/scene.mp4";
  const auto firstActive = controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(firstActive.size() == 2);
  assert(launched.size() == 2);
  assert(launched[0][0] == "mpvpaper");
  assert(launched[0][1] == "-o");
  assert(launched[0][2].find("no-audio") != std::string::npos);
  assert(launched[0][2].find("loop-file=inf") != std::string::npos);
  assert(launched[0][2].find("panscan=1") != std::string::npos);
  assert(launched[0][3] == "DP-1");

  // A healthy second tick only polls process state and does not restart children.
  assert(!controller.onSecondTick());
  (void)controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(launched.size() == 2);

  // An exit is detected by the next tick even without a config/output event.
  processes[0]->simulateCrash();
  assert(controller.onSecondTick());
  failNextLaunch = true;
  const auto fallback = controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(fallback.size() == 1);
  assert(launchAttempts == 2);
  assert(launched.size() == 2);
  assert(processes.size() == 2);

  // Backoff makes the next reconciliation deterministic, retries once, then recovers.
  assert(controller.onSecondTick());
  const auto retryFallback = controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(retryFallback.size() == 1);
  assert(launchAttempts == 3);
  assert(launched.size() == 2);
  assert(!controller.onSecondTick());
  assert(controller.onSecondTick());
  const auto recovered = controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(recovered.size() == 2);
  assert(launchAttempts == 4);
  assert(launched.size() == 3);
  assert(processes.size() == 3);
  assert(!controller.onSecondTick());
  (void)controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(launchAttempts == 4);
  assert(launched.size() == 3);

  // A clean exit follows the same tick-driven recovery path with one backoff.
  processes[1]->simulateExit();
  assert(controller.onSecondTick());
  const auto exitFallback = controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(exitFallback.size() == 1);
  assert(launchAttempts == 4);
  assert(controller.onSecondTick());
  (void)controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(launchAttempts == 5);
  assert(launched.size() == 4);
  assert(!controller.onSecondTick());
  (void)controller.apply(livePath, {"DP-1", "HDMI-A-1"});
  assert(launchAttempts == 5);
  assert(launched.size() == 4);

  // Switching to a static wallpaper terminates every owned live child.
  const auto staticActive = controller.apply(std::vector<noctalia::wallpaper::LiveWallpaperTarget>{});
  assert(staticActive.empty());
  assert(processes[2]->terminated);
  assert(processes[3]->terminated);
  assert(!processes[2]->running);
  assert(!processes[3]->running);

  // clear() must terminate the remaining live child as well.
  (void)controller.apply(livePath, {"DP-1"});
  assert(launched.size() == 5);
  controller.clear();
  assert(processes[4]->terminated);
  assert(!processes[4]->running);

  // Repeated launch failures use exponential backoff and stop at the bound.
  failAllLaunches = true;
  assert(controller.apply(livePath, {"DP-1"}).empty());
  const std::size_t boundedStart = launchAttempts;
  assert(controller.onSecondTick());
  assert(controller.apply(livePath, {"DP-1"}).empty());
  assert(!controller.onSecondTick());
  assert(controller.onSecondTick());
  assert(controller.apply(livePath, {"DP-1"}).empty());
  const std::size_t boundedEnd = launchAttempts;
  assert(boundedEnd == boundedStart + 2);
  for (int i = 0; i < 8; ++i) {
    assert(!controller.onSecondTick());
  }
  assert(launchAttempts == boundedEnd);

  // A successful spawn that exits before the next tick counts as a failure.
  std::vector<std::shared_ptr<ProcessState>> stormProcesses;
  std::size_t stormAttempts = 0;
  noctalia::wallpaper::LiveWallpaperController stormController(
      [&stormProcesses, &stormAttempts](const std::vector<std::string>&) {
        ++stormAttempts;
        auto state = std::make_shared<ProcessState>();
        stormProcesses.push_back(state);
        return std::unique_ptr<noctalia::wallpaper::ProcessHandle>(
            std::make_unique<FakeProcess>(std::move(state))
        );
      }
  );
  assert(stormController.apply(livePath, {"DP-1"}).size() == 1);
  stormProcesses[0]->simulateExit();
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).empty());
  assert(stormAttempts == 1);
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).size() == 1);
  stormProcesses[1]->simulateExit();
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).empty());
  assert(stormAttempts == 2);
  assert(!stormController.onSecondTick());
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).size() == 1);
  assert(!stormController.onSecondTick());
  assert(!stormController.onSecondTick());
  assert(!stormController.onSecondTick());
  // Three healthy ticks clear the prior failure history before the next exit.
  stormProcesses[2]->simulateExit();
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).empty());
  assert(stormAttempts == 3);
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).size() == 1);
  stormProcesses[3]->simulateExit();
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).empty());
  assert(stormAttempts == 4);
  assert(!stormController.onSecondTick());
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).size() == 1);
  stormProcesses[4]->simulateExit();
  assert(stormController.onSecondTick());
  assert(stormController.apply(livePath, {"DP-1"}).empty());
  assert(stormAttempts == 5);
  for (int i = 0; i < 8; ++i) {
    assert(!stormController.onSecondTick());
    assert(stormController.apply(livePath, {"DP-1"}).empty());
  }
  assert(stormAttempts == 5);
  return 0;
}
