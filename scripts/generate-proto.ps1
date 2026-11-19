# Script to generate Protocol Buffer code (Windows port of generate-proto.sh)
# Usage: powershell -ExecutionPolicy Bypass -File scripts\generate-proto.ps1
$ErrorActionPreference = 'Stop'

function Invoke-Native {
    param([string]$Command, [string[]]$Arguments)
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "'$Command $($Arguments -join ' ')' failed with exit code $LASTEXITCODE"
    }
}

# Run from the repository root, like the bash script expects.
Push-Location (Join-Path $PSScriptRoot '..')
try {
    $goBin = Join-Path (& go env GOPATH) 'bin'
    $env:PATH = "$env:PATH;$goBin"

    # Check for protoc
    if (-not (Get-Command protoc -ErrorAction SilentlyContinue)) {
        Write-Host 'protoc not found. Please install Protocol Buffers compiler:'
        Write-Host '  winget: winget install protobuf'
        Write-Host '  Scoop:  scoop install protobuf'
        Write-Host '  Choco:  choco install protoc'
        exit 1
    }

    # Install Go protobuf plugin if not present
    if (-not (Get-Command protoc-gen-go -ErrorAction SilentlyContinue)) {
        Write-Host 'Installing protoc-gen-go...'
        Invoke-Native go @('install', 'google.golang.org/protobuf/cmd/protoc-gen-go@latest')
    }

    Invoke-Native npm @('ci')

    # Generate Go code from proto files
    Write-Host 'Generating Protocol Buffer code...'
    Invoke-Native protoc @(
        '--go_out=internal/protocol',
        '--go_opt=paths=source_relative',
        '--proto_path=internal/protocol',
        'internal/protocol/messages.proto'
    )

    # Generate TypeScript code with JSON support
    Write-Host 'Generating TypeScript Protocol Buffer code...'
    Invoke-Native npx @(
        'protoc',
        '--ts_out=internal/protocol',
        '--proto_path=internal/protocol',
        'internal/protocol/messages.proto'
    )
}
finally {
    Pop-Location
}
