#!/usr/bin/env bash
set -euo pipefail

if [[ $EUID -eq 0 ]]; then
    echo "Run this script as your normal user, not with sudo."
    exit 1
fi

cd /opt/ygglanding/src

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

cp ygglanding "$old_binary"

echo "Installing..."
sudo install -m 0755 "$new_binary" /opt/ygglanding/src/ygglanding

echo "Restarting..."
if ! sudo systemctl restart ygglanding; then
    echo "Restart failed; restoring previous binary."
    sudo install -m 0755 "$old_binary" /opt/ygglanding/src/ygglanding
    sudo systemctl restart ygglanding
    exit 1
fi

if ! systemctl is-active --quiet ygglanding; then
    echo "Service did not become active; restoring previous binary."
    sudo install -m 0755 "$old_binary" /opt/ygglanding/src/ygglanding
    sudo systemctl restart ygglanding
    exit 1
fi

echo "YggLanding deployed successfully."
