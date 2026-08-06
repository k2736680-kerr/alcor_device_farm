param(
    [string]$DatabaseURL = "postgres://postgres@127.0.0.1:55432/device_farm_df004?sslmode=disable",
    [string]$PostgresBin = $env:DEVICE_FARM_POSTGRES_BIN
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ConsoleRoot = Join-Path $ProjectRoot "console"
$FixtureRoot = Join-Path $ConsoleRoot "e2e/fixtures"
$RuntimeRoot = Join-Path $ProjectRoot "tmp/console-local-e2e"

function Resolve-Executable {
    param(
        [string]$EnvironmentValue,
        [string]$CommandName,
        [string]$ErrorMessage
    )

    if ($EnvironmentValue) {
        if (-not (Test-Path -LiteralPath $EnvironmentValue -PathType Leaf)) {
            throw $ErrorMessage
        }
        return $EnvironmentValue
    }
    $Command = Get-Command $CommandName -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($Command) {
        return $Command.Source
    }
    throw $ErrorMessage
}

function Invoke-Checked {
    param(
        [string]$Executable,
        [string[]]$Arguments
    )

    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Executable failed with exit code $LASTEXITCODE"
    }
}

$DatabaseURI = [Uri]$DatabaseURL
$DatabaseName = $DatabaseURI.AbsolutePath.TrimStart("/")
if ($DatabaseURI.Scheme -notin @("postgres", "postgresql") -or
    $DatabaseURI.Host -notin @("127.0.0.1", "localhost", "::1") -or
    $DatabaseName -notmatch '^device_farm_[A-Za-z0-9_]+$') {
    throw "The local E2E reset only accepts a loopback PostgreSQL URL whose database name starts with device_farm_."
}

$GoExecutable = Resolve-Executable -EnvironmentValue $env:DEVICE_FARM_GO -CommandName "go" -ErrorMessage "Set DEVICE_FARM_GO to go.exe or add Go to PATH."
$PnpmExecutable = Resolve-Executable -EnvironmentValue $env:DEVICE_FARM_PNPM -CommandName "pnpm" -ErrorMessage "Set DEVICE_FARM_PNPM to pnpm or add pnpm to PATH."
$NodeExecutable = Resolve-Executable -EnvironmentValue $env:DEVICE_FARM_NODE -CommandName "node" -ErrorMessage "Set DEVICE_FARM_NODE to node or add Node.js to PATH."
if ($PostgresBin) {
    $PsqlExecutable = Join-Path $PostgresBin "psql.exe"
    if (-not (Test-Path -LiteralPath $PsqlExecutable -PathType Leaf)) {
        $PsqlExecutable = Join-Path $PostgresBin "psql"
    }
} else {
    $PsqlExecutable = (Get-Command psql -ErrorAction SilentlyContinue | Select-Object -First 1).Source
}
if (-not $PsqlExecutable -or -not (Test-Path -LiteralPath $PsqlExecutable -PathType Leaf)) {
    throw "Set DEVICE_FARM_POSTGRES_BIN or -PostgresBin to a PostgreSQL bin directory containing psql."
}

foreach ($Port in @(18080, 18083)) {
    if (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue) {
        throw "TCP port $Port is already in use; stop the existing local E2E service first."
    }
}

New-Item -ItemType Directory -Path $RuntimeRoot -Force | Out-Null
$ServerBinary = Join-Path $RuntimeRoot $(if ($IsWindows -or $env:OS -eq "Windows_NT") { "df-server.exe" } else { "df-server" })
$ServerConfig = Join-Path $RuntimeRoot "server-config.yaml"
$UsersFile = (Join-Path $FixtureRoot "console-users.yaml").Replace("\", "/")
$MockLog = Join-Path $RuntimeRoot "mock-stf.log"
$MockError = Join-Path $RuntimeRoot "mock-stf.err"

$Config = @"
server:
  address: "127.0.0.1:18080"
database:
  url: "$DatabaseURL"
console:
  enabled: true
  users_file: "$UsersFile"
  development_insecure: true
security:
  service_token: "local-e2e-service-token"
  agent_token: "local-e2e-agent-token"
reconcile:
  interval: 1h
  host_timeout: 2h
  failure_threshold: 1000
"@
[System.IO.File]::WriteAllText($ServerConfig, $Config, [System.Text.UTF8Encoding]::new($false))

$SavedEnvironment = @{}
foreach ($Name in @("NODE_OPTIONS", "DEVICE_FARM_STF_ENABLED", "DEVICE_FARM_STF_BASE_URL", "DEVICE_FARM_STF_API_TOKEN", "DEVICE_FARM_E2E_SERVER_BINARY", "DEVICE_FARM_E2E_SERVER_CONFIG", "DEVICE_FARM_E2E_REUSE_SERVER")) {
    $SavedEnvironment[$Name] = [Environment]::GetEnvironmentVariable($Name, "Process")
}

$MockProcess = $null
try {
    Push-Location $ProjectRoot
    try {
        Invoke-Checked -Executable $PnpmExecutable -Arguments @("--dir", "console", "install", "--frozen-lockfile")
        Invoke-Checked -Executable $PnpmExecutable -Arguments @("--dir", "console", "build")
        Invoke-Checked -Executable $GoExecutable -Arguments @("build", "-o", $ServerBinary, "./cmd/device-farm-server")
        Invoke-Checked -Executable $PsqlExecutable -Arguments @($DatabaseURL, "-X", "--set", "ON_ERROR_STOP=1", "--file", (Join-Path $FixtureRoot "seed.sql"))

        $MockProcess = Start-Process -FilePath $NodeExecutable -ArgumentList (Join-Path $FixtureRoot "mock-stf.mjs") `
            -WorkingDirectory $ProjectRoot -WindowStyle Hidden -RedirectStandardOutput $MockLog `
            -RedirectStandardError $MockError -PassThru
        $Deadline = (Get-Date).AddSeconds(10)
        do {
            Start-Sleep -Milliseconds 200
            $MockListener = Get-NetTCPConnection -State Listen -LocalPort 18083 -ErrorAction SilentlyContinue
        } until ($MockListener -or (Get-Date) -ge $Deadline)
        if (-not $MockListener) {
            throw "Mock STF did not listen on 127.0.0.1:18083; see $MockError"
        }

        $env:NODE_OPTIONS = ""
        $env:DEVICE_FARM_STF_ENABLED = "true"
        $env:DEVICE_FARM_STF_BASE_URL = "http://127.0.0.1:18083"
        $env:DEVICE_FARM_STF_API_TOKEN = "local-e2e-stf-token"
        $env:DEVICE_FARM_E2E_SERVER_BINARY = $ServerBinary
        $env:DEVICE_FARM_E2E_SERVER_CONFIG = $ServerConfig
        $env:DEVICE_FARM_E2E_REUSE_SERVER = "0"
        Invoke-Checked -Executable $PnpmExecutable -Arguments @("--dir", "console", "test:e2e")

        $CleanupQuery = "SELECT count(*) FROM device_reservations WHERE status IN ('pending','active'); SELECT count(*) FROM device_console_sessions WHERE revoked_at IS NULL AND expires_at > NOW(); SELECT count(*) FROM devices WHERE lifecycle_status IN ('reserved','busy');"
        $CleanupCounts = & $PsqlExecutable $DatabaseURL -X -A -t --set ON_ERROR_STOP=1 --command $CleanupQuery
        if ($LASTEXITCODE -ne 0 -or @($CleanupCounts | Where-Object { $_.Trim() -ne "0" }).Count -ne 0) {
            throw "Local E2E cleanup verification failed: $($CleanupCounts -join ', ')"
        }
        Write-Output "Console local E2E passed; open reservations, live sessions and reserved/busy devices are zero."
    } finally {
        Pop-Location
    }
} finally {
    if ($MockProcess -and -not $MockProcess.HasExited) {
        Stop-Process -Id $MockProcess.Id
        $MockProcess.WaitForExit(5000) | Out-Null
    }
    foreach ($Name in $SavedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($Name, $SavedEnvironment[$Name], "Process")
    }
}
