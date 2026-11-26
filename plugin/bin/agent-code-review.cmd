@echo off
rem Starts the executable of this system, which sits in windows-<arch>\ beside
rem this file, with the arguments it was given. It does nothing else.
setlocal
set "arch=%PROCESSOR_ARCHITECTURE%"
if defined PROCESSOR_ARCHITEW6432 set "arch=%PROCESSOR_ARCHITEW6432%"
set "target="
if /i "%arch%"=="AMD64" set "target=windows-amd64"
if /i "%arch%"=="ARM64" set "target=windows-arm64"
if not defined target goto unsupported
set "exe=%~dp0%target%\agent-code-review.exe"
if not exist "%exe%" goto missing
"%exe%" %*
exit /b %ERRORLEVEL%

:unsupported
echo agent-code-review: cannot start on this processor 1>&2
echo(  why: there is no executable for %arch%; the supported ones are AMD64 and ARM64 1>&2
echo(  what to do: install the executable of the GitHub Release if it has one for your processor 1>&2
exit /b 1

:missing
echo agent-code-review: the executable for %target% is missing 1>&2
echo(  why: %exe% is not there 1>&2
echo(  what to do: reinstall the plugin, or install the executable of the GitHub Release for your system 1>&2
exit /b 1
