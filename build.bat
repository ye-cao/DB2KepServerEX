@echo off
rem ===========================================================================
rem  Build script for DB2KepServerEX
rem
rem  Requires: Go 1.20+ , Python 3 (only for generating the icon resource)
rem
rem  NOTE: this file is intentionally ASCII-only. A .bat containing non-ASCII
rem        text gets garbled by cmd.exe depending on the active code page,
rem        which would silently produce a mis-named output file.
rem ===========================================================================
setlocal

echo [1/3] Generating app.syso (icon + manifest) ...
python "%~dp0tools\make_resources.py"
if errorlevel 1 goto :fail

echo.
echo [2/3] Building GUI version ...
pushd "%~dp0"
go build -trimpath -ldflags "-s -w -H windowsgui" -o DB2KepServerEX.exe .
if errorlevel 1 goto :failpop

echo.
echo [3/3] Building CLI version ...
go build -tags cli -trimpath -ldflags "-s -w" -o DB2KepServerEX_cli.exe .
if errorlevel 1 goto :failpop

popd
echo.
echo Done. Output:
echo   DB2KepServerEX.exe       (GUI)
echo   DB2KepServerEX_cli.exe   (CLI)
exit /b 0

:failpop
popd
:fail
echo.
echo BUILD FAILED.
exit /b 1
