# FP implementation plan

This file is the authoritative backlog. `DONE` means the acceptance criteria below have implementation and verification evidence. `BLOCKED` means the remaining acceptance criteria depend on an external account, permission, source, or data-use grant that this repository cannot supply. A blocked item must not be represented as complete.

## Product scope

FP is an offline-capable pilot-information application, not an approved navigation system. It combines cached reference information with current third-party aviation data when those services are available. Data scope, currency, and offline availability remain source-specific.

## Completed items

### 1. Go backend and cross-platform application shell — DONE

Acceptance: Go serves the embedded web UI and APIs; desktop targets build for macOS, Linux, and Windows on amd64 and arm64; Android and iOS clients build; automated checks run in GitHub Actions.

Evidence: `make check`, Go tests and vet, Android instrumentation, iOS device/Simulator build and Simulator launch passed for commit `06d8a5e`; GitHub CI run [41229236392](https://github.com/rusik69/fp/actions/runs/41229236392) succeeded.

### 2. Weather briefing and airport reference — DONE

Acceptance: airport, METAR, and TAF lookup retains raw source reports, presents decoded METAR/TAF details, and serves cached data with stale/currency state when refresh fails; saved airport reference data is available for offline search without implying live weather.

Evidence: `internal/aviation`, `internal/airports`, and `internal/httpapi` tests pass; `make check` and CI succeeded. Provider coverage and freshness remain source-dependent.

### 3. U.S. procedures, VFR charts, and NOTAM access — DONE

Acceptance: FAA procedure plates/airport diagrams and VFR chart catalog can be retrieved and saved as PDFs for offline use; the U.S. NOTAM action links to FAA search and is not falsely described as an FP-fetched feed.

Evidence: `internal/charts` and HTTP API tests pass; the UI documents FAA coverage and cached PDF behavior; `make check` and CI succeeded.

### 4. Offline browser library and local briefing storage — DONE

Acceptance: saved PDFs and user-imported regional PDFs can be listed, removed, and reopened offline; local NOTAM/PIB copies preserve source and issue-time labels; service-worker cached responses are marked as cached and potentially stale.

Evidence: browser storage/HTTP service-worker behavior is implemented and documented in `AGENTS.md`; Go and Android instrumentation checks pass. Browser storage remains subject to user clearing and platform eviction.

### 5. Aviation reference map and offline geographic context — DONE

Acceptance: selected-airport map shows available SIGMET, PIREP, FAA airspace/TFR, and airport/navaid reference overlays; the Natural Earth 1:110m land outline is available offline; all schematic/reference layers are labeled as non-navigation data.

Evidence: Natural Earth geometry is validated by `TestNaturalEarthBasemapContainsClosedGeographicLandRings`; browser smoke test rendered the land layer offline; map rendering and platform checks passed. This is not a navigation chart.

### 6. Conditional international map and NOTAM integration — IMPLEMENTED; LIVE VALIDATION BLOCKED

Acceptance for implementation: the iBonav map token stays server-side, map messages are restricted by origin/source, NOTAM requests are validated and cached for offline reference, and provider coverage/authentication caveats are visible.

Evidence: `internal/ibonav` and `internal/httpapi/ibonav_test.go` cover mocked provider requests, credential non-disclosure, validation, and error behavior; the UI and README document required roles. The selected-airport map center was browser-smoke-tested for KJFK, KLAX, and missing-coordinate fallback. No live provider connection was made.

### 7. Region-specific AIS/AIM discovery — PARTIAL

Delivered: country-specific official AIS/NOTAM links, a general State AIS/AIP fallback, and provider coverage/access caveats are documented in `AGENTS.md` and exposed by the UI. These links are not integrated feeds and are not cached by FP. The existing iBonav API is a conditional aggregation path.

Acceptance for this delivered portion: source links are explicit about account requirements, scope, freshness, and whether the page is a route PIB. This does not mean every country's operational data is integrated.

### 8. Android/iOS app versions and preview distribution — DONE

Acceptance: Android APK and unsigned iOS device/Simulator bundles build with version metadata; Android instrumentation and iOS Simulator launch pass; desktop/mobile release artifacts have checksums.

Evidence: local Android instrumentation and iOS Simulator launch passed; release `v0.1.3` contains desktop amd64/arm64 binaries, Android debug APK, unsigned iOS bundles, and `SHA256SUMS`.

## Remaining external blockers

### B1. Live iBonav end-to-end validation — BLOCKED

Required prerequisite: an authorized server operator must configure `FP_IBONAV_PERSONAL_TOKEN` locally and the provider must grant the account the AIS Map Frame, PIB API, and Charts API roles. The variable is not configured in the current environment. Do not put the token in source control, a mobile package, or chat.

Acceptance once unblocked: validate a real map token exchange, chart/map layer access, and NOTAM query against the provider; capture provider issue/update metadata; verify stale-cache behavior on a failed/offline refresh. Mock tests do not satisfy this live-validation item.

### B2. Worldwide operational charts and NOTAM feeds — BLOCKED / PARTIAL

Required prerequisite: a named provider or State AIS source for each intended region, authorized account/API access where needed, and written/official terms permitting the intended retrieval, display, and offline storage. Existing regional links, manual PDF imports, and Natural Earth geography are not operational chart/NOTAM feeds. User direction is to stop adding more source research for now.

Acceptance once sources and permissions are available: maintain a region-by-region coverage matrix; integrate only permitted official data; show source, issue/effective time, retrieval time, and coverage; preserve old cached material on refresh failure and mark it stale; test one operationally representative route/area per integrated region. Never label the schematic map or an expired/unverified product as current navigation data.

## Unblocked work

No additional implementation item is currently unblocked in this backlog. Items B1 and B2 require external access or permissions. When those prerequisites are supplied, update this file with the newly verified scope and continue with their acceptance criteria.