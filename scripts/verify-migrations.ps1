param(
    [string]$PostgresBin = $env:DEVICE_FARM_POSTGRES_BIN,
    [int]$Port = 55432
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$TempRoot = [System.IO.Path]::GetFullPath((Join-Path $ProjectRoot "tmp"))
$DataDirectory = [System.IO.Path]::GetFullPath((Join-Path $TempRoot "df004-postgres-data"))
$LogPath = [System.IO.Path]::GetFullPath((Join-Path $TempRoot "df004-postgres.log"))
$DatabaseName = "device_farm_df004"

if ([string]::IsNullOrWhiteSpace($PostgresBin)) {
    throw "Set DEVICE_FARM_POSTGRES_BIN to the PostgreSQL bin directory."
}
$PostgresBin = [System.IO.Path]::GetFullPath($PostgresBin)

foreach ($name in @("initdb.exe", "pg_ctl.exe", "createdb.exe", "psql.exe")) {
    if (-not (Test-Path -LiteralPath (Join-Path $PostgresBin $name) -PathType Leaf)) {
        throw "PostgreSQL tool is missing: $name"
    }
}
if (-not $DataDirectory.StartsWith($TempRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to use a data directory outside the project tmp directory."
}

function Invoke-PostgresTool {
    param(
        [string]$Name,
        [string[]]$Arguments
    )
    & (Join-Path $PostgresBin $Name) @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE."
    }
}

function Invoke-SQLFile {
    param([string]$Path)
    Invoke-PostgresTool "psql.exe" @(
        "-X", "--set", "ON_ERROR_STOP=1", "--host", "127.0.0.1", "--port", "$Port",
        "--username", "postgres", "--dbname", $DatabaseName, "--file", $Path
    )
}

function Invoke-SQL {
    param([string]$Statement)
    Invoke-PostgresTool "psql.exe" @(
        "-X", "--set", "ON_ERROR_STOP=1", "--host", "127.0.0.1", "--port", "$Port",
        "--username", "postgres", "--dbname", $DatabaseName, "--command", $Statement
    )
}

$started = $false
New-Item -ItemType Directory -Path $TempRoot -Force | Out-Null
if (Test-Path -LiteralPath $DataDirectory) {
    throw "Temporary PostgreSQL data directory already exists: $DataDirectory"
}

try {
    Invoke-PostgresTool "initdb.exe" @(
        "--pgdata", $DataDirectory, "--username", "postgres", "--auth", "trust",
        "--encoding", "UTF8", "--locale", "C"
    )
    Invoke-PostgresTool "pg_ctl.exe" @(
        "--pgdata", $DataDirectory, "--log", $LogPath, "--wait", "start",
        "--options", "-h 127.0.0.1 -p $Port"
    )
    $started = $true

    Invoke-PostgresTool "createdb.exe" @(
        "--host", "127.0.0.1", "--port", "$Port", "--username", "postgres", $DatabaseName
    )

    $Up = Join-Path $ProjectRoot "migrations/000001_device_domain.up.sql"
    $Down = Join-Path $ProjectRoot "migrations/000001_device_domain.down.sql"
    $Constraints = Join-Path $ProjectRoot "migrations/test/constraints.sql"

    Invoke-SQLFile $Up
    Invoke-SQLFile $Constraints
    Write-Output "constraint checks: passed"

    Invoke-SQLFile $Down
    Invoke-SQL "DO `$test`$ BEGIN IF EXISTS (SELECT 1 FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename LIKE 'device_%') THEN RAISE EXCEPTION 'down migration left device tables'; END IF; END `$test`$;"
    Write-Output "down migration: passed"

    Invoke-SQLFile $Up
    Invoke-SQL "DO `$test`$ DECLARE table_count integer; BEGIN SELECT count(*) INTO table_count FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename LIKE 'device_%'; IF table_count <> 11 THEN RAISE EXCEPTION 'expected 11 device tables, got %', table_count; END IF; END `$test`$;"
    Write-Output "up-down-up migration: passed"
}
finally {
    if ($started) {
        & (Join-Path $PostgresBin "pg_ctl.exe") --pgdata $DataDirectory --wait --mode fast stop | Out-Null
    }
    if (Test-Path -LiteralPath $DataDirectory) {
        Remove-Item -LiteralPath $DataDirectory -Recurse -Force
    }
    if (Test-Path -LiteralPath $LogPath) {
        Remove-Item -LiteralPath $LogPath -Force
    }
}
