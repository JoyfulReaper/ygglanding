#!/usr/bin/env bash
set -euo pipefail

if [[ $EUID -eq 0 ]]; then
    echo "Run this script as your normal user, not with sudo."
    exit 1
fi

src_dir="/opt/ygglanding/src"
installed_binary="$src_dir/ygglanding"

cd "$src_dir"

echo "Formatting..."
gofmt -w main.go

echo "Testing..."
go test ./...

new_binary="$(mktemp /tmp/ygglanding-new.XXXXXX)"
old_binary="$(mktemp /tmp/ygglanding-old.XXXXXX)"

cleanup() {
    rm -f "$new_binary" "$old_binary"
}
trap cleanup EXIT

echo "Building..."
go build -o "$new_binary" .

echo "Backing up current binary..."

if [[ -f "$installed_binary" ]]; then
    cp "$installed_binary" "$old_binary"
else
    pid="$(systemctl show ygglanding -p MainPID --value)"

    if [[ -n "$pid" && "$pid" != "0" && -L "/proc/$pid/exe" ]]; then
        sudo cp "/proc/$pid/exe" "$old_binary"
        echo "Recovered rollback binary from running process PID $pid."
    else
        echo "Unable to locate current ygglanding binary for rollback."
        exit 1
    fi
fi

chmod 0755 "$old_binary"

echo "Installing..."
sudo install -m 0755 "$new_binary" "$installed_binary"

echo "Restarting..."
if ! sudo systemctl restart ygglanding; then
    echo "Restart failed; restoring previous binary."
    sudo install -m 0755 "$old_binary" "$installed_binary"
    sudo systemctl restart ygglanding
    exit 1
fi

if ! systemctl is-active --quiet ygglanding; then
    echo "Service did not become active; restoring previous binary."
    sudo install -m 0755 "$old_binary" "$installed_binary"
    sudo systemctl restart ygglanding
    exit 1
fi

echo "YggLanding deployed successfully."
