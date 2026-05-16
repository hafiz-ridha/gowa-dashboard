@echo off
REM Helper untuk jalankan dashboard di Windows.
REM Pastikan core REST API (../src) sudah jalan dulu di port 3000.

if not exist .env (
    echo Menyalin .env.example -^> .env (silakan edit sesuai kebutuhan)
    copy .env.example .env >nul
)

go mod tidy
if errorlevel 1 (
    echo go mod tidy gagal. Pastikan Go terinstall ^(https://go.dev/dl/^).
    pause
    exit /b 1
)

go run .
