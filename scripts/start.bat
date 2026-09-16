@echo off
cd /d "%~dp0\.."
go build -o loginserver.exe .\cmd\loginserver
loginserver.exe -conf config\loginserver.yaml -logger config\tlog.yaml
