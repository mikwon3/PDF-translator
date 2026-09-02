# Build the Windows app WITHOUT the wails3 Task system (avoids the beta.6
# "uname/tail: executable file not found" Taskfile error entirely).
#
# Produces a portable bin\ folder: bin\paperko.exe + bin\resources\python\.
# For an installer, run `wails3 package` afterwards (needs the Taskfile ios/android
# includes commented out — see BUILD-WINDOWS.md), or distribute bin\ as portable.
#
# Run from desktop\paperko:
#   powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1
$ErrorActionPreference = "Stop"

$AppDir = Split-Path -Parent $PSScriptRoot      # desktop\paperko
Set-Location $AppDir

# `go install` puts wails3 in $(go env GOPATH)\bin, which isn't always on PATH.
try { $goBin = (& go env GOPATH); if ($goBin) { $env:PATH = "$env:PATH;$goBin\bin" } } catch {}

Write-Host "==> 1/4  generating Wails bindings (safe; does not use Taskfile)"
try { wails3 generate bindings | Out-Null } catch { Write-Host "   (bindings step skipped: $_)" }

Write-Host "==> 2/4  building frontend"
Push-Location frontend
npm install
npm run build
Pop-Location

Write-Host "==> 3/4  building GUI exe (no console, CGO off)"
New-Item -ItemType Directory -Force bin | Out-Null
$env:CGO_ENABLED = "0"
go build -tags production -ldflags="-w -s -H windowsgui" -o bin\paperko.exe .

Write-Host "==> 4/4  bundling standalone Python engine into bin\resources\python"
powershell -ExecutionPolicy Bypass -File scripts\fetch-python-windows.ps1

Write-Host "==> bundling local llama.cpp (offline mode) if present"
if (Test-Path "resources\llama") {
  New-Item -ItemType Directory -Force "bin\resources\llama" | Out-Null
  Copy-Item -Recurse -Force "resources\llama\*" "bin\resources\llama\"
  Write-Host "    bundled llama.cpp (offline mode enabled)"
} else {
  Write-Host "    (no resources\llama - run scripts\fetch-llama-windows.ps1 first to include offline mode)"
}

Write-Host ""
Write-Host "done -> bin\paperko.exe  (portable; run it directly)"
Write-Host "       installer (optional): wails3 package   # requires Taskfile ios/android includes commented out"
