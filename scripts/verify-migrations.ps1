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

function Invoke-GoTest {
    param(
        [string]$GoExecutable,
        [string]$Package,
        [string]$FailureMessage
    )

    # Convert native output into PowerShell host output so Start-Transcript in
    # the acceptance wrapper can record it. PostgreSQL is already running at
    # this point, so this pipeline cannot leak a startup pipe into the server.
    & $GoExecutable test -count=1 -v $Package 2>&1 | ForEach-Object { Write-Output $_ }
    if ($LASTEXITCODE -ne 0) {
        throw $FailureMessage
    }
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

    $PlatformMigration = $UpFiles | Where-Object Name -EQ "000014_platform_neutral_device_domain.up.sql" | Select-Object -First 1
    if (-not $PlatformMigration) {
        throw "Platform-neutral migration is missing."
    }
    foreach ($Migration in $UpFiles | Where-Object Name -LT $PlatformMigration.Name) { Invoke-SQLFile $Migration.FullName }
    Invoke-SQL @"
INSERT INTO device_images(id,name,docker_image,docker_digest,api_level,abi,resolution,status)
VALUES('legacy_image_00000001','legacy-android-image','registry.example/alcor/android-emulator:legacy',
'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',34,'x86_64','1080x2400','ready');
INSERT INTO device_hosts(id,name,host_type,status)
VALUES('legacy_host_000000001','legacy-android-host','docker_emulator','online');
INSERT INTO device_pools(id,name,default_lease_seconds,max_lease_seconds,max_concurrency,total_target,min_ready)
VALUES('legacy_pool_000000001','legacy-android-pool',600,3600,1,1,1);
INSERT INTO devices(id,host_id,image_id,device_kind,provider_type,provider_ref,lifecycle_mode,serial,lifecycle_status,health_status)
VALUES('legacy_device_000001','legacy_host_000000001','legacy_image_00000001','emulator','mock','legacy-provider','rebuild','legacy-serial','ready','healthy');
INSERT INTO device_pool_devices(pool_id,device_id,enabled)
VALUES('legacy_pool_000000001','legacy_device_000001',true);
"@
    Invoke-SQLFile $PlatformMigration.FullName
    Invoke-SQL "DO `$test`$ BEGIN IF EXISTS (SELECT 1 FROM device_hosts WHERE id='legacy_host_000000001' AND (host_os<>'linux' OR host_arch<>'unknown')) OR EXISTS (SELECT 1 FROM device_pools WHERE id='legacy_pool_000000001' AND platform<>'android') OR EXISTS (SELECT 1 FROM devices WHERE id='legacy_device_000001' AND platform<>'android') THEN RAISE EXCEPTION 'legacy Android backfill failed'; END IF; END `$test`$;"
    Write-Output "legacy Android backfill: passed"
    Invoke-SQL "DELETE FROM device_pool_devices WHERE device_id='legacy_device_000001'; DELETE FROM devices WHERE id='legacy_device_000001'; DELETE FROM device_pools WHERE id='legacy_pool_000000001'; DELETE FROM device_hosts WHERE id='legacy_host_000000001'; DELETE FROM device_images WHERE id='legacy_image_00000001';"
    foreach ($Migration in $UpFiles | Where-Object Name -GT $PlatformMigration.Name) { Invoke-SQLFile $Migration.FullName }
    Invoke-SQLFile $Constraints
    Write-Output "constraint checks: passed"

    foreach ($Migration in $DownFiles) { Invoke-SQLFile $Migration.FullName }
    Invoke-SQL "DO `$test`$ BEGIN IF EXISTS (SELECT 1 FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename LIKE 'device_%') THEN RAISE EXCEPTION 'down migration left device tables'; END IF; END `$test`$;"
    Write-Output "down migration: passed"

    foreach ($Migration in $UpFiles) { Invoke-SQLFile $Migration.FullName }
    Invoke-SQL "DO `$test`$ DECLARE table_count integer; BEGIN SELECT count(*) INTO table_count FROM pg_catalog.pg_tables WHERE schemaname='public' AND tablename LIKE 'device_%'; IF table_count <> 15 THEN RAISE EXCEPTION 'expected 15 device tables, got %', table_count; END IF; END `$test`$;"
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
            Invoke-GoTest $GoExecutable "./internal/repository" "Repository integration tests failed."
            Invoke-GoTest $GoExecutable "./internal/scheduler" "Scheduler integration tests failed."
            Invoke-GoTest $GoExecutable "./internal/reaper" "Reservation lease and Reaper integration tests failed."
            Invoke-GoTest $GoExecutable "./internal/reconcile" "Reconciler and health integration tests failed."
            Invoke-GoTest $GoExecutable "./internal/hostcommand" "Host command protocol integration tests failed."
            Invoke-GoTest $GoExecutable "./internal/metrics" "Metrics integration tests failed."
            Invoke-GoTest $GoExecutable "./internal/api" "Management API integration tests failed."
            Invoke-GoTest $GoExecutable "./internal/warmpool" "Warm pool controller integration tests failed."
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
