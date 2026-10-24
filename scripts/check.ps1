param(
    [switch]$Unit
)

$ErrorActionPreference = 'Stop'
$previousDatabaseURL = $env:TEST_DATABASE_URL

if (-not $Unit -and [string]::IsNullOrWhiteSpace($env:TEST_DATABASE_URL)) {
    throw 'Для полной проверки задайте TEST_DATABASE_URL - инструкция в docs/testing.md'
}

Push-Location (Split-Path -Parent $PSScriptRoot)
try {
    if ($Unit) {
        # без подключения к БД интеграционные тесты будут пропущены
        Remove-Item Env:TEST_DATABASE_URL -ErrorAction SilentlyContinue
    }

    go test -count=1 -timeout=2m ./...
    if ($LASTEXITCODE -ne 0) { throw 'Тесты завершились с ошибкой' }

    go test -count=1 -race -timeout=2m '-coverprofile=coverage.out' ./...
    if ($LASTEXITCODE -ne 0) { throw 'Проверка с race detector завершилась с ошибкой' }

    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet нашёл ошибки' }

    go build ./...
    if ($LASTEXITCODE -ne 0) { throw 'Сборка завершилась с ошибкой' }

    if ($Unit) {
        Write-Host 'Проверки без PostgreSQL завершены - интеграционные тесты пропущены'
    } else {
        Write-Host 'Полная проверка с PostgreSQL завершена'
    }
}
finally {
    $env:TEST_DATABASE_URL = $previousDatabaseURL
    Pop-Location
}
