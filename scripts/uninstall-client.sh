#!/usr/bin/env bash
# Removes what scripts/install-client.sh installed: its desktop entry(ies),
# the freetube-sync binary, and its config (including the shadow snapshot).
# Safe to re-run.
#
# Desktop entries are found by scanning ~/.local/share/applications for
# the X-FreetubeSync marker install-client.sh writes into every entry it
# manages — this covers both the separate "FreeTube (synced)" entry and a
# shadowed original entry, whatever its filename. A shadowed entry that
# has a <file>.freetube-sync-bak backup (the pre-shadow original) is
# restored instead of just deleted.
set -euo pipefail
shopt -s nullglob

CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/freetube-sync"
BIN_PATH="$HOME/.local/bin/freetube-sync"
APPLICATIONS_DIR="$HOME/.local/share/applications"
DESKTOP_MARKER_KEY="X-FreetubeSync"

KEEP_CONFIG=0
for arg in "$@"; do
	case "$arg" in
	--keep-config) KEEP_CONFIG=1 ;;
	-h | --help)
		echo "Usage: $0 [--keep-config]"
		exit 0
		;;
	*)
		echo "uninstall-client.sh: unknown argument: $arg" >&2
		exit 1
		;;
	esac
done

removed=0

for f in "$APPLICATIONS_DIR"/*.desktop; do
	grep -q "^$DESKTOP_MARKER_KEY=" "$f" 2>/dev/null || continue
	if [ -f "$f.freetube-sync-bak" ]; then
		mv "$f.freetube-sync-bak" "$f"
		echo "Restored original desktop entry: $f"
	else
		rm -f "$f"
		echo "Removed desktop entry: $f"
	fi
	removed=1
done

if [ -f "$BIN_PATH" ]; then
	rm -f "$BIN_PATH"
	echo "Removed binary: $BIN_PATH"
	removed=1
fi

if [ "$KEEP_CONFIG" = 0 ] && [ -d "$CONFIG_DIR" ]; then
	rm -rf "$CONFIG_DIR"
	echo "Removed config: $CONFIG_DIR"
	removed=1
elif [ "$KEEP_CONFIG" = 1 ] && [ -d "$CONFIG_DIR" ]; then
	echo "Kept config (--keep-config): $CONFIG_DIR"
fi

if command -v update-desktop-database >/dev/null 2>&1; then
	update-desktop-database "$APPLICATIONS_DIR" >/dev/null 2>&1 || true
fi

if [ "$removed" = 0 ]; then
	echo "Nothing to remove — freetube-sync's client wasn't installed."
else
	echo "Done."
fi
