# План портирования TorrServer в iOS `c-archive`

## 1. Цель документа

Этот документ предназначен для агента, который будет реализовывать мобильную сборку TorrServer.

Требуемый результат:

- BitTorrent-ядро TorrServer компилируется для iOS как статический Go `c-archive`;
- архив встраивается в Swift-приложение;
- Swift управляет ядром через стабильный C ABI;
- Go поднимает HTTP-сервер только на loopback-интерфейсе;
- пользователь получает HTTP-ссылку и открывает ее во внешнем VLC или Infuse;
- во время воспроизведения Swift удерживает Go-процесс через `BGContinuedProcessingTask` на iOS 26+;
- desktop-сборка TorrServer продолжает работать;
- из iOS-сборки исключаются UI, DLNA, FUSE, Telegram, WebDAV, Swagger, `ffprobe` и остальные серверные функции, не нужные мобильному ядру.

Итоговый артефакт должен быть не `.dylib`, а статический архив `libTorrCore.a` с C-заголовком. Для удобного подключения в Xcode device- и simulator-архивы следует упаковать в `TorrCore.xcframework`.

## 2. Важные ограничения

### 2.1. Использовать `c-archive`, а не `c-shared`

Go поддерживает `-buildmode=c-archive` для `ios/arm64`, но не поддерживает `c-shared` для iOS. Поэтому точкой входа должен быть Go-пакет `main`, содержащий `import "C"`, функции с директивой `//export` и пустую функцию `main()`.

### 2.2. Сборка выполняется только на macOS

Нужны:

- актуальный Xcode;
- iOS 26 SDK, если приложение использует `BGContinuedProcessingTask`;
- Command Line Tools;
- версия Go, совместимая с `go.mod`;
- физический iPhone для проверки фоновой работы и cross-app loopback.

Windows/Linux могут запускать unit-тесты мобильного ядра, но не собирать финальный iOS-архив.

### 2.3. Live Activity не является механизмом выполнения

Обычная ActivityKit Live Activity лишь отображает состояние. Фоновое выполнение обеспечивает `BGContinuedProcessingTask`, доступный с iOS 26. Система сама показывает его прогресс в Live Activity.

Swift должен:

1. создать continued-processing task в foreground;
2. дождаться, что задача принята и запущена;
3. только после этого передать HTTP URL во внешний плеер;
4. при `expirationHandler` вызвать отмену Go-сессии;
5. регулярно обновлять `task.progress` данными из Go;
6. завершить task после окончания streaming-сессии.

### 2.4. Фоновая задача должна иметь конечный и измеримый прогресс

Для iOS предпочтительна модель, в которой выбранный файл постепенно скачивается полностью, а HTTP-reader VLC/Infuse лишь повышает приоритет диапазонов около текущей позиции. Это дает монотонный прогресс `downloaded file bytes / file length`.

Бесконечный daemon или бессрочное сидирование не являются целью. После закрытия плеера и истечения grace timeout streaming-сессия и background task должны завершиться.

## 3. Рекомендуемая структура каталогов

Добавить внутри Go-модуля `server`:

```text
server/
  mobile/
    core/
      engine.go
      config.go
      errors.go
      response.go
      torrent.go
      stream.go
      session.go
      progress.go
      http.go
      http_range.go
      lifecycle.go
      engine_test.go
      http_range_test.go
    carchive/
      main.go
      exports.go
      strings.go
  scripts/
    build-ios-carchive.sh
    verify-ios-archive.sh
  include/
    module.modulemap
```

В корне проекта артефакты складывать в игнорируемый каталог:

```text
build/ios/
  iphoneos/
    libTorrCore.a
    libTorrCore.h
  iphonesimulator-arm64/
    libTorrCore.a
    libTorrCore.h
  TorrCore.xcframework/
```

Не добавлять бинарные артефакты в Git.

## 4. Архитектура мобильного ядра

### 4.1. Не вызывать `server.Start()` и `web.Start()`

Текущий `server.Start()` запускает полный desktop/server runtime и импортирует Telegram, web package и другие неподходящие для iOS компоненты. `web.Start()` импортирует FUSE, WebDAV, DLNA, Rutor, Swagger, страницы и Gin middleware.

Мобильное ядро должно запускать только необходимые части напрямую:

1. проверить и создать каталоги;
2. инициализировать logging без переназначения `os.Stdout`/`os.Stderr`;
3. заполнить мобильные настройки;
4. открыть базу;
5. создать `torr.NewBTS()`;
6. вызвать `Connect()`;
7. передать экземпляр в `torr.InitApiHelper()` либо заменить глобальный helper явной зависимостью;
8. запустить минимальный `net/http.Server` на `127.0.0.1:0`;
9. вернуть фактический порт и session token в Swift.

### 4.2. Новый объект `Engine`

Создать `server/mobile/core.Engine`:

```go
type Engine struct {
    mu sync.Mutex

    state EngineState
    cfg   Config

    bt *torr.BTServer

    listener   net.Listener
    httpServer *http.Server
    baseURL    string
    authToken  string

    ctx    context.Context
    cancel context.CancelFunc

    sessions map[string]*StreamSession
}
```

Состояния:

```go
type EngineState string

const (
    EngineStopped  EngineState = "stopped"
    EngineStarting EngineState = "starting"
    EngineRunning  EngineState = "running"
    EngineStopping EngineState = "stopping"
    EngineFailed   EngineState = "failed"
)
```

Требования:

- в процессе разрешен один экземпляр Engine;
- `Start` и `Stop` идемпотентны;
- повторный `Start` с другими путями во время работы возвращает ошибку;
- все переходы состояния защищены mutex;
- длительные операции не выполняются под общим mutex;
- stop sequence имеет контекст и конечный timeout;
- panic не должна пересечь C ABI.

### 4.3. Конфигурация

Swift передает один JSON:

```json
{
  "applicationSupportPath": "/.../Library/Application Support/TorrServer",
  "cachePath": "/.../Library/Caches/TorrServer",
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

Правила валидации:

- пути должны быть абсолютными и непустыми;
- `applicationSupportPath` и `cachePath` создаются с `0755` или более строгими правами;
- база хранится только в Application Support;
- torrent pieces хранятся только в Caches;
- loopback host жестко ограничить значениями `127.0.0.1` и `::1`; по умолчанию использовать `127.0.0.1`;
- для production всегда использовать порт `0`;
- ограничить cache size разумным диапазоном, например 32 MiB–1 GiB;
- UPnP, DLNA, FUSE, WebDAV и встроенный поиск всегда отключены;
- не использовать рабочую директорию процесса и `os.Getwd()`.

## 5. Изменения существующих пакетов

### 5.1. `settings`: убрать аварийное завершение процесса

Сейчас `settings.InitSets()` вызывает `os.Exit(1)` при ошибках открытия БД. Библиотека не имеет права завершать процесс Swift-приложения.

Рефакторинг:

```go
func InitSetsE(readOnly, searchWA bool) error {
    // текущая логика, но все ошибки возвращаются
}

func InitSets(readOnly, searchWA bool) {
    if err := InitSetsE(readOnly, searchWA); err != nil {
        log.TLogln(err)
        os.Exit(1) // оставить только для desktop compatibility
    }
}
```

Мобильный код вызывает только `InitSetsE`.

Также вернуть ошибки из функций открытия/миграции БД там, где сейчас они только логируются. Минимум ошибки создания/open database должны доходить до C ABI.

### 5.2. Исправить lifetime singleton-баз

Текущие глобальные переменные:

- `globalBboltDB`;
- `globalJsonDB`;
- `tdb`;

мешают корректному `Start -> Stop -> Start` в одном iOS-процессе.

После `CloseDB()` требуется:

- закрыть router/read cache;
- присвоить `tdb = nil`;
- присвоить `globalBboltDB = nil`;
- присвоить `globalJsonDB = nil`;
- обнулить закрытый `*bolt.DB`;
- не возвращать закрытый singleton при следующем `NewTDB()`.

В `NewJsonDB()` есть shadowing:

```go
globalJsonDB := &JsonDB{...}
```

Здесь создается локальная переменная вместо записи в package global. Нужно заменить на:

```go
globalJsonDB = &JsonDB{...}
return globalJsonDB
```

Добавить mutex вокруг глобального DB lifecycle, потому что Swift может вызвать stop одновременно с опросом статуса.

### 5.3. `settings.Args`

До `BTServer.Connect()` мобильное ядро должно создать `settings.ExecArgs`, иначе `BTServer.configureProxy()` обращается к `settings.Args.ProxyURL` и может получить nil pointer.

Для mobile установить как минимум:

```go
settings.Args = &settings.ExecArgs{
    IP:       "127.0.0.1",
    Port:     "0",
    Path:     applicationSupportPath,
    ProxyURL: "",
    ProxyMode: "",
}
```

Дополнительно установить:

- `settings.Path`;
- `settings.IP`;
- `settings.Port` после `net.Listen`;
- `settings.MaxSize` при необходимости;
- `settings.TorAddr` пустым или `:0`;
- `settings.PubIPv4/PubIPv6` пустыми.

### 5.4. Мобильные значения `BTSets`

После загрузки настроек применить принудительный mobile override:

```go
BTsets.EnableDLNA = false
BTsets.EnableRutorSearch = false
BTsets.EnableTorznabSearch = false
BTsets.DisableUPNP = true
BTsets.UseDisk = cfg.DiskCacheEnabled
BTsets.TorrentsSavePath = cfg.CachePath
BTsets.RemoveCacheOnDrop = false
BTsets.CacheSize = cfg.CacheSizeBytes
BTsets.ConnectionsLimit = cfg.ConnectionsLimit
BTsets.TorrentDisconnectTimeout = cfg.TorrentDisconnectTimeoutSeconds
BTsets.DisableUpload = cfg.DisableUpload
```

Не использовать `/` и пустой путь как cache path.

Решить явно, какие настройки сохраняются между запусками. Рекомендуется сохранять пользовательские torrent/settings данные, но принудительные platform constraints повторно применять после каждого чтения БД.

### 5.5. `torr.BTServer`: убрать скрытые глобальные зависимости

Краткосрочно допустимо оставить `torr.InitApiHelper(bt)`, так как мобильный процесс использует один Engine.

Предпочтительный рефакторинг:

- методы add/get/drop/list сделать методами сервиса, содержащего `*BTServer`;
- не использовать package-global `bts` в новом mobile/core;
- desktop API может временно использовать singleton wrapper.

Минимально необходимо:

- добавить проверку nil в `GetTorrent`, `AddTorrent`, `ListTorrent`, `DropTorrent`;
- не допускать panic при вызове после Engine Stop;
- дать `Disconnect()` корректно закрыть все torrents/readers/storage;
- не вызывать `os.Exit` в `Shutdown()` из mobile path.

### 5.6. `BTServer.configure`: mobile overrides

Проверить на устройстве:

- `publicip.Get4/Get6`;
- DHT UDP sockets;
- TCP peer connections;
- uTP;
- tracker HTTP/HTTPS;
- IPv6-only сеть.

Для первого proof of concept:

- `DisableUPNP = true`;
- listen port `0`;
- не публиковать ручной public IP;
- upload можно отключить;
- DHT и PEX оставить включенными;
- при проблемах с uTP временно отключить только uTP, не TCP;
- ошибки определения public IP не считать fatal.

### 5.7. Исключить `ffprobe`

`server/ffprobe` использует `os/exec` и не должен попадать в dependency graph iOS-архива.

Сейчас `server/torr/preload.go` напрямую импортирует `server/ffprobe`. Нужно разделить реализацию:

```text
preload.go             // общая preload-логика, без ffprobe
preload_probe.go       // //go:build !ios
preload_probe_ios.go   // //go:build ios, no-op
```

Либо ввести интерфейс/функцию `probeMedia()` с desktop и iOS реализациями.

На iOS метаданные, продолжительность и кодеки определяет внешний VLC/Infuse либо Swift AVFoundation; Go-ядру эти данные для streaming не нужны.

### 5.8. FUSE build tags

`server/torrfs/fuse/fuse.go` имеет `//go:build !windows`, следовательно считается подходящим для iOS. Даже если mobile/core его не импортирует, это нужно исправить для корректности проекта:

```go
//go:build !windows && !ios
```

Добавить `fuse_ios.go`:

```go
//go:build ios

package fuse

func FuseAutoMount() {}
func FuseCleanup() {}
```

Если mobile/core полностью исключает `server/web`, заглушка не должна попадать в архив, но она защитит будущие случайные импорты.

### 5.9. Остальные desktop-компоненты

Mobile dependency graph не должен содержать:

- `server/cmd`;
- `server/web` целиком;
- `server/tgbot`;
- `server/dlna`;
- `server/torrfs/fuse`;
- `server/torrfs/webdav`;
- `server/rutor`;
- `server/torznab`;
- `server/docs`;
- `server/ffprobe`;
- `github.com/pkg/browser`;
- `github.com/hanwen/go-fuse`;
- Gin/Swagger, если minimal HTTP router построен на `net/http`.

После реализации проверить через:

```bash
go list -deps ./mobile/carchive
```

и отдельным скриптом завершать сборку ошибкой, если список содержит запрещенные package prefixes.

## 6. Минимальный HTTP-сервер

### 6.1. Не использовать полный Gin router

Для mobile достаточно стандартного `http.ServeMux`. Это резко уменьшит dependency graph и поверхность ошибок.

Маршруты:

```text
GET|HEAD /health
GET|HEAD /stream/{sessionID}/{safeFileName}?token=...
GET      /status/{sessionID}?token=...
```

Управление torrent-объектами происходит через C ABI, а не через HTTP API.

### 6.2. Listener

Использовать:

```go
listener, err := net.Listen("tcp4", "127.0.0.1:0")
```

Фактический адрес получать через `listener.Addr()`.

HTTP server:

```go
srv := &http.Server{
    Handler:           mux,
    ReadHeaderTimeout: 10 * time.Second,
    IdleTimeout:       2 * time.Minute,
}
```

Не задавать короткий `WriteTimeout`: долгий media stream иначе оборвется.

При Stop использовать `Shutdown(ctx)` с timeout, затем `Close()`.

### 6.3. Авторизация ссылки

При каждом Engine Start генерировать криптографически случайный token минимум 128 бит:

```go
crypto/rand.Read(...)
```

Токен включать в URL или path. Сервер должен отклонять запросы без token через `403`.

Дополнительно:

- слушать только loopback;
- не писать полный token в лог;
- URL-encode имя файла;
- session ID генерировать отдельно от token;
- после Stop session URL должен стать недействительным.

### 6.4. Range и HEAD

Существующий `Torrent.Stream()` использует `http.ServeContent`, это можно переиспользовать после удаления DLNA-зависимости либо вынести в mobile handler.

Обязательные ответы:

- `HEAD` без чтения torrent pieces;
- `200 OK` для полного запроса;
- `206 Partial Content` для корректного Range;
- `416 Range Not Satisfiable` для неверного диапазона;
- `Accept-Ranges: bytes` всегда;
- `Content-Length`;
- `Content-Range` для 206;
- MIME по расширению;
- стабильный `ETag` на основе infohash + file path;
- корректная обработка отмены `Request.Context()`.

Проверить диапазоны:

- `bytes=0-`;
- `bytes=1000-2000`;
- `bytes=-65536`;
- запрос около конца файла;
- repeated seek;
- несколько последовательных подключений;
- VLC сначала делает `HEAD`, затем несколько Range GET.

`http.ServeContent` умеет большую часть этого. Не реализовывать Range parser вручную без необходимости.

### 6.5. Удалить DLNA import из общей stream-логики

`server/torr/stream.go` импортирует `github.com/anacrolix/dms/dlna` только ради DLNA headers. Для iOS это лишняя зависимость.

Варианты:

1. mobile handler повторяет только необходимую часть stream-кода;
2. общую функцию работы с reader вынести из `Torrent.Stream`, а DLNA headers оставить в desktop adapter;
3. platform-specific helper добавляет DLNA headers только на `!ios`.

Рекомендуется вариант 2:

```go
func (t *Torrent) OpenFileReader(fileID int) (reader io.ReadSeekCloser, meta FileMeta, err error)
```

Desktop `Stream()` и mobile handler используют один reader, но разные HTTP headers.

## 7. Streaming session

### 7.1. Модель

```go
type StreamSession struct {
    ID       string
    Hash     string
    FileID   int
    FileName string
    Length   int64

    CreatedAt    time.Time
    LastRequest  atomic.Int64
    ActiveHTTP   atomic.Int32
    BytesServed  atomic.Int64

    DownloadedBytes atomic.Int64
    TotalBytes      int64

    state  SessionState
    ctx    context.Context
    cancel context.CancelFunc
    done   chan struct{}
}
```

Состояния:

```text
preparing -> ready -> streaming -> grace -> completed
                              \-> cancelled
                              \-> failed
```

### 7.2. Session URL

`PrepareStream(hash, fileID)`:

1. находит torrent;
2. проверяет metadata;
3. проверяет file ID;
4. создает session;
5. запускает background full-file downloader или piece-priority worker;
6. возвращает JSON:

```json
{
  "sessionId": "...",
  "url": "http://127.0.0.1:49152/stream/...?...",
  "fileName": "movie.mkv",
  "fileLength": 1234567890,
  "mimeType": "video/x-matroska"
}
```

Swift не должен самостоятельно собирать внутренний URL.

### 7.3. Завершение

Когда последний HTTP client отключился:

- перейти в grace state;
- ждать 60–120 секунд, потому что VLC/Infuse могут переподключиться после seek/pause;
- новый запрос отменяет grace timer;
- после timeout закрыть reader/download worker;
- снять piece priorities;
- закрыть session.done;
- Swift увидит terminal state через polling и вызовет `setTaskCompleted`.

Явный `TS_CancelStream` завершает немедленно.

### 7.4. Полная загрузка файла

Текущий `Preload()` загружает ограниченные start/end ranges и не является полной загрузкой. Для continued-processing progress нужен новый worker.

Возможная реализация:

- определить первый и последний piece выбранного файла;
- выставить фоновым pieces нормальный/низкий приоритет;
- pieces около активного HTTP reader имеют `Now/Next/Readahead/High`;
- считать completed bytes только в пересечении piece с выбранным файлом;
- первый/последний piece могут включать байты соседнего файла, поэтому progress считать с учетом пересечения диапазонов;
- не считать cache `Filled`, потому что он включает другие файлы и может уменьшаться при eviction;
- progress должен быть монотонным для одной session;
- если disk cache ограничен меньше размера файла, full-file progress не сможет дойти до 100%; для режима полной загрузки либо не evict pieces выбранного файла, либо явно использовать отдельный persistent session cache.

Рекомендуемое решение для iOS:

- во время активной session pin все pieces выбранного файла;
- хранить их в `Library/Caches/TorrServer/<hash>/`;
- не удалять pinned pieces eviction-механизмом;
- unpin после session completion/cancel;
- затем обычный cache cleanup может их удалить согласно лимиту.

Нужно добавить pin/reference count в `torrstor.Cache/Piece`, иначе текущий `cleanPieces()` может удалить уже загруженные части.

### 7.5. Отмена

Все длительные операции должны слушать context:

- metadata wait;
- full-file download worker;
- HTTP reader;
- grace timer;
- engine stop.

Текущий `WaitInfo()` имеет жесткий timer и не принимает context. Добавить:

```go
func (t *Torrent) WaitInfoContext(ctx context.Context) error
```

Desktop `WaitInfo()` может вызывать его с существующим timeout.

## 8. C ABI

### 8.1. Общий принцип

Не экспортировать Go structs, slices, maps, interfaces, channels или Go pointers. Все входы и выходы — C primitives и UTF-8 JSON.

Преимущества JSON ABI:

- Swift models декодируются `Codable`;
- C ABI остается стабильным при изменении Go structs;
- ошибки имеют единый формат;
- меньше риска нарушить правила cgo pointers.

### 8.2. Предлагаемые exports

```c
char* TS_Version(void);

char* TS_Start(const char* config_json);
char* TS_Stop(void);
char* TS_EngineStatus(void);

char* TS_AddTorrent(const char* request_json);
char* TS_TorrentStatus(const char* hash);
char* TS_ListTorrents(void);
char* TS_DropTorrent(const char* hash);

char* TS_PrepareStream(const char* request_json);
char* TS_StreamStatus(const char* session_id);
char* TS_CancelStream(const char* session_id);

void TS_Free(char* value);
```

Request examples:

```json
{"link":"magnet:?xt=...","title":"optional"}
```

```json
{"hash":"...","fileId":1,"completeDownload":true}
```

### 8.3. Response envelope

Успех:

```json
{
  "ok": true,
  "data": {}
}
```

Ошибка:

```json
{
  "ok": false,
  "error": {
    "code": "torrent_metadata_timeout",
    "message": "torrent metadata was not received before timeout"
  }
}
```

Стабильные error codes задокументировать и не строить Swift-логику на тексте `message`.

Минимальные codes:

```text
invalid_argument
invalid_json
engine_not_running
engine_already_running
engine_start_failed
database_open_failed
torrent_parse_failed
torrent_add_failed
torrent_not_found
torrent_metadata_pending
torrent_metadata_timeout
file_not_found
stream_not_found
stream_cancelled
http_start_failed
internal_error
```

### 8.4. Владение памятью

Каждая функция, возвращающая `char*`, создает строку через `C.CString`. Swift обязан вызвать `TS_Free` ровно один раз.

Go:

```go
//export TS_Free
func TS_Free(value *C.char) {
    if value != nil {
        C.free(unsafe.Pointer(value))
    }
}
```

Правила:

- Go не сохраняет входящие C pointers после возврата функции;
- входящий `char*` сразу копируется через `C.GoString`;
- C/Swift не сохраняет Go pointers;
- nil input валидируется;
- результат `C.CString` никогда не освобождается автоматически;
- в Swift создать один helper, который всегда делает `defer { TS_Free(ptr) }`.

### 8.5. Panic barrier

Каждая экспортированная функция должна проходить через общий wrapper:

```go
func exportJSON(fn func() (any, error)) (result *C.char) {
    defer func() {
        if recovered := recover(); recovered != nil {
            result = toCString(internalErrorResponse(recovered))
        }
    }()
    return toCString(runAndMarshal(fn))
}
```

Panic не должен пересечь C boundary или завершить Swift process.

### 8.6. Потоки

Swift может вызывать C ABI с разных dispatch queues. Все exports должны быть thread-safe.

- не требовать вызова на main thread;
- длительные операции не блокируют Swift main thread;
- `TS_AddTorrent` должен быстро добавить spec и вернуть hash/state;
- получение metadata выполняется асинхронно, Swift polls `TS_TorrentStatus`;
- `TS_Stop` может быть синхронным, но Swift вызывает его на background queue;
- не делать callbacks из произвольных Go goroutines в Swift на первом этапе;
- polling 1 раз/сек достаточно для UI и background progress.

## 9. Реализация `package main` для c-archive

`server/mobile/carchive/main.go`:

```go
package main

/*
#include <stdlib.h>
*/
import "C"

func main() {}
```

`exports.go` содержит функции `//export` непосредственно перед Go declaration:

```go
//export TS_Start
func TS_Start(configJSON *C.char) *C.char {
    // copy input, call core, return allocated JSON
}
```

Особенности cgo:

- комментарий `//export` должен стоять непосредственно над функцией;
- пакет обязательно `main`;
- должен существовать `func main()`;
- в C preamble файла с `//export` оставлять только declarations/includes, не размещать определения C-функций, которые cgo может продублировать;
- generated header создается рядом с `.a` автоматически;
- не редактировать generated header вручную.

## 10. iOS build tags

Go устанавливает build tag `ios`; также iOS наследует некоторые Darwin constraints. Явно разделить platform code.

Примеры:

```go
//go:build ios
```

```go
//go:build !ios
```

Для mobile-only кода не обязательно ставить `ios` на core files: это позволит тестировать core на macOS/Linux/Windows. `ios` tag нужен для platform adapters и исключения несовместимых функций.

Добавить compile-only проверку:

```bash
GOOS=ios GOARCH=arm64 CGO_ENABLED=1 go list ./mobile/carchive
```

Полная сборка требует Xcode SDK и выполняется на macOS.

## 11. Скрипт сборки `c-archive`

### 11.1. Device arm64

Скрипт выполняется из `server/` и должен:

1. проверить `uname -s == Darwin`;
2. проверить `xcode-select`, `xcrun`, `go`, `xcodebuild`;
3. получить SDK path;
4. создать временный clang wrapper;
5. собрать archive;
6. проверить наличие `.a` и `.h`.

Концептуальная команда:

```bash
SDK_PATH="$(xcrun --sdk iphoneos --show-sdk-path)"

CGO_ENABLED=1 \
GOOS=ios \
GOARCH=arm64 \
CC="$DEVICE_CLANG_WRAPPER" \
CGO_CFLAGS="-isysroot $SDK_PATH" \
CGO_LDFLAGS="-isysroot $SDK_PATH -framework CoreFoundation -framework Security" \
go build \
  -trimpath \
  -buildmode=c-archive \
  -ldflags="-s -w" \
  -o ../build/ios/iphoneos/libTorrCore.a \
  ./mobile/carchive
```

Wrapper должен вызывать примерно:

```bash
xcrun --sdk iphoneos clang \
  -target arm64-apple-ios26.0 \
  "$@"
```

Не копировать пример вслепую: проверить flags с установленным Xcode/Go. За эталон можно взять `$GOROOT/misc/ios/clangwrap.sh` и текущую реализацию `golang.org/x/mobile`.

### 11.2. Simulator arm64

Использовать `iphonesimulator` SDK и отдельный wrapper:

```bash
xcrun --sdk iphonesimulator clang \
  -target arm64-apple-ios26.0-simulator \
  "$@"
```

Окружение:

```bash
CGO_ENABLED=1 GOOS=ios GOARCH=arm64
```

Выход:

```text
build/ios/iphonesimulator-arm64/libTorrCore.a
build/ios/iphonesimulator-arm64/libTorrCore.h
```

При необходимости поддержки Intel Simulator собрать `GOARCH=amd64` с target `x86_64-apple-ios...-simulator`, затем объединить только simulator slices через `lipo`. Не объединять device и simulator в один fat archive.

### 11.3. Заголовки и module map

Для каждой platform library создать headers directory:

```text
Headers/
  TorrCore.h
  module.modulemap
```

Generated `libTorrCore.h` либо переименовать в `TorrCore.h`, либо module map должен ссылаться на точное имя.

Пример:

```modulemap
module TorrCore {
    header "TorrCore.h"
    export *
}
```

Сравнить device и simulator generated headers. Exported signatures должны совпадать.

### 11.4. XCFramework

```bash
xcodebuild -create-xcframework \
  -library build/ios/iphoneos/libTorrCore.a \
  -headers build/ios/iphoneos/Headers \
  -library build/ios/iphonesimulator-arm64/libTorrCore.a \
  -headers build/ios/iphonesimulator-arm64/Headers \
  -output build/ios/TorrCore.xcframework
```

Swift target подключает XCFramework как `Do Not Embed`, потому что внутри статическая библиотека.

### 11.5. Link dependencies

После первой сборки проверить undefined symbols через `nm -u`. Возможные системные зависимости Go/cgo/network stack:

- `CoreFoundation.framework`;
- `Security.framework`;
- `libresolv`;
- `libSystem`/pthread подключаются платформой автоматически.

Добавлять framework/library только по фактическим link errors. Не использовать `-ObjC` без необходимости.

Проверить приложение на актуальном Xcode linker. Старые проблемы `c-archive` с Xcode 15/ld-prime исправлялись в новых Go, поэтому зафиксировать актуальную tested Go version в README и CI.

## 12. Swift integration contract

### 12.1. Wrapper

Swift не должен вызывать C exports по всему приложению. Создать один actor/class:

```swift
actor TorrCoreClient {
    func start(config: CoreConfig) async throws -> StartResult
    func add(link: String) async throws -> TorrentSummary
    func status(hash: String) async throws -> TorrentStatus
    func prepareStream(hash: String, fileID: Int) async throws -> StreamInfo
    func streamStatus(id: String) async throws -> StreamStatus
    func cancelStream(id: String) async throws
    func stop() async
}
```

Он отвечает за:

- JSON encode/decode;
- C string conversion;
- обязательный `TS_Free`;
- сериализацию lifecycle calls;
- перевод error codes в Swift Error;
- выполнение blocking calls вне MainActor.

### 12.2. BGContinuedProcessingTask

Минимальная версия iOS — 26.

До открытия VLC/Infuse:

1. `PrepareStream`;
2. зарегистрировать/submit `BGContinuedProcessingTaskRequest` с `.fail`;
3. убедиться, что task handler начал выполнение;
4. выполнить health check stream URL;
5. открыть внешний player URL.

В handler:

- polling `TS_StreamStatus` примерно раз в секунду;
- `totalUnitCount = fileLength`;
- `completedUnitCount = downloadedBytes`, значение не должно уменьшаться;
- обновлять title/subtitle редко;
- при terminal state вызвать `setTaskCompleted(success:)`;
- в `expirationHandler` вызвать `TS_CancelStream`;
- не вызывать `TS_Stop`, если существуют другие активные sessions.

Если task request отклонен, не открывать внешний плеер и показать пользователю ошибку.

### 12.3. Открытие VLC/Infuse

Go возвращает обычный `http://127.0.0.1:port/...` URL. Swift может:

- показать share sheet;
- скопировать URL;
- открыть зарегистрированную схему VLC/Infuse;
- дать пользователю выбрать приложение.

Конкретные URL schemes VLC/Infuse держать в Swift adapter, не в Go.

## 13. Сетевые и iOS-настройки

В Xcode проверить:

- ATS/local networking declaration для loopback HTTP;
- Background Tasks capability;
- `BGTaskSchedulerPermittedIdentifiers`;
- идентификатор continued-processing task;
- Live Activities support, если требуется отдельная custom ActivityKit UI;
- минимальный deployment target iOS 26.

Не запрашивать Local Network permission без фактической необходимости. Loopback и LAN discovery — разные сценарии. Если сервер слушает только `127.0.0.1`, Bonjour не нужен.

## 14. Логи и диагностика

Desktop `log.Init` может переназначать `os.Stdout` и `os.Stderr`; для library это нежелательно.

Добавить mobile logger:

- обычный Go `log` в stderr для debug;
- опциональный ring buffer последних N строк;
- C export `TS_GetLogs` только для диагностики;
- не логировать magnet query целиком, auth token и полные stream URL;
- не писать безлимитный log file;
- при debug file logging хранить его в `Library/Caches/Logs` и ограничивать размер.

Обязательные метрики session status:

- engine/session state;
- metadata state;
- active HTTP connections;
- last request timestamp;
- bytes served;
- file bytes completed;
- file length;
- download/upload speed;
- active/total peers;
- terminal error code.

## 15. Проверка dependency compatibility

Особое внимание fork dependency:

```text
github.com/anacrolix/torrent => github.com/tsynik/torrent v1.2.22
```

Нужно проверить на iOS device:

- наличие platform-specific файлов в fork;
- `github.com/anacrolix/utp`;
- `github.com/anacrolix/dht/v2`;
- `github.com/wlynxg/anet`;
- `golang.org/x/sys/unix`;
- mmap/file storage dependencies;
- DNS resolver;
- root certificate loading для HTTPS trackers.

Первый compile spike должен импортировать только `torr.NewBTS`, применить mobile settings и выполнить `Connect/Disconnect`. Если он не компилируется, исправлять platform constraints в зависимости или заменить проблемную необязательную функцию; не тащить весь web server ради обхода.

## 16. Тесты

### 16.1. Go unit tests

- config validation;
- response envelope;
- error code mapping;
- engine state transitions;
- repeated Start/Stop;
- concurrent status + stop;
- token validation;
- URL escaping;
- HEAD;
- все основные Range forms;
- grace reconnect;
- cancel idempotency;
- progress не уменьшается;
- piece/file intersection math;
- panic barrier.

Запускать с race detector на macOS/Linux для core package, где возможно:

```bash
go test -race ./mobile/core/...
```

### 16.2. C ABI smoke test на macOS

До iOS сделать host `c-archive` smoke test:

1. собрать archive для macOS;
2. скомпилировать маленький C executable;
3. вызвать `TS_Version`, invalid `TS_Start`, `TS_Free`;
4. прогнать под Address Sanitizer, если совместимо;
5. повторить тысячи allocations/free для поиска утечек ABI.

### 16.3. iOS Simulator

Simulator проверяет:

- линковку XCFramework;
- C ABI;
- Start/Stop;
- DB paths;
- loopback HTTP;
- Swift JSON wrapper.

Simulator не является доказательством работоспособности BitTorrent networking или background execution.

### 16.4. Физическое устройство

Обязательные тесты:

1. magnet получает metadata по Wi-Fi;
2. HTTP URL открывается из Safari;
3. URL открывается во внешнем VLC;
4. URL открывается в Infuse;
5. external app видит loopback server другого sandboxed app;
6. HEAD/Range/seek работают;
7. переход TorrServer в background не обрывает task;
8. блокировка экрана;
9. Wi-Fi -> cellular и обратно;
10. временная потеря сети;
11. VLC pause больше минуты и reconnect;
12. пользователь отменяет system Live Activity/task;
13. iOS вызывает expiration handler;
14. memory warning;
15. disk full/cache eviction;
16. force quit ожидаемо прекращает streaming;
17. повторный запуск приложения восстанавливает БД без reuse закрытых singleton объектов;
18. несколько последовательных streaming sessions;
19. два одновременных внешних HTTP clients, если это поддерживается продуктом;
20. IPv6-only external tracker/network scenario.

## 17. Этапы реализации

### Этап 0. Compile spike

- создать минимальный `mobile/carchive`;
- экспортировать `TS_Version` и `TS_Free`;
- собрать device `.a/.h`;
- подключить к пустому Swift app;
- вызвать `TS_Version` на физическом устройстве.

Критерий: Go runtime запускается внутри iOS процесса без crash.

### Этап 1. Torrent-core spike

- mobile settings без БД либо с временной БД;
- `NewBTS -> Connect -> Disconnect`;
- проверить dependency graph;
- исключить `ffprobe`;
- получить metadata тестового magnet.

Критерий: metadata и список файлов доступны через C ABI.

### Этап 2. Loopback streaming

- minimal `net/http` server;
- stream session;
- HEAD/Range;
- token;
- VLC/Infuse foreground test.

Критерий: внешний player воспроизводит и перематывает файл, пока TorrServer открыт.

### Этап 3. Background execution

- `BGContinuedProcessingTask`;
- session polling;
- progress;
- expiration/cancel;
- grace reconnect.

Критерий: VLC/Infuse продолжает получать bytes после перехода TorrServer в background.

### Этап 4. Disk cache и full-file progress

- pinned pieces;
- file-specific progress;
- recovery/cleanup;
- disk pressure behavior.

Критерий: progress монотонен, seek не ломает загрузку, pinned pieces не удаляются во время session.

### Этап 5. Hardening

- повторный lifecycle;
- race fixes;
- memory limits;
- network transitions;
- structured logs;
- build reproducibility;
- CI на macOS.

## 18. Definition of Done

Работа считается выполненной, когда:

- `go build -buildmode=c-archive` успешно создает device и simulator archives;
- создан корректный `TorrCore.xcframework`;
- generated C header содержит только задокументированные `TS_*` exports;
- Swift может Start/Stop ядро без crash;
- Start/Stop/Start работает в одном процессе;
- ошибки не вызывают `os.Exit` или panic через C ABI;
- mobile dependency graph не содержит FUSE, DLNA, Telegram, Gin UI, Swagger или `ffprobe`;
- magnet metadata и file list работают на физическом iPhone;
- VLC и Infuse могут читать localhost URL другого приложения;
- HEAD и Range seek работают;
- stream URL защищен случайным token и listener не доступен извне устройства;
- `BGContinuedProcessingTask` удерживает streaming на iOS 26 в пределах системных ограничений;
- expiration/cancel корректно останавливают Go workers;
- background progress является монотонным и отражает выбранный файл;
- disk cache расположен в `Library/Caches`, база — в Application Support;
- нет известных data races в core tests;
- README содержит точные tested версии Go, Xcode и iOS.

## 19. Известные риски, которые нельзя считать решенными до device test

1. Cross-app доступ VLC/Infuse к `127.0.0.1` server должен быть подтвержден на реальном iPhone.
2. `BGContinuedProcessingTask` может быть завершен системой при resource pressure; код обязан корректно переживать expiration.
3. Torrent fork может иметь неочевидные Darwin/iOS assumptions.
4. Текущий cache eviction не подходит для монотонной полной загрузки без pinning.
5. Текущие package globals рассчитаны на один desktop process lifetime и требуют аккуратного reset.
6. Внешний player может создавать несколько Range connections и часто переподключаться.
7. Full-file pinning может занять много места; перед запуском нужна проверка available capacity и понятная ошибка.
8. Force quit полностью прекращает Go runtime и HTTP server — это ожидаемое поведение iOS.

## 20. Полезные источники для исполнителя

- Go supported build modes: <https://go.dev/src/internal/platform/supported.go>
- Go iOS notes и clang wrapper: <https://go.dev/misc/ios/README>
- Реализация Apple archive/XCFramework в gomobile: <https://go.googlesource.com/mobile/>
- `BGContinuedProcessingTask`: <https://developer.apple.com/documentation/backgroundtasks/bgcontinuedprocessingtask>
- WWDC25 Finish tasks in the background: <https://developer.apple.com/videos/play/wwdc2025/227/>

Главный принцип реализации: не пытаться превратить весь desktop TorrServer в iOS-библиотеку. Нужно переиспользовать torrent/storage ядро, создать отдельный управляемый mobile Engine и окружить его минимальным C ABI и loopback HTTP adapter.
