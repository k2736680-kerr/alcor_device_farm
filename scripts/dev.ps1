param(
    [ValidateSet("build", "test", "fmt", "check")]
    [string]$Task = "check"
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot

function Resolve-GoExecutable {
    if ($env:DEVICE_FARM_GO) {
        if (-not (Test-Path -LiteralPath $env:DEVICE_FARM_GO -PathType Leaf)) {
            throw "DEVICE_FARM_GO does not point to a Go executable."
        }
        return $env:DEVICE_FARM_GO
    }

    $GoCommand = Get-Command go -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($GoCommand) {
        return $GoCommand.Source
    }

    throw "Go was not found. Set DEVICE_FARM_GO to go.exe or add Go to PATH."
}

function Invoke-Go {
    param([string[]]$Arguments)

    & $script:GoExecutable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Go command failed: go $($Arguments -join ' ')"
    }
}

function Invoke-FormatCheck {
    $Unformatted = @()
    $Files = Get-ChildItem -LiteralPath $ProjectRoot -Recurse -Filter "*.go" -File |
        Where-Object { $_.FullName -notmatch '[\\/]vendor[\\/]' }

    foreach ($File in $Files) {
        $Result = & $script:GofmtExecutable -l $File.FullName
        if ($LASTEXITCODE -ne 0) {
            throw "Unable to check formatting for $($File.FullName)."
        }
        if ($Result) {
            $Unformatted += $File.FullName
        }
    }

    if ($Unformatted.Count -gt 0) {
        throw "Go files were not formatted: $($Unformatted -join ', ')"
    }
}

function Invoke-ImplementationEvidenceCheck {
    & (Join-Path $PSScriptRoot "verify-implementation-evidence.ps1")
    if (-not $?) {
        throw "Implementation evidence gate failed."
    }
}

$script:GoExecutable = Resolve-GoExecutable
$script:GofmtExecutable = Join-Path (Split-Path -Parent $script:GoExecutable) "gofmt.exe"
if (-not (Test-Path -LiteralPath $script:GofmtExecutable -PathType Leaf)) {
    throw "gofmt was not found next to the configured Go executable."
}
Push-Location $ProjectRoot
try {
    switch ($Task) {
        "build" {
            New-Item -ItemType Directory -Path "bin" -Force | Out-Null
            Invoke-Go -Arguments @("build", "-o", "bin/device-farm-server.exe", "./cmd/device-farm-server")
            Invoke-Go -Arguments @("build", "-o", "bin/device-host-agent.exe", "./cmd/device-host-agent")
            Invoke-Go -Arguments @("build", "-o", "bin/dafit-farm-harness.exe", "./cmd/dafit-farm-harness")
            Invoke-Go -Arguments @("build", "-o", "bin/device-farm-adapter-mock.exe", "./cmd/device-farm-adapter-mock")
        }
        "test" {
            Invoke-Go -Arguments @("test", "./...")
        }
        "fmt" {
            Invoke-Go -Arguments @("fmt", "./...")
        }
        "check" {
            Invoke-FormatCheck
            Invoke-Go -Arguments @("vet", "./...")
            Invoke-Go -Arguments @("test", "./...")
            New-Item -ItemType Directory -Path "bin" -Force | Out-Null
            Invoke-Go -Arguments @("build", "-o", "bin/device-farm-server.exe", "./cmd/device-farm-server")
            Invoke-Go -Arguments @("build", "-o", "bin/device-host-agent.exe", "./cmd/device-host-agent")
            Invoke-Go -Arguments @("build", "-o", "bin/dafit-farm-harness.exe", "./cmd/dafit-farm-harness")
            Invoke-Go -Arguments @("build", "-o", "bin/device-farm-adapter-mock.exe", "./cmd/device-farm-adapter-mock")
            Invoke-ImplementationEvidenceCheck
        }
    }
}
finally {
    Pop-Location
}
