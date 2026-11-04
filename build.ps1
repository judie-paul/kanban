# Builds the Kanban binary into .\build on Windows (PowerShell).
#
#   .\build.ps1          build for this computer only
#   .\build.ps1 all      build for Windows, Linux and macOS (amd64 + arm64)
#   .\build.ps1 test     run the test suite
#
# Requires Go 1.26+. No C compiler is needed (the SQLite driver is pure Go).
param([string]$Target = "native")
$ErrorActionPreference = "Stop"

$Root = $PSScriptRoot
$Out = Join-Path $Root "build"
$Version = (git -C $Root describe --tags --always --dirty 2>$null)
if (-not $Version) { $Version = "dev" }
$LdFlags = "-s -w -X main.version=$Version"

New-Item -ItemType Directory -Force $Out | Out-Null
Push-Location (Join-Path $Root "code")
try {
    function Build-Kanban([string]$os, [string]$arch) {
        $ext = if ($os -eq "windows") { ".exe" } else { "" }
        $name = "kanban-$os-$arch$ext"
        Write-Host "building $name"
        $env:CGO_ENABLED = "0"; $env:GOOS = $os; $env:GOARCH = $arch
        go build -trimpath -ldflags $LdFlags -o (Join-Path $Out $name) ./cmd/web
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $os/$arch" }
    }

    switch ($Target) {
        "all" {
            foreach ($t in @("windows/amd64", "windows/arm64", "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64")) {
                $os, $arch = $t.Split("/")
                Build-Kanban $os $arch
            }
        }
        "test" {
            go vet ./...; if ($LASTEXITCODE -ne 0) { throw "go vet failed" }
            go test ./...; if ($LASTEXITCODE -ne 0) { throw "tests failed" }
            return
        }
        "native" { Build-Kanban (go env GOOS) (go env GOARCH) }
        default { throw "usage: .\build.ps1 [native|all|test]" }
    }
    Write-Host "done -> $Out"
}
finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Pop-Location
}
