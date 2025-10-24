@echo off
REM Version update script for MariaDB Backup Tool (Windows)

if "%1"=="" (
    echo Usage: %0 ^<new_version^>
    echo Example: %0 1.0.1
    exit /b 1
)

set NEW_VERSION=%1
set VERSION_FILE=version.txt

REM Update version.txt
echo %NEW_VERSION% > %VERSION_FILE%
echo ✅ Updated %VERSION_FILE% to %NEW_VERSION%

REM Update README.md if it contains version info
if exist README.md (
    powershell -Command "(Get-Content README.md) -replace '[0-9]+\.[0-9]+\.[0-9]+', '%NEW_VERSION%' | Set-Content README.md"
    echo ✅ Updated README.md version references
)

REM Update BUILD.md if it contains version info
if exist BUILD.md (
    powershell -Command "(Get-Content BUILD.md) -replace '[0-9]+\.[0-9]+\.[0-9]+', '%NEW_VERSION%' | Set-Content BUILD.md"
    echo ✅ Updated BUILD.md version references
)

echo.
echo 🎉 Version updated to %NEW_VERSION%!
echo 📝 Don't forget to:
echo    - Commit the changes
echo    - Create a git tag: git tag v%NEW_VERSION%
echo    - Push the tag: git push origin v%NEW_VERSION%
echo    - Run build scripts to create new binaries
