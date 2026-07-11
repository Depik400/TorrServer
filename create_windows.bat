@echo off
setlocal

cd /d "%~dp0web" || exit /b 1
call bun i || exit /b 1
call npm run build-vite || exit /b 1

cd /d "%~dp0" || exit /b 1
go run gen_web.go || exit /b 1

cd /d "%~dp0server" || exit /b 1
set CGO_ENABLED=1
go build -ldflags="-s -w -checklinkname=0 -H=windowsgui" -tags=nosqlite -trimpath -o ..\torr_server-tt.exe .\cmd\ || exit /b 1
