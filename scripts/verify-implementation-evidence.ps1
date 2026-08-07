param(
    [string]$ProjectRoot = (Split-Path -Parent $PSScriptRoot)
)

$ErrorActionPreference = "Stop"
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path
$PlanPath = Join-Path $ProjectRoot "docs/05_step_by_step_implementation.md"
$ManifestPath = Join-Path $ProjectRoot "docs/evidence/implementation_manifest.md"
$Plan = Get-Content -LiteralPath $PlanPath -Raw -Encoding UTF8
$Manifest = Get-Content -LiteralPath $ManifestPath -Raw -Encoding UTF8
$TaskIDs = 3..30 | ForEach-Object { "DF-{0:D3}" -f $_ }
$ImplementedStatuses = @("completed", "blocked")
$PlannedStatuses = @("pending", "in_progress")
$UnblockHeading = -join ([char[]](0x963B, 0x585E, 0x89E3, 0x9664, 0x6761, 0x4EF6))

function Read-TaskStatuses {
    param(
        [string]$Document,
        [string]$Pattern,
        [string]$SourceName
    )

    $Statuses = @{}
    foreach ($Match in [regex]::Matches($Document, $Pattern, [System.Text.RegularExpressions.RegexOptions]::Multiline)) {
        $TaskID = $Match.Groups[1].Value
        if ($Statuses.ContainsKey($TaskID)) {
            throw "$TaskID has duplicate status rows in $SourceName"
        }
        $Statuses[$TaskID] = $Match.Groups[2].Value
    }
    return $Statuses
}

function Assert-CommitIsReachable {
    param(
        [string]$Commit,
        [string]$TaskID
    )

    & git -C $ProjectRoot cat-file -e "$Commit`^{commit}" 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "$TaskID references missing commit $Commit"
    }
    & git -C $ProjectRoot merge-base --is-ancestor $Commit HEAD
    if ($LASTEXITCODE -ne 0) {
        throw "$TaskID commit $Commit is not reachable from HEAD"
    }
}

$PlanStatuses = Read-TaskStatuses -Document $Plan `
    -Pattern '^\|\s*(DF-\d{3})\s*\|[^\r\n]*?\|\s*(completed|blocked|pending|in_progress|waiting_external)\s*\|' `
    -SourceName "docs/05_step_by_step_implementation.md"
$ManifestStatuses = Read-TaskStatuses -Document $Manifest `
    -Pattern '^\|\s*(DF-\d{3})\s*\|\s*(completed|blocked|pending|in_progress)\s*\|' `
    -SourceName "docs/evidence/implementation_manifest.md"

foreach ($TaskID in $TaskIDs) {
    if (-not $PlanStatuses.ContainsKey($TaskID)) {
        throw "$TaskID is missing from docs/05_step_by_step_implementation.md"
    }
    $Status = $PlanStatuses[$TaskID]
    if ($Status -notin ($ImplementedStatuses + $PlannedStatuses)) {
        throw "$TaskID has unsupported evidence status $Status"
    }
    if (-not $ManifestStatuses.ContainsKey($TaskID)) {
        throw "$TaskID is missing from docs/evidence/implementation_manifest.md"
    }
    if ($ManifestStatuses[$TaskID] -ne $Status) {
        throw "$TaskID status mismatch: plan=$Status manifest=$($ManifestStatuses[$TaskID])"
    }

    $EvidenceRelativePath = "docs/evidence/$TaskID/acceptance.md"
    $EvidencePath = Join-Path $ProjectRoot $EvidenceRelativePath
    if (-not (Test-Path -LiteralPath $EvidencePath -PathType Leaf)) {
        throw "$TaskID acceptance evidence is missing: $EvidencePath"
    }
    $Evidence = Get-Content -LiteralPath $EvidencePath -Raw -Encoding UTF8
    if ($Evidence.Length -lt 200 -or $Evidence -notmatch [regex]::Escape($TaskID)) {
        throw "$TaskID acceptance evidence is empty or does not identify the task"
    }
    $ManifestRow = [regex]::Match(
        $Manifest,
        "(?m)^\|\s*$([regex]::Escape($TaskID))\s*\|\s*$Status\s*\|([^\r\n]*)$"
    )
    if (-not $ManifestRow.Success -or $ManifestRow.Groups[1].Value -notmatch [regex]::Escape($EvidenceRelativePath)) {
        throw "$TaskID manifest row does not point to $EvidenceRelativePath"
    }
    if ($Status -in $PlannedStatuses) {
        if ($TaskID -notin @('DF-026', 'DF-027', 'DF-028')) {
            throw "$TaskID cannot remain $Status because it is part of the implemented evidence range"
        }
        if ($Evidence -notmatch [regex]::Escape($Status)) {
            throw "$TaskID planned evidence must record status $Status"
        }
        Write-Output "$TaskID plan/evidence: passed ($Status)"
        continue
    }
    $HasBlockedStatus = [regex]::IsMatch(
        $Evidence,
        '`blocked`',
        [System.Text.RegularExpressions.RegexOptions]::IgnoreCase
    )
    $HasUnblockConditions = $Evidence.Contains($UnblockHeading)
    if ($Status -eq "blocked" -and (-not $HasBlockedStatus -or -not $HasUnblockConditions)) {
        throw "$TaskID blocked evidence must record blocked status and unblock conditions"
    }
    if ($Status -eq "completed" -and $HasBlockedStatus) {
        throw "$TaskID completed evidence still contains a blocked status marker"
    }
    & git -C $ProjectRoot ls-files --error-unmatch -- $EvidenceRelativePath *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "$TaskID acceptance evidence is not tracked by Git"
    }

    $Commits = [regex]::Matches($ManifestRow.Groups[1].Value, '`([0-9a-f]{7,40})(?:\s+[^`]*)?`') |
        ForEach-Object { $_.Groups[1].Value }
    if ($Commits.Count -eq 0) {
        throw "$TaskID manifest row has no Git commit"
    }
    foreach ($Commit in $Commits) {
        Assert-CommitIsReachable -Commit $Commit -TaskID $TaskID
    }

    Write-Output "$TaskID evidence/status/commits: passed ($Status, $($Commits.Count) commit(s))"
}

$UnexpectedPlanTasks = $PlanStatuses.Keys | Where-Object { $_ -like 'DF-*' -and $_ -notin $TaskIDs -and $_ -notin @('DF-000', 'DF-001', 'DF-002') }
if ($UnexpectedPlanTasks) {
    throw "unexpected DF task rows in implementation plan: $($UnexpectedPlanTasks -join ', ')"
}
$UnexpectedManifestTasks = $ManifestStatuses.Keys | Where-Object { $_ -notin $TaskIDs }
if ($UnexpectedManifestTasks) {
    throw "unexpected DF task rows in evidence manifest: $($UnexpectedManifestTasks -join ', ')"
}

$TrackedWorkBuddy = & git -C $ProjectRoot ls-files -- ".workbuddy"
if ($TrackedWorkBuddy) {
    throw ".workbuddy must remain untracked"
}
if ((& git -C $ProjectRoot branch --show-current).Trim() -ne "master") {
    throw "Device Farm development must stay on master"
}

Write-Output "implementation evidence gate: passed"
