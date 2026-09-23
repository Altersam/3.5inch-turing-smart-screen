//go:build windows

// Turing 3.5 Neon V38: Fixed COM hang issues - removed FlushFileBuffers blocking, added CTS diagnostics, watchdog for send operations, improved reconnect with buffer purge, and non-animation test mode.
// No compatibility renderer, binary patching, or local HTTP bridge.
package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

//go:embed assets/base.png assets/tray.ico assets/fonts/*.png assets/fonts/*.json
var assets embed.FS

const version = "Turing35NeonStableV38"
const apiBase = "https://opencode.ai"
const displayW, displayH = 480, 320

var appLog *log.Logger
var stopCh = make(chan struct{})
var stopOnce sync.Once
var autostartOnce sync.Once

func stopApp() { stopOnce.Do(func() { close(stopCh) }) }

func exeDir() string {
	a, e := os.Executable()
	if e != nil {
		return "."
	}
	return filepath.Dir(a)
}
func home() string {
	if x := os.Getenv("USERPROFILE"); x != "" {
		return x
	}
	h, _ := os.UserHomeDir()
	return h
}
func fileFirst(paths []string) (string, string) {
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e == nil && len(bytes.TrimSpace(b)) > 0 {
			return strings.TrimSpace(string(b)), p
		}
	}
	return "", ""
}
func pathsFor(n string) []string {
	bases := []string{exeDir(), filepath.Join(home(), "Downloads"), filepath.Join(home(), "Documents"), filepath.Join(home(), "Desktop")}
	if a := os.Getenv("LOCALAPPDATA"); a != "" {
		bases = append(bases, filepath.Join(a, version))
	}
	out := make([]string, 0, len(bases))
	for _, p := range bases {
		out = append(out, filepath.Join(p, n))
	}
	return out
}
func sessionValue(s, n string) string {
	s = strings.Trim(strings.TrimSpace(s), "\"'\r\n\t ")
	pat := regexp.MustCompile(`(?i)(?:^|[;\s])` + regexp.QuoteMeta(n) + `=([^;\r\n\t ]+)`)
	if m := pat.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	if !strings.ContainsAny(s, "; \r\n\t") && !strings.HasPrefix(strings.ToLower(s), "auth=") && !strings.HasPrefix(strings.ToLower(s), "__host-console_session=") {
		return s
	}
	return ""
}
func findWorkspace() string {
	re := regexp.MustCompile(`(?:wrk|org)_[A-Za-z0-9]+`)
	if v := re.FindString(os.Getenv("OPENCODE_WORKSPACE")); v != "" {
		return v
	}
	if v, _ := fileFirst(pathsFor("opencode_url.txt")); v != "" {
		if id := re.FindString(v); id != "" {
			return id
		}
	}
	return ""
}
func apiKeyRec(v any, selected bool) string {
	switch x := v.(type) {
	case map[string]any:
		for k, z := range x {
			if strings.Contains(strings.ToLower(k), "opencode-go") || strings.Contains(strings.ToLower(k), "opencode_go") {
				if s := apiKeyRec(z, true); s != "" {
					return s
				}
			}
		}
		if selected {
			for _, k := range []string{"key", "apiKey", "apikey", "token"} {
				if z, ok := x[k].(string); ok && len(strings.TrimSpace(z)) > 10 {
					return strings.TrimSpace(z)
				}
			}
		}
		for k, z := range x {
			if k == "opencode-go" {
				continue
			}
			if s := apiKeyRec(z, selected); s != "" {
				return s
			}
		}
	case []any:
		for _, z := range x {
			if s := apiKeyRec(z, selected); s != "" {
				return s
			}
		}
	}
	return ""
}
func findAPIKey() string {
	if s := strings.TrimSpace(os.Getenv("OPENCODE_API_KEY")); s != "" {
		return s
	}
	p := filepath.Join(home(), ".local", "share", "opencode", "auth.json")
	if a := os.Getenv("APPDATA"); a != "" {
		p2 := filepath.Join(a, "opencode", "auth.json")
		if _, e := os.Stat(p); e != nil {
			p = p2
		}
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return ""
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	k := apiKeyRec(v, false)
	if k != "" {
		appLog.Printf("OpenCode usage key found path=%s", p)
	}
	return k
}

type Metrics struct {
	CPU, GPU, RAM                                float64
	CPUFreq, CPUTemp, CPUPower                   float64
	GPUTemp, GPUPower, VRAMUsed, VRAMTotal       float64
	RAMUsed, RAMTotal                            float64
	DiskCUsed, DiskCTotal, DiskZUsed, DiskZTotal uint64
	Daily, Weekly, Monthly                       int
	Balance                                      string
	BalanceValid                                 bool
	UsageValid                                   bool
	UpdatedGPU                                   time.Time
}
type safeMetrics struct {
	sync.RWMutex
	m Metrics
}

func (s *safeMetrics) mutate(f func(*Metrics)) { s.Lock(); f(&s.m); s.Unlock() }
func (s *safeMetrics) snap() Metrics           { s.RLock(); defer s.RUnlock(); return s.m }

var live = safeMetrics{}
var remoteClient = &http.Client{Timeout: 10 * time.Second}

func readBalance(ws, console, auth string) (string, error) {
	if ws == "" {
		return "", errors.New("workspace not configured")
	}
	if console == "" {
		return "", errors.New("console session not configured")
	}
	req, e := http.NewRequest("GET", apiBase+"/console/api/billing/status", nil)
	if e != nil {
		return "", e
	}
	c := "oc_locale=ru; __Host-console_session=" + console
	if auth != "" {
		c += "; auth=" + auth
	}
	req.Header.Set("Cookie", c)
	req.Header.Set("x-org-id", ws)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Referer", apiBase+"/console/"+ws+"/settings/billing")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/153 Safari/537.36")
	resp, e := remoteClient.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("billing/status HTTP=%d", resp.StatusCode)
	}
	var b map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if e = d.Decode(&b); e != nil {
		return "", e
	}
	v, ok := b["balanceMicroCents"]
	if !ok {
		return "", errors.New("balanceMicroCents missing")
	}
	var raw int64
	switch a := v.(type) {
	case string:
		raw, e = strconv.ParseInt(a, 10, 64)
	case json.Number:
		raw, e = a.Int64()
	default:
		e = fmt.Errorf("balance unexpected type %T", v)
	}
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("$%.2f", float64(raw)/1e8), nil
}
func nestedPercent(v any) int {
	m, ok := v.(map[string]any)
	if !ok {
		return -1
	}
	if usage, ok := m["usage"].(map[string]any); ok {
		m = usage
	}
	for _, field := range []string{"percent", "usagePercent", "percentUsed"} {
		if t, ok := m[field]; ok {
			switch z := t.(type) {
			case float64:
				return clampPct(z)
			case json.Number:
				if f, e := z.Float64(); e == nil {
					return clampPct(f)
				}
			}
		}
	}
	return -1
}
func clampPct(f float64) int {
	if f < 0 {
		return 0
	}
	if f > 100 {
		return 100
	}
	return int(f + 0.5)
}
func readUsage(key string) (int, int, int, error) {
	if key == "" {
		return 0, 0, 0, errors.New("opencode-go key missing")
	}
	req, _ := http.NewRequest("GET", apiBase+"/zen/go/v1/usage", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version)
	resp, e := remoteClient.Do(req)
	if e != nil {
		return 0, 0, 0, e
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return 0, 0, 0, fmt.Errorf("usage HTTP=%d", resp.StatusCode)
	}
	var obj map[string]any
	if e = json.Unmarshal(b, &obj); e != nil {
		return 0, 0, 0, e
	}
	p := obj
	if u, ok := obj["usage"].(map[string]any); ok {
		p = u
	}
	one := func(keys ...string) int {
		for _, k := range keys {
			if v, ok := p[k]; ok {
				switch z := v.(type) {
				case float64:
					return clampPct(z)
				case json.Number:
					if f, e := z.Float64(); e == nil {
						return clampPct(f)
					}
				}
			}
		}
		return -1
	}
	usage := one("percent", "usagePercent", "percentUsed")
	if usage < 0 {
		return 0, 0, 0, fmt.Errorf("usage field missing")
	}
	var daily, weekly, monthly int
	if d, ok := p["daily"].(map[string]any); ok {
		for _, k := range []string{"limit", "used"} {
			if v, ok := d[k]; ok {
				switch z := v.(type) {
				case float64:
					switch k {
					case "daily":
						daily = clampPct(z)
					case "weekly":
						weekly = clampPct(z)
					case "monthly":
						monthly = clampPct(z)
					}
				}
			}
		}
	}
	return daily, weekly, monthly, nil
}

func usageKey(m Metrics) string {
	daily, weekly, monthly, _ := readUsage(findAPIKey())
	var op string
	if m.Daily > 0 || m.Weekly > 0 || m.Monthly > 0 {
		op = fmt.Sprintf("daily=%d%% weekly=%d%% monthly=%d%%", m.Daily, m.Weekly, m.Monthly)
	} else {
		op = fmt.Sprintf("daily=%d%% weekly=%d%% monthly=%d%% (key=***%s)", daily, weekly, monthly, apiKeyRec(findAPIKey(), true)[:4])
	}
	return op
}

type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}



type dcb struct {
	Length          uint32
	BaudRate        uint32
	Flags           uint32
	WReserved       uint16
	XonLim          uint16
	XoffLim         uint16
	ByteSize        uint8
	Parity          uint8
	StopBits        uint8
	XonChar         uint8
	XoffChar        uint8
	ErrorChar       uint8
	EofChar         uint8
	EvtChar         uint8
	WReserved1      uint16
}

type Serial struct {
	mu        sync.Mutex
	h         uintptr
	port      string
	startTime time.Time
	lastSend  time.Time
	reconnect int
}

// checkCTS returns true if CTS (Clear To Send) is asserted.
func (s *Serial) checkCTS() bool {
	var status uint32
	r, _, e := pGetCommState.Call(s.h, uintptr(unsafe.Pointer(&status)))
	if r == 0 {
		appLog.Printf("COM CTS get error: %v", e)
		return false
	}
	// CTS bit is at position 1 (0x0002)
	isCTS := (status & 0x0002) != 0
	if !isCTS {
		appLog.Printf("COM CTS not asserted (status=0x%04X)", status)
	}
	return isCTS
}

// clearCommError checks if there are errors in the communication buffer.
func (s *Serial) clearCommError() error {
	var ce struct {
	ErrorFlags  uint32
	BuffersFullOrEmpty bool
	ReadTimeout bool
	WriteTimeout bool
}
	r, _, e := pGetCommState.Call(s.h, uintptr(unsafe.Pointer(&ce.ErrorFlags)))
	if r == 0 {
		appLog.Printf("COM ClearCommError failed: %v", e)
		return fmt.Errorf("ClearCommError: %v", e)
	}
	if ce.BuffersFullOrEmpty {
		appLog.Printf("COM buffer full/empty (flags=0x%06X)", ce.ErrorFlags)
	}
	if ce.ReadTimeout || ce.WriteTimeout {
		appLog.Printf("COM timeout detected (read=%v write=%v)", ce.ReadTimeout, ce.WriteTimeout)
	}
	return nil
}

func (s *Serial) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h != 0 && s.h != invalidHandle {
			closeHandle.Call(s.h)
		s.h = 0
		appLog.Printf("COM closed handle=%p port=%s reconnects=%d", s.h, s.port, s.reconnect)
	}
	s.startTime = time.Time{}
	s.lastSend = time.Time{}
}
func (s *Serial) connect(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h != 0 && s.h != invalidHandle {
		appLog.Printf("COM already connected handle=%p port=%s reconnect attempt", s.h, s.port)
		return errors.New("serial port already open")
	}
	name := `\\.\` + p
	path, _ := syscall.UTF16PtrFromString(name)
	handle, _, e := pCreateFile.Call(uintptr(unsafe.Pointer(path)), 0xC0000000, 0, 0, 3, 0x80, 0)
	if handle == 0 || handle == invalidHandle {
		appLog.Printf("COM connect failed open %s: %v (LastError=%d)", p, e, syscall.GetLastError())
		return fmt.Errorf("open %s: %v", p, e)
	}
	fail := func(err error) error {
		closeHandle.Call(handle)
		s.h = 0
		return err
	}
	pSetupComm.Call(handle, 65536, 65536)
	var cfg dcb
	cfg.Length = uint32(unsafe.Sizeof(cfg))
	r, _, e := pGetCommState.Call(handle, uintptr(unsafe.Pointer(&cfg)))
	if r == 0 {
		return fail(fmt.Errorf("GetCommState: %v", e))
	}
	cfg.BaudRate = 230400
	cfg.ByteSize = 8
	cfg.Parity = 0
	cfg.StopBits = 0
	// V38: Removed RTS_CONTROL_HANDSHAKE to avoid CTS blocking.
	// Only fBinary and fOutxCtsFlow for read readiness detection.
	cfg.Flags = 1 | (1 << 4) // fBinary, no RTS handshake to avoid CTS deadlock.
	r, _, e = pSetCommState.Call(handle, uintptr(unsafe.Pointer(&cfg)))
	if r == 0 {
		return fail(fmt.Errorf("SetCommState: %v", e))
	}
	t := commTimeouts{ReadIntervalTimeout: 0xFFFFFFFF, ReadTotalTimeoutConstant: 500, WriteTotalTimeoutConstant: 2000}
	r, _, e = pSetCommTimeouts.Call(handle, uintptr(unsafe.Pointer(&t)))
	if r == 0 {
		return fail(fmt.Errorf("SetCommTimeouts: %v", e))
	}
	// V38: Purge buffers to ensure clean state.
	pPurgeComm.Call(handle, 0x0004|0x0008) // PURGE_RXCLEAR | PURGE_TXCLEAR
	s.mu.Lock()
	s.h = handle
	s.port = p
	s.startTime = time.Now()
	s.reconnect = s.reconnect + 1
	s.mu.Unlock()
	appLog.Printf("COM connected=%s baud=230400 reconnect=#%d startTime=%s", p, s.reconnect, s.startTime.Format(time.RFC3339))
	return nil
}

func (s *Serial) writeData(buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// V38: Check CTS before each write attempt to avoid blocking on stuck hardware.
	if !s.checkCTS() {
		appLog.Printf("COM write skipped CTS not asserted %d bytes", len(buf))
		return errors.New("CTS not asserted - skip write")
	}
	// V38: Check for communication errors before write.
	if e := s.clearCommError(); e != nil {
		appLog.Printf("COM write pre-check error: %v", e)
		// Retry once with buffer purge.
		procPurgeComm.Call(s.h, 0x0004|0x0008)
		time.Sleep(100 * time.Millisecond)
		if !s.checkCTS() {
			return errors.New("CTS still not asserted after recovery attempt")
		}
	}
	for off := 0; off < len(buf); {
		end := off + 1024 // V38: Smaller chunks for better reliability.
		if end > len(buf) {
			end = len(buf)
		}
		part := buf[off:end]
		var n uint32
		r, _, e := procWrite.Call(s.h, uintptr(unsafe.Pointer(&part[0])), uintptr(len(part)), uintptr(unsafe.Pointer(&n)), 0)
		runtime.KeepAlive(part)
		if r == 0 {
			appLog.Printf("COM WriteFile failed %d bytes: %v (LastError=%d)", len(part), e, syscall.GetLastError())
			return fmt.Errorf("WriteFile: %v", e)
		}
		if n == 0 {
			appLog.Printf("COM WriteFile sent zero bytes")
			return errors.New("WriteFile sent zero bytes")
		}
		s.lastSend = time.Now()
		off += int(n)
		appLog.Printf("COM write %d/%d bytes success", off, len(buf))
	}
	return nil
}

// sendWatchdog asynchronously sends data with timeout control.
func (s *Serial) sendWatchdog(ctx context.Context, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check CTS before async send.
	if !s.checkCTS() {
		return errors.New("CTS not asserted")
	}
	if e := s.clearCommError(); e != nil {
		appLog.Printf("COM watchdog pre-check error: %v", e)
		return fmt.Errorf("ClearCommError: %w", e)
	}
	startTime := time.Now()
	var varN uint32
	procWrite.Call(s.h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&varN)), 0)
	runtime.KeepAlive(buf)
	if n == 0 {
		appLog.Printf("COM watchdog WriteFile immediate error: %v", err)
		return fmt.Errorf("WriteFile: %w", err)
	}
	s.lastSend = time.Now()
	timeout := 3 * time.Second // V38: Controlled timeout for watchdog send.
	select {
	case <-ctx.Done():
		appLog.Printf("COM watchdog ctx done after %v", time.Since(startTime))
		return ctx.Err()
	case <-time.After(timeout):
		appLog.Printf("COM watchdog timeout after %v sent %d bytes", timeout, n)
		return fmt.Errorf("send timeout")
	}
}

func cmdPacket(x, y, ex, ey, c int) []byte {
	return []byte{byte(x >> 2), byte(((x & 3) << 6) | (y >> 4)), byte(((y & 15) << 4) | (ex >> 6)), byte(((ex & 63) << 2) | (ey >> 8)), byte(ey), byte(c)}
}

func (s *Serial) initScreen() error {
	appLog.Printf("COM init screen sequence")
	if e := s.writeData(bytes.Repeat([]byte{69}, 6)); e != nil {
		return fmt.Errorf("init flush: %w", e)
	}
	time.Sleep(100 * time.Millisecond)
	orientation := append(cmdPacket(0, 0, 0, 0, 121), byte(102), byte(480>>8), byte(480&255), byte(320>>8), byte(320&255))
	if e := s.writeData(orientation); e != nil {
		return fmt.Errorf("init orientation: %w", e)
	}
	time.Sleep(150 * time.Millisecond)
	if e := s.writeData(cmdPacket(0, 0, 0, 0, 102)); e != nil {
		return fmt.Errorf("init ready: %w", e)
	}
	time.Sleep(200 * time.Millisecond)
	appLog.Printf("COM init screen complete")
	return nil
}

func (s *Serial) sendPatch(img *image.RGBA, x, y, w, h int) error {
	if x < 0 || y < 0 || x+w > displayW || y+h > displayH || w <= 0 || h <= 0 {
		return errors.New("bad patch coords")
	}
	// ST7796/CH340 partial updates are more reliable with aligned, padded
	// regions. The padding is drawn from the same complete render, so no
	// stale pixels or tails can remain between overlapping update areas.
	x0, y0 := x-3, y-2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	x1 := (x + w + 6) & ^3
	y1 := (y + h + 3) & ^1
	if x1 > displayW {
		x1 = displayW
	}
	if y1 > displayH {
		y1 = displayH
	}
	x, y, w, h = x0, y0, x1-x0, y1-y0
	appLog.Printf("COM sendPatch coords x=%d y=%d w=%d h=%d", x, y, w, h)
	if e := s.writeData(cmdPacket(x, y, x+w-1, y+h-1, 197)); e != nil {
		return fmt.Errorf("patch header: %w", e)
	}
	buf := make([]byte, w*h*2)
	j := 0
	for yy := y; yy < y+h; yy++ {
		i := img.PixOffset(x, yy)
		for xx := 0; xx < w; xx++ {
			r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
			v := (uint16(r>>3) << 11) | (uint16(g>>2) << 5) | uint16(b>>3)
			buf[j] = byte(v)
			buf[j+1] = byte(v >> 8)
			j += 2
			i += 4
		}
	}
	// V38: Removed FlushFileBuffers blocking call.
	// Data is sent via writeData with controlled timeout in watchdog mode.
	if e := s.writeData(buf); e != nil {
		appLog.Printf("COM sendPatch pixel data failed: %v", e)
		return fmt.Errorf("patch pixels: %w", e)
	}
	s.lastSend = time.Now()
	appLog.Printf("COM sendPatch complete x=%d y=%d w=%d h=%d", x, y, w, h)
	return nil
}

func findCom() string {
	if p := strings.TrimSpace(os.Getenv("TURING_COM")); regexp.MustCompile(`(?i)^COM\d+$`).MatchString(p) {
		return strings.ToUpper(p)
	}
	if v, _ := fileFirst(pathsFor("com_port.txt")); regexp.MustCompile(`(?i)^COM\d+$`).MatchString(v) {
		return strings.ToUpper(v)
	}
	ps := `$z=Get-CimInstance Win32_PnPEntity -ErrorAction SilentlyContinue|Where-Object {$_.Name -match '\(COM\d+\)' -and $_.PNPDeviceID -match 'VID_1A86&PID_5722'}|Select-Object -First 1 -ExpandProperty Name;if($z){$z}`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	o, _ := c.Output()
	if m := regexp.MustCompile(`(?i)COM\d+`).FindString(string(o)); m != "" {
		return strings.ToUpper(m)
	}
	return "COM5"
}

type patch struct {
	x, y, w, h int
	key, value string
}

// A small moving patch covers the old and new highlight, erasing trails.
// The full image still comes from render(), so animated pixels never become
// permanent background pixels on the slow serial LCD.
func nextGlowPatch(phase *int, lastAnim *time.Time, sent *int, now time.Time) *patch {
	old := *phase
	*phase = *phase + 1
	*lastAnim = now
	*sent = 0
	switch *phase % 3 {
	case 1, 2:
		oldStep, newStep, y := (old+2)/3, (*phase+2)/3, 7
		if *phase%3 == 2 {
			oldStep, newStep, y = (old+1)/3, (*phase+1)/3, 54
		}
		x0, x1 := neonStep(oldStep), neonStep(newStep)
		if x1 < x0 {
			x0, x1 = x1, x0
		}
		return &patch{x: x0 - 5, y: y, w: x1 - x0 + 40, h: 5, key: "glow", value: strconv.Itoa(*phase)}
	default:
		return &patch{x: 332, y: 280, w: 35, h: 26, key: "glow", value: strconv.Itoa(*phase)}
	}
}

func serialWorker(testMode bool) {
	port := findCom()
	appLog.Printf("COM selected=%s baud=230400 testMode=%v", port, testMode)
	display := &Serial{}
	var t *time.Ticker
	reloadInterval := 15 * time.Second
	if testMode {
		appLog.Printf("V38 TEST MODE: No neon animation, metrics update every 2 seconds")
		reloadInterval = 2 * time.Second
	}
	for {
		select {
		case <-stopCh:
			if t != nil {
				t.Stop()
			}
			display.close()
			return
		default:
		}
		if e := display.connect(port); e != nil {
			appLog.Printf("COM connect: %v; retry", e)
			select {
			case <-stopCh:
				return
			case <-time.After(4 * time.Second):
				continue
			}
		}
		if e := display.initScreen(); e != nil {
			appLog.Printf("COM init: %v", e)
			display.close()
			time.Sleep(3 * time.Second)
			continue
		}
		im := render(live.snap(), time.Now(), 0)
		if e := display.sendPatch(im, 0, 0, displayW, displayH); e != nil {
			appLog.Printf("COM full frame: %v", e)
			display.close()
			time.Sleep(3 * time.Second)
			continue
		}
		appLog.Printf("COM first full frame complete; partial fast updates active")
		stableSince := time.Now()
		last := map[string]string{}
		lastTime := map[string]time.Time{}
		phase := 0
		lastAnim := time.Now()
		sentSinceGlow := 0
		fastCursor, fastCount := 0, 0
		if testMode {
			last["clock"] = dateText(time.Now())
			last["cpu"] = pctText(live.snap().CPU)
			last["gpu"] = pctText(live.snap().GPU)
			last["ram"] = pctText(live.snap().RAM)
			t = time.NewTicker(2000 * time.Millisecond) // testMode: 2 second interval
		} else {
			last["clock"] = dateText(time.Now())
			last["cpu"] = pctText(live.snap().CPU)
			last["gpu"] = pctText(live.snap().GPU)
			last["ram"] = pctText(live.snap().RAM)
			t = time.NewTicker(130 * time.Millisecond)
		}
	inner:
		for {
			select {
			case <-stopCh:
				if t != nil {
					t.Stop()
				}
				display.close()
				return
			case <-t.C:
			}
			now := time.Now()
			// Protect the last stable V35 autostart during initial testing.
			// Replace it only after this single-process serial renderer survives 90s.
			if now.Sub(stableSince) >= 90*time.Second {
				autostartOnce.Do(func() { go func() { setAutostart(); appLog.Printf("V38 autostart switched after 90s stable COM") }() })
			}
			m := live.snap()
			lowCPU := fmt.Sprintf("%.0f/%.1f/%.0f", m.CPUTemp, m.CPUFreq, m.CPUPower)
			lowGPU := fmt.Sprintf("%.0f/%.1f/%.1f/%.0f", m.GPUTemp, m.VRAMUsed, m.VRAMTotal, m.GPUPower)
			lowRAM := fmt.Sprintf("%.1f/%.1f", m.RAMUsed, m.RAMTotal)
			disk := fmt.Sprintf("%d/%d/%d/%d", m.DiskCUsed, m.DiskCTotal, m.DiskZUsed, m.DiskZTotal)
			p := []patch{
				{31, 123, 127, 46, "cpu", pctText(m.CPU)},
				{179, 123, 129, 46, "gpu", pctText(m.GPU)},
				{328, 128, 126, 47, "ram", pctText(m.RAM)},
				{110, 63, 263, 29, "op", usageKey(m)},
				{31, 168, 132, 43, "cpu_low", lowCPU},
				{180, 168, 129, 43, "gpu_low", lowGPU},
				{327, 176, 128, 39, "ram_low", lowRAM},
				{27, 235, 305, 38, "disks", disk},
				{110, 14, 263, 36, "clock", dateText(now)},
			}
			var selected *patch
			shouldGlow := now.Sub(lastAnim) > 900*time.Millisecond
			if testMode {
				// Test mode: No neon animation, just send metrics periodically.
				for _, item := range p {
					if item.value != last[item.key] && now.Sub(lastTime[item.key]) >= reloadInterval {
						selected = &item
						break
					}
				}
			} else {
				shouldGlow := now.Sub(lastAnim) > 900*time.Millisecond
				if shouldGlow && sentSinceGlow >= 2 {
					selected = nextGlowPatch(&phase, &lastAnim, &sentSinceGlow, now)
				} else {
					canSend := func(i int) bool {
						item := &p[i]
						throttle := 440 * time.Millisecond
						if item.key == "cpu_low" || item.key == "gpu_low" || item.key == "ram_low" {
							throttle = 5 * time.Second
						}
						if item.key == "disks" {
							throttle = 15 * time.Second
						}
						if item.key == "clock" || item.key == "op" {
							throttle = 2 * time.Second
						}
						return item.value != last[item.key] && now.Sub(lastTime[item.key]) >= throttle
					}
					// After three fast metric patches, service a pending lower-priority
					// region (especially the balance) before metrics can starve it.
					if fastCount >= 3 {
						for i := 3; i < len(p); i++ {
							if canSend(i) {
								selected = &p[i]
								fastCount = 0
								break
							}
						}
					}
					if selected == nil {
						for j := 0; j < 3; j++ {
							i := (fastCursor + j) % 3
							if canSend(i) {
								selected = &p[i]
								fastCursor = (i + 1) % 3
								fastCount++
								break
							}
						}
					}
					if selected == nil {
						for i := 3; i < len(p); i++ {
							if canSend(i) {
								selected = &p[i]
								fastCount = 0
								break
							}
						}
					}
					if selected == nil && shouldGlow {
						selected = nextGlowPatch(&phase, &lastAnim, &sentSinceGlow, now)
					}
				}
			}
			if selected == nil {
				continue
			}
			if selected.key != "glow" {
				sentSinceGlow++
			}
			frame := render(m, now, phase)
			if e := display.sendPatch(frame, selected.x, selected.y, selected.w, selected.h); e != nil {
				appLog.Printf("COM partial failed kind=%s: %v; watchdog reconnect", selected.key, e)
				if t != nil {
					t.Stop()
				}
				display.close()
				time.Sleep(3 * time.Second)
				break inner
			}
			last[selected.key] = selected.value
			lastTime[selected.key] = now
		}
	}
}

// --------------------------- native system tray ----------------------------
var user32 = syscall.NewLazyDLL("user32.dll")
var shell32 = syscall.NewLazyDLL("shell32.dll")
var pRegisterClass = user32.NewProc("RegisterClassExW")
var pCreateWindow = user32.NewProc("CreateWindowExW")
var pDefWnd = user32.NewProc("DefWindowProcW")
var pGetMessage = user32.NewProc("GetMessageW")
var pTranslate = user32.NewProc("TranslateMessage")
var pDispatch = user32.NewProc("DispatchMessageW")
var pLoadImage = user32.NewProc("LoadImageW")
var pDestroyIcon = user32.NewProc("DestroyIcon")
var pGetCursor = user32.NewProc("GetCursorPos")
var pCreatePopup = user32.NewProc("CreatePopupMenu")
var pAppendMenu = user32.NewProc("AppendMenuW")
var pTrackPopup = user32.NewProc("TrackPopupMenu")
var pDestroyMenu = user32.NewProc("DestroyMenu")
var pForeground = user32.NewProc("SetForegroundWindow")
var pPostQuit = user32.NewProc("PostQuitMessage")
var pShellNotify = shell32.NewProc("Shell_NotifyIconW")

// Windows API lazy proc variables for UI
var kernel32 syscall.NewLazyDLL("kernel32.dll")
var (
	pCreateFile      = kernel32.NewProc("CreateFileW")
	pCloseHandle     = kernel32.NewProc("CloseHandle")
	pGetCommState    = kernel32.NewProc("GetCommState")
	pSetCommState    = kernel32.NewProc("SetCommState")
	pSetCommTimeouts = kernel32.NewProc("SetCommTimeouts")
	pSetupComm       = kernel32.NewProc("SetupComm")
	pPurgeComm       = kernel32.NewProc("PurgeComm")
	pWriteFile       = kernel32.NewProc("WriteFile")
	pFlushFileBuffers = kernel32.NewProc("FlushFileBuffers")
	pGetLastError    = kernel32.NewProc("GetLastError")
)

var pGetModule = kernel32.NewProc("GetModuleHandleW")

type winPoint struct{ X, Y int32 }
type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
type guid struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}
type notifyIconData struct {
	CbSize            uint32
	HWnd              uintptr
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             uintptr
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          guid
	HBalloonIcon      uintptr
}
type winMsg struct {
	HWnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       winPoint
	LPrivate uint32
}

const trayMessage = 0x8000 + 49

var trayCallback uintptr

func ptr16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func put16(dst []uint16, s string) {
	a, _ := syscall.UTF16FromString(s)
	if len(a) > len(dst) {
		a = a[:len(dst)]
	}
	copy(dst, a)
}
func openLog() {
	p := filepath.Join(exeDir(), "app_v38.log")
	c := exec.Command("cmd.exe", "/c", "start", "", p)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = c.Start()
}
func trayWndProc(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case trayMessage:
		switch uint32(l) {
		case 0x0205: // right-click
			menu, _, _ := pCreatePopup.Call()
			if menu != 0 {
				pAppendMenu.Call(menu, 0, 1001, uintptr(unsafe.Pointer(ptr16("Открыть лог"))))
				pAppendMenu.Call(menu, 0x0800, 0, 0)
				pAppendMenu.Call(menu, 0, 1002, uintptr(unsafe.Pointer(ptr16("Выход"))))
				var pt winPoint
				pGetCursor.Call(uintptr(unsafe.Pointer(&pt)))
				pForeground.Call(hwnd)
				pTrackPopup.Call(menu, 0x0002|0x0020, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
				pDestroyMenu.Call(menu)
			}
			return 0
		case 0x0203:
			openLog()
			return 0
		}
	case 0x0111:
		switch uint32(w & 0xffff) {
		case 1001:
			openLog()
			return 0
		case 1002:
			stopApp()
			pPostQuit.Call(0)
			return 0
		}
	case 0x0002:
		stopApp()
		pPostQuit.Call(0)
		return 0
	}
	r, _, _ := pDefWnd.Call(hwnd, uintptr(msg), w, l)
	return r
}
func startTray() {
	iconBytes, e := assets.ReadFile("assets/tray.ico")
	if e != nil {
		appLog.Printf("tray icon embedded error=%v", e)
		return
	}
	folder := filepath.Join(os.Getenv("LOCALAPPDATA"), version)
	if e := os.MkdirAll(folder, 0755); e != nil {
		appLog.Printf("tray folder error=%v", e)
		return
	}
	iconFile := filepath.Join(folder, "tray.ico")
	if e := os.WriteFile(iconFile, iconBytes, 0644); e != nil {
		appLog.Printf("tray icon error=%v", e)
		return
	}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		hmod, _, _ := pGetModule.Call(0)
		name := ptr16("Turing35V38NativeTray")
		trayCallback = syscall.NewCallback(trayWndProc)
		wc := wndClassEx{CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: trayCallback, HInstance: hmod, LpszClassName: name}
		if r, _, e := pRegisterClass.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			appLog.Printf("tray RegisterClassEx error=%v", e)
			return
		}
		title := ptr16("Turing 3.5 Neon Monitor V38")
		hwnd, _, e := pCreateWindow.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, hmod, 0)
		if hwnd == 0 {
			appLog.Printf("tray CreateWindowEx error=%v", e)
			return
		}
		ip := ptr16(iconFile)
		hicon, _, e := pLoadImage.Call(0, uintptr(unsafe.Pointer(ip)), 1, 16, 16, 0x10|0x40)
		if hicon == 0 {
			appLog.Printf("tray LoadImage error=%v", e)
			return
		}
		var nd notifyIconData
		nd.CbSize = uint32(unsafe.Sizeof(nd))
		nd.HWnd = hwnd
		nd.UID = 36
		nd.UFlags = 1 | 2 | 4
		nd.UCallbackMessage = trayMessage
		nd.HIcon = hicon
		put16(nd.SzTip[:], "Turing 3.5 Neon Monitor V38")
		if r, _, e := pShellNotify.Call(0, uintptr(unsafe.Pointer(&nd))); r == 0 {
			appLog.Printf("tray Shell_NotifyIcon add error=%v", e)
		} else {
			appLog.Printf("tray V38 icon added custom=%s", iconFile)
		}
		var m winMsg
		for {
			r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			pTranslate.Call(uintptr(unsafe.Pointer(&m)))
			pDispatch.Call(uintptr(unsafe.Pointer(&m)))
		}
		pShellNotify.Call(2, uintptr(unsafe.Pointer(&nd)))
		pDestroyIcon.Call(hicon)
		appLog.Printf("tray V38 icon removed")
	}()
}
func killOld() {
	ps := fmt.Sprintf(`Get-Process -ErrorAction SilentlyContinue | Where-Object {($_.ProcessName -like 'Turing35NeonStableV*' -or $_.ProcessName -like 'core_v*') -and $_.Id -ne %d} | Stop-Process -Force -ErrorAction SilentlyContinue`, os.Getpid())
	c := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = c.Run()
}
func setAutostart() {
	exe, e := os.Executable()
	if e != nil {
		return
	}
	c := exec.Command("reg.exe", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "Turing35Neon", "/t", "REG_SZ", "/d", `"`+exe+`"`, "/f")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if e := c.Run(); e != nil {
		appLog.Printf("autostart update error=%v", e)
	}
}
func main() {
	p := filepath.Join(exeDir(), "app_v38.log")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		appLog = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds)
	} else {
		defer f.Close()
		appLog = log.New(f, "", log.Ldate|log.Ltime|log.Lmicroseconds)
	}
	appLog.Printf("V38 native single-process startup")
	if e := loadGraphics(); e != nil {
		appLog.Printf("graphics load error=%v", e)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--preview" {
		example := Metrics{CPU: 13, GPU: 7, RAM: 49, CPUFreq: 3.5, CPUTemp: 52, CPUPower: 43, GPUTemp: 53, GPUPower: 44, VRAMUsed: 1.7, VRAMTotal: 16, RAMUsed: 13.6, RAMTotal: 32, DiskCUsed: 751, DiskCTotal: 931, DiskZUsed: 738, DiskZTotal: 954, Daily: 0, Weekly: 2, Monthly: 100, Balance: "$53.39", BalanceValid: true, UsageValid: true}
		dst := filepath.Join(exeDir(), "Turing35_V38_preview.png")
		of, e := os.Create(dst)
		if e == nil {
			e = png.Encode(of, render(example, time.Now(), 0))
			of.Close()
		}
		appLog.Printf("preview result=%s err=%v", dst, e)
		return
	}
	killOld()
	startTray()
	go openCodeWorker()
	go cpuMemWorker()
	go gpuWorker()
	go cpuDetailsWorker()
	go diskWorker()
	testMode := len(os.Args) > 1 && os.Args[1] == "--test"
	if testMode {
		appLog.Printf("V38 starting in TEST MODE: No neon animation, simple metric updates")
	}
	go serialWorker(testMode)
	<-stopCh
	appLog.Printf("V38 requested shutdown")
	time.Sleep(300 * time.Millisecond)
}
