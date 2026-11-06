#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo=$(CDPATH= cd -- "$root/../.." && pwd)

classes="$root/.classes"
sources="$root/.sources"
jar="$root/plugins/demo-plugin.jar"

rm -rf "$classes"
mkdir -p "$classes" "$(dirname "$jar")"

find "$root/plugin/src" -name '*.java' -print | sort > "$sources"
javac --release 17 -sourcepath "$repo/internal/plugins/java" -d "$classes" @"$sources"
rm -f "$sources"

# the host provides the api classes at runtime
rm -rf "$classes/dev"

jar --create --file "$jar" -C "$classes" . -C "$root/plugin" META-INF
rm -rf "$classes"

echo "built $jar"
