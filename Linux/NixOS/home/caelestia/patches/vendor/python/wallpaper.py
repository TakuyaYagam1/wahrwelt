import json
import os
import random
import subprocess
from argparse import Namespace
from pathlib import Path
from typing import cast

from materialyoucolor.hct import Hct
from materialyoucolor.utils.color_utils import argb_from_rgb
from PIL import Image

from caelestia.utils.video_cache import (
    VALID_WALLPAPER_EXTENSIONS,
    VIDEO_EXTENSIONS,
    classify_path,
    is_animated_image,
    is_animated_webp,
    is_video_file,
    should_regenerate_cache,
)

from caelestia.utils.colourfulness import get_variant
from caelestia.utils.hypr import message
from caelestia.utils.material import get_colours_for_image
from caelestia.utils.paths import (
    compute_hash,
    get_config,
    wallpaper_link_path,
    wallpaper_path_path,
    wallpaper_thumbnail_path,
    wallpapers_cache_dir,
)
from caelestia.utils.scheme import Scheme, get_scheme
from caelestia.utils.theme import apply_colours


def is_valid_image(path: Path) -> bool:
    return path.is_file() and path.suffix.lower() in VALID_WALLPAPER_EXTENSIONS


def is_live_wallpaper(path: Path) -> bool:
    return is_video_file(path) or path.suffix.lower() == ".gif" or is_animated_webp(path)


def check_wall(wall: Path, filter_size: tuple[int, int], threshold: float) -> bool:
    if is_video_file(wall):
        return True
    try:
        with Image.open(wall) as img:
            width, height = img.size
            return width >= filter_size[0] * threshold and height >= filter_size[1] * threshold
    except (OSError, ValueError):
        return False


def get_wallpaper() -> str | None:
    try:
        return wallpaper_path_path.read_text()
    except IOError:
        return None


def get_wallpapers(args: Namespace) -> list[Path]:
    directory = Path(args.random)
    if not directory.is_dir():
        return []

    walls = [f for f in directory.rglob("*") if is_valid_image(f)]

    if args.no_filter:
        return walls

    monitors = cast(list[dict[str, int]], message("monitors"))
    filter_size = min(m["width"] for m in monitors), min(m["height"] for m in monitors)

    return [f for f in walls if check_wall(f, filter_size, args.threshold)]


def get_thumb(wall: Path, cache: Path) -> Path:
    thumb = cache / "thumbnail.jpg"

    if thumb.exists() and not should_regenerate_cache(wall, thumb):
        return thumb

    thumb.parent.mkdir(parents=True, exist_ok=True)

    previous = None
    try:
        previous = wallpaper_thumbnail_path.resolve(strict=True)
    except OSError:
        pass

    temporary = thumb.with_suffix(".tmp.jpg")
    temporary.unlink(missing_ok=True)
    try:
        if is_video_file(wall):
            subprocess.run(
                [
                    "ffmpeg",
                    "-hide_banner",
                    "-loglevel",
                    "error",
                    "-y",
                    "-i",
                    str(wall),
                    "-frames:v",
                    "1",
                    "-vf",
                    "scale=384:-1:flags=lanczos",
                    "-q:v",
                    "2",
                    str(temporary),
                ],
                check=True,
                stderr=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
            )
        elif is_animated_image(wall):
            with Image.open(wall) as img:
                img.seek(0)
                frame = img.convert("RGB")
                frame.thumbnail((384, 384), Image.Resampling.LANCZOS)
                frame.save(temporary, "JPEG", quality=82, optimize=True)
        else:
            with Image.open(wall) as img:
                frame = img.convert("RGB")
                frame.thumbnail((384, 384), Image.Resampling.LANCZOS)
                frame.save(temporary, "JPEG", quality=82, optimize=True)
        if not temporary.exists():
            raise OSError(f"thumbnail command produced no output for {wall}")
        os.replace(temporary, thumb)
    except (OSError, ValueError, subprocess.SubprocessError):
        temporary.unlink(missing_ok=True)
        if previous and previous.exists():
            return previous
        if not thumb.exists():
            Image.new("RGB", (128, 128), color="black").save(thumb, "JPEG", quality=70, optimize=True)

    return thumb


def get_smart_opts(wall: Path, cache: Path) -> dict:
    opts_cache = cache / "smart.json"

    try:
        return json.loads(opts_cache.read_text())
    except (IOError, json.JSONDecodeError):
        pass

    opts = {"mode": "dark", "variant": "tonalSpot"}

    try:
        with Image.open(get_thumb(wall, cache)) as img:
            opts["variant"] = get_variant(img)
            img.thumbnail((1, 1), Image.Resampling.LANCZOS)

            pixel = cast(tuple[int, int, int], img.getpixel((0, 0)))
            hct = Hct.from_int(argb_from_rgb(*pixel))
            opts["mode"] = "light" if hct.tone > 60 else "dark"
    except (OSError, ValueError, TypeError):
        pass

    opts_cache.parent.mkdir(parents=True, exist_ok=True)
    with opts_cache.open("w") as f:
        json.dump(opts, f)

    return opts


def get_colours_for_wall(wall: Path | str, no_smart: bool) -> None:
    wall = Path(wall)
    scheme = get_scheme()
    try:
        cache = wallpapers_cache_dir / compute_hash(wall)
    except OSError:
        cache = wallpapers_cache_dir / "current"

    name = "dynamic"

    if not no_smart:
        smart_opts = get_smart_opts(wall, cache)
        scheme = Scheme(
            {
                "name": name,
                "flavour": scheme.flavour,
                "mode": smart_opts["mode"],
                "variant": smart_opts["variant"],
                "colours": scheme.colours,
            }
        )

    try:
        colours = get_colours_for_image(get_thumb(wall, cache), scheme)
    except (OSError, ValueError, TypeError):
        colours = scheme.colours

    return {
        "name": name,
        "flavour": scheme.flavour,
        "mode": scheme.mode,
        "variant": scheme.variant,
        "colours": colours,
    }


def convert_gif(wall: Path) -> Path:
    """Keep the upstream helper name for callers while handling all live images."""

    cache = wallpapers_cache_dir / compute_hash(wall)
    output_path = cache / "first_frame.png"

    if not output_path.exists():
        output_path.parent.mkdir(parents=True, exist_ok=True)
        try:
            with Image.open(wall) as img:
                img.seek(0)
                img.convert("RGB").save(output_path, "PNG")
        except (OSError, EOFError, ValueError):
            output_path.unlink(missing_ok=True)

    return output_path


def set_wallpaper(wall: Path, no_smart: bool) -> None:
    # Make path absolute
    wall = Path(wall).resolve()

    if not is_valid_image(wall):
        raise ValueError(f'"{wall}" is not a valid image')

    # Update files
    wallpaper_path_path.parent.mkdir(parents=True, exist_ok=True)
    wallpaper_path_path.write_text(str(wall))
    wallpaper_link_path.parent.mkdir(parents=True, exist_ok=True)
    wallpaper_link_path.unlink(missing_ok=True)
    wallpaper_link_path.symlink_to(wall)

    try:
        cache = wallpapers_cache_dir / compute_hash(wall)
    except OSError:
        cache = wallpapers_cache_dir / "current"

    # Generate thumbnail or get from cache
    thumb = get_thumb(wall, cache)
    wallpaper_thumbnail_path.parent.mkdir(parents=True, exist_ok=True)
    wallpaper_thumbnail_path.unlink(missing_ok=True)
    wallpaper_thumbnail_path.symlink_to(thumb)

    scheme = get_scheme()

    # Change mode and variant based on wallpaper colour
    if scheme.name == "dynamic" and not no_smart:
        smart_opts = get_smart_opts(wall, cache)
        scheme.mode = smart_opts["mode"]
        scheme.variant = smart_opts["variant"]

    # Update colours
    scheme.update_colours()
    apply_colours(scheme.colours, scheme.mode)

    # Run custom post-hook if configured
    cfg = get_config().get("wallpaper", {})
    if post_hook := cfg.get("postHook"):
        subprocess.run(
            post_hook,
            shell=True,
            env={
                **os.environ,
                "WALLPAPER_PATH": str(wall),
                "SCHEME_NAME": scheme.name,
                "SCHEME_FLAVOUR": scheme.flavour,
                "SCHEME_MODE": scheme.mode,
                "SCHEME_VARIANT": scheme.variant,
                "SCHEME_COLOURS": json.dumps(scheme.colours),
                "THUMBNAIL_PATH": str(thumb),
            },
            stderr=subprocess.DEVNULL,
        )


def set_random(args: Namespace) -> None:
    wallpapers = get_wallpapers(args)

    if not wallpapers:
        raise ValueError("No valid wallpapers found")

    try:
        last_wall = wallpaper_path_path.read_text()
        wallpapers.remove(Path(last_wall))

        if not wallpapers:
            raise ValueError("Only valid wallpaper is current")
    except (FileNotFoundError, ValueError):
        pass

    set_wallpaper(random.choice(wallpapers), args.no_smart)
