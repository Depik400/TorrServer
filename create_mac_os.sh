#!/bin/bash

set -euo pipefail

cd web
bun i
npm run build-vite
cd ..

go run gen_web.go

cd server
CGO_ENABLED=1 GOARCH=arm64 ~/go/bin/fyne package --name "TorrServer" -icon ../../web/public/icon.png --src=./cmd

mkdir -p ../builds/macOS
mv TorrServer.app ../builds/macOS/TorrServer.app
