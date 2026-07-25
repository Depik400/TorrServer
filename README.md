# TorrServerLibrary

Библиотека TorrServer — fork оригинального [TorrServer](https://github.com/YouROK/TorrServer) с поддержкой iOS через C ABI (c-archive). Включает полное BitTorrent-ядро, скомпилированное как статическая библиотека для iOS (arm64 device + simulator).

## Для чего

Позволяет встроить торрент-ядро прямо в iOS-приложение без внешнего сервера. Приложение может:
- Добавлять magnet-ссылки, infohash, URL .torrent-файлов
- Просматривать файлы внутри торрента
- Стримить видео в VLC/Infuse через локальный HTTP-сервер
- Показывать карту чанков, скорость, пиры
- Управлять RAM/disk кэшем

## Сборка

### Требования
- macOS с Xcode 26+ (Command Line Tools)
- Go 1.24+

### iOS c-archive и XCFramework

```bash
cd server
./scripts/build-ios-carchive.sh
```

Результат: `build/ios/TorrCore.xcframework/` (статическая библиотека + заголовки для device и simulator).

### Desktop сборка (macOS/Linux)

```bash
cd server
go build -o torrserver ./cmd/torrserver
```

### Тесты

```bash
cd server
go test -race ./...
```

## C ABI

После сборки xcframework, заголовочный файл содержит 12+ экспортов:

| Экспорт | Описание |
|---------|----------|
| `TS_Start` | Запуск ядра с конфигурацией |
| `TS_Stop` | Остановка ядра |
| `TS_EngineStatus` | Состояние ядра, порт, версия |
| `TS_AddTorrent` | Добавить magnet/URL/hash |
| `TS_ListTorrents` | Список торрентов |
| `TS_TorrentStatus` | Статус торрента + список файлов |
| `TS_DropTorrent` | Удалить торрент |
| `TS_PrepareStream` | Подготовить стрим-сессию для файла |
| `TS_StreamStatus` | Прогресс, скорость, пиры сессии |
| `TS_CancelStream` | Отменить стрим-сессию |
| `TS_ChunkMap` | Карта чанков (ranges + states) |
| `TS_Settings` | Текущие настройки кэша/сети |
| `TS_UpdateSettings` | Изменить настройки |
| `TS_Version` | Версия TorrServer |
| `TS_Free` | Освободить C-строку |

Все экспорты возвращают JSON-envelope: `{ "ok": true, "data": {...} }` или `{ "ok": false, "error": { "code": "...", "message": "..." } }`.

## Лицензия

GPL-3.0 (как оригинальный TorrServer).

---

Разработано с использованием DeepSeek V4 Pro.
