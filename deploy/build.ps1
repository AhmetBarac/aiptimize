#Requires -Version 5.1
<#
.SYNOPSIS
    Build the aiptimize image for the target platform and optionally push it.

.DESCRIPTION
    Reads the minor version from VERSION and the build counter from
    .buildnumber, increments the counter, and tags the image three ways:

        0.12.041     immutable build tag
        dev          moving tag the server follows
        sha-abc1234  commit tag for rollbacks

    The application reports its version as "dev-0.12.041" so the binary stays
    on the dev line while still being identifiable.

    Builder stages run natively and cross-compile, so no QEMU is involved even
    when producing a linux/arm64 image on an amd64 workstation.

.EXAMPLE
    ./deploy/build.ps1 -Push

.EXAMPLE
    ./deploy/build.ps1 -Platform linux/amd64 -Push
#>
[CmdletBinding()]
param(
    [string]$Registry = 'docker.io/ahmetbarac/aiptimize',
    [string]$Platform = 'linux/arm64',
    [string]$Notes = '',
    [switch]$Push,
    [switch]$NoIncrement
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Resolve-Docker {
    $command = Get-Command docker -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    $candidates = @(
        (Join-Path $env:ProgramFiles 'Docker\Docker\resources\bin\docker.exe'),
        (Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\resources\bin\docker.exe')
    )
    foreach ($candidate in $candidates) {
        if (Test-Path $candidate) { return $candidate }
    }
    throw 'docker CLI not found. Start Docker Desktop or add the CLI to PATH.'
}

$docker = Resolve-Docker

# The Docker Desktop credential helper (docker-credential-desktop) lives next to
# the CLI and must be on PATH for buildx and registry pushes.
$dockerDir = Split-Path -Parent $docker
if (($env:PATH -split ';') -notcontains $dockerDir) {
    $env:PATH = "$dockerDir;$env:PATH"
}

$minor = (Get-Content (Join-Path $root 'VERSION') -Raw).Trim()
if ($minor -notmatch '^\d+\.\d+$') {
    throw "VERSION must look like 0.12, got '$minor'"
}

$counterFile = Join-Path $root '.buildnumber'
$build = [int]((Get-Content $counterFile -Raw).Trim())
if (-not $NoIncrement) { $build++ }
$version = '{0}.{1:000}' -f $minor, $build
$appVersion = "dev-$version"

$sha = (& git rev-parse --short=7 HEAD).Trim()
if (-not $sha) { $sha = 'unknown' }
$dirty = [bool](& git status --porcelain)
$buildDate = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')

# One line of release notes stored as an image label, read back by the in-app
# update check. Defaults to the commit subject.
if (-not $Notes) {
    $Notes = (& git log -1 --pretty=%s).Trim()
}
if ($Notes.Length -gt 120) { $Notes = $Notes.Substring(0, 117) + '...' }

$tags = @(
    "$Registry`:$version",
    "$Registry`:dev",
    "$Registry`:sha-$sha"
)

$buildArgs = @(
    'buildx', 'build',
    '--platform', $Platform,
    '-f', 'deploy/Dockerfile',
    '--build-arg', "VERSION=$appVersion",
    '--build-arg', "COMMIT=$sha",
    '--build-arg', "BUILD_DATE=$buildDate",
    '--build-arg', "NOTES=$Notes"
)
foreach ($tag in $tags) { $buildArgs += @('-t', $tag) }
$buildArgs += '.'
if ($Push) { $buildArgs += '--push' }

Write-Host "aiptimize $appVersion  (commit $sha$(if ($dirty) { ', dirty tree' }), $Platform)"
Write-Host "notes: $Notes"
Write-Host "tags: $($tags -join ', ')"
if ($dirty) {
    Write-Warning 'Working tree has uncommitted changes; the commit tag does not describe them.'
}

& $docker @buildArgs
if ($LASTEXITCODE -ne 0) { throw "docker buildx build failed with exit code $LASTEXITCODE" }

if (-not $NoIncrement) {
    Set-Content -Path $counterFile -Value $build -NoNewline
}

Write-Host "built $appVersion"
if ($Push) {
    Write-Host "pushed to $Registry ($version, dev, sha-$sha)"
    if (-not $NoIncrement) {
        # Keep the repository history aligned with the published image: commit
        # the bumped counter, then tag that commit and push the tag.
        $tag = "v$version"
        & git add -- $counterFile
        & git commit --quiet -m "Bump build number to $version"
        if ($LASTEXITCODE -ne 0) {
            Write-Warning 'Build counter was not committed; the tag will not include it.'
        }
        if (-not (& git tag -l $tag)) {
            & git tag -a $tag -m "Release $version"
        }
        & git push --quiet origin $tag 2>&1 | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "tagged $tag and pushed it"
        } else {
            Write-Warning "created $tag locally but could not push it"
        }
    }
} else {
    Write-Host 'not pushed (pass -Push to publish)'
}
exit 0
