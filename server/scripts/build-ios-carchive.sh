#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_DIR/../build/ios"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

log()  { echo -e "${GREEN}[build-ios]${NC} $*"; }
err()  { echo -e "${RED}[build-ios]${NC} $*" >&2; exit 1; }

if [[ "$(uname -s)" != "Darwin" ]]; then
    err "This script must run on macOS"
fi

for tool in xcode-select xcrun go; do
    command -v "$tool" >/dev/null 2>&1 || err "$tool not found"
done

IOS_SDK="$(xcrun --sdk iphoneos --show-sdk-path 2>/dev/null)" || err "iPhoneOS SDK not found"
SIM_SDK="$(xcrun --sdk iphonesimulator --show-sdk-path 2>/dev/null)" || err "iPhoneSimulator SDK not found"

IOS_VERSION="18.5"
log "iPhoneOS SDK:  $IOS_SDK"
log "Simulator SDK: $SIM_SDK"
log "Target iOS:    $IOS_VERSION"

DEVICE_OUT="$BUILD_DIR/iphoneos"
SIM_OUT="$BUILD_DIR/iphonesimulator-arm64"

mkdir -p "$DEVICE_OUT" "$SIM_OUT"

make_clang_wrapper() {
    local out="$1"
    local sdk="$2"
    local target="$3"
    cat > "$out" <<EOF
#!/bin/bash
exec xcrun --sdk "$sdk" clang -target "$target" "\$@"
EOF
    chmod +x "$out"
}

DEVICE_CLANG="/tmp/clang-ios-device.sh"
SIM_CLANG="/tmp/clang-ios-simulator.sh"
make_clang_wrapper "$DEVICE_CLANG" "iphoneos" "arm64-apple-ios${IOS_VERSION}"
make_clang_wrapper "$SIM_CLANG"  "iphonesimulator" "arm64-apple-ios${IOS_VERSION}-simulator"

log "Building device archive (arm64)..."
CGO_ENABLED=1 \
GOOS=ios \
GOARCH=arm64 \
CC="$DEVICE_CLANG" \
CGO_CFLAGS="-isysroot $IOS_SDK" \
CGO_LDFLAGS="-isysroot $IOS_SDK -framework CoreFoundation -framework Security" \
go build \
    -trimpath \
    -buildmode=c-archive \
    -ldflags="-s -w" \
    -o "$DEVICE_OUT/libTorrCore.a" \
    ./mobile/carchive

log "Building simulator archive (arm64)..."
CGO_ENABLED=1 \
GOOS=ios \
GOARCH=arm64 \
CC="$SIM_CLANG" \
CGO_CFLAGS="-isysroot $SIM_SDK" \
CGO_LDFLAGS="-isysroot $SIM_SDK -framework CoreFoundation -framework Security" \
go build \
    -trimpath \
    -buildmode=c-archive \
    -ldflags="-s -w" \
    -o "$SIM_OUT/libTorrCore.a" \
    ./mobile/carchive

log "Device archive:"
ls -lh "$DEVICE_OUT/libTorrCore.a"
log "Simulator archive:"
ls -lh "$SIM_OUT/libTorrCore.a"

# Copy headers
log "Setting up headers..."
DEVICE_HEADERS="$DEVICE_OUT/Headers"
SIM_HEADERS="$SIM_OUT/Headers"
mkdir -p "$DEVICE_HEADERS" "$SIM_HEADERS"

cp "$DEVICE_OUT/libTorrCore.h" "$DEVICE_HEADERS/TorrCore.h" 2>/dev/null || {
    err "Device header not generated. Check if .h file exists alongside .a"
}
cp "$SIM_OUT/libTorrCore.h" "$SIM_HEADERS/TorrCore.h" 2>/dev/null || {
    err "Simulator header not generated"
}

MODULE_MAP="$PROJECT_DIR/include/module.modulemap"
if [ -f "$MODULE_MAP" ]; then
    cp "$MODULE_MAP" "$DEVICE_HEADERS/"
    cp "$MODULE_MAP" "$SIM_HEADERS/"
fi

# Create XCFramework
XCFRAMEWORK="$BUILD_DIR/TorrCore.xcframework"
log "Creating XCFramework..."
rm -rf "$XCFRAMEWORK"

xcodebuild -create-xcframework \
    -library "$DEVICE_OUT/libTorrCore.a" \
    -headers "$DEVICE_HEADERS" \
    -library "$SIM_OUT/libTorrCore.a" \
    -headers "$SIM_HEADERS" \
    -output "$XCFRAMEWORK"

log "XCFramework created at: $XCFRAMEWORK"
log "Done."
