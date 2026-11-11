$ErrorActionPreference = 'Stop'

$projectRoot = $PSScriptRoot
$outputPath = Join-Path $projectRoot 'terminal.exe'

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go was not found in PATH. Please install Go and try again.'
}

Push-Location $projectRoot
try {
    Write-Host 'Building terminal.exe...'
    & go build -ldflags '-H=windowsgui' -o $outputPath ./cmd/terminal
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed with exit code $LASTEXITCODE."
    }

    Write-Host "Build succeeded: $outputPath" -ForegroundColor Green
}
finally {
    Pop-Location
}
