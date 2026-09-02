#!/usr/bin/env python3
"""Build the deterministic WOFF2 faces bundled by the ProxyLens UI.

The source files are intentionally supplied by the caller and are not kept in
the repository. From the ui/ directory:

  python scripts/build-production-fonts.py \
    --manrope-source C:/path/to/Manrope[wght].ttf \
    --sarasa-regular C:/path/to/SarasaUiSC-Regular.ttf \
    --sarasa-semibold C:/path/to/SarasaUiSC-SemiBold.ttf

Requires fontTools with the brotli extra (for example, ``pip install
fonttools brotli``). Manrope is statically instantiated at 400 and 500; the
Sarasa SemiBold source is deliberately mapped to CSS weight 500 by fonts.css.
"""

from __future__ import annotations

import argparse
from pathlib import Path

from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont


def write_woff2(font: TTFont, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    font.recalcTimestamp = False
    font.flavor = "woff2"
    font.save(output)


def build_manrope(source: Path, output_dir: Path) -> None:
    variable = TTFont(source, recalcTimestamp=False)
    axes = {axis.axisTag for axis in variable["fvar"].axes}
    if axes != {"wght"}:
        raise ValueError(f"Expected Manrope to contain only the wght axis, found {sorted(axes)}")

    for weight, filename in ((400, "Manrope-Regular.woff2"), (500, "Manrope-Medium.woff2")):
        instance = instantiateVariableFont(
            variable,
            {"wght": weight},
            inplace=False,
            optimize=True,
            updateFontNames=True,
            static=True,
        )
        write_woff2(instance, output_dir / filename)


def build_static(source: Path, output: Path) -> None:
    font = TTFont(source, recalcTimestamp=False)
    if "fvar" in font:
        raise ValueError(f"Expected a static source font: {source}")
    write_woff2(font, output)


def parse_args() -> argparse.Namespace:
    default_output = Path(__file__).resolve().parent.parent / "src" / "assets" / "fonts"
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manrope-source", type=Path, required=True)
    parser.add_argument("--sarasa-regular", type=Path, required=True)
    parser.add_argument("--sarasa-semibold", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, default=default_output)
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    sources = (args.manrope_source, args.sarasa_regular, args.sarasa_semibold)
    missing = [str(path) for path in sources if not path.is_file()]
    if missing:
        raise FileNotFoundError("Missing source font(s): " + ", ".join(missing))

    output_dir = args.output_dir.resolve()
    build_manrope(args.manrope_source, output_dir)
    build_static(args.sarasa_regular, output_dir / "SarasaUiSC-Regular.woff2")
    build_static(args.sarasa_semibold, output_dir / "SarasaUiSC-SemiBold.woff2")

    for output in sorted(output_dir.glob("*.woff2")):
        print(f"{output.name}\t{output.stat().st_size} bytes")


if __name__ == "__main__":
    main()
