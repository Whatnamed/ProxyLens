# ProxyLens bundled fonts

ProxyLens bundles only the normal WOFF2 faces used by the UI. Narrative
typography is script-aware and locale-independent: Manrope is preferred for
Latin glyphs, Sarasa UI SC for Han/CJK glyphs, and JetBrains Mono is fixed for
technical/evidence values. `font-synthesis: none` is enabled for the product.
All bundled fonts are distributed under the SIL Open Font License 1.1. The
full license text is included in `OFL-1.1.txt` and packaged under the
application `licenses/` resources directory.

| Family | Files | Source | License |
| --- | --- | --- | --- |
| Manrope | `Manrope-Regular.woff2`, `Manrope-Medium.woff2` | [Google Fonts `google/fonts` `ofl/manrope`](https://github.com/google/fonts/tree/main/ofl/manrope), upstream Manrope v4.501 | SIL Open Font License 1.1 |
| Sarasa Gothic UI SC | `SarasaUiSC-Regular.woff2`, `SarasaUiSC-SemiBold.woff2` | [Sarasa Gothic v1.0.41 release](https://github.com/be5invis/Sarasa-Gothic/releases/tag/v1.0.41), `SarasaUiSC-TTF-1.0.41.7z` | SIL Open Font License 1.1 |
| JetBrains Mono | `JetBrainsMono-Regular.woff2`, `JetBrainsMono-Medium.woff2` | [JetBrains Mono v2.304](https://github.com/JetBrains/JetBrainsMono/tree/v2.304) | SIL Open Font License 1.1 |

## Source and build record

- Manrope source: `google/fonts` `main` at `ofl/manrope/Manrope[wght].ttf`;
  source SHA-256 `d0639be45d0af36e798172419d7bd173c4bd4f29e2b76cbb69db1d11bf8b0a40`.
  The Google Fonts file is byte-identical to upstream
  `aaronbell/manrope` commit `6f81ebecdf65e4463b798cc07b16a4f8d5216917`.
- Sarasa source archive: `SarasaUiSC-TTF-1.0.41.7z`, release SHA-256
  `568015e578037cdcfb7c55d522f7e861e11ecd304824d6c94965cbb2187a8d78`.
  Extracted source SHA-256 values: `SarasaUiSC-Regular.ttf`
  `7af09826004585298057f4e9a73c4e01804ba104ba1368351693bb6bc9ff5e84`,
  `SarasaUiSC-SemiBold.ttf`
  `eefb8bb755a38bbbb5940bedd709f3b4d4d0a1730e644a7911b39cd5a4544e78`.
- The reproducible conversion entry point is
  [`ui/scripts/build-production-fonts.py`](../../../scripts/build-production-fonts.py).
  It uses `fontTools` with `brotli`, statically instantiates Manrope at CSS
  weights 400 and 500, and converts the two static Sarasa faces to WOFF2.
  The Sarasa SemiBold source has a native 600 weight; `fonts.css` deliberately
  maps that source face to CSS weight 500 because Sarasa does not ship a native
  Medium face.
- Generated output sizes at this revision: Manrope Regular `30,840` bytes,
  Manrope Medium `30,608` bytes, Sarasa UI SC Regular `8,339,124` bytes,
  Sarasa UI SC SemiBold `8,479,520` bytes, JetBrains Mono Regular `92,164`
  bytes and JetBrains Mono Medium `93,824` bytes; total `17,066,080` bytes.
- Example command from `ui/`:

  ```text
  python scripts/build-production-fonts.py --manrope-source C:/path/to/Manrope[wght].ttf --sarasa-regular C:/path/to/SarasaUiSC-Regular.ttf --sarasa-semibold C:/path/to/SarasaUiSC-SemiBold.ttf
  ```

No source archives, source TTFs, runtime CDN URLs, or system-wide font
installation are required by the product build.
