$ErrorActionPreference='Stop'
$env:GOOS='windows';$env:GOARCH='amd64';$env:CGO_ENABLED='0'
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Unit tests failed' }
go build -trimpath -ldflags='-s -w -H windowsgui' -o Malowar_News_Agent.exe .
if ($LASTEXITCODE -ne 0) { throw 'Build failed' }
Write-Host 'Malowar_News_Agent.exe is ready.'
