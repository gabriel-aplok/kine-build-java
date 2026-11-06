$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$classes = Join-Path $root "game\.classes"
$jar = Join-Path $root "game\minecraft.jar"
$sourceRoot = Join-Path $root "game\src"

Remove-Item -Recurse -Force $classes -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $classes | Out-Null

$files = Get-ChildItem -Path $sourceRoot -Recurse -Filter *.java |
    Sort-Object FullName |
    ForEach-Object { $_.FullName }

& javac --release 17 -d $classes $files
if ($LASTEXITCODE -ne 0) {
    throw "javac failed"
}

& jar --create --file $jar -C $classes .
if ($LASTEXITCODE -ne 0) {
    throw "jar failed"
}
Remove-Item -Recurse -Force $classes

Write-Host "built $jar"
