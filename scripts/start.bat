@echo off
cd /d "%~dp0\.."
go build -o loginserver.exe .\cmd\loginserver
set CONF=%1
set LOGGER=%2
if "%CONF%"=="" set CONF=config\loginserver.yaml
if "%LOGGER%"=="" set LOGGER=config\tlog.yaml
loginserver.exe -conf %CONF% -logger %LOGGER%
