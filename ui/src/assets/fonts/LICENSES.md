# ProxyLens bundled fonts

ProxyLens bundles only the normal WOFF2 weights used by the UI: 400 Regular and
500 Medium. No system-wide font installation is required.

| Family | Files | Source | License |
| --- | --- | --- | --- |
| Public Sans | `PublicSans-Regular.woff2`, `PublicSans-Medium.woff2` | [USWDS Public Sans v2.001](https://github.com/uswds/public-sans/tree/v2.001) | SIL Open Font License 1.1; Public Sans also includes USWDS CC0 modifications |
| IBM Plex Sans SC | `IBMPlexSansSC-Regular.woff2`, `IBMPlexSansSC-Medium.woff2` | [`@ibm/plex-sans-sc` 1.1.0](https://github.com/IBM/plex/tree/v1.1.0) | SIL Open Font License 1.1 |
| JetBrains Mono | `JetBrainsMono-Regular.woff2`, `JetBrainsMono-Medium.woff2` | [JetBrains Mono v2.304](https://github.com/JetBrains/JetBrainsMono/tree/v2.304) | SIL Open Font License 1.1 |

IBM Plex Sans SC uses the complete Simplified Chinese WOFF2 files from the
official package, rather than a local OS font or a runtime CDN subset. Public
Sans is the Latin narrative face; IBM Plex Sans SC is selected for the Chinese
locale; JetBrains Mono remains the technical/evidence face.
