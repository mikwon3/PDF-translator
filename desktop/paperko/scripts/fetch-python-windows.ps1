# Download a relocatable standalone CPython (python-build-standalone) into
# resources\python and install the engine + deps into it. This is a FULL CPython
# (normal pip, normal site-packages, NO ._pth) so it avoids the Windows
# "embeddable package" ._pth pitfalls that cause import errors.
#
# Run from desktop\paperko:   powershell -ExecutionPolicy Bypass -File scripts\fetch-python-windows.ps1
$ErrorActionPreference = "Stop"

$AppDir = Split-Path -Parent $PSScriptRoot      # desktop\paperko
Set-Location $AppDir
$Engine = (Resolve-Path "..\..\engine-py").Path

# Pin a python-build-standalone release + CPython version.
# Update from https://github.com/astral-sh/python-build-standalone/releases
$Rel    = "20260610"
$Ver    = "3.12.13"
$Triple = "x86_64-pc-windows-msvc"
$Url    = "https://github.com/astral-sh/python-build-standalone/releases/download/$Rel/cpython-$Ver+$Rel-$Triple-install_only.tar.gz"

# Install next to the built exe (bin\resources\python) so both `wails3 dev` and
# the packaged app find it via resolveEngine(). `wails3 build` writes bin\paperko.exe
# but does not clean bin\, so this survives a later `wails3 package`.
Write-Host "==> downloading $Url"
Remove-Item -Recurse -Force "bin\resources\python" -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force "bin\resources" | Out-Null
$Tgz = Join-Path $env:TEMP "paperko_pbs.tar.gz"
Invoke-WebRequest -Uri $Url -OutFile $Tgz
tar -xzf $Tgz -C "bin\resources"                 # -> bin\resources\python\

$Py = "bin\resources\python\python.exe"

# A freshly-extracted python.exe often has its first outbound sockets denied by
# Windows Defender Firewall / AV (WinError 10013 WSAEACCES) while the OS decides
# whether to trust the new binary; the next attempt succeeds. Retry so a fresh
# machine self-heals instead of failing the whole build.
function Invoke-Pip {
  param([Parameter(ValueFromRemainingArguments = $true)][string[]] $PipArgs)
  for ($i = 1; $i -le 4; $i++) {
    & $Py -m pip @PipArgs
    if ($LASTEXITCODE -eq 0) { return }
    if ($i -lt 4) { Write-Host "   (pip failed, retry $i/3 in 5s...)"; Start-Sleep -Seconds 5 }
  }
  throw "pip failed after retries: pip $($PipArgs -join ' ')"
}

Write-Host "==> installing engine + deps into the bundled runtime"
Invoke-Pip install --upgrade pip
# deps first, then the engine with --ignore-installed so a prior RECORD-less
# install (pip "uninstall-no-record-file" error) is overwritten, not uninstalled.
Invoke-Pip install --upgrade pymupdf httpx pysbd pyahocorasick python-docx
Invoke-Pip install --ignore-installed --no-deps --no-cache-dir "$Engine"

Write-Host "==> verifying"
& $Py -s -c "import translate_engine, pymupdf, httpx, pysbd, ahocorasick, docx; print('bundled python OK', translate_engine.__version__)"
Write-Host "done -> bin\resources\python  (python.exe at bin\resources\python\python.exe)"
