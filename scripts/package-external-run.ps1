[CmdletBinding()]
param(
    [string]$OutputName = ("situational-teaching-external-run-{0}.zip" -f (Get-Date -Format "yyyyMMdd-HHmmss"))
)

$ErrorActionPreference = "Stop"

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptRoot
$deliveryRoot = Join-Path $repoRoot "交付"
$stageRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("situational-teaching-package-" + [System.Guid]::NewGuid().ToString("N"))
$stageProjectRoot = Join-Path $stageRoot "计算机设计大赛"
$zipPath = Join-Path $deliveryRoot $OutputName

$includeRoots = @(
    "backend",
    "frontend",
    "scripts",
    "README.md",
    "package.json",
    "docker-compose.yml",
    ".gitignore"
)

$excludedDirNames = @(
    ".git",
    ".idea",
    ".codex",
    ".agents",
    ".playwright-mcp",
    ".serena",
    ".venv",
    ".gotmp",
    "node_modules",
    "dist",
    "playwright-report",
    "test-results",
    "tmp",
    "output",
    "交付",
    "流程",
    "规划",
    "img",
    "Design",
    "__pycache__"
)

$excludedLeafNames = @(
    ".env"
)

$excludedFilePatterns = @(
    "*.pyc",
    "*.pyo"
)

function Get-RelativePath {
    param(
        [string]$BasePath,
        [string]$TargetPath
    )

    $baseResolved = (Resolve-Path -LiteralPath $BasePath).Path
    $targetResolved = (Resolve-Path -LiteralPath $TargetPath).Path
    $baseUri = New-Object System.Uri(($baseResolved.TrimEnd('\') + '\'))
    $targetUri = New-Object System.Uri($targetResolved)
    return [System.Uri]::UnescapeDataString($baseUri.MakeRelativeUri($targetUri).ToString()).Replace('/', '\')
}

function Test-ExcludedPath {
    param(
        [string]$RelativePath
    )

    $separator = [System.IO.Path]::DirectorySeparatorChar
    $normalized = $RelativePath.Replace('/', $separator).TrimStart($separator)
    if ([string]::IsNullOrWhiteSpace($normalized)) {
        return $false
    }

    $segments = @($normalized -split '[\\/]') | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    if ($segments.Count -eq 0) {
        return $false
    }

    foreach ($segment in $segments) {
        if ($excludedDirNames -contains $segment) {
            return $true
        }
    }

    $leaf = $segments[$segments.Count - 1]
    if ($excludedLeafNames -contains $leaf) {
        return $true
    }

    foreach ($pattern in $excludedFilePatterns) {
        if ($leaf -like $pattern) {
            return $true
        }
    }

    if ($segments.Count -ge 2 -and $segments[0] -eq "backend" -and $segments[1] -eq "data") {
        return $true
    }

    return $false
}

function Copy-IncludedPath {
    param(
        [string]$SourcePath
    )

    $item = Get-Item -LiteralPath $SourcePath -Force
    $relative = Get-RelativePath -BasePath $repoRoot -TargetPath $item.FullName

    if (Test-ExcludedPath -RelativePath $relative) {
        return
    }

    if ($item.PSIsContainer) {
        Get-ChildItem -LiteralPath $item.FullName -Recurse -File -Force | ForEach-Object {
            $fileRelative = Get-RelativePath -BasePath $repoRoot -TargetPath $_.FullName
            if (Test-ExcludedPath -RelativePath $fileRelative) {
                return
            }

            $destination = Join-Path $stageProjectRoot $fileRelative
            $destinationDir = Split-Path -Parent $destination
            if (-not (Test-Path -LiteralPath $destinationDir)) {
                New-Item -ItemType Directory -Path $destinationDir | Out-Null
            }
            Copy-Item -LiteralPath $_.FullName -Destination $destination -Force
        }
        return
    }

    $destination = Join-Path $stageProjectRoot $relative
    $destinationDir = Split-Path -Parent $destination
    if ($destinationDir -and -not (Test-Path -LiteralPath $destinationDir)) {
        New-Item -ItemType Directory -Path $destinationDir | Out-Null
    }
    Copy-Item -LiteralPath $item.FullName -Destination $destination -Force
}

try {
    if (-not (Test-Path -LiteralPath $deliveryRoot)) {
        New-Item -ItemType Directory -Path $deliveryRoot | Out-Null
    }
    if (Test-Path -LiteralPath $zipPath) {
        Remove-Item -LiteralPath $zipPath -Force
    }

    New-Item -ItemType Directory -Path $stageProjectRoot -Force | Out-Null

    foreach ($path in $includeRoots) {
        $source = Join-Path $repoRoot $path
        if (Test-Path -LiteralPath $source) {
            Copy-IncludedPath -SourcePath $source
        }
    }

    Compress-Archive -LiteralPath $stageProjectRoot -DestinationPath $zipPath -CompressionLevel Optimal -Force
    Write-Output $zipPath
}
finally {
    if (Test-Path -LiteralPath $stageRoot) {
        Remove-Item -LiteralPath $stageRoot -Recurse -Force
    }
}
