$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$repo = Split-Path -Parent (Split-Path -Parent $root)
$classes = Join-Path $root ".classes"
$jar = Join-Path $root "plugins\demo-plugin.jar"
$pluginDir = Join-Path $root "plugin"
$sourceRoot = Join-Path $root "plugin\src"
$apiSource = Join-Path $repo "internal\plugins\java"

Remove-Item -Recurse -Force $classes -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $classes | Out-Null
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $jar) | Out-Null

$files = Get-ChildItem -Path $sourceRoot -Recurse -Filter *.java |
    Sort-Object FullName |
    ForEach-Object { $_.FullName }

& javac --release 17 -sourcepath $apiSource -d $classes $files
if ($LASTEXITCODE -ne 0) {
    throw "javac failed"
}

# the host provides the API classes at runtime
Remove-Item -Recurse -Force (Join-Path $classes "dev") -ErrorAction SilentlyContinue

& jar --create --file $jar -C $classes . -C $pluginDir META-INF
if ($LASTEXITCODE -ne 0) {
    throw "jar failed"
}
Remove-Item -Recurse -Force $classes

Write-Host "built $jar"
