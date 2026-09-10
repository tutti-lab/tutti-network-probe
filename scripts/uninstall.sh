#!/bin/sh
set -eu

label="sh.tutti.network-probe"
plist_path="$HOME/Library/LaunchAgents/$label.plist"
binary_path="${TUTTI_NETWORK_PROBE_INSTALL_DIR:-$HOME/.local/bin}/tutti-network-probe"
user_id="$(id -u)"

launchctl bootout "gui/$user_id/$label" >/dev/null 2>&1 || true
if [ -f "$plist_path" ]; then
  rm "$plist_path"
fi
if [ -f "$binary_path" ]; then
  rm "$binary_path"
fi

echo "Uninstalled tutti-network-probe."
echo "Observation logs were preserved in $HOME/Library/Logs/Tutti/"
