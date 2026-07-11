#!/bin/bash

set -euo pipefail

cd web
bun i
npm run build-vite
cd ..

go run gen_web.go

cd server
CGO_ENABLED=1 go build -ldflags='-s -w -checklinkname=0' -tags=nosqlite -trimpath -o ../torr_server-tt ./cmd/
