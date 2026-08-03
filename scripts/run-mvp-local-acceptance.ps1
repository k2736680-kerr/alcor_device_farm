param(
    [string]$GoExecutable = $env:DEVICE_FARM_GO,
    [string]$PostgresBin = $env:DEVICE_FARM_POSTGRES_BIN,
    [string]$PythonExecutable = "D:\python\python.exe",
    [string]$DaFitRoot = "E:\AutoTestTools\Projects\dafit_auto_platform",
    [string]$OutputFile = ""
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
if (-not $OutputFile) {
    $OutputFile = Join-Path $ProjectRoot "docs/evidence/mvp-acceptance/artifacts/local-gate.txt"
}
$OutputFile = [System.IO.Path]::GetFullPath($OutputFile)
$AllowedRoot = [System.IO.Path]::GetFullPath((Join-Path $ProjectRoot "docs/evidence/mvp-acceptance/artifacts"))
if (-not $OutputFile.StartsWith($AllowedRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "OutputFile must stay inside docs/evidence/mvp-acceptance/artifacts."
}
if (-not (Test-Path -LiteralPath $GoExecutable -PathType Leaf)) {
    throw "GoExecutable is required."
}
if (-not (Test-Path -LiteralPath $PostgresBin -PathType Container)) {
    throw "PostgresBin is required."
}
if (-not (Test-Path -LiteralPath $PythonExecutable -PathType Leaf)) {
    throw "PythonExecutable is required."
}
if (-not (Test-Path -LiteralPath $DaFitRoot -PathType Container)) {
    throw "DaFitRoot is required."
}

New-Item -ItemType Directory -Path (Split-Path -Parent $OutputFile) -Force | Out-Null
$Commit = (& git -C $ProjectRoot rev-parse HEAD).Trim()

function Write-AcceptanceLog {
    param([Parameter(ValueFromPipeline = $true)][AllowEmptyString()][string]$Message)

    process {
        Write-Host $Message
        Add-Content -LiteralPath $OutputFile -Value $Message -Encoding UTF8
    }
}

function Invoke-LoggedCommand {
    param(
        [Parameter(Mandatory = $true)][scriptblock]$Command,
        [Parameter(Mandatory = $true)][string]$FailureMessage
    )

    # Do not pipe the command output. PostgreSQL started by pg_ctl inherits a
    # pipeline handle on Windows and keeps that pipe open until the server is
    # stopped, which deadlocks the acceptance wrapper. Transcription captures
    # the console output without changing child-process stdout handles.
    $TranscriptPath = Join-Path (Split-Path -Parent $OutputFile) ("command-{0}.transcript.txt" -f ([guid]::NewGuid().ToString("N")))
    $CommandError = $null
    $CommandExitCode = 0
    Start-Transcript -LiteralPath $TranscriptPath -Force | Out-Null
    try {
        & $Command
        $CommandExitCode = $LASTEXITCODE
    }
    catch {
        $CommandError = $_
    }
    finally {
        Stop-Transcript | Out-Null
    }

    Get-Content -LiteralPath $TranscriptPath | Add-Content -LiteralPath $OutputFile -Encoding UTF8
    Remove-Item -LiteralPath $TranscriptPath -Force

    if ($CommandError) {
        throw $CommandError
    }
    if ($null -ne $CommandExitCode -and $CommandExitCode -ne 0) {
        throw $FailureMessage
    }
}

function Invoke-PipelinedLoggedCommand {
    param(
        [Parameter(Mandatory = $true)][scriptblock]$Command,
        [Parameter(Mandatory = $true)][string]$FailureMessage
    )

    & $Command 2>&1 | ForEach-Object { Write-AcceptanceLog ([string]$_) }
    if ($LASTEXITCODE -ne 0) {
        throw $FailureMessage
    }
}

@(
    "Device Farm MVP local acceptance",
    "timestamp_utc=$([DateTime]::UtcNow.ToString('o'))",
    "commit=$Commit",
    "environment=E0 Windows Mock + temporary PostgreSQL",
    ""
) | Set-Content -LiteralPath $OutputFile -Encoding UTF8

$env:DEVICE_FARM_GO = $GoExecutable
$env:DEVICE_FARM_POSTGRES_BIN = $PostgresBin

Push-Location $ProjectRoot
try {
    Write-AcceptanceLog "== Device Farm full gate =="
    Invoke-PipelinedLoggedCommand -Command { & .\scripts\dev.ps1 -Task check } -FailureMessage "Device Farm full gate failed."

    Write-AcceptanceLog "== PostgreSQL migration and integration gate =="
    Invoke-LoggedCommand -Command { & .\scripts\verify-migrations.ps1 -RunRepositoryTests } -FailureMessage "PostgreSQL integration gate failed."

    Write-AcceptanceLog "== DaFit current collect-only baseline =="
    Push-Location $DaFitRoot
    try {
        $PreviousPythonIOEncoding = $env:PYTHONIOENCODING
        $env:PYTHONIOENCODING = "utf-8"
        Invoke-PipelinedLoggedCommand -Command { & $PythonExecutable tools/run_full.py --collect-only } -FailureMessage "DaFit collect-only failed."
    }
    finally {
        $env:PYTHONIOENCODING = $PreviousPythonIOEncoding
        Pop-Location
    }

    Write-AcceptanceLog "LOCAL_ACCEPTANCE=PASS"
}
catch {
    Write-AcceptanceLog "LOCAL_ACCEPTANCE=FAIL"
    throw
}
finally {
    Pop-Location
}
