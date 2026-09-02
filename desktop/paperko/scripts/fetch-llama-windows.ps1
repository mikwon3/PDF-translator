# Fetch a prebuilt llama.cpp `llama-server.exe` (Windows x64) and stage it + its
# DLLs into resources\llama\ so the app can run models offline.
#
#   powershell -ExecutionPolicy Bypass -File scripts\fetch-llama-windows.ps1
#   $env:LLAMA_ASSET="win-cuda-12.4-x64"; .\scripts\fetch-llama-windows.ps1   # NVIDIA build
$ErrorActionPreference = "Stop"
Set-Location (Split-Path -Parent $PSScriptRoot)     # desktop\paperko

$AssetPat = if ($env:LLAMA_ASSET) { $env:LLAMA_ASSET } else { "win-cpu-x64" }
$Dest = "resources\llama"
$Api  = if ($env:LLAMA_TAG) {
  "https://api.github.com/repos/ggml-org/llama.cpp/releases/tags/$($env:LLAMA_TAG)"
} else {
  "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest"
}

Write-Host "==> resolving llama.cpp release ($AssetPat)"
$rel = Invoke-RestMethod -Uri $Api -Headers @{ "User-Agent" = "paperko" }
$asset = $rel.assets | Where-Object { $_.name -match "bin-$AssetPat\.zip$" } | Select-Object -First 1
if (-not $asset) { throw "no windows asset found (pattern: bin-$AssetPat.zip)" }
Write-Host "    $($asset.browser_download_url)"

$tmp = New-Item -ItemType Directory -Path ([IO.Path]::Combine($env:TEMP, "llama_" + [guid]::NewGuid()))
$zip = Join-Path $tmp "llama.zip"
Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zip
Expand-Archive -Path $zip -DestinationPath $tmp -Force

$bin = Get-ChildItem -Path $tmp -Recurse -Filter "llama-server.exe" | Select-Object -First 1
if (-not $bin) { throw "llama-server.exe not found in archive" }
$src = $bin.DirectoryName

if (Test-Path $Dest) { Remove-Item -Recurse -Force $Dest }
New-Item -ItemType Directory -Path $Dest | Out-Null
Copy-Item (Join-Path $src "llama-server.exe") $Dest
Get-ChildItem -Path $src -Filter *.dll | ForEach-Object { Copy-Item $_.FullName $Dest }

Write-Host "==> staged into $Dest :"
Get-ChildItem $Dest | ForEach-Object { Write-Host "    $($_.Name)" }
Remove-Item -Recurse -Force $tmp
Write-Host "OK.  (CUDA build: also bundle the cudart-*.zip DLLs, or install the CUDA runtime.)"
