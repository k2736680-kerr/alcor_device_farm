param()

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$PlanPath = Join-Path $ProjectRoot "docs/05_step_by_step_implementation.md"
$ManifestPath = Join-Path $ProjectRoot "docs/evidence/implementation_manifest.md"
$Plan = Get-Content -LiteralPath $PlanPath -Raw -Encoding UTF8
$Manifest = Get-Content -LiteralPath $ManifestPath -Raw -Encoding UTF8

$ExpectedStatus = @{}
foreach ($number in 3..13) { $ExpectedStatus[("DF-{0:D3}" -f $number)] = "completed" }
foreach ($number in 14..24) { $ExpectedStatus[("DF-{0:D3}" -f $number)] = "blocked" }
$ExpectedStatus["DF-025"] = "completed"

foreach ($entry in $ExpectedStatus.GetEnumerator() | Sort-Object Name) {
    $TaskID = $entry.Name
    $Status = $entry.Value
    $EvidencePath = Join-Path $ProjectRoot ("docs/evidence/{0}/acceptance.md" -f $TaskID)
    if (-not (Test-Path -LiteralPath $EvidencePath -PathType Leaf)) {
        throw "$TaskID acceptance evidence is missing: $EvidencePath"
    }
    $EscapedTaskID = [regex]::Escape($TaskID)
    if ($Plan -notmatch "(?m)^\|\s*$EscapedTaskID\s*\|[^\r\n]*\|\s*$Status\s*\|") {
        throw "$TaskID status is not $Status in docs/05_step_by_step_implementation.md"
    }
    if ($Manifest -notmatch "(?m)^\|\s*$EscapedTaskID\s*\|") {
        throw "$TaskID is missing from docs/evidence/implementation_manifest.md"
    }
    Write-Output "$TaskID evidence/status: passed ($Status)"
}

$TrackedWorkBuddy = & git -C $ProjectRoot ls-files -- ".workbuddy"
if ($TrackedWorkBuddy) {
    throw ".workbuddy must remain untracked"
}
if ((& git -C $ProjectRoot branch --show-current).Trim() -ne "master") {
    throw "Device Farm development must stay on master"
}

Write-Output "implementation evidence gate: passed"
