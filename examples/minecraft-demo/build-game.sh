#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
classes="$root/game/.classes"
sources="$root/game/.sources"

rm -rf "$classes"
mkdir -p "$classes"

find "$root/game/src" -name '*.java' -print | sort > "$sources"
javac --release 17 -d "$classes" @"$sources"
rm -f "$sources"

jar --create --file "$root/game/minecraft.jar" -C "$classes" .
rm -rf "$classes"

echo "built $root/game/minecraft.jar"
