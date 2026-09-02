#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=${1:-"$root/dist/my_assistant-linux-arm64"}
mkdir -p "$(dirname -- "$out")"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o "$out/my_assistant" "$root/cmd/my_assistant"
cp "$root/.env.example" "$out/my_assistant.env.example"
cp "$root/deploy/my_assistant.service" "$out/"
cp "$root/deploy/my_assistant.logrotate" "$out/"
tar -C "$(dirname -- "$out")" -czf "$out.tar.gz" "$(basename -- "$out")"
printf 'Created %s.tar.gz\n' "$out"
