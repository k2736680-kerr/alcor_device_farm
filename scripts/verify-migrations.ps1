param(
    [string]$PostgresBin = $env:DEVICE_FARM_POSTGRES_BIN,
    [int]$Port = 55432,
    [switch]$RunRepositoryTests
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

    $UpFiles = Get-ChildItem -LiteralPath (Join-Path $ProjectRoot "migrations") -Filter "*.up.sql" -File | Sort-Object Name
    $DownFiles = Get-ChildItem -LiteralPath (Join-Path $ProjectRoot "migrations") -Filter "*.down.sql" -File | Sort-Object Name -Descending
    $Constraints = Join-Path $ProjectRoot "migrations/test/constraints.sql"

    foreach ($Migration in $UpFiles) { Invoke-SQLFile $Migration.FullName }
    Invoke-SQLFile $Constraints
    Write-Output "constraint checks: passed"

    foreach ($Migration in $DownFiles) { Invoke-SQLFile $Migration.FullName }
    Invoke-SQL "DO `$test`$ BEGIN IF EXISTS (SELECT 1 FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename LIKE 'device_%') THEN RAISE EXCEPTION 'down migration left device tables'; END IF; END `$test`$;"
    Write-Output "down migration: passed"

    foreach ($Migration in $UpFiles) { Invoke-SQLFile $Migration.FullName }
    Invoke-SQL "DO `$test`$ DECLARE table_count integer; BEGIN SELECT count(*) INTO table_count FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename LIKE 'device_%'; IF table_count <> 12 THEN RAISE EXCEPTION 'expected 12 device tables, got %', table_count; END IF; END `$test`$;"
    Write-Output "up-down-up migration: passed"

    if ($RunRepositoryTests) {
        $GoExecutable = $env:DEVICE_FARM_GO
        if ([string]::IsNullOrWhiteSpace($GoExecutable)) {
            $GoCommand = Get-Command go -ErrorAction SilentlyContinue | Select-Object -First 1
            if (-not $GoCommand) {
                throw "Set DEVICE_FARM_GO before running repository tests."
            }
            $GoExecutable = $GoCommand.Source
        }
        $PreviousDatabaseURL = $env:DEVICE_FARM_TEST_DATABASE_URL
        try {
            $env:DEVICE_FARM_TEST_DATABASE_URL = "postgres://postgres@127.0.0.1:$Port/$DatabaseName`?sslmode=disable"
            & $GoExecutable test -count=1 -v ./internal/repository
            if ($LASTEXITCODE -ne 0) {
                throw "Repository integration tests failed."
            }
            & $GoExecutable test -count=1 -v ./internal/scheduler
            if ($LASTEXITCODE -ne 0) {
                throw "Scheduler integration tests failed."
            }
            & $GoExecutable test -count=1 -v ./internal/reaper
            if ($LASTEXITCODE -ne 0) {
                throw "Reservation lease and Reaper integration tests failed."
            }
            & $GoExecutable test -count=1 -v ./internal/reconcile
            if ($LASTEXITCODE -ne 0) {
                throw "Reconciler and health integration tests failed."
            }
            & $GoExecutable test -count=1 -v ./internal/api
            if ($LASTEXITCODE -ne 0) {
                throw "Management API integration tests failed."
            }
        }
        finally {
            $env:DEVICE_FARM_TEST_DATABASE_URL = $PreviousDatabaseURL
        }
    }
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
