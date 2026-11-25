$ErrorActionPreference='Stop'
$env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Тесты Go завершились с ошибкой' }
go build -trimpath -ldflags='-s -w -H windowsgui' -o Malowar_News_Agent.exe .
if ($LASTEXITCODE -ne 0) { throw 'Не удалось собрать News Agent' }
go build -trimpath -ldflags='-s -w -H windowsgui' -o updater/Malowar_News_Updater.exe ./updater
if ($LASTEXITCODE -ne 0) { throw 'Не удалось собрать обновлятор' }
go build -tags mna_uninstall -trimpath -ldflags='-s -w -H windowsgui' -o installer/payload/Malowar_News_Uninstall.exe ./installer
if ($LASTEXITCODE -ne 0) { throw 'Не удалось собрать деинсталлятор' }
Copy-Item Malowar_News_Agent.exe installer/payload/Malowar_News_Agent.exe -Force
Copy-Item updater/Malowar_News_Updater.exe installer/payload/Malowar_News_Updater.exe -Force
Copy-Item app/favicon.ico installer/payload/Malowar_News_Agent.ico -Force
go build -tags mna_setup -trimpath -ldflags='-s -w -H windowsgui' -o Malowar_News_Agent_1_3_19_Setup.exe ./installer
if ($LASTEXITCODE -ne 0) { throw 'Не удалось собрать установщик' }
Write-Host 'News Agent 1.3.19, установщик и обновлятор готовы.'
