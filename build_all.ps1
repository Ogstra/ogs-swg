Param(
    [switch]$SkipBackend,
    [switch]$SkipFrontend
)

$ErrorActionPreference = "Stop"
$TOOLS_DIR = "$PSScriptRoot\.tools"

# Ensure relative paths work even if invoked from elsewhere
Set-Location $PSScriptRoot

# Prefer local portable toolchains if present
if (Test-Path "$TOOLS_DIR\go\bin\go.exe") {
    $env:PATH = "$TOOLS_DIR\go\bin;$env:PATH"
}
if (Test-Path "$TOOLS_DIR\node\npm.cmd") {
    $env:PATH = "$TOOLS_DIR\node;$env:PATH"
}

function Assert-Cmd {
    param($Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "No se encontro '$Name' en PATH. Instalalo o usa build_and_run.ps1 para entorno portatil."
    }
}

function Run-Step {
    param($Title, [scriptblock]$Action)
    Write-Host ">>> $Title" -ForegroundColor Green
    & $Action
    Write-Host ""
}

# 0. Requisitos
if (-not $SkipBackend) {
    Assert-Cmd go
}
if (-not $SkipFrontend) {
    Assert-Cmd npm
}

# Prefer module mode even if vendor exists to avoid missing deps
$goModFlag = "-mod=mod"

# 1. Backend: test + build
if (-not $SkipBackend) {
    Run-Step "Backend tests (go test ./...)" {
        go test $goModFlag ./...
    }
    Run-Step "Backend build (swg.exe)" {
        go build $goModFlag -o swg.exe .
    }
}

# 2. Frontend: install + build
if (-not $SkipFrontend) {
    Push-Location frontend
    Run-Step "Frontend deps (npm ci)" {
        npm ci
    }
    Run-Step "Frontend build (npm run build)" {
        npm run build
    }
    Pop-Location
}

Write-Host "DONE - Todo OK" -ForegroundColor Cyan
