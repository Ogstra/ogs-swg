param (
    [switch]$ForceBuild
)

$ErrorActionPreference = "Stop"
$BUILD_DIR = Join-Path $PSScriptRoot "build"

# --- Configuration ---
$GO_VERSION = "1.22.1"
$NODE_VERSION = "20.11.1"
$TOOLS_DIR = "$PSScriptRoot\.tools"
$GO_URL = "https://go.dev/dl/go$GO_VERSION.windows-amd64.zip"
$NODE_URL = "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-win-x64.zip"

# Cleanup old tools dir if exists
if (Test-Path "$PSScriptRoot\tools") {
    Write-Host ">>> Cleaning up old tools directory..." -ForegroundColor Yellow
    Remove-Item "$PSScriptRoot\tools" -Recurse -Force -ErrorAction SilentlyContinue
}

# --- Helper Functions ---
function Download-And-Extract {
    param($Url, $DestDir, $Name)
    if (-not (Test-Path $DestDir)) {
        Write-Host ">>> Downloading $Name..." -ForegroundColor Cyan
        $ZipPath = "$PSScriptRoot\$Name.zip"
        Invoke-WebRequest -Uri $Url -OutFile $ZipPath
        
        Write-Host ">>> Extracting $Name..." -ForegroundColor Cyan
        Expand-Archive -Path $ZipPath -DestinationPath $PSScriptRoot -Force
        Remove-Item $ZipPath
        
        # Move to tools dir
        if (-not (Test-Path $TOOLS_DIR)) { New-Item -ItemType Directory -Path $TOOLS_DIR | Out-Null }
        
        # Handle Go's folder structure (go/bin)
        if ($Name -eq "go") {
            Move-Item "$PSScriptRoot\go" $DestDir
        }
        # Handle Node's folder structure (node-v.../node.exe)
        if ($Name -eq "node") {
            $ExtractedFolder = Get-ChildItem "$PSScriptRoot" -Directory | Where-Object { $_.Name -like "node-v*" }
            Move-Item $ExtractedFolder.FullName $DestDir
        }
    } else {
        Write-Host ">>> $Name already installed in $DestDir" -ForegroundColor Gray
    }
}

# --- Main Setup ---
Write-Host ">>> Setting up Portable Environment..." -ForegroundColor Green

# 1. Setup Go
Download-And-Extract -Url $GO_URL -DestDir "$TOOLS_DIR\go" -Name "go"
$env:GOROOT = "$TOOLS_DIR\go"
$env:PATH = "$TOOLS_DIR\go\bin;$env:PATH"

# 2. Setup Node
Download-And-Extract -Url $NODE_URL -DestDir "$TOOLS_DIR\node" -Name "node"
$env:PATH = "$TOOLS_DIR\node;$env:PATH"

# Verify versions
Write-Host "--- Environment Check ---"
go version
node --version
npm --version
Write-Host "-------------------------"

# --- Build Process ---



# ... (Configuration section remains same) ...

# --- Build Process ---

Write-Host ">>> Step 1: Building Backend (Go)..." -ForegroundColor Green
$goos = & go env GOOS
$goarch = & go env GOARCH
$binName = "ogs-swg-$goos-$goarch"
if ($goos -eq "windows") { $binName += ".exe" }
$procName = [System.IO.Path]::GetFileNameWithoutExtension($binName)
$binPath = Join-Path $BUILD_DIR $binName

# Optimization: Skip vendoring if vendor dir exists and not forced
if ($ForceBuild -or -not (Test-Path "vendor")) {
    Write-Host "    Tidying modules..."
    go mod tidy
    Write-Host "    Vendoring dependencies..."
    go mod vendor
} else {
    Write-Host "    Skipping dependency check (vendor exists). Use -ForceBuild to update." -ForegroundColor Gray
}

# Always build the binary (it's fast), but we could skip if the binary exists
Write-Host "    Building binary..."
if (-not (Test-Path $BUILD_DIR)) { New-Item -ItemType Directory -Path $BUILD_DIR | Out-Null }
go build -mod=vendor -o $binPath main.go

if (-not (Test-Path $binPath)) {
    Write-Error "Build failed! $binPath was not created."
}

Write-Host ">>> Step 2: Building Frontend (React)..." -ForegroundColor Green
Set-Location frontend

# Optimization: Skip npm install if node_modules exists, unless forced
if ($ForceBuild -or -not (Test-Path "node_modules")) {
    Write-Host "    Installing npm dependencies locally..."
    npm install
}

# Optimization: Skip npm build if dist exists and not forced
if ($ForceBuild -or -not (Test-Path "dist")) {
    Write-Host "    Building static assets..."
    npm run build
} else {
    Write-Host "    Skipping frontend build (dist exists). Use -ForceBuild to rebuild." -ForegroundColor Gray
}

Set-Location ..
if (Test-Path "frontend\\dist") {
    $frontendOut = Join-Path $BUILD_DIR "frontend"
    if (Test-Path $frontendOut) { Remove-Item $frontendOut -Recurse -Force }
    New-Item -ItemType Directory -Path $frontendOut | Out-Null
    Copy-Item "frontend\\dist\\*" $frontendOut -Recurse -Force
}

Write-Host ">>> Step 3: Preparing Environment..." -ForegroundColor Green

# Kill existing process if running
$ExistingProcess = Get-Process -Name $procName -ErrorAction SilentlyContinue
if ($ExistingProcess) {
    Write-Host "    Stopping existing $procName process..." -ForegroundColor Yellow
    Stop-Process -InputObject $ExistingProcess -Force
    Start-Sleep -Seconds 1
}

$TEST_DB = "./stats.db"
$XRAY_CONFIG = "./config.json"
$ACCESS_LOG = "./access.log"

# Create empty DB if not exists (app will init schema)
if (-not (Test-Path $TEST_DB)) {
    New-Item -ItemType File -Path $TEST_DB -Force | Out-Null
}
if (-not (Test-Path $ACCESS_LOG)) {
    New-Item -ItemType File -Path $ACCESS_LOG -Force | Out-Null
}

Write-Host ">>> Step 4: Starting SWG..." -ForegroundColor Green
Write-Host "-----------------------------------------------------"
Write-Host "App running at: http://localhost:8080"
Write-Host "Press Ctrl+C to stop."
Write-Host "-----------------------------------------------------"

& $binPath --config $XRAY_CONFIG --log $ACCESS_LOG --db $TEST_DB
