//go:build windows

package main

const setupDialog = `
param([string]$ChoiceFile,[string]$DefaultDir)
$ErrorActionPreference='Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
$f=New-Object System.Windows.Forms.Form
$f.Text='Установка Malowar News Agent 1.3.19'
$f.StartPosition='CenterScreen';$f.ClientSize=[System.Drawing.Size]::new(660,310)
$f.FormBorderStyle='FixedDialog';$f.MaximizeBox=$false
$f.Font=[System.Drawing.Font]::new('Segoe UI',[float]10)
$l=New-Object System.Windows.Forms.Label
$l.Text='Malowar News Agent 1.3.19'
$l.Font=[System.Drawing.Font]::new('Segoe UI',[float]17,[System.Drawing.FontStyle]::Bold)
$l.Location=[System.Drawing.Point]::new(20,14);$l.Size=[System.Drawing.Size]::new(610,40);$f.Controls.Add($l)
$info=New-Object System.Windows.Forms.Label
$info.Text='Мониторинг Steam, Workshop, GitHub и RSS. Черновики сохраняются локально. Выбери папку установки, доступную для записи. Права администратора не требуются.'
$info.Location=[System.Drawing.Point]::new(20,58);$info.Size=[System.Drawing.Size]::new(616,56);$f.Controls.Add($info)
$lab=New-Object System.Windows.Forms.Label
$lab.Text='Папка установки:';$lab.Location=[System.Drawing.Point]::new(22,122);$lab.Size=[System.Drawing.Size]::new(250,24);$f.Controls.Add($lab)
$path=New-Object System.Windows.Forms.TextBox
$path.Text=$DefaultDir;$path.Location=[System.Drawing.Point]::new(22,148);$path.Size=[System.Drawing.Size]::new(495,27);$f.Controls.Add($path)
$browse=New-Object System.Windows.Forms.Button
$browse.Text='Обзор...';$browse.Location=[System.Drawing.Point]::new(529,146);$browse.Size=[System.Drawing.Size]::new(105,31);$f.Controls.Add($browse)
$browse.Add_Click({$fd=New-Object System.Windows.Forms.FolderBrowserDialog;$fd.ShowNewFolderButton=$true;if(Test-Path -LiteralPath $path.Text -PathType Container){$fd.SelectedPath=$path.Text};if($fd.ShowDialog($f) -eq 'OK'){$path.Text=$fd.SelectedPath}})
$desktop=New-Object System.Windows.Forms.CheckBox
$desktop.Text='Создать ярлык на рабочем столе';$desktop.Checked=$true;$desktop.AutoSize=$true;$desktop.Location=[System.Drawing.Point]::new(22,193);$f.Controls.Add($desktop)
$start=New-Object System.Windows.Forms.CheckBox
$start.Text='Запустить программу после установки';$start.Checked=$true;$start.AutoSize=$true;$start.Location=[System.Drawing.Point]::new(22,224);$f.Controls.Add($start)
$ok=New-Object System.Windows.Forms.Button
$ok.Text='Установить';$ok.Location=[System.Drawing.Point]::new(397,260);$ok.Size=[System.Drawing.Size]::new(113,33);$f.Controls.Add($ok)
$no=New-Object System.Windows.Forms.Button
$no.Text='Отмена';$no.Location=[System.Drawing.Point]::new(524,260);$no.Size=[System.Drawing.Size]::new(110,33);$f.Controls.Add($no)
$no.Add_Click({$f.Close()})
$ok.Add_Click({$p=$path.Text.Trim().Trim('"');if([string]::IsNullOrWhiteSpace($p) -or -not [System.IO.Path]::IsPathRooted($p)){[void][System.Windows.Forms.MessageBox]::Show('Выбери полный путь, например D:\Apps\MalowarNewsAgent');return};$obj=[pscustomobject]@{path=$p;desktop=[bool]$desktop.Checked;start=[bool]$start.Checked};[System.IO.File]::WriteAllText($ChoiceFile,($obj|ConvertTo-Json -Compress),(New-Object System.Text.UTF8Encoding($false)));$f.Close()})
[void]$f.ShowDialog()
`
const uninstallDialog = `
param([string]$ChoiceFile,[string]$InstallDir)
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
$f=New-Object System.Windows.Forms.Form
$f.Text='Удаление Malowar News Agent';$f.StartPosition='CenterScreen';$f.ClientSize=[System.Drawing.Size]::new(620,245)
$f.FormBorderStyle='FixedDialog';$f.MaximizeBox=$false;$f.Font=[System.Drawing.Font]::new('Segoe UI',[float]10)
$l=New-Object System.Windows.Forms.Label
$l.Text='Удалить Malowar News Agent?';$l.Font=[System.Drawing.Font]::new('Segoe UI',[float]16,[System.Drawing.FontStyle]::Bold)
$l.Location=[System.Drawing.Point]::new(20,16);$l.Size=[System.Drawing.Size]::new(580,44);$f.Controls.Add($l)
$p=New-Object System.Windows.Forms.Label
$p.Text='Папка: '+$InstallDir;$p.Location=[System.Drawing.Point]::new(22,66);$p.Size=[System.Drawing.Size]::new(580,42);$f.Controls.Add($p)
$c=New-Object System.Windows.Forms.CheckBox
$c.Text='Также удалить новости, черновики, изображения и настройки';$c.Checked=$false
$c.Location=[System.Drawing.Point]::new(22,122);$c.Size=[System.Drawing.Size]::new(570,29);$f.Controls.Add($c)
$n=New-Object System.Windows.Forms.Label
$n.Text='По умолчанию редакционная база сохранится. Сначала закрой программу кнопкой «Выход».'
$n.Location=[System.Drawing.Point]::new(22,158);$n.Size=[System.Drawing.Size]::new(580,35);$f.Controls.Add($n)
$yes=New-Object System.Windows.Forms.Button
$yes.Text='Удалить';$yes.Location=[System.Drawing.Point]::new(394,202);$yes.Size=[System.Drawing.Size]::new(105,31);$f.Controls.Add($yes)
$no=New-Object System.Windows.Forms.Button
$no.Text='Отмена';$no.Location=[System.Drawing.Point]::new(508,202);$no.Size=[System.Drawing.Size]::new(105,31);$f.Controls.Add($no)
$no.Add_Click({$f.Close()})
$yes.Add_Click({$obj=[pscustomobject]@{removeData=[bool]$c.Checked};[System.IO.File]::WriteAllText($ChoiceFile,($obj|ConvertTo-Json -Compress),(New-Object System.Text.UTF8Encoding($false)));$f.Close()})
[void]$f.ShowDialog()
`
