# Project guidance

## Scope
- Keep the backend in Go. Frontend technology may be selected independently.
- Preserve offline-first behavior: failed refreshes must not discard cached data, and cached operational data must retain source and freshness metadata.
- Do not present stale or unavailable aviation data as current. Keep official source, issue/effective time, and refresh time visible.
- AviationWeather.gov airport, METAR, and TAF lookups support worldwide ICAO identifiers. Its SIGMET feed includes U.S. domestic and international products and is cached independently; PIREPs are requested within 100 NM for the previous six hours but are primarily available in the U.S. and North Atlantic.
- The METAR panel keeps the raw AviationWeather.gov report and summarizes its `clouds`, `wxString`, and `altim` fields. Ceiling is the lowest reported BKN/OVC/VV layer; cloud bases are AGL. Treat this as a convenience decode and verify the raw report.
- The TAF panel retains raw text and renders AviationWeather.gov `fcsts` periods in UTC, ordered by `timeFrom`; source change groups and probabilities are shown without converting them into a deterministic forecast.
- The SIGMET map is a schematic 500 NM overlay centered on the selected airport, not a basemap or navigation chart.
- FAA ArcGIS Class Airspace is queried as GeoJSON within 250 NM of a selected location and cached with an eight-week freshness interval; its published cycle is updated every eight weeks. Coverage is U.S., Puerto Rico, and U.S. Virgin Islands only. The FAA item states data is for public use and credits FAA Aeronautical Information Services. This layer is reference-only and is not for navigation or clearances: https://services6.arcgis.com/ssFJjBXIUyZDrSYZ/ArcGIS/rest/services/Class_Airspace/FeatureServer
- FAA Graphic TFRs provides a public current TFR location GeoJSON layer (max 300 returned polygons) through `https://tfr.faa.gov/geoserver/TFR/ows`; the app caches successful responses for 15 minutes and stale responses on disk for fallback. FAA publishes per-feature title, NOTAM key, and last-modified time; human-readable validity dates may occur in the title but are not structured and the app does not parse them into active/future/expired status. The map lists at most 20 nearby FAA locations, with polygon count and list count reported separately. FAA publishes no guaranteed update cadence. The overlay is U.S.-focused and a supplemental reference only; verify current NOTAMs directly with FAA before flight.
- Once the user saves the global OurAirports directory, the airport search supports offline name, ICAO, IATA, and local-identifier matches; selecting a result can show only the cached reference record without live weather.
- FAA procedures, VFR charts, and NOTAM search are U.S.-specific. AIP:Aero indexes official AIP chart links for 64 countries, but is external and does not integrate charts. EUROCONTROL AIS Online lists AIS/AIM websites worldwide; entries are user-provided and may be incomplete or inaccurate. International operational NOTAMs must come from the destination State’s AIS/AIP. ICAO’s global NOTAM portal reports 12-hour collection and is reference-only; the separate ICAO API requires a key, has licensing terms, and its NOTAM database was reported not updated since late 2025. EAD Basic is not connected to operational data.
- Confirm data access and redistribution terms before adding a provider.

## Engineering
- Prefer the Go standard library until a concrete requirement warrants a dependency.
- Keep provider integrations and HTTP handlers small, testable packages.
- Use context cancellation and bounded timeouts for network operations.
- Add behavior-focused tests for API contracts, stale/cache behavior, and provider parsing.
- Run `make check` before submitting changes. Run `make build-all` for desktop cross-builds; run `make build-android-apk` and `make android-test` for Android when the SDK/emulator is available.
- Never commit credentials, downloaded aviation datasets, generated binaries, or local environment files.

## Platform builds
- Desktop Go builds target macOS, Linux, and Windows on amd64 and arm64 where supported by Go.
- Android APK packaging uses the checked-in Gradle wrapper, Go Mobile bridge, and Android SDK; see `android/README.md`. iOS app packaging is not yet available.
