# ProxyLens Background Process Catalog v1

## Purpose and evidence boundary

The background process catalog is embedded, versioned knowledge used to add
context to observed PROXY traffic. Raw event/accounting evidence answers what
traffic was observed; the catalog only answers whether the event-time process
name and process path matched a reviewed catalog entry.

A catalog match is not executable verification. It does not assert that a file
is signed, trusted, safe, malicious, currently foreground/background, or that
the traffic should be DIRECT. It is not a routing recommendation.

## Runtime contract

The v1 catalog is compiled into the storage package with `go:embed`. It is not
user-editable, is never fetched at runtime, and has no remote refresh path.
Source URLs are provenance metadata only. Matching does not read the filesystem,
resolve symlinks, inspect Authenticode, query Windows services, or add a network
client.

The detector is read-only and is evaluated during the existing single
`GET /api/v1/intelligence/findings` accounting scan. It considers only positive
accounted `PROXY` bytes in the requested `[from,to)` window.

## Matching rules

Matching requires both:

1. an exact case-insensitive process-name match; and
2. an event-time `process_path` match against the entry's path rules.

Missing paths and same-name paths outside the catalog rule do not match. The
catalog candidate is selected by normalized process name in O(1); the selected
entry's path rules are then checked.

Windows path normalization trims whitespace and one surrounding quote pair,
converts `/` to `\\`, collapses repeated separators, removes a non-root
trailing separator, and lowercases. Path rules are deliberately limited to
`exact` and `under_directory`. The latter requires the executable path to be
under the directory and its basename to equal the observed process name.

## Version and provenance

The embedded version is `background-processes-v1`, reviewed on `2026-09-06`.
Production entries use only these official Microsoft Learn sources:

- [Microsoft Defender Antivirus in Windows overview](https://learn.microsoft.com/en-us/defender-endpoint/microsoft-defender-antivirus-windows)
- [Microsoft Defender for Endpoint standard connectivity URLs - commercial](https://learn.microsoft.com/en-us/defender-endpoint/standard-device-connectivity-urls-commercial)

## v1 production entries

The seed is intentionally limited to exactly three Microsoft Defender entries:

| Entry | Process | Path rule | Family |
| --- | --- | --- | --- |
| `microsoft-defender-antivirus-service` | `MsMpEng.exe` | under `C:\\ProgramData\\Microsoft\\Windows Defender\\Platform` | Microsoft Defender Antivirus |
| `microsoft-defender-core-service` | `MpDefenderCoreService.exe` | under the Defender Platform directory, or exact `C:\\Program Files\\Windows Defender\\MpDefenderCoreService.exe` | Microsoft Defender Core Service |
| `microsoft-defender-network-inspection-service` | `NisSrv.exe` | under the Defender Platform directory, or exact `C:\\Program Files\\Windows Defender\\NisSrv.exe` | Microsoft Defender Network Inspection Service |

The v1 catalog does not include `svchost.exe`, Windows Update processes,
`SecurityHealthService.exe`, third-party updaters, browser background
processes, or any other unreviewed family. Generic process-name-only
classification is forbidden.

## Finding identity and future changes

Catalog findings use the stable detector kind
`cataloged_background_process_proxy` and aggregate by catalog entry, normalized
process name, and the existing Phase 4 target precedence:
`host → sniff_host → destination_ip → missing`. The finding ID excludes the
observed path, byte/count evidence, source URL, review date, catalog version,
and query end time. A later matching platform path may update display evidence
without changing identity.

Future entries require an official vendor source, deterministic exact/path-rule
semantics, negative spoof/pathless tests, and a separately reviewed catalog
change. Adding a catalog entry must not be used to infer a DIRECT rule. Rule
candidate suggestions and rule application remain Phase 4C2 scope.
