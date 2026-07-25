# TorrServer (Go engine)

Mobile-friendly fork of TorrServer — local torrent engine for iOS.

## Folder Layout

```
TorrServerIos/
  TorrServerLibrary/        ← THIS REPO — Go engine (branch feat/native-ios-app)
  TorrServerCoreBridge/     ← Swift SPM package, wraps xcframework
  TorrServerClient/         ← iOS app, depends on CoreBridge via SPM
```

## How it works together

1. **TorrServerLibrary** — Go source. Compiled to `TorrCore.xcframework` (C archive).
2. **TorrServerCoreBridge** — Swift Package. Commits the xcframework. Wraps C functions in a Swift `actor TorrCoreClient`.
3. **TorrServerClient** — iOS app. Depends on CoreBridge via SPM (`Package.swift`).
   Also uses `.xcodeproj` for Signing & Capabilities. Widget extension for Live Activity.

Flow: `iOS App → TorrCoreClient (Swift actor) → C-exported functions → Go engine`

## Build

```bash
# Build iOS XCFramework (arm64 + simulator arm64)
cd server && bash scripts/build-ios-carchive.sh
# Output: build/ios/TorrCore.xcframework
```

## Architecture

```
server/
  main.go          — Original CLI/HTTP entry point (kept for desktop builds)
  mobile/
    core/
      engine.go    — Engine lifecycle (start/stop/torrents/streams/warmup/downloadzip)
      link.go      — Parse magnet/infohash links
      config.go    — Engine config parsing and validation
    carchive/
      exports.go   — C-exported API (TS_Start, TS_Stop, TS_AddTorrent, …)
      strings.go   — C string helpers (toCString, fromCString, TS_Free)
  torr/
    torrent.go     — Torrent wrapper (NewTorrent, Status, watch, …)
    apihelper.go   — SaveTorrentToDB, AddTorrent, GetTorrent, ListTorrent
    dbwrapper.go   — AddTorrentDB, GetTorrentDB, ListTorrentsDB
  settings/
    settings.go    — Global init, DB routing
    torrent.go     — Torrent DB CRUD (AddTorrent, ListTorrent, RemTorrent)
    db.go          — Bbolt database (Open, Get, Set, Close)
    dbreadcache.go — Read-cache wrapper
```

## C ABI Contract

All exported functions are in `mobile/carchive/exports.go`:
- Return `*C.char` (JSON) — caller must free via `TS_Free(ptr)`
- Input takes `*C.char` (JSON request)
- JSON envelope: `{"ok":true,"data":{…}}` or `{"ok":false,"error":{"code":"…","message":"…"}}`

## Concurrency Notes

- `anacrolix/torrent` spawns background goroutines (DHT, trackers, watch)
- All `recover()` calls must be in the same goroutine as potential panic
- `bolt.DB` uses file locking — concurrent writes serialize
- `tsFiles.TorrServer.Files` is nil for torrents without metadata (fresh magnet links)

## Key Dependencies

- `github.com/anacrolix/torrent` — BitTorrent protocol
- `go.etcd.io/bbolt` — Embedded DB (torrents, settings, viewed)
- `github.com/anacrolix/dht/v2` — DHT networking
