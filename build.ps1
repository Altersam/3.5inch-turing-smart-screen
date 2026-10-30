$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go 1.23+ is required. Install it from https://go.dev/dl/'
}

$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:GO111MODULE = 'on'
go build -ldflags '-H=windowsgui -s -w' -o (Join-Path $PSScriptRoot 'Turing35NeonStableV44.exe') .
if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
Write-Host "Built: $(Join-Path $PSScriptRoot 'Turing35NeonStableV44.exe')"
