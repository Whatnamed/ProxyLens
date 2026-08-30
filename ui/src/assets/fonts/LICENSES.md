# ProxyLens bundled fonts

ProxyLens bundles only the normal WOFF2 weights used by the UI: 400 Regular and
500 Medium. No system-wide font installation is required.

| Family | Files | Source | License |
| --- | --- | --- | --- |
| IBM Plex Sans SC | `IBMPlexSansSC-Regular.woff2`, `IBMPlexSansSC-Medium.woff2` | [`@ibm/plex-sans-sc` 1.1.0](https://github.com/IBM/plex/tree/v1.1.0) | SIL Open Font License 1.1 |
| JetBrains Mono | `JetBrainsMono-Regular.woff2`, `JetBrainsMono-Medium.woff2` | [JetBrains Mono v2.304](https://github.com/JetBrains/JetBrainsMono/tree/v2.304) | SIL Open Font License 1.1 |

IBM Plex Sans SC uses the complete Simplified Chinese WOFF2 files from the
official package, rather than a local OS font or a runtime CDN subset. It is the
formal production default for both logical Narrative script axes (Latin and
CJK); JetBrains Mono remains the technical/evidence face. DEV-only Font Lab
candidate fonts are kept outside this bundled asset set.
