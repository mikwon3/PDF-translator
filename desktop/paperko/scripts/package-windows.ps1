# Build the NSIS installer (PaperKo-<ver>-amd64-installer.exe) from an already
# populated bin\ folder — WITHOUT `wails3 package`, which reruns the build and
# can wipe bin\resources (the bundled Python/llama) and trip the beta Taskfile.
#
# Prerequisites (run build-windows.ps1 first):
#   bin\paperko.exe                 (wails GUI exe)
#   bin\resources\python\...        (fetch-python-windows.ps1)
#   bin\resources\llama\...         (optional, offline mode)
# NSIS (makensis) must be on PATH: winget install NSIS.NSIS
#
# Run from desktop\paperko:
#   powershell -ExecutionPolicy Bypass -File scripts\package-windows.ps1
$ErrorActionPreference = "Stop"

$AppDir = Split-Path -Parent $PSScriptRoot      # desktop\paperko
$Nsis   = Join-Path $AppDir "build\windows\nsis"
$Exe    = Join-Path $AppDir "bin\paperko.exe"

if (-not (Test-Path $Exe)) { throw "bin\paperko.exe not found — run scripts\build-windows.ps1 first." }
if (-not (Test-Path (Join-Path $AppDir "bin\resources\python\python.exe"))) {
  throw "bin\resources\python missing — run scripts\fetch-python-windows.ps1 first."
}

Set-Location $Nsis
# Each token is a separate array element so the -D value (a path with '=') is
# passed intact — PowerShell otherwise splits `-DNAME=value` at the '='.
& makensis "-DARG_WAILS_AMD64_BINARY=..\..\..\bin\paperko.exe" "project.nsi"
if ($LASTEXITCODE -ne 0) { throw "makensis failed ($LASTEXITCODE)" }

$out = Get-ChildItem (Join-Path $AppDir "bin") -Filter "*-installer.exe" |
       Sort-Object LastWriteTime -Descending | Select-Object -First 1
Write-Host ""
Write-Host ("done -> bin\{0}  ({1:N1} MB)" -f $out.Name, ($out.Length / 1MB))
