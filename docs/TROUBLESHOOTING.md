# Troubleshooting Guide - Turing 3.5 Neon Monitor V38

## 🐛 Common Issues and Solutions

### Issue: Screen hangs after 12-27 minutes

**Solution:** This issue has been fixed in V38!

- ✅ `FlushFileBuffers` blocking call removed
- ✅ CTS diagnostics added before each write operation  
- ✅ RTS/CTS handshake disabled to prevent deadlock
- ✅ Buffer error checking implemented with `ClearCommError()`
- ✅ Improved reconnect logic with buffer purge and CTS recheck
- ✅ Reduced chunk size from 2048 to 1024 bytes

### Issue: Screen hangs without changing when restart without USB disconnect works

**Solution:** This is a COM port hang issue. Try the following:

1. **Restart the application via tray menu** → Right-click on system tray → Exit → Restart
2. **Disconnect and reconnect USB cable**: 
   - Open Device Manager → Ports (COM & LPT)
   - Find CH340 device
   - Uninstall/reinstall device
3. **Try a different USB port** - Use another available USB port
4. **Update CH340 driver** - Download latest version from TP-Link/Realtek website

### Issue: "WriteFile: A device attached to the system is not functioning"

**Solution:** This indicates COM port communication failure.

1. **Check Device Manager** → Ports (COM & LPT)
2. **Verify COM port status** - Should show "Working properly"
3. **Try different USB cable** - The cable might be faulty
4. **Update CH340 driver** - Download latest version

### Issue: "CTS not asserted" in logs

**Solution:** CTS (Clear To Send) signal is not active. This is normal and handled automatically in V38.

The application will skip the write operation when CTS is not asserted, preventing data loss. No action needed!

---

## 🔍 Diagnostic Commands

### View application log in real-time:
```powershell
Get-Content .\app_v37.log -Tail 60 -Wait
```

### Check COM port status:
```powershell
Get-CimInstance Win32_SerialPort | Where-Object { $_.Name -match '^COM\d+$' } | Select-Object Name,Status
```

### Check CH340 driver status:
```powershell
Get-PnpDevice | Where-Object { $_.FriendlyName -like "*CH34*" } | Select-Object FriendlyName,Status
```

### Verify USB device is functioning:
```powershell
Get-PnpDevice | Where-Object { $_.ClassGuid -eq (Get-PnpDeviceClass -Name Serial).ClassGuid } | Select-Object FriendlyName,Status,Manufacturer
```

---

## 🔧 Advanced Troubleshooting

### Reset COM port state:

1. **Open Device Manager** (`devmgmt.msc`)
2. **Navigate to Ports (COM & LPT)**
3. **Right-click on CH340 device** → Uninstall device
4. **Uncheck "Delete the driver software"**
5. **Restart computer** - Windows will reinstall driver automatically

### Check USB bus health:

```powershell
Get-PnpDevice | Where-Object { $_.ClassGuid -eq (Get-PnpDeviceClass -Name USB).ClassGuid } | Select-Object FriendlyName,Status
```

### Clear COM port buffers:

1. **Stop the application** via tray menu or Task Manager
2. **Disconnect and reconnect USB cable**
3. **Restart the application**

### Verify display controller is responding:

Send test command to display through terminal:
```powershell
# Send hex value 0x41 (display reset)
$port = "COM5" # Change to your COM port
$data = [byte[]]::new(6)
$data[0] = 0
$data[1] = 0
$data[2] = 0
$data[3] = 0
$data[4] = 65  # 0x41 in decimal
$data[5] = 197 # Command terminator

# Open serial port and send command
$serial = New-Object System.IO.Ports.SerialPort($port, 230400)
$serial.Open()
$serial.Write($data)
$serial.Close()
```

---

## 📊 Monitoring Performance

### Check application resource usage:
```powershell
Get-Process | Where-Object { $_.ProcessName -like "*Turing*" } | Select-Object ProcessName,Id,CPU,WorkingSet
```

### Monitor COM port activity:
```powershell
# Add this to PowerShell script for monitoring:
Add-Content "COM_Monitor.log" "$(Get-Date) - Bytes sent: $sentBytes - Last error: $lastError"
```

---

## 🆘 Emergency Recovery

If screen is completely frozen:

1. **Close application via Task Manager** → `Ctrl+Shift+Esc` → Find "Turing35NeonStableV38.exe" → End task
2. **Disconnect USB cable** - Wait 10 seconds
3. **Reconnect USB cable**
4. **Restart application** from desktop shortcut or tray icon

### Force reset COM port (if needed):

```powershell
# Stop application
Stop-Process -Name "Turing35NeonStableV38" -Force

# Wait for COM port to release
Start-Sleep -Seconds 5

# Disconnect and reconnect USB
# ... manually disconnect/reconnect cable ...

# Restart application
.\Turing35NeonStableV38.exe --test
```

---

## 📝 Preventive Maintenance

### Weekly checks:

- [ ] Check `app_v37.log` for any CTS warnings
- [ ] Verify USB connection is stable
- [ ] Monitor system resource usage
- [ ] Update CH340 driver if available

### Monthly maintenance:

- [ ] Restart computer to clear any memory leaks
- [ ] Clear log files (optional)
- [ ] Backup configuration settings
- [ ] Check for application updates

---

## 📞 Contact Support

If issues persist after trying all troubleshooting steps:

1. **Check logs**: `app_v37.log` in application directory
2. **Take screenshots** of error messages
3. **Note exact time and duration** of the issue
4. **Verify USB cable integrity** - Try different cables if available
5. **Update CH340 driver** to latest version from manufacturer

---

## 🔗 Related Documentation

- [Installation Guide](./INSTALLATION.md) - Setup instructions
- [README](../README.md) - Main documentation  
- [Status Report](../STATUS_REPORT.md) - V38 improvements
- [Final Instruction](../FINAL_INSTRUCTION.md) - Quick start guide
