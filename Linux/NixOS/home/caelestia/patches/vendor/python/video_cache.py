from __future__ import annotations

from pathlib import Path

from PIL import Image

VIDEO_EXTENSIONS = frozenset({".mp4", ".webm", ".mkv", ".mov", ".avi"})
IMAGE_EXTENSIONS = frozenset({".jpg", ".jpeg", ".png", ".webp", ".tif", ".tiff", ".gif"})
VALID_WALLPAPER_EXTENSIONS = VIDEO_EXTENSIONS | IMAGE_EXTENSIONS


def is_animated_image(path: str | Path) -> bool:
    """Return whether Pillow reports multiple frames for a supported image."""

    image_path = Path(path)
    if image_path.suffix.lower() not in {".gif", ".webp"}:
        return False

    try:
        with Image.open(image_path) as image:
            return bool(getattr(image, "is_animated", False) or getattr(image, "n_frames", 1) > 1)
    except (OSError, ValueError):
        return False


def is_animated_webp(path: str | Path) -> bool:
    image_path = Path(path)
    return image_path.suffix.lower() == ".webp" and is_animated_image(image_path)


def is_video_file(path: str | Path) -> bool:
    return Path(path).suffix.lower() in VIDEO_EXTENSIONS


def is_live_file(path: str | Path) -> bool:
    image_path = Path(path)
    suffix = image_path.suffix.lower()
    return suffix in VIDEO_EXTENSIONS or suffix == ".gif" or is_animated_webp(image_path)


def classify_path(path: str | Path) -> str:
    """Classify a wallpaper as static or live without trusting the extension alone."""

    image_path = Path(path)
    if image_path.suffix.lower() not in VALID_WALLPAPER_EXTENSIONS:
        return "unsupported"
    return "live" if is_live_file(image_path) else "static"


def should_regenerate_cache(source: str | Path, cache: str | Path) -> bool:
    source_path = Path(source)
    cache_path = Path(cache)

    if not source_path.exists():
        return False

    if not cache_path.exists():
        return True

    try:
        return source_path.stat().st_mtime_ns > cache_path.stat().st_mtime_ns
    except OSError:
        return True
