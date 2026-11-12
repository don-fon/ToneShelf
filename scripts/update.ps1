[CmdletBinding()]
param(
    [string]$InstanceId,
    [string]$Region = 'cn-hangzhou',
    [string]$Profile,
    [string]$WorkbenchPath = 'workbench',
    [ValidateSet('amd64', 'arm64')][string]$Arch = 'amd64',
    [switch]$PackageOnly
)
$ErrorActionPreference = 'Stop'
$updateArgs = @((Join-Path $PSScriptRoot 'update.py'), '--region', $Region, '--workbench', $WorkbenchPath, '--arch', $Arch)
if ($InstanceId) { $updateArgs += @('--instance-id', $InstanceId) }
if ($Profile) { $updateArgs += @('--profile', $Profile) }
if ($PackageOnly) { $updateArgs += '--package-only' }
if (-not (Get-Command python -ErrorAction SilentlyContinue)) { throw 'Python 3.10+ is required.' }
$savedErrorAction = $ErrorActionPreference
try {
    # Windows PowerShell wraps redirected native stderr as ErrorRecords, even
    # when tools (such as unittest) write successful progress there.
    $ErrorActionPreference = 'Continue'
    & python @updateArgs
    $updateExit = $LASTEXITCODE
} finally {
    $ErrorActionPreference = $savedErrorAction
}
if ($updateExit -ne 0) { throw "Update failed (exit $updateExit)." }
