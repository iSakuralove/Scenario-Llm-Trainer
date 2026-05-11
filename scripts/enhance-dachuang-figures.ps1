[CmdletBinding()]
param(
    [string]$Source = "",
    [string]$OutputDir = "",
    [int]$Scale = 2,
    [double]$SharpenStrength = 0.32,
    [double]$Dpi = 300
)

$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    $OutputDir = Join-Path $repoRoot "docs\figures\2026-dachuang-enhanced"
}

$tempRoot = Join-Path $repoRoot "tmp\image-enhance"
New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null

function Resolve-InputDirectory {
    param(
        [Parameter(Mandatory = $true)]
        [string]$SourcePath,
        [Parameter(Mandatory = $true)]
        [string]$TempBase
    )

    if (Test-Path $SourcePath -PathType Container) {
        return (Resolve-Path $SourcePath).Path
    }

    if (-not (Test-Path $SourcePath -PathType Leaf)) {
        throw "Input path not found: $SourcePath"
    }

    if ([System.IO.Path]::GetExtension($SourcePath).ToLowerInvariant() -ne ".zip") {
        throw "Only directories or .zip inputs are supported: $SourcePath"
    }

    $extractDir = Join-Path $TempBase ("extract-" + (Get-Date -Format "yyyyMMdd-HHmmss"))
    Expand-Archive -LiteralPath $SourcePath -DestinationPath $extractDir -Force
    return $extractDir
}

$inputDir = Resolve-InputDirectory -SourcePath $Source -TempBase $tempRoot
$nodeScript = Join-Path $PSScriptRoot "enhance-dachuang-figures.mjs"

if (-not (Test-Path $nodeScript -PathType Leaf)) {
    throw "Enhancer script not found: $nodeScript"
}

$arguments = @(
    $nodeScript,
    "--input", $inputDir,
    "--output", $OutputDir,
    "--scale", $Scale,
    "--sharpen", $SharpenStrength.ToString([System.Globalization.CultureInfo]::InvariantCulture),
    "--dpi", $Dpi.ToString([System.Globalization.CultureInfo]::InvariantCulture)
)

& node @arguments
if ($LASTEXITCODE -ne 0) {
    throw "Image enhancement script failed."
}

Write-Output ("Input directory: " + $inputDir)
Write-Output ("Output directory: " + $OutputDir)
