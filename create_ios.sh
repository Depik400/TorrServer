#!/bin/bash

set -euo pipefail

cd web
bun i
npm run build-vite
cd ..

go run gen_web.go

cd server/cmd 

~/go/bin/fyne package -os ios -app-id org.yourok.torrserver --name TorrServer -icon ../../web/public/icon.png

cd ..
mkdir -p ../builds/ios
mv cmd/TorrServer.app ../builds/ios/TorrServer.app

rm -rf ../builds/ios/Payload
mkdir -p ../builds/ios/Payload
cp -R ../builds/ios/TorrServer.app ../builds/ios/Payload/TorrServer.app

cd ../builds/ios
rm -f TorrServer.ipa
zip -qry TorrServer.ipa Payload
rm -rf Payload
rm -rf TorrServer.app
