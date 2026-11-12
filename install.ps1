<#
.SYNOPSIS
  git-memory installer for Windows.

.DESCRIPTION
  Downloads git-memory from the project's GitHub releases page, checks it against the release's SHA256SUMS, installs it
  as git-memory.exe and adds its folder to your user PATH. No administrator rights are needed, and nothing outside your
  user profile is changed.

    irm https://github.com/Rickedb/git-memory/releases/latest/download/install.ps1 | iex

  To pass options, run it as a script block (or download it first and run the file):

    & ([scriptblock]::Create((irm https://github.com/Rickedb/git-memory/releases/latest/download/install.ps1))) -Version 0.0.1

.PARAMETER Version
  The release to install (0.0.1 or v0.0.1). Default: the latest.

.PARAMETER Dir
  The folder to install into. Default: %LOCALAPPDATA%\Programs\git-memory

.PARAMETER NoPath
  Do not change your user PATH.

  Set GIT_MEMORY_BASE_URL (default https://github.com/Rickedb/git-memory/releases) to use a mirror.
#>
[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$Dir = '',
    [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
$Repo = 'Rickedb/git-memory'
$Base = if ($env:GIT_MEMORY_BASE_URL) { $env:GIT_MEMORY_BASE_URL } else { "https://github.com/$Repo/releases" }

# ---- steps (functions, so each can be tested on its own) ----------------------------------------------------------

function Get-GmArch {
    # a 32-bit PowerShell on a 64-bit Windows reports the real CPU in PROCESSOR_ARCHITEW6432
    $a = $env:PROCESSOR_ARCHITEW6432
    if (-not $a) { $a = $env:PROCESSOR_ARCHITECTURE }
    if ($a -match '^(AMD64|x86_64)$') { return 'amd64' }
    if ($a -match '^ARM64$') { return 'arm64' }
    throw "unsupported CPU architecture: $a"
}

# The hash SHA256SUMS lists for an asset, or $null. Lines look like "<64 hex>  <file name>".
function Get-ExpectedHash([string]$SumsText, [string]$Asset) {
    foreach ($line in ($SumsText -split "`r?`n")) {
        if ($line -match '^([0-9a-fA-F]{64})\s+\*?(.+?)\s*$' -and $Matches[2] -eq $Asset) {
            return $Matches[1].ToLower()
        }
    }
    return $null
}

# Throws unless the file is what SHA256SUMS says it is.
function Assert-GmChecksum([string]$Path, [string]$SumsText, [string]$Asset) {
    $want = Get-ExpectedHash $SumsText $Asset
    if (-not $want) { throw "SHA256SUMS has no entry for $Asset" }
    $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLower()
    if ($got -ne $want) { throw "checksum mismatch for $Asset (expected $want, got $got): not installing" }
}

# Puts the file at <Dir>\git-memory.exe. A running program cannot be overwritten on Windows but it can be renamed, so an
# upgrade while an agent's hook is running moves the old copy aside instead of failing.
function Install-GmFile([string]$Source, [string]$Dir) {
    New-Item -ItemType Directory -Force -Path $Dir | Out-Null
    $final = Join-Path $Dir 'git-memory.exe'
    $staged = "$final.new"
    Copy-Item -LiteralPath $Source -Destination $staged -Force
    try {
        Move-Item -LiteralPath $staged -Destination $final -Force
    } catch {
        $old = "$final.old"
        Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
        try {
            Move-Item -LiteralPath $final -Destination $old -Force
            Move-Item -LiteralPath $staged -Destination $final -Force
        } catch {
            Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
            throw "cannot replace $final (is git-memory running? close it and run the installer again): $($_.Exception.Message)"
        }
    }
    return $final
}

# The PATH value with Dir added, or $null when Dir is already in it. Entries are compared as Windows compares paths:
# without case, without a trailing backslash, with %VARIABLES% expanded.
function Get-PathWith([string]$Current, [string]$Dir) {
    $norm = { param($p) [Environment]::ExpandEnvironmentVariables($p).TrimEnd('\') }
    $want = & $norm $Dir
    foreach ($entry in ($Current -split ';')) {
        if ($entry -and ((& $norm $entry) -ieq $want)) { return $null }
    }
    if ($Current) { return $Current.TrimEnd(';') + ';' + $Dir }
    return $Dir
}

# Reads and writes the user PATH in the registry. [Environment]::SetEnvironmentVariable would store the expanded text and
# lose any %VARIABLES% the entries use.
function Get-UserPathValue {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment')
    try { return [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) }
    finally { $key.Close() }
}

function Set-UserPathValue([string]$Value) {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    try { $key.SetValue('Path', $Value, [Microsoft.Win32.RegistryValueKind]::ExpandString) }
    finally { $key.Close() }
    # tell running programs (Explorer, new terminals) that the environment changed
    if (-not ('GitMemory.Native' -as [type])) {
        Add-Type -Namespace GitMemory -Name Native -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
    }
    $result = [UIntPtr]::Zero
    [void][GitMemory.Native]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
}

# Tests load the functions above without installing anything.
if ($env:GIT_MEMORY_INSTALLER_LIB) { return }

# ---- install ------------------------------------------------------------------------------------------------------

if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) {
    throw 'install.ps1 installs the Windows build; on Linux use install.sh'
}
# Windows PowerShell 5.1 does not offer TLS 1.2 by default, which GitHub requires
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
$ProgressPreference = 'SilentlyContinue' # the progress bar makes Invoke-WebRequest much slower in 5.1

$arch = Get-GmArch
$asset = "git-memory_windows_$arch.exe"
if (-not $Dir) { $Dir = Join-Path $env:LOCALAPPDATA 'Programs\git-memory' }
$urlBase = if ($Version -eq 'latest') { "$Base/latest/download" } else { "$Base/download/v$($Version.TrimStart('v'))" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('git-memory-install-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    Write-Host "Downloading $asset ($Version) from $urlBase"
    try {
        Invoke-WebRequest -UseBasicParsing -Uri "$urlBase/$asset" -OutFile (Join-Path $tmp $asset)
        Invoke-WebRequest -UseBasicParsing -Uri "$urlBase/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')
    } catch {
        throw "could not download from $urlBase (is there a release with that version?): $($_.Exception.Message)"
    }
    Assert-GmChecksum (Join-Path $tmp $asset) (Get-Content -Raw -LiteralPath (Join-Path $tmp 'SHA256SUMS')) $asset
    Write-Host 'Checksum verified.'

    $existing = Join-Path $Dir 'git-memory.exe'
    $previous = $null
    if (Test-Path -LiteralPath $existing) { $previous = (& $existing agent version 2>$null) }

    $final = Install-GmFile (Join-Path $tmp $asset) $Dir
    $installed = & $final agent version
    if ($LASTEXITCODE -ne 0 -or -not $installed) { throw 'the downloaded binary does not run on this machine' }
    if ($previous) { Write-Host "Upgraded: $previous -> $installed ($final)" } else { Write-Host "Installed: $installed ($final)" }
} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

if ($NoPath) {
    Write-Host "Add $Dir to your PATH to run git-memory by name."
} else {
    $new = Get-PathWith (Get-UserPathValue) $Dir
    if ($new) {
        Set-UserPathValue $new
        Write-Host "Added $Dir to your user PATH. New terminals pick it up; this one already has it."
    } else {
        Write-Host "$Dir is already on your user PATH."
    }
    # this session too, so the command works right away
    if (-not (($env:Path -split ';') -contains $Dir)) { $env:Path = $env:Path.TrimEnd(';') + ';' + $Dir }
}

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Warning 'git was not found on your PATH: git-memory runs git, so install it too (https://git-scm.com/download/win)'
}
Write-Host ''
Write-Host 'Next:  cd your-project; git-memory agent init       (git-memory doctor checks everything)'
