#!/bin/bash

set -euo pipefail

cd web
bun i
npm run build-vite
cd ..

go run gen_web.go

cd server
fyne package -os ios -app-id org.yourok.torrserver -icon ../web/public/icon.png
