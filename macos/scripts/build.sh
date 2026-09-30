#!/bin/bash
# Builds Sushi.app and, from it, the installer and the disk image.
#
#   build.sh app   dist/Sushi.app
#   build.sh pkg   dist/Sushi-VERSION.pkg  (installs the app and the sushi command)
#   build.sh dmg   dist/Sushi-VERSION.dmg  (drag the app to Applications)
#
# Everything is signed ad hoc, which is enough to run on the Mac that built
# it. To distribute it, set SIGN_IDENTITY to a Developer ID and notarize.
set -euo pipefail

cd "$(dirname "$0")/../.."
ROOT=$PWD
MACOS=$ROOT/macos
DIST=$ROOT/dist
CACHE=$MACOS/.cache
VERSION=${VERSION:-0.1.0}
SIGN_IDENTITY=${SIGN_IDENTITY:--}
IDENTIFIER=com.icichainz.sushi
APP=$DIST/Sushi.app

FONT_BASE=https://github.com/ryanoasis/nerd-fonts/raw/v3.3.0/patched-fonts/JetBrainsMono/Ligatures
# name:sha256 of the JetBrainsMono Nerd Font Mono files, v3.3.0 (SIL OFL 1.1)
FONTS="
Regular:9e4dad8c34fb31045d53790a936a0afc3aae3fb830e874faadf3670662b04853
Bold:174b245db4097e08b372d8800ebfe4fd324fb81b2c9223da7b9ec16eeb7db901
Italic:56f00fb26f697fd5c3300dc8fd0b86ec87ceee59896e6d5aeaaae713fef80806
BoldItalic:9f7facb668ea56e857c969701dc32af38c3a1b2056c1b789bc47625a916c5fff
"

step() { printf '\n==> %s\n' "$1"; }

fetch_fonts() {
    mkdir -p "$CACHE/fonts"
    for entry in $FONTS; do
        local name=${entry%%:*} sum=${entry##*:}
        local file=$CACHE/fonts/JetBrainsMonoNerdFontMono-$name.ttf
        if [ ! -f "$file" ]; then
            echo "Downloading $(basename "$file")"
            curl -fsSL -o "$file" "$FONT_BASE/$name/$(basename "$file")"
        fi
        # A changed download is not bundled
        if [ "$(shasum -a 256 "$file" | cut -d' ' -f1)" != "$sum" ]; then
            echo "error: checksum mismatch for $file; delete it and try again" >&2
            exit 1
        fi
    done
}

# universal OUT BUILD_COMMAND...: BUILD_COMMAND is run once per processor
# type with ARCH set, and must print the path of what it built
universal() {
    local out=$1; shift
    local built=()
    for ARCH in arm64 x86_64; do
        export ARCH
        built+=("$("$@")")
    done
    lipo -create -output "$out" "${built[@]}"
}

build_go() {
    local goarch=$ARCH out=$CACHE/sushi-$ARCH
    [ "$ARCH" = x86_64 ] && goarch=amd64
    (cd "$ROOT" && CGO_ENABLED=0 GOOS=darwin GOARCH=$goarch go build -trimpath -ldflags "-s -w" -o "$out" .) >&2
    echo "$out"
}

build_swift() {
    local triple=$ARCH-apple-macosx12.0
    (cd "$MACOS" && swift build -c release --triple "$triple") >&2
    echo "$MACOS/.build/$ARCH-apple-macosx/release/Sushi"
}

build_app() {
    step "Building Sushi.app $VERSION"
    fetch_fonts
    rm -rf "$APP"
    mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources/Fonts" "$CACHE"

    universal "$APP/Contents/Resources/sushi" build_go
    universal "$APP/Contents/MacOS/Sushi" build_swift

    swift "$MACOS/scripts/make-icon.swift" "$MACOS/icon/sushi.svg" "$CACHE/Sushi.iconset"
    iconutil -c icns -o "$APP/Contents/Resources/Sushi.icns" "$CACHE/Sushi.iconset"

    cp "$CACHE"/fonts/*.ttf "$APP/Contents/Resources/Fonts/"
    sed "s/__VERSION__/$VERSION/g" "$MACOS/Info.plist" > "$APP/Contents/Info.plist"
    plutil -lint "$APP/Contents/Info.plist" > /dev/null

    # Inside out: the bundled program first, then the app around it
    codesign --force --sign "$SIGN_IDENTITY" "$APP/Contents/Resources/sushi"
    codesign --force --sign "$SIGN_IDENTITY" "$APP"
    codesign --verify --deep --strict "$APP"
    echo "Built $APP"
}

build_pkg() {
    [ -d "$APP" ] || build_app
    step "Building the installer"
    local root=$CACHE/pkg-root plist=$CACHE/components.plist out=$DIST/Sushi-$VERSION.pkg
    rm -rf "$root"
    mkdir -p "$root/Applications" "$root/usr/local/bin"
    cp -R "$APP" "$root/Applications/"
    cp "$APP/Contents/Resources/sushi" "$root/usr/local/bin/sushi"

    # Always install into /Applications. By default the installer would
    # "relocate" the app over any other copy it finds, such as dist/Sushi.app.
    pkgbuild --analyze --root "$root" "$plist" > /dev/null
    /usr/libexec/PlistBuddy -c "Set :0:BundleIsRelocatable false" "$plist"

    pkgbuild --root "$root" --component-plist "$plist" \
        --identifier "$IDENTIFIER" --version "$VERSION" --install-location / "$out" > /dev/null
    rm -rf "$root"
    echo "Built $out"
}

build_dmg() {
    [ -d "$APP" ] || build_app
    step "Building the disk image"
    local stage=$CACHE/dmg out=$DIST/Sushi-$VERSION.dmg
    rm -rf "$stage" "$out"
    mkdir -p "$stage"
    cp -R "$APP" "$stage/"
    ln -s /Applications "$stage/Applications"
    hdiutil create -volname "Sushi" -srcfolder "$stage" -format UDZO -ov "$out" > /dev/null
    rm -rf "$stage"
    echo "Built $out"
}

case "${1:-}" in
    app) build_app ;;
    pkg) build_pkg ;;
    dmg) build_dmg ;;
    all) build_app; build_pkg; build_dmg ;;
    *) echo "usage: $0 app|pkg|dmg|all" >&2; exit 2 ;;
esac
