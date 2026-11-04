@echo off
rem Convenience wrapper so Windows users can double-click or run "build" from cmd.exe.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
