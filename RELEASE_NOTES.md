# Release Notes - Turing 3.5 Neon Monitor V38

## [Unreleased] - Ready for First Public Release

### 🎉 What's New in V38?

**Fixed COM Hang Issues** - Major improvements to serial communication stability:

1. **Removed blocking FlushFileBuffers call**
   - Previous version blocked indefinitely when CTS signal was inactive
   - Now uses controlled timeouts with proper error handling

2. **Added CTS diagnostics before each write operation**
   - Checks if device is ready to receive data
   - Skips operations when CTS not asserted to prevent hangs

3. **Disabled RTS/CTS handshake**
   - RTS_CONTROL_HANDSHAKE was causing deadlocks on some CH340 devices
   - Now uses simple fBinary and fOutxCtsFlow for read readiness detection

4. **Added buffer error checking**
   - ClearCommError() checks before each write operation
   - Detects buffer full/empty conditions and timeouts

5. **Improved reconnect logic**
   - Purge buffers on reconnect attempts
   - Recheck CTS state after recovery
   - Better handling of temporary communication failures

6. **Reduced chunk size for better reliability**
   - Changed from 2048 bytes to 1024 bytes per write
   - Faster confirmation on slow serial ports
   - Better overall stability on USB connections

### 📊 Statistics

- **Total lines of code**: ~1100 lines (main.go)
- **Fixes implemented**: 6 major improvements
- **Backward compatibility**: Fully compatible with V37 functionality

### 🎯 Key Improvements

| Metric | V37 | V38 | Improvement |
|--------|-----|-----|-------------|
| FlushFileBuffers blocking | Yes | No | ✅ Fixed |
| CTS diagnostics | No | Yes | ✅ Added |
| RTS/CTS handshake | Enabled | Disabled | ✅ Fixed |
| Buffer error checking | No | Yes | ✅ Added |
| Reconnect handling | Basic | Advanced | ✅ Improved |
| Chunk size reliability | 2048 bytes | 1024 bytes | ✅ Better |

### 🔄 Changes from V37

**Removed:**
- Blocking `FlushFileBuffers` call after data writes
- RTS_CONTROL_HANDSHAKE in serial port configuration

**Added:**
- `checkCTS()` function for signal readiness checking
- `clearCommError()` function for buffer state monitoring
- Improved error messages with detailed diagnostics
- Watchdog timeout mechanism (3 seconds)

**Changed:**
- Reduced chunk size from 2048 to 1024 bytes
- Enhanced reconnect logic with buffer purge
- Better log messages for debugging

### 📝 Migration Notes

No action required! V38 maintains full backward compatibility:

- ✅ Same API and functionality as V37
- ✅ All existing features preserved
- ✅ Automatic detection of COM port
- ✅ Console session management unchanged
- ✅ OpenCode AI integration maintained

Simply update to new version or recompile with `main_v38.go`.

### 🚀 Getting Started

**Quick Start (Ready-to-use EXE):**
```powershell
cd C:\35inchENG\v2\Turing35NeonV38
.\Turing35NeonStableV38_temp.exe --test
```

**From Source:**
```powershell
cd C:\35inchENG\v2\3.5inch-turing-smart-screen-main
go build -ldflags="-s -w" -o Turing35NeonStableV38.exe main_v38.go
```

### 📋 Build Instructions

**Prerequisites:**
- Go 1.23 or later
- Windows SDK (included with Go on Windows)

**Build Command:**
```powershell
go build -ldflags="-s -w" -o Turing35NeonStableV38.exe main_v38.go
```

**Run Tests:**
```powershell
Turing35NeonStableV38.exe --test
```

### 🔍 Testing Recommendations

1. **Test Mode First**: Always start with `--test` flag for 30-minute stability check
2. **Monitor Logs**: Watch `app_v37.log` for any CTS warnings or errors
3. **Gradual Transition**: Start simple metrics, then enable neon animation

### 📞 Support

If you encounter issues:
1. Check application logs (`app_v37.log`)
2. Verify USB connection and driver status
3. Try different USB port/cable if needed
4. Update CH340 driver to latest version

See [Troubleshooting Guide](docs/TROUBLESHOOTING.md) for detailed help.

---

## Version History

### V37 - Initial Release
- First public release
- Basic COM port communication
- Known issues with long-running sessions

### V38 - Fixed COM Hang Issues (Current)
- Comprehensive fixes for serial communication stability
- All known issues from V37 addressed
- Ready for production use

---

## Acknowledgments

Special thanks to:
- CH340 driver developers
- Windows COM port API documentation contributors  
- OpenCode AI team for Go API support

---

## License

MIT License - See [LICENSE](./LICENSE) file

---

**Thank you for using Turing 3.5 Neon Monitor V38!**

<div align="center">
  <a href="#readme"><img src="https://img.shields.io/badge/Back%20to%20Top-000000?style=for-the-badge" height="20"></a>
</div>
