# AGENTS.md — TorrServerLibrary (Go-ядро)

Mobile-friendly форк TorrServer. Даёт локальный torrent-движок для iOS,
скомпилированный в `TorrCore.xcframework` (C-archive).

- **Ветка для всей iOS-работы:** `feat/native-ios-app`. Не переключаться, не мержить.
- **Git:** коммитить здесь, **не пушить**, если пользователь не попросил явно.
- Трейлер коммита: `Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>`.
- **Релизы:** на каждый релиз приложения `X.Y.Z` этот репо тоже получает
  аннотированный тег `vX.Y.Z` (на коммит, зафиксированный хабом), даже если
  Go-ядро в релизе не менялось. Схема — `../AGENTS.md` → «Релизы и теги».

## Раскладка репозиториев (клонированы рядом)

```
torrServerIos/
  TorrServerLibrary/      ← ЭТОТ РЕПО — Go-движок (feat/native-ios-app)
  TorrServerCoreBridge/   — Swift-пакет ITorrentStreamCoreBridge, коммитит xcframework
  TorrServerClient/       — iOS-приложение (ITorrentStream.xcodeproj)
  TorrServerVLCKit/       — локальный SPM-пакет с MobileVLCKit (двоичный)
```

Поток: `iOS App → TorrCoreClient (Swift actor) → C-экспорты → Go-движок`.

## Где что лежит

```
server/
  mobile/
    core/
      engine.go     — жизненный цикл движка: старт/стоп, торренты, стримы,
                      WarmupTorrents, handleDownloadZip, applyMobileSettings,
                      TorrentStatus, PrepareStream, SetFilePriorities,
                      SetTorrentMeta, rate limits / seed policy
      session.go    — StreamSession.ServeHTTP: раздача файла (http.ServeContent,
                      Range/206), Content-Type (в т.ч. субтитры vtt/srt/ass),
                      ?download=1 → Content-Disposition, pinPieces/unpinPieces,
                      graceTimeout = 60s
      search.go     — Engine.Search / TestIndexer / SearchProviders:
                      агрегатор torznab + rutor, прокси-транспорт, дедуп, дедлайн
      config.go     — разбор/валидация CoreConfig
      link.go       — разбор magnet/infohash
    carchive/
      exports.go    — все C-экспорты `//export TS_*`
      strings.go    — toCString / fromCString / TS_Free
  torr/             — обёртка торрента: torrent.go, apihelper.go, dbwrapper.go
  settings/         — bbolt DB: settings.go, torrent.go (TorrentDB), db.go
  rutor/ torznab/ tgbot/  — индексаторы (намеренно сохранены в iOS-сборке)
  web/ api/ ...     — десктопные части, в мобильную сборку не входят
```

## Сборка и проверки

```bash
cd server
go build ./...                                            # обязательный sanity
GOOS=ios GOARCH=arm64 CGO_ENABLED=1 go build ./mobile/... # iOS-замыкание
go vet ./mobile/... ./torr/... ./settings/...
```

После правок Go xcframework пересобирается **из соседнего bridge-репозитория**:

```bash
bash ../TorrServerCoreBridge/Scripts/build-xcframework-local.sh   # ~20 c
```

Скрипт `Scripts/build-xcframework-local.sh` собирает из ЛОКАЛЬНОГО чекаута.
Старый `build-xcframework.sh` клонирует GitHub — **для локальной итерации не
использовать**.

## Известные предсуществующие проблемы (НЕ чинить, не считать регрессом)

- `TestParseConfigInvalidHost` в `mobile/core` падает на чистом дереве.
- `gofmt -d` показывает дрейф в `engine.go` (map-литерал `ChunkMap`) и `session.go`
  (порядок импортов + выравнивание структур). Свои хунки держать gofmt-чистыми,
  чужой дрейф не трогать.
- Вендорный форк торрента — `github.com/tsynik/torrent v1.2.22` (replace для
  `anacrolix/torrent`). У `*torrent.File` **нет** `BytesCompleted()` — считать по
  `f.State()` (сумма завершённых кусков), как в `session.fileBytesCompleted`.

## C ABI

Все `TS_*` в `mobile/carchive/exports.go`:
- вход `*C.char` (JSON), выход `*C.char` (JSON), caller обязан `TS_Free(ptr)`;
- конверт: `{"ok":true,"data":{…}}` либо
  `{"ok":false,"error":{"code":"…","message":"…"}}`;
- новый экспорт = функция `//export TS_Xxx` + `exportJSON`-обёртка + envelope,
  по образцу `TS_Search` / `TS_SetFilePriorities`.

## Конкурентность

- `anacrolix/torrent` поднимает фоновые горутины (DHT, трекеры, watch);
  `recover()` — в той же горутине, где возможна паника.
- `bolt.DB` — файловая блокировка, конкурентные записи сериализуются.
- Список файлов у свежего magnet ещё `nil` до `GotInfo()` — приоритеты и прочее
  применять отложенно.

## Рабочий процесс агента

- Одна задача роадмапа (`plans/product-roadmap/`) = один субагент = один коммит
  в каждом затронутом репозитории.
- Прокси: сетевые запросы движка (в т.ч. поиск) должны уважать
  `CoreConfig.proxyURL` (SOCKS), иначе утечка реального IP на трекеры.
- Персист новых настроек торрента — в `settings.TorrentDB` (JSON целиком),
  миграция не ломающая: отсутствие поля = дефолт.
