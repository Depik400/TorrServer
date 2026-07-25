# TorrCore iOS Archive — Результат сборки

## Описание

Статический Go `c-archive` (`libTorrCore.a`), встраивающий BitTorrent-ядро TorrServer
в iOS-приложение через C ABI. Упакован в `TorrCore.xcframework` для удобного
подключения в Xcode.

**Ветка**: `feat/native-ios-app`
**Версия TorrServer**: `MatriX.137`
**Размер архива**: ~20 MB (arm64)

---

## 1. Артефакты

| Артефакт | Путь |
|-----------|------|
| Device archive | `build/ios/iphoneos/libTorrCore.a` |
| Device headers | `build/ios/iphoneos/Headers/TorrCore.h` |
| Simulator archive | `build/ios/iphonesimulator-arm64/libTorrCore.a` |
| Simulator headers | `build/ios/iphonesimulator-arm64/Headers/TorrCore.h` |
| **XCFramework** | `build/ios/TorrCore.xcframework/` |

Артефакты в `.gitignore`, в репозиторий не попадают. Пересобираются скриптом
`server/scripts/build-ios-carchive.sh` (только macOS, нужен Xcode).

---

## 2. Подключение XCFramework в Xcode

1. Перетащить `build/ios/TorrCore.xcframework` в Xcode → Target → General →
   **Frameworks, Libraries, and Embedded Content**
2. Установить **Embed** = `Do Not Embed` (это статическая библиотека)
3. В Target → Build Phases → Link Binary With Libraries добавить:
   - `CoreFoundation.framework`
   - `Security.framework`
4. В Target → Build Settings → **Other Linker Flags** добавить `-lresolv`
   (если линкер выдаст ошибки символов DNS/libresolv)
5. Создать **Bridging Header** (если Swift) или подключить модуль в Objective-C:
   ```objc
   #import <TorrCore/TorrCore.h>
   ```

---

## 3. C ABI контракт

### 3.1. Память

Правило: **каждая** `char*`, возвращённая из `TS_*`, должна быть освобождена ровно один
раз вызовом `TS_Free`.

```
Swift вызывает TS_Start(...) → получает char*
Swift читает/декодирует JSON  → defer { TS_Free(ptr) }
```

### 3.2. JSON envelope

Все экспорты возвращают JSON в формате:

**Успех:**
```json
{
  "ok": true,
  "data": { ... }
}
```

**Ошибка:**
```json
{
  "ok": false,
  "error": {
    "code": "engine_not_running",
    "message": "engine is not running"
  }
}
```

Стройте Swift-логику на `code`, не на `message`.

### 3.3. Все экспорты

#### `TS_Version() → char*`
Возвращает строку версии TorrServer.
```json
{ "ok": true, "data": "MatriX.137" }
```
Примечание: результат — сырая строка, не JSON-конверт. Swift должен обернуть в
JSON-парсинг или проверять `ok` на наличие `{`.

Фактически возвращается чистый `"MatriX.137"`. Для единообразия рекомендуется в
Swift сделать:
```swift
let raw = String(cString: ptr)
defer { TS_Free(ptr) }
let version = raw.hasPrefix("{") ? decode(raw) : raw
```

#### `TS_Start(const char* configJSON) → char*`
Запускает Engine. Принимает JSON-конфигурацию. Возвращает базовый URL и токен.

**Request** (JSON-строка):
```json
{
  "applicationSupportPath": "/path/to/Library/Application Support/TorrServer",
  "cachePath": "/path/to/Library/Caches/TorrServer",
  "listenHost": "127.0.0.1",
  "listenPort": 0,
  "cacheSizeBytes": 134217728,
  "diskCacheEnabled": true,
  "downloadRateLimitKB": 0,
  "uploadRateLimitKB": 0,
  "connectionsLimit": 25,
  "torrentDisconnectTimeoutSeconds": 120,
  "disableUpload": true,
  "disableUPnP": true,
  "disableIPv6": false,
  "debug": false
}
```

**Response:**
```json
{
  "ok": true,
  "data": {
    "baseURL": "http://127.0.0.1:49152",
    "port": 49152,
    "token": "f3a8c1b2d4e5f67890ab1234567890cd"
  }
}
```

- `libcurlHost` — всегда `127.0.0.1` или `::1` (валидация в Go)
- `libcurlPort` — всегда `0` (автовыбор свободного порта). Фактический порт возвращается в ответе
- `token` — 128-битный криптографический random hex, включается в stream URL
- Пути создаются с правами `0755`, если не существуют
- `cacheSizeBytes` ограничен диапазоном 32 MiB – 1 GiB
- UPnP, DLNA, FUSE, WebDAV, Rutor, Torznab, Telegram — принудительно отключены

#### `TS_Stop() → char*`
Останавливает Engine. Идемпотентен (повторный вызов — не ошибка).

**Response:**
```json
{ "ok": true, "data": null }
```

Stop выполняет:
1. Отменяет все stream-сессии
2. Shutdown HTTP-сервера с 10-секундным grace-периодом
3. Disconnect BTS (закрывает DHT, peer-коннекты)
4. Закрывает и обнуляет базы данных (bbold + JSON)
5. Сбрасывает global engine

#### `TS_EngineStatus() → char*`
Возвращает состояние Engine без JSON-ошибки.

**Response (running):**
```json
{
  "ok": true,
  "data": {
    "state": "running",
    "version": "MatriX.137",
    "sessionCount": 2,
    "port": 49152,
    "baseURL": "http://127.0.0.1:49152"
  }
}
```

**Response (stopped):**
```json
{
  "ok": true,
  "data": {
    "state": "stopped",
    "sessionCount": 0,
    "version": "MatriX.137"
  }
}
```

Состояния: `stopped`, `starting`, `running`, `stopping`, `failed`.

#### `TS_AddTorrent(const char* requestJSON) → char*`
Добавляет торрент по magnet-ссылке или infohash.

**Request:**
```json
{
  "link": "magnet:?xt=urn:btih:abc123...",
  "title": "optional display name"
}
```

`link` принимает:
- `magnet:?xt=urn:btih:<hash>...`
- `urn:btih:<hash>`
- Голый 40-символьный hex infohash (автоматически приводится к magnet)

**Response:**
```json
{
  "ok": true,
  "data": {
    "hash": "abc123...",
    "title": "optional display name",
    "state": "added"
  }
}
```

#### `TS_TorrentStatus(const char* hash) → char*`
Возвращает полный статус торрента. Принимает hex-хэш торрента как строку.

**Response:**
```json
{
  "ok": true,
  "data": {
    "hash": "abc123...",
    "title": "My Movie",
    "state": "Torrent working",
    "torrentSize": 1234567890,
    "loadedSize": 12345678,
    "preloadedSize": 87654321,
    "downloadSpeed": 1048576.0,
    "uploadSpeed": 0.0,
    "activePeers": 5,
    "totalPeers": 12,
    "files": [
      {
        "id": 1,
        "path": "My Movie.mkv",
        "length": 1234567890
      },
      {
        "id": 2,
        "path": "Subtitles.srt",
        "length": 45678
      }
    ]
  }
}
```

Состояния торрента (поле `state`):
- `Torrent added` — добавлен, метаданные ещё не получены
- `Torrent getting info` — получает метаданные из DHT/трекеров
- `Torrent working` — метаданные получены, раздача активна
- `Torrent preload` — идёт предзагрузка
- `Torrent closed` — остановлен/удалён

Примечание: вызывайте `TS_TorrentStatus` периодически (раз в 1–2 сек), чтобы:
- Отслеживать получение метаданных (переход `added` → `getting info` → `working`)
- Получать список файлов для выбора пользователем
- Обновлять download/upload speed для UI

#### `TS_ListTorrents() → char*`
Возвращает список всех торрентов.

**Response:**
```json
{
  "ok": true,
  "data": [
    {
      "hash": "abc123...",
      "title": "My Movie",
      "state": "Torrent working",
      "size": 1234567890,
      "poster": "",
      "added": 1753360800
    }
  ]
}
```

#### `TS_DropTorrent(const char* hash) → char*`
Удаляет торрент и чистит кэш. Принимает hex-хэш.

**Response:**
```json
{ "ok": true, "data": null }
```

#### `TS_PrepareStream(const char* requestJSON) → char*`
Создаёт streaming-сессию для воспроизведения файла во внешнем плеере.

**Request:**
```json
{
  "hash": "abc123...",
  "fileId": 1
}
```

- `hash` — hex-хэш торрента
- `fileId` — ID файла из `TS_TorrentStatus.files[].id` (нумерация с 1)

**Response:**
```json
{
  "ok": true,
  "data": {
    "sessionId": "s1",
    "url": "http://127.0.0.1:49152/stream/s1/My%20Movie.mkv?token=f3a8c1b2...",
    "fileName": "My Movie.mkv",
    "fileLength": 1234567890
  }
}
```

**Важно:**

- `url` — **полный** URL, Swift НЕ должен модифицировать его (токен, имя файла,
  session ID уже закодированы правильно). Отдавайте его в плеер как есть.
- Metadata должна быть уже получена (state = `Torrent working`), иначе вернётся
  ошибка `torrent_metadata_pending`
- Сессия автоматически добавляет `expiredTime` к торренту, предотвращая его
  disconnect по таймауту, пока есть активный streaming
- Каждый вызов создаёт **новую** сессию

#### `TS_StreamStatus(const char* sessionID) → char*`
Возвращает статус streaming-сессии. Принимает `sessionId` из ответа
`TS_PrepareStream`.

**Response:**
```json
{
  "ok": true,
  "data": {
    "sessionId": "s1",
    "state": "streaming",
    "downloadedBytes": 524288000,
    "totalBytes": 1234567890,
    "downloadSpeed": 1048576.0,
    "activePeers": 5,
    "totalPeers": 12,
    "activeHTTP": 1,
    "lastRequest": 1753360800,
    "fileName": "My Movie.mkv",
    "hash": "abc123..."
  }
}
```

Состояния сессии (поле `state`):
- `preparing` — создана, ожидает первого HTTP-запроса
- `ready` — готова к streaming
- `streaming` — активный HTTP-клиент читает данные
- `grace` — клиент отключился, ожидание переподключения (60–120 сек)
- `completed` — успешно завершена
- `cancelled` — отменена через `TS_CancelStream`
- `failed` — ошибка

Рекомендуется polling раз в 1 секунду для:
- Обновления прогресса `downloadedBytes / totalBytes`
- Отслеживания `activeHTTP` (1 = плеер подключён, 0 = grace или завершение)
- Детекции terminal-состояний (`completed`, `cancelled`, `failed`)

#### `TS_CancelStream(const char* sessionID) → char*`
Немедленно отменяет streaming-сессию. Принимает `sessionId`.

**Response:**
```json
{ "ok": true, "data": null }
```

Идемпотентен — повторный вызов для уже завершённой сессии не ошибка.

#### `TS_Free(char* value)`
Освобождает память, выделенную Go через `C.CString`. Вызывать ровно один раз для
каждого возвращённого `char*`.

В Swift используйте helper:
```swift
func callTS<T: Codable>(_ fn: () -> UnsafeMutablePointer<CChar>?) throws -> T {
    guard let ptr = fn() else {
        throw TorrCoreError.nullPointer
    }
    defer { TS_Free(ptr) }
    let json = String(cString: ptr)
    guard let data = json.data(using: .utf8) else {
        throw TorrCoreError.invalidUTF8
    }
    let envelope = try JSONDecoder().decode(ResponseEnvelope<T>.self, from: data)
    if !envelope.ok {
        throw TorrCoreError.from(code: envelope.error?.code ?? "unknown",
                                 message: envelope.error?.message ?? "")
    }
    return envelope.data
}
```

---

## 4. HTTP Streaming протокол

Go-ядро поднимает минимальный `net/http` сервер на `127.0.0.1:<random>`.
Маршруты:

### `GET|HEAD /health`
Проверка живости сервера. Используйте для health-check перед открытием плеера.

**Response**: `{"status":"ok","version":"MatriX.137"}`

### `GET|HEAD /stream/{sessionID}/{fileName}?token={token}`
Основной streaming endpoint для плеера.

**Особенности:**
- **Range**: поддерживает `bytes=0-`, `bytes=1000-2000`, `bytes=-65536`
- Возвращает `206 Partial Content` для range, `200 OK` для полного запроса
- `Accept-Ranges: bytes` — всегда
- `Content-Type` — определяется по расширению файла
- 403 при неверном/отсутствующем токене
- Плеер может делать несколько Range-запросов и переподключаться при seek/pause

### `GET /status/{sessionID}?token={token}`
Возвращает JSON-статус сессии (тот же, что `TS_StreamStatus`).

---

## 5. Рекомендуемая архитектура Swift-приложения

### 5.1. TorrCoreClient (actor, thread-safe)

```swift
actor TorrCoreClient {
    private var baseURL: String?
    private var authToken: String?
    private var port: Int?

    var isRunning: Bool { baseURL != nil }

    func start(config: CoreConfig) async throws -> StartResult
    func stop() async
    func addTorrent(link: String, title: String?) async throws -> TorrentSummary
    func torrentStatus(hash: String) async throws -> TorrentFullStatus
    func listTorrents() async throws -> [TorrentSummary]
    func dropTorrent(hash: String) async throws
    func prepareStream(hash: String, fileId: Int) async throws -> StreamInfo
    func streamStatus(sessionId: String) async throws -> StreamStatus
    func cancelStream(sessionId: String) async throws
}
```

### 5.2. Жизненный цикл

```
App Launch
  └─► User открывает magnet/infohash
        └─► TS_Start(config)         // один раз, если не запущен
              └─► TS_AddTorrent(link)
                    └─► Polling TS_TorrentStatus(hash)  // ждём metadata + files
                          └─► User выбирает файл (fileId из files[])
                                └─► TS_PrepareStream(hash, fileId)
                                      └─► Проверить URL через HEAD /health
                                      └─► Открыть URL в VLC/Infuse
                                      └─► Polling TS_StreamStatus(sessionId)
                                            └─► app background → продолжать polling
                                            └─► activeHTTP == 0 → grace (ждём)
                                            └─► state == completed → TS_Stop() (опционально)
        └─► ... другие торренты, другие сессии
  └─► App terminate
        └─► TS_Stop()
```

### 5.3. Получение путей (iOS sandbox)

```swift
let appSupport = FileManager.default.urls(
    for: .applicationSupportDirectory, in: .userDomainMask
).first!.appendingPathComponent("TorrServer")

let caches = FileManager.default.urls(
    for: .cachesDirectory, in: .userDomainMask
).first!.appendingPathComponent("TorrServer")
```

### 5.4. Открытие внешнего плеера

```swift
// streamInfo.url = "http://127.0.0.1:49152/stream/s1/My%20Movie.mkv?token=..."
if let url = URL(string: streamInfo.url) {
    // Вариант 1: Share Sheet
    let activityVC = UIActivityViewController(activityItems: [url], ...)

    // Вариант 2: Открыть в VLC (url scheme: vlc-x-callback://)
    // Вариант 3: Открыть в Infuse (url scheme: infuse://)
    // Вариант 4: Универсальный
    await UIApplication.shared.open(url)
}
```

---

## 6. Коды ошибок

| Код | Значение |
|-----|----------|
| `invalid_argument` | Некорректный аргумент (путь, хост, и т.д.) |
| `invalid_json` | Невозможно распарсить входной JSON |
| `engine_not_running` | Engine не запущен — вызовите `TS_Start` |
| `engine_already_running` | Engine уже работает |
| `engine_start_failed` | Не удалось запустить Engine |
| `database_open_failed` | Ошибка открытия/миграции БД |
| `torrent_parse_failed` | Не удалось распарсить magnet/hash |
| `torrent_add_failed` | Не удалось добавить торрент |
| `torrent_not_found` | Торрент с таким хэшем не найден |
| `torrent_metadata_pending` | Метаданные ещё не получены |
| `torrent_metadata_timeout` | Таймаут получения метаданных |
| `file_not_found` | Файл с таким ID не найден в торренте |
| `stream_not_found` | Сессия с таким ID не найдена |
| `stream_cancelled` | Сессия была отменена |
| `http_start_failed` | Не удалось запустить HTTP-сервер |
| `internal_error` | Внутренняя ошибка/panic |

---

## 7. Фоновая работа на iOS 26+

При использовании `BGContinuedProcessingTask` (доступен с iOS 26):

1. Получите `PrepareStream` → `url`
2. Зарегистрируйте `BGContinuedProcessingTaskRequest` с типом `.fail`
3. Дождитесь активации task handler
4. Проверьте `/health` перед открытием
5. Откройте `url` во внешнем плеере
6. В task handler:
   - Poll `TS_StreamStatus(sessionID)` ~ 1 раз/сек
   - `totalUnitCount = fileLength`, `completedUnitCount = downloadedBytes`
   - При terminal-состоянии: `setTaskCompleted(success:)`
   - В `expirationHandler`: `TS_CancelStream(sessionID)`
   - **Не вызывайте** `TS_Stop()` если есть другие активные сессии

Текущий SDK: iOS 18.5. Для `BGContinuedProcessingTask` потребуется iOS 26 SDK.

---

## 8. Зависимости для линковки (Link Frameworks)

При сборке iOS-приложения в Xcode добавьте:

```
CoreFoundation.framework   // обязательно
Security.framework         // обязательно (HTTPS trackers)
libresolv.tbd              // если линкер жалуется на _res_9_*
```

Только по фактическим link-ошибкам, не добавляйте библиотеки заранее.

---

## 9. Ограничения и принятые решения

### 9.1. Зависимость `github.com/anacrolix/dms/dlna`
Пакет DLNA попадает в граф зависимостей через `server/torr/stream.go`. Это ~300 KB
dead code в архиве, который не вызывается из мобильного ядра. Удаление потребует
вынесения DLNA-хедеров через build tag (план: секция 6.5). **Не влияет на работу.**

### 9.2. Только magnet и infohash
Мобильный link parser поддерживает только magnet-ссылки и infohash (40 hex).
HTTP-ссылки на `.torrent`-файлы и `torrs://`-токены не поддерживаются.
Это упрощает dependency graph (не тащит `net/http` клиент и парсинг `.torrent`).

### 9.3. Только loopback
HTTP-сервер слушает только `127.0.0.1` (или `::1`). Bonjour/Local Network
не используются. VLC/Infuse должны видеть `127.0.0.1` другого sandbox-приложения —
это нужно проверить на физическом iPhone.

### 9.4. Desktop-совместимость
Desktop-сборка (`server/cmd`) продолжает работать без изменений в поведении:
- `InitSets()` сохраняет `os.Exit(1)` для desktop
- `Shutdown()` сохраняет `os.Exit(0)`
- Все новые файлы — отдельный пакет `mobile/`, не импортируемый из desktop

### 9.5. Production-рекомендации
- Для production-сборки используйте `listenPort: 0` (автовыбор)
- Не сохраняйте `authToken` в UserDefaults в открытом виде
- Кэш (torrent pieces) — в `Library/Caches`, база — в `Application Support`
- При `debug: false` логи Go минимальны
- После закрытия плеера и grace-таймаута сессия завершается, Engine можно не
  останавливать

---

## 10. Пересборка

Только на macOS с Xcode и Go 1.24+:

```bash
cd server
bash scripts/build-ios-carchive.sh
bash scripts/verify-ios-archive.sh
```

Артефакты → `build/ios/iphoneos/`, `build/ios/iphonesimulator-arm64/`,
`build/ios/TorrCore.xcframework/`.

Для проверки desktop-сборки:
```bash
cd server && go build ./cmd/
```

---

## 11. Структура проекта (новые файлы)

```
server/
  mobile/
    core/
      config.go       # Парсинг и валидация Config JSON
      engine.go       # Engine (Start/Stop, HTTP на loopback, управление)
      errors.go       # EngineError, 16 кодов ошибок
      link.go         # Парсер magnet/infohash ссылок
      session.go      # StreamSession (ServeHTTP, Status, StatusJSON)
      state.go        # EngineState, SessionState (константы)
      version.go      # TS_Version()
    carchive/
      main.go         # package main, пустая main(), import "C"
      exports.go      # 12 функций с //export
      strings.go      # toCString, fromCString, TS_Free
  scripts/
    build-ios-carchive.sh    # Сборка device + simulator + xcframework
    verify-ios-archive.sh    # Проверка .a, .h, symbols, xcframework
  include/
    module.modulemap         # module TorrCore { header "TorrCore.h"; export * }
build/ios/                   # (в .gitignore) Артефакты сборки
```

---

## 12. План дальнейших шагов

Для полноценного iOS-приложения остаётся:

- [ ] Swift `actor TorrCoreClient` + `Codable`-модели
- [ ] UI согласно визуальному плану (добавление торрентов, выбор файлов, прогресс)
- [ ] Интеграция `BGContinuedProcessingTask` (нужен iOS 26 SDK)
- [ ] Тест cross-app loopback на физическом iPhone (VLC/Infuse видят `127.0.0.1`)
- [ ] Вынос DLNA из графа зависимостей (опционально, секция 6.5 плана)
- [ ] Pin pieces для full-file progress (этап 4 плана)
- [ ] CI на macOS для автосборки XCFramework
