#!/usr/bin/env bash
# Installs the freetube-sync desktop launcher: detects whether FreeTube is
# a Flatpak or native install (independently of the Go binary — this
# mirrors internal/installdetect's logic in shell so it works even before
# freetube-sync is built), writes a .desktop entry that runs
# `freetube-sync run` around the real launch command, and persists the
# server URL/token to ~/.config/freetube-sync/config.json.
#
# The desktop entry can either sit alongside the original FreeTube entry
# ("FreeTube (synced)", a separate icon) or shadow it (replace the
# original entry in place, so the normal "FreeTube" icon always syncs).
# You'll be prompted to choose unless --desktop-mode is given. Shadowing
# backs up any original file it overwrites (<file>.freetube-sync-bak) the
# first time; uninstall-client.sh restores it.
#
# Safe to re-run (idempotent), including after switching between Flatpak
# and native installs, or between desktop modes.
#
# Usage:
#   scripts/install-client.sh [--install=flatpak|native] [--server=URL] \
#       [--token=TOKEN] [--desktop-mode=separate|shadow]
#
# Env overrides (mainly for testing):
#   FREETUBE_SYNC_BIN   path to a pre-built freetube-sync binary to install
#                        (default: ../freetube-sync relative to this script)
set -euo pipefail

APP_ID="io.freetubeapp.FreeTube"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/freetube-sync"
CONFIG_FILE="$CONFIG_DIR/config.json"
BIN_DIR="$HOME/.local/bin"
DESKTOP_DIR="$HOME/.local/share/applications"
# Marker key written into every desktop entry this script manages, so
# uninstall-client.sh (and re-runs of this script) can find them reliably
# instead of assuming a fixed filename.
DESKTOP_MARKER_KEY="X-FreetubeSync"

err() {
	echo "install-client.sh: $*" >&2
	exit 1
}

FORCE_INSTALL=""
ARG_SERVER=""
ARG_TOKEN=""
ARG_DESKTOP_MODE=""
for arg in "$@"; do
	case "$arg" in
	--install=*) FORCE_INSTALL="${arg#*=}" ;;
	--server=*) ARG_SERVER="${arg#*=}" ;;
	--token=*) ARG_TOKEN="${arg#*=}" ;;
	--desktop-mode=*) ARG_DESKTOP_MODE="${arg#*=}" ;;
	-h | --help)
		sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*) err "unknown argument: $arg" ;;
	esac
done

command -v python3 >/dev/null 2>&1 || err "python3 is required (used to read/write config.json)"

# --- detect install type, mirroring internal/installdetect ---

flatpak_present() {
	if command -v flatpak >/dev/null 2>&1 && flatpak info "$APP_ID" >/dev/null 2>&1; then
		return 0
	fi
	[ -d "$HOME/.var/app/$APP_ID" ]
}

native_present() {
	[ -d "$HOME/.config/FreeTube" ] && command -v freetube >/dev/null 2>&1
}

find_flatpak_desktop() {
	for p in \
		"$HOME/.local/share/flatpak/exports/share/applications/$APP_ID.desktop" \
		"/var/lib/flatpak/exports/share/applications/$APP_ID.desktop"; do
		if [ -f "$p" ]; then
			printf '%s' "$p"
			return 0
		fi
	done
	return 1
}

find_native_desktop() {
	for p in \
		"$HOME/.local/share/applications/freetube.desktop" \
		"/usr/local/share/applications/freetube.desktop" \
		"/usr/share/applications/freetube.desktop"; do
		if [ -f "$p" ]; then
			printf '%s' "$p"
			return 0
		fi
	done
	return 1
}

desktop_field() {
	# desktop_field <file> <Key> -> the first matching "Key=value" value,
	# ignoring localized variants like "Name[fr]=".
	grep -m1 -E "^$2=" "$1" 2>/dev/null | cut -d= -f2- || true
}

HAVE_FLATPAK=0
HAVE_NATIVE=0
flatpak_present && HAVE_FLATPAK=1
native_present && HAVE_NATIVE=1

if [ -n "$FORCE_INSTALL" ]; then
	case "$FORCE_INSTALL" in
	flatpak | native) INSTALL_TYPE="$FORCE_INSTALL" ;;
	*) err "unknown --install value '$FORCE_INSTALL' (want 'flatpak' or 'native')" ;;
	esac
elif [ "$HAVE_FLATPAK" = 1 ] && [ "$HAVE_NATIVE" = 1 ]; then
	echo "Both a Flatpak and a native FreeTube install were found."
	read -r -p "Choose install type [flatpak/native]: " INSTALL_TYPE
	case "$INSTALL_TYPE" in
	flatpak | native) ;;
	*) err "unrecognized choice '$INSTALL_TYPE'" ;;
	esac
elif [ "$HAVE_FLATPAK" = 1 ]; then
	INSTALL_TYPE="flatpak"
elif [ "$HAVE_NATIVE" = 1 ]; then
	INSTALL_TYPE="native"
else
	err "no FreeTube installation found (checked Flatpak and native paths)"
fi
echo "Using install type: $INSTALL_TYPE"

# --- resolve name/icon + the real launch command ---

if [ "$INSTALL_TYPE" = "flatpak" ]; then
	SRC_DESKTOP="$(find_flatpak_desktop || true)"
	LAUNCH_CMD="flatpak run $APP_ID"
else
	SRC_DESKTOP="$(find_native_desktop || true)"
	FREETUBE_BIN="$(command -v freetube)"
	LAUNCH_CMD="$FREETUBE_BIN"
fi

APP_NAME="FreeTube"
APP_ICON="freetube"
if [ -n "$SRC_DESKTOP" ]; then
	name="$(desktop_field "$SRC_DESKTOP" Name)"
	icon="$(desktop_field "$SRC_DESKTOP" Icon)"
	[ -n "$name" ] && APP_NAME="$name"
	[ -n "$icon" ] && APP_ICON="$icon"
else
	echo "warning: couldn't find FreeTube's .desktop file to source icon/name from; using defaults" >&2
fi

# --- separate entry, or shadow the original one? ---

DESKTOP_MODE="$ARG_DESKTOP_MODE"
if [ -n "$DESKTOP_MODE" ]; then
	case "$DESKTOP_MODE" in
	separate | shadow) ;;
	*) err "unknown --desktop-mode value '$DESKTOP_MODE' (want 'separate' or 'shadow')" ;;
	esac
	if [ "$DESKTOP_MODE" = "shadow" ] && [ -z "$SRC_DESKTOP" ]; then
		err "--desktop-mode=shadow requires an existing FreeTube .desktop file to shadow, but none was found"
	fi
elif [ -z "$SRC_DESKTOP" ]; then
	DESKTOP_MODE="separate"
	echo "No original .desktop file found to shadow; installing a separate entry." >&2
else
	echo "Desktop entry:"
	echo "  [1] separate — adds \"$APP_NAME (synced)\" alongside the original (default)"
	echo "  [2] shadow    — replaces the original \"$APP_NAME\" entry so it always syncs"
	read -r -p "Choose 1 or 2 [1]: " choice
	case "${choice:-1}" in
	1) DESKTOP_MODE="separate" ;;
	2) DESKTOP_MODE="shadow" ;;
	*) err "unrecognized choice '$choice'" ;;
	esac
fi

if [ "$DESKTOP_MODE" = "shadow" ]; then
	DESKTOP_FILE="$DESKTOP_DIR/$(basename "$SRC_DESKTOP")"
	DISPLAY_NAME="$APP_NAME"
else
	DESKTOP_FILE="$DESKTOP_DIR/freetube-synced.desktop"
	DISPLAY_NAME="$APP_NAME (synced)"
fi

# --- locate and install the freetube-sync binary ---

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
CANDIDATE_BIN="${FREETUBE_SYNC_BIN:-$SCRIPT_DIR/../freetube-sync}"
[ -x "$CANDIDATE_BIN" ] || err "freetube-sync binary not found or not executable at $CANDIDATE_BIN — build it first (go build -o freetube-sync ./cmd/freetube-sync) or set FREETUBE_SYNC_BIN"

mkdir -p "$BIN_DIR"
install -m 0755 "$CANDIDATE_BIN" "$BIN_DIR/freetube-sync"
echo "Installed binary: $BIN_DIR/freetube-sync"

# --- server URL / token, persisted to config.json (never baked into Exec=) ---

EXISTING_SERVER=""
EXISTING_TOKEN=""
if [ -f "$CONFIG_FILE" ]; then
	EXISTING_SERVER="$(python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get('serverUrl',''))" "$CONFIG_FILE" 2>/dev/null || true)"
	EXISTING_TOKEN="$(python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get('token',''))" "$CONFIG_FILE" 2>/dev/null || true)"
fi

SERVER_URL="$ARG_SERVER"
if [ -z "$SERVER_URL" ]; then
	read -r -p "Server URL${EXISTING_SERVER:+ [$EXISTING_SERVER]}: " SERVER_URL
	SERVER_URL="${SERVER_URL:-$EXISTING_SERVER}"
fi
[ -n "$SERVER_URL" ] || err "a server URL is required"

TOKEN="$ARG_TOKEN"
if [ -z "$TOKEN" ]; then
	read -r -s -p "Token${EXISTING_TOKEN:+ [leave blank to keep existing]}: " TOKEN
	echo
	TOKEN="${TOKEN:-$EXISTING_TOKEN}"
fi
[ -n "$TOKEN" ] || err "a token is required"

mkdir -p "$CONFIG_DIR"
python3 - "$CONFIG_FILE" "$INSTALL_TYPE" "$SERVER_URL" "$TOKEN" <<'PYEOF'
import json, os, sys

path, install, server, token = sys.argv[1:5]
data = {}
if os.path.exists(path):
    try:
        with open(path) as f:
            data = json.load(f)
    except (OSError, ValueError):
        data = {}
data["install"] = install
data["serverUrl"] = server
data["token"] = token
tmp = path + ".tmp"
with open(tmp, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
os.replace(tmp, path)
os.chmod(path, 0o600)
PYEOF
echo "Wrote config: $CONFIG_FILE"

# --- desktop entry ---

mkdir -p "$DESKTOP_DIR"

# If we're about to overwrite a file we didn't already generate (detected
# via our marker key) — i.e. a real pre-existing entry, only possible in
# shadow mode — back it up first, once. Re-running this script never
# clobbers that backup with our own prior output.
if [ -f "$DESKTOP_FILE" ] && ! grep -q "^$DESKTOP_MARKER_KEY=" "$DESKTOP_FILE" 2>/dev/null; then
	cp "$DESKTOP_FILE" "$DESKTOP_FILE.freetube-sync-bak"
	echo "Backed up original desktop entry: $DESKTOP_FILE.freetube-sync-bak"
fi

cat >"$DESKTOP_FILE" <<EOF
[Desktop Entry]
Type=Application
Version=1.0
Name=$DISPLAY_NAME
Comment=FreeTube, syncing subscriptions via freetube-sync on launch and exit
Exec=$BIN_DIR/freetube-sync run -- $LAUNCH_CMD
Icon=$APP_ICON
Terminal=false
Categories=AudioVideo;Video;Network;
StartupNotify=true
StartupWMClass=FreeTube
$DESKTOP_MARKER_KEY=1
EOF
chmod 0644 "$DESKTOP_FILE"
echo "Wrote desktop entry ($DESKTOP_MODE): $DESKTOP_FILE"

if command -v update-desktop-database >/dev/null 2>&1; then
	update-desktop-database "$DESKTOP_DIR" >/dev/null 2>&1 || true
fi

echo "Done. Launch \"$DISPLAY_NAME\" from your application menu."
