@echo off
setlocal
title KfuPet upgrade test - mock install

rem ==========================================================================
rem  Mock an installed KfuPet from a release zip, so that the updater main
rem  screen shows the "Upgrade" button (it only looks for the registry record).
rem
rem  It does what a real install does: unpack the zip into the install dir,
rem  drop the updater there as the resident copy, create the all-users
rem  desktop/start menu shortcuts, and write the machine-level install record.
rem
rem  Usage:
rem    1. Put the KfuPet release zip next to this script.
rem    2. Run this script (it asks for admin rights automatically).
rem    3. Open dist\KfuPetInstall.exe - the main screen shows "Upgrade".
rem
rem  Optional: first argument sets the local version written to the registry
rem            (default 0.0.0, intentionally low so the online release is newer).
rem            e.g.  test-upgrade.bat 0.0.5
rem  Re-run the script to reset everything back to the old version.
rem
rem  NOTE: keep this file ASCII-only. cmd.exe mis-parses batch files that
rem  contain multi-byte characters and the script breaks in odd ways.
rem ==========================================================================

set "DEST=%ProgramFiles%\KfuPet"
set "VERSION=0.0.0"
if not "%~1"=="" set "VERSION=%~1"

rem ---- admin rights: we write into Program Files ----
net session >nul 2>&1
if errorlevel 1 (
    echo Asking for administrator rights...
    powershell -NoProfile -Command "Start-Process -FilePath '%~f0' -Verb RunAs"
    exit /b
)

rem ---- 1/4 find the zip next to this script (newest one if several) ----
set "ZIP="
for /f "delims=" %%f in ('dir /b /a-d /o-d "%~dp0*.zip" 2^>nul') do (
    if not defined ZIP set "ZIP=%~dp0%%f"
)
if not defined ZIP (
    set "ERR=No zip found next to this script. Put the KfuPet release zip here."
    goto :die
)
echo [1/5] package: %ZIP%

rem ---- 2/4 find the updater (a real install copies it into the install dir) ----
set "UPD="
if exist "%~dp0KfuPetUpdate.exe" set "UPD=%~dp0KfuPetUpdate.exe"
if not defined UPD if exist "%~dp0KfuPetInstall.exe" set "UPD=%~dp0KfuPetInstall.exe"
if not defined UPD if exist "%~dp0dist\KfuPetInstall.exe" set "UPD=%~dp0dist\KfuPetInstall.exe"
if not defined UPD (
    set "ERR=Updater not found. Run build.ps1 first to get dist\KfuPetInstall.exe."
    goto :die
)

rem ---- 3/4 extract into the install dir ----
where tar >nul 2>&1
if errorlevel 1 (
    set "ERR=tar.exe not found (built into Windows 10 1803+), cannot unzip."
    goto :die
)
echo [2/5] extracting to %DEST%
if exist "%DEST%" rmdir /s /q "%DEST%"
mkdir "%DEST%" 2>nul
tar -xf "%ZIP%" -C "%DEST%"
if errorlevel 1 (
    set "ERR=Failed to extract: %ZIP%"
    goto :die
)

rem If the zip has a single top-level folder, hoist its contents up,
rem same as the real installer does when unpacking a release.
set "TOP="
set "MULTI="
for /f "delims=" %%d in ('dir /b /ad "%DEST%" 2^>nul') do (
    if defined TOP (set "MULTI=1") else (set "TOP=%%d")
)
for /f "delims=" %%f in ('dir /b /a-d "%DEST%" 2^>nul') do set "MULTI=1"
if defined MULTI set "TOP="
if defined TOP (
    rem robocopy (not "move *") because move only handles files, not subfolders.
    robocopy "%DEST%\%TOP%" "%DEST%" /E /MOVE /NFL /NDL /NJH /NJS /NP >nul
    if errorlevel 8 (
        set "ERR=Failed to hoist the top-level folder %TOP%\ up one level."
        goto :die
    )
)

copy /y "%UPD%" "%DEST%\KfuPetUpdate.exe" >nul
if not exist "%DEST%\KfuPet.exe" (
    set "ERR=KfuPet.exe is missing in the install dir - is this really a KfuPet release zip?"
    goto :die
)

rem ---- 3/5 create shortcuts (all users, same folders and name as a real install) ----
echo [3/5] creating shortcuts
powershell -NoProfile -Command "$sh=New-Object -ComObject WScript.Shell; $t='%DEST%\KfuPet.exe'; $w='%DEST%'; $dirs=@(); $d=[Environment]::GetFolderPath('CommonDesktopDirectory'); if($d){ $dirs+=$d }; $s=[Environment]::GetFolderPath('CommonStartMenu'); if($s){ $dirs+=(Join-Path $s 'Programs') }; foreach($d in $dirs){ $l=$sh.CreateShortcut((Join-Path $d 'KfuPet.lnk')); $l.TargetPath=$t; $l.WorkingDirectory=$w; $l.Save() }"
if errorlevel 1 echo    (warning: shortcuts not created, continuing)

rem ---- 4/5 write the install record (machine-level, matching a real install) ----
echo [4/5] writing install record (local version %VERSION%)
reg add "HKLM\Software\KfuPet" /v InstallPath /t REG_SZ /d "%DEST%" /f >nul
reg add "HKLM\Software\KfuPet" /v DisplayVersion /t REG_SZ /d "%VERSION%" /f >nul
rem drop the legacy user-level record so the mock matches a clean install
reg delete "HKCU\Software\KfuPet" /f >nul 2>&1

echo [5/5] done: %DEST%
echo.
echo Open %UPD% - the main screen will show the "Upgrade" button.
echo.
pause
exit /b 0

:die
echo.
echo [ERROR] %ERR%
echo.
pause
exit /b 1
