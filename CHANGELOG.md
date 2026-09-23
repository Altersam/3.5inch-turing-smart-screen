# Changelog

All notable changes to Turing 3.5 Neon Monitor V38 will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- Removed blocking `FlushFileBuffers` call in COM send operations
- Added CTS (Clear To Send) diagnostics before each data write
- Disabled RTS/CTS handshake to prevent deadlock
- Added buffer error checking with `ClearCommError()`
- Improved reconnect logic with buffer purge and CTS recheck
- Reduced chunk size from 2048 to 1024 bytes for better reliability on slow ports
- Added watchdog timeout (3 seconds) for async COM operations

### Changed
- Improved log messages for debugging COM port issues
- Better error handling for WriteFile operations

### Deprecated
- None

## [V37] - Initial Release

### Added
- Real-time CPU, GPU, RAM usage display
- Disk space monitoring and reporting
- OpenCode AI Go API integration with balance display
- Neon animation trail effect
- Native Windows system tray integration

### Features
- 3.5-inch LCD display support (ST7796 controller)
- USB serial communication via CH340 driver
- Auto-start on Windows login
- Console session management
