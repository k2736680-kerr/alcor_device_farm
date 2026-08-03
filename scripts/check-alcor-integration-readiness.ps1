param(
    [string]$AlcorRoot = "E:\AutoTestTools\Projects\Alcor",
    [string]$Ref = "origin/feature/refactoring",
    [switch]$RequireReady
)

$ErrorActionPreference = "Stop"
if (-not (Test-Path -LiteralPath $AlcorRoot -PathType Container)) {
    throw "AlcorRoot does not exist: $AlcorRoot"
}

function Read-GitFile {
    param([string]$Path)
    $Value = & git -C $AlcorRoot show ("{0}:{1}" -f $Ref, $Path) 2>$null
    if ($LASTEXITCODE -ne 0) {
        return ""
    }
    return ($Value -join "`n")
}

$Commit = (& git -C $AlcorRoot rev-parse $Ref).Trim()
if ($LASTEXITCODE -ne 0 -or -not $Commit) {
    throw "Cannot resolve Alcor ref: $Ref"
}

$Swagger = Read-GitFile "alcor_server/docs/swagger.yaml"
$Worker = Read-GitFile "alcor_server/internal/platform/worker.go"
$Config = (Read-GitFile "alcor_server/pkg/config/config.go") + "`n" + (Read-GitFile "alcor_server/config/platform.secrets.yaml.example")

$Checks = @(
    [pscustomobject]@{ Name = "RunAttempt database/worker model"; Passed = $Worker -match "platform_run_attempts"; Detail = "Worker must claim UUID RunAttempt records" },
    [pscustomobject]@{ Name = "Run/RunAttempt published OpenAPI"; Passed = ($Swagger -match "(?m)^\s{2}/runs:") -and ($Swagger -match "(?i)run.?attempt"); Detail = "Swagger must publish the actual Run and Attempt contract" },
    [pscustomobject]@{ Name = "Device Farm worker adapter"; Passed = $Worker -match "(?i)device.?farm"; Detail = "Worker must contain an explicit Device Farm adapter boundary" },
    [pscustomobject]@{ Name = "Device Farm configuration"; Passed = $Config -match "(?i)device.?farm"; Detail = "Endpoint, service credential reference, pool and timeout configuration must exist" },
    [pscustomobject]@{ Name = "X-Eval correlation headers"; Passed = ($Worker -match "X-Eval-Run-Id") -and ($Worker -match "X-Eval-Attempt-Id"); Detail = "Worker headers must match the approved platform plan" }
)

Write-Output "ALCOR_REF=$Ref"
Write-Output "ALCOR_COMMIT=$Commit"
foreach ($Check in $Checks) {
    $State = if ($Check.Passed) { "PASS" } else { "WAIT" }
    Write-Output ("{0} {1}: {2}" -f $State, $Check.Name, $Check.Detail)
}

$Ready = @($Checks | Where-Object { -not $_.Passed }).Count -eq 0
Write-Output "ALCOR_DEVICE_FARM_READY=$($Ready.ToString().ToLowerInvariant())"
if ($RequireReady -and -not $Ready) {
    throw "Alcor Device Farm integration prerequisites are incomplete"
}
