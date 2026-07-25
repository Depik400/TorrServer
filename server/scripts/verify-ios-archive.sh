#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_DIR/../build/ios"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { echo -e "${GREEN}[PASS]${NC} $*"; }
fail() { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }
info() { echo -e "[INFO] $*"; }

DEVICE_OUT="$BUILD_DIR/iphoneos"
SIM_OUT="$BUILD_DIR/iphonesimulator-arm64"
XCFRAMEWORK="$BUILD_DIR/TorrCore.xcframework"

info "Verifying device archive..."
[ -f "$DEVICE_OUT/libTorrCore.a" ] || fail "Device archive not found: $DEVICE_OUT/libTorrCore.a"
pass "Device archive exists ($(du -sh "$DEVICE_OUT/libTorrCore.a" | cut -f1))"

info "Verifying simulator archive..."
[ -f "$SIM_OUT/libTorrCore.a" ] || fail "Simulator archive not found: $SIM_OUT/libTorrCore.a"
pass "Simulator archive exists ($(du -sh "$SIM_OUT/libTorrCore.a" | cut -f1))"

info "Verifying xcframework..."
[ -d "$XCFRAMEWORK" ] || fail "XCFramework not found: $XCFRAMEWORK"
pass "XCFramework exists"

info "Verifying device header..."
[ -f "$DEVICE_OUT/Headers/TorrCore.h" ] || fail "Device header not found"
pass "Device header exists"

info "Verifying simulator header..."
[ -f "$SIM_OUT/Headers/TorrCore.h" ] || fail "Simulator header not found"
pass "Simulator header exists"

info "Comparing exported symbols..."
DEVICE_SYMBOLS=$(grep -o 'TS_[A-Za-z]*' "$DEVICE_OUT/Headers/TorrCore.h" | sort -u)
SIM_SYMBOLS=$(grep -o 'TS_[A-Za-z]*' "$SIM_OUT/Headers/TorrCore.h" | sort -u)
if [ "$DEVICE_SYMBOLS" = "$SIM_SYMBOLS" ]; then
    pass "Device and simulator exported symbols match"
    echo "$DEVICE_SYMBOLS" | while read sym; do info "  $sym"; done
else
    fail "Symbol mismatch between device and simulator headers"
fi

info "Checking device archive architecture..."
ARCHS=$(lipo -info "$DEVICE_OUT/libTorrCore.a" 2>/dev/null || file "$DEVICE_OUT/libTorrCore.a")
info "Device: $ARCHS"

info "Checking simulator archive architecture..."
ARCHS=$(lipo -info "$SIM_OUT/libTorrCore.a" 2>/dev/null || file "$SIM_OUT/libTorrCore.a")
info "Simulator: $ARCHS"

info "Checking for undefined symbols (device)..."
UNDEF_DEVICE=$(nm -u "$DEVICE_OUT/libTorrCore.a" 2>/dev/null | head -20 || echo "nm not available or not a mach-o file")
info "Undefined symbols (sample): $UNDEF_DEVICE"

pass "All verifications passed"
