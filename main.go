//go:build windows

// Turing 3.5 Neon V37: one process, one tray, one serial owner, real Console balance.
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

const version = "Turing35NeonStableV37"
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
				if n := nestedPercent(v); n >= 0 {
					return n
				}
			}
		}
		return -1
	}
	day, week, month := one("rolling", "rollingUsage", "fiveHour"), one("weekly", "weeklyUsage", "week"), one("monthly", "monthlyUsage", "month")
	if day < 0 {
		return 0, 0, 0, errors.New("usage rolling percentage missing")
	}
	if week < 0 {
		week = 0
	}
	if month < 0 {
		month = 0
	}
	return day, week, month, nil
}
func openCodeWorker() {
	ws := findWorkspace()
	key := findAPIKey()
	a, ap := fileFirst(pathsFor("opencode_cookie.txt"))
	c, cp := fileFirst(pathsFor("opencode_console_cookie.txt"))
	if v := os.Getenv("OPENCODE_COOKIE"); v != "" {
		a = v
		ap = "ENV"
	}
	if v := os.Getenv("OPENCODE_CONSOLE_COOKIE"); v != "" {
		c = v
		cp = "ENV"
	}
	auth := sessionValue(a, "auth")
	console := sessionValue(c, "__Host-console_session")
	appLog.Printf("OpenCode V37 config workspaceSet=%t authCookie=%t authPath=%s consoleCookie=%t consolePath=%s key=%t", ws != "", auth != "", ap, console != "", cp, key != "")
	update := func() {
		if d, w, m, e := readUsage(key); e == nil {
			live.mutate(func(s *Metrics) { s.Daily = d; s.Weekly = w; s.Monthly = m; s.UsageValid = true })
		} else {
			appLog.Printf("OpenCode usage refresh: %v (keeping last good)", e)
		}
		if b, e := readBalance(ws, console, auth); e == nil {
			live.mutate(func(s *Metrics) { s.Balance = b; s.BalanceValid = true })
			appLog.Printf("OpenCode balance displayed data=%s", b)
		} else {
			appLog.Printf("OpenCode balance refresh: %v (keeping last good)", e)
		}
	}
	update()
	t := time.NewTicker(35 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-t.C:
			update()
		}
	}
}

// --------------------------- native Windows sensors -------------------------
var kernel = syscall.NewLazyDLL("kernel32.dll")
var procTimes = kernel.NewProc("GetSystemTimes")
var procMem = kernel.NewProc("GlobalMemoryStatusEx")
var procDrives = kernel.NewProc("GetDiskFreeSpaceExW")

type fileTime struct{ Lo, Hi uint32 }

func (x fileTime) u64() uint64 { return uint64(x.Hi)<<32 | uint64(x.Lo) }

var idleOld, kernelOld, userOld uint64

func cpuSample() float64 {
	var idle, kern, user fileTime
	r, _, _ := procTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kern)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0
	}
	a, b, c := idle.u64(), kern.u64(), user.u64()
	if kernelOld == 0 {
		idleOld, kernelOld, userOld = a, b, c
		return 0
	}
	tot := (b - kernelOld) + (c - userOld)
	idleDelta := a - idleOld
	idleOld, kernelOld, userOld = a, b, c
	if tot == 0 {
		return 0
	}
	if idleDelta > tot {
		idleDelta = tot
	}
	return 100 * float64(tot-idleDelta) / float64(tot)
}

type memoryStatus struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func memorySample() (float64, float64, float64) {
	var m memoryStatus
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := procMem.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 || m.TotalPhys == 0 {
		return 0, 0, 0
	}
	u := float64(m.TotalPhys-m.AvailPhys) / 1073741824.
	return float64(m.MemoryLoad), u, float64(m.TotalPhys) / 1073741824.
}
func diskSample(d string) (uint64, uint64) {
	p, _ := syscall.UTF16PtrFromString(d)
	var avail, total, free uint64
	r, _, _ := procDrives.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 {
		return 0, 0
	}
	return (total - free) / 1073741824, total / 1073741824
}
func cpuMemWorker() {
	_ = cpuSample()
	t := time.NewTicker(550 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-t.C:
			cpu := cpuSample()
			mem, mu, mt := memorySample()
			live.mutate(func(m *Metrics) { m.CPU = cpu; m.RAM = mem; m.RAMUsed = mu; m.RAMTotal = mt })
		}
	}
}
func findNvidiaSMI() string {
	if p, e := exec.LookPath("nvidia-smi.exe"); e == nil {
		return p
	}
	for _, p := range []string{`C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`, `C:\Windows\System32\nvidia-smi.exe`} {
		if _, e := os.Stat(p); e == nil {
			return p
		}
	}
	return ""
}
func gpuWorker() {
	tool := findNvidiaSMI()
	if tool == "" {
		appLog.Printf("GPU nvidia-smi unavailable; GPU values omitted")
		return
	}
	appLog.Printf("GPU source=%s", tool)
	query := []string{"--query-gpu=utilization.gpu,temperature.gpu,memory.used,memory.total,power.draw", "--format=csv,noheader,nounits"}
	t := time.NewTicker(950 * time.Millisecond)
	defer t.Stop()
	sample := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tool, query...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		out, e := cmd.Output()
		if e != nil {
			return
		}
		line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
		a := strings.Split(line, ",")
		if len(a) < 5 {
			return
		}
		vals := make([]float64, 5)
		for i := 0; i < 5; i++ {
			vals[i], _ = strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(a[i], "[N/A]", "0")), 64)
		}
		live.mutate(func(m *Metrics) {
			m.GPU = vals[0]
			m.GPUTemp = vals[1]
			m.VRAMUsed = vals[2] / 1024
			m.VRAMTotal = vals[3] / 1024
			m.GPUPower = vals[4]
			m.UpdatedGPU = time.Now()
		})
	}
	sample()
	for {
		select {
		case <-stopCh:
			return
		case <-t.C:
			sample()
		}
	}
}
func cpuDetailsWorker() {
	script := `$f=(Get-CimInstance Win32_Processor -ErrorAction SilentlyContinue|Measure-Object CurrentClockSpeed -Average).Average;$t=$null;$p=$null;foreach($ns in @('root/LibreHardwareMonitor','root/OpenHardwareMonitor','root/LibreHardwareMonitor/Hardware','root/OpenHardwareMonitor/Hardware')){try{$s=Get-CimInstance -Namespace $ns -ClassName Sensor -ErrorAction Stop;if($t -eq $null){$a=$s|Where-Object {$_.SensorType -eq 'Temperature' -and $_.Name -match 'CPU Package|Tctl|Tdie|CPU Core|CPU'}|Select-Object -First 1;if($a){$t=$a.Value}};if($p -eq $null){$a=$s|Where-Object {$_.SensorType -eq 'Power' -and $_.Name -match 'CPU Package|CPU'}|Select-Object -First 1;if($a){$p=$a.Value}}}catch{}};if($t -eq $null){try{$z=Get-CimInstance MSAcpi_ThermalZoneTemperature -Namespace root/wmi -ErrorAction Stop|Select-Object -First 1;if($z){$t=($z.CurrentTemperature/10)-273.15}}catch{}};"$f,$t,$p"`
	sample := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		b, e := c.Output()
		if e != nil {
			return
		}
		parts := strings.Split(strings.TrimSpace(string(b)), ",")
		if len(parts) == 0 {
			return
		}
		vals := make([]float64, 3)
		for i := 0; i < len(vals) && i < len(parts); i++ {
			vals[i], _ = strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		}
		live.mutate(func(m *Metrics) {
			if vals[0] > 0 {
				m.CPUFreq = vals[0] / 1000
			}
			if vals[1] > 5 && vals[1] < 120 {
				m.CPUTemp = vals[1]
			}
			if vals[2] > 0 && vals[2] < 500 {
				m.CPUPower = vals[2]
			}
		})
	}
	sample()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-t.C:
			sample()
		}
	}
}
func diskWorker() {
	sample := func() {
		cu, ct := diskSample(`C:\`)
		zu, zt := diskSample(`Z:\`)
		// Do not turn a transient Windows volume read error into "--/--G".
		live.mutate(func(m *Metrics) {
			if ct > 0 {
				m.DiskCUsed, m.DiskCTotal = cu, ct
			}
			if zt > 0 {
				m.DiskZUsed, m.DiskZTotal = zu, zt
			}
		})
		if zt == 0 {
			appLog.Printf("disk Z: GetDiskFreeSpaceExW unavailable; keeping last good")
		}
		if ct == 0 {
			appLog.Printf("disk C: GetDiskFreeSpaceExW unavailable; keeping last good")
		}
		if zt > 0 {
			appLog.Printf("disk Z: used=%dGiB total=%dGiB", zu, zt)
		}
	}
	sample()
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-t.C:
			sample()
		}
	}
}

// ----------------------------- text renderer -------------------------------
type glyph struct {
	X int `json:"x"`
	W int `json:"w"`
}
type fontMeta struct {
	Size   int              `json:"size"`
	Height int              `json:"height"`
	Glyphs map[string]glyph `json:"glyphs"`
}
type bitmapFont struct {
	image image.Image
	meta  fontMeta
}

var fonts = map[int]bitmapFont{}
var baseImage *image.RGBA

func loadGraphics() error {
	b, e := assets.ReadFile("assets/base.png")
	if e != nil {
		return e
	}
	im, e := png.Decode(bytes.NewReader(b))
	if e != nil {
		return e
	}
	baseImage = image.NewRGBA(image.Rect(0, 0, displayW, displayH))
	draw.Draw(baseImage, baseImage.Bounds(), im, image.Point{}, draw.Src)
	for _, sz := range []int{10, 12, 14, 17, 19, 22, 26, 28, 34, 40} {
		pb, e := assets.ReadFile(fmt.Sprintf("assets/fonts/%d.png", sz))
		if e != nil {
			return e
		}
		ib, e := png.Decode(bytes.NewReader(pb))
		if e != nil {
			return e
		}
		mb, e := assets.ReadFile(fmt.Sprintf("assets/fonts/%d.json", sz))
		if e != nil {
			return e
		}
		var meta fontMeta
		if e = json.Unmarshal(mb, &meta); e != nil {
			return e
		}
		fonts[sz] = bitmapFont{ib, meta}
	}
	return nil
}
func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

var (
	txtWhite  = rgb(246, 244, 255)
	txtPink   = rgb(243, 74, 240)
	txtCyan   = rgb(55, 235, 255)
	txtYellow = rgb(255, 231, 132)
	txtDim    = rgb(178, 162, 215)
)

func blendDot(im *image.RGBA, x, y int, c color.RGBA, a uint8) {
	if x < 0 || y < 0 || x >= displayW || y >= displayH || a == 0 {
		return
	}
	n := im.PixOffset(x, y)
	v := im.Pix[n : n+4]
	aa := int(a)
	v[0] = uint8((int(c.R)*aa + int(v[0])*(255-aa)) / 255)
	v[1] = uint8((int(c.G)*aa + int(v[1])*(255-aa)) / 255)
	v[2] = uint8((int(c.B)*aa + int(v[2])*(255-aa)) / 255)
	v[3] = 255
}
func fontWidth(size int, s string) int {
	f, ok := fonts[size]
	if !ok {
		return 0
	}
	w := 0
	for _, r := range s {
		g, ok := f.meta.Glyphs[string(r)]
		if !ok {
			g = f.meta.Glyphs["?"]
		}
		w += g.W + 1
	}
	if w > 0 {
		w--
	}
	return w
}
func textAt(im *image.RGBA, size, x, y int, s string, c color.RGBA) {
	f, ok := fonts[size]
	if !ok {
		return
	}
	for _, r := range s {
		g, ok := f.meta.Glyphs[string(r)]
		if !ok {
			g = f.meta.Glyphs["?"]
		}
		if g.W < 1 {
			continue
		}
		for yy := 0; yy < f.meta.Height; yy++ {
			py := y + yy
			if py < 0 || py >= displayH {
				continue
			}
			for xx := 0; xx < g.W; xx++ {
				px := x + xx
				if px < 0 || px >= displayW {
					continue
				}
				k, _, _, _ := f.image.At(g.X+xx, yy).RGBA()
				a := uint8(k >> 8)
				if a > 0 {
					blendDot(im, px, py, c, a)
				}
			}
		}
		x += g.W + 1
	}
}
func centerText(im *image.RGBA, size, x0, x1, y int, s string, c color.RGBA) {
	textAt(im, size, x0+(x1-x0-fontWidth(size, s))/2, y, s, c)
}
func rightText(im *image.RGBA, size, right, y int, s string, c color.RGBA) {
	textAt(im, size, right-fontWidth(size, s), y, s, c)
}
func paintRect(im *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > displayW {
		x1 = displayW
	}
	if y1 > displayH {
		y1 = displayH
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			im.SetRGBA(x, y, c)
		}
	}
}
func bar(im *image.RGBA, x, y, w, h int, pct float64, fg, bg color.RGBA) {
	paintRect(im, x, y, x+w, y+h, bg)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	fill := int(float64(w) * pct / 100)
	if fill > 0 {
		paintRect(im, x, y, x+fill, y+h, fg)
	}
}

var months = [12]string{"ЯНВ", "ФЕВ", "МАР", "АПР", "МАЙ", "ИЮН", "ИЮЛ", "АВГ", "СЕН", "ОКТ", "НОЯ", "ДЕК"}

func dateText(now time.Time) string {
	return fmt.Sprintf("%s %02d %s", now.Format("15:04"), now.Day(), months[int(now.Month())-1])
}
func pctText(f float64) string { return fmt.Sprintf("%02.0f%%", f) }
func diskText(u, t uint64) string {
	if t == 0 {
		return "--/--G"
	}
	return fmt.Sprintf("%d/%dG", u, t)
}
func balanceText(m Metrics) string {
	if m.BalanceValid {
		return m.Balance
	}
	return "$--.--"
}
func usageKey(m Metrics) string {
	if !m.UsageValid {
		return "Д:-- Н:-- М:-- " + balanceText(m)
	}
	return fmt.Sprintf("Д:%d%% Н:%d%% М:%d%% %s", m.Daily, m.Weekly, m.Monthly, balanceText(m))
}

// Animation phases are independent: a line is redrawn only when its own
// highlight moves. The little Gengar-side embers are a separate tiny patch.
func neonStep(n int) int {
	p := n % 28
	if p > 14 {
		p = 28 - p
	}
	return 122 + 12*p
}
func lineGlint(im *image.RGBA, y, phase int, c color.RGBA) {
	start := neonStep(phase)
	for k := 0; k < 28; k++ {
		dist := k - 14
		if dist < 0 {
			dist = -dist
		}
		a := uint8(48 + (14-dist)*12)
		col := c
		if dist < 5 {
			col = txtWhite
		}
		blendDot(im, start+k, y, col, a)
		if k%2 == 0 {
			blendDot(im, start+k, y+1, c, a/3)
		}
	}
}
func bottomGlow(im *image.RGBA, phase int) {
	for i := 0; i < 2; i++ {
		x := 340 + (phase*3+i*7)%17
		y := 286 + (phase*2+i*6)%11
		c := txtPink
		if i == 1 {
			c = txtCyan
		}
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				d := dx*dx + dy*dy
				if d <= 9 {
					blendDot(im, x+dx, y+dy, c, uint8((9-d)*16))
				}
			}
		}
	}
}
func render(m Metrics, now time.Time, phase int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, displayW, displayH))
	copy(out.Pix, baseImage.Pix)
	// Top: the only black fills are INSIDE the two pre-rendered neon cards.
	centerText(out, 26, 111, 370, 14, dateText(now), txtWhite)
	if m.UsageValid {
		centerText(out, 14, 110, 173, 66, fmt.Sprintf("Д:%d%%", m.Daily), txtCyan)
		centerText(out, 14, 174, 232, 66, fmt.Sprintf("Н:%d%%", m.Weekly), txtPink)
		centerText(out, 12, 234, 299, 67, fmt.Sprintf("М:%d%%", m.Monthly), txtYellow)
	} else {
		centerText(out, 14, 110, 173, 66, "Д:--", txtDim)
		centerText(out, 14, 174, 232, 66, "Н:--", txtDim)
		centerText(out, 12, 234, 299, 67, "М:--", txtDim)
	}
	balance := balanceText(m)
	bs := 14
	if fontWidth(bs, balance) > 72 {
		bs = 12
	}
	centerText(out, bs, 301, 372, 66, balance, txtYellow)
	textAt(out, 14, 348, 112, "RAM", txtWhite)
	for x := 323; x <= 337; x++ {
		blendDot(out, x, 118, txtPink, 245)
		blendDot(out, x, 133, txtPink, 245)
	}
	for y := 118; y <= 133; y++ {
		blendDot(out, 323, y, txtPink, 245)
		blendDot(out, 337, y, txtPink, 245)
	}
	// Main cards. Narrow text-only partial updates are used when values change.
	centerText(out, 34, 31, 157, 124, pctText(m.CPU), txtPink)
	centerText(out, 34, 179, 305, 124, pctText(m.GPU), txtPink)
	centerText(out, 40, 328, 452, 129, pctText(m.RAM), txtPink)
	if m.CPUTemp > 0 {
		textAt(out, 17, 33, 169, fmt.Sprintf("%.0f°C", m.CPUTemp), txtCyan)
	}
	if m.CPUPower > 0 {
		rightText(out, 12, 155, 174, fmt.Sprintf("%.0fW", m.CPUPower), txtWhite)
	}
	if m.CPUFreq > 0 {
		textAt(out, 10, 33, 194, fmt.Sprintf("%.1fG", m.CPUFreq), txtWhite)
	}
	if m.GPUTemp > 0 {
		textAt(out, 17, 183, 169, fmt.Sprintf("%.0f°C", m.GPUTemp), txtCyan)
	}
	if m.GPUPower > 0 {
		rightText(out, 12, 303, 174, fmt.Sprintf("%.0fW", m.GPUPower), txtWhite)
	}
	if m.VRAMTotal > 0 {
		textAt(out, 10, 183, 194, fmt.Sprintf("V %.1f/%.0fG", m.VRAMUsed, m.VRAMTotal), txtWhite)
	}
	if m.RAMTotal > 0 {
		centerText(out, 14, 326, 452, 173, fmt.Sprintf("USED %.1fG", m.RAMUsed), txtWhite)
		centerText(out, 14, 326, 452, 191, fmt.Sprintf("TOTAL %.0fG", m.RAMTotal), txtWhite)
	}
	bar(out, 33, 211, 124, 4, m.CPU, txtPink, rgb(54, 30, 90))
	bar(out, 181, 211, 123, 4, m.GPU, txtCyan, rgb(30, 44, 79))
	bar(out, 328, 211, 124, 4, m.RAM, txtPink, rgb(54, 30, 90))
	centerText(out, 22, 30, 170, 236, diskText(m.DiskCUsed, m.DiskCTotal), txtYellow)
	centerText(out, 22, 187, 329, 236, diskText(m.DiskZUsed, m.DiskZTotal), txtYellow)
	if m.DiskCTotal > 0 {
		bar(out, 34, 264, 132, 5, 100*float64(m.DiskCUsed)/float64(m.DiskCTotal), txtYellow, rgb(61, 44, 111))
	}
	if m.DiskZTotal > 0 {
		bar(out, 186, 264, 139, 5, 100*float64(m.DiskZUsed)/float64(m.DiskZTotal), txtYellow, rgb(61, 44, 111))
	}
	// Two subtle moving highlights: tiny serial patches, not a full-row redraw.
	lineGlint(out, 8, (phase+2)/3, txtPink)
	lineGlint(out, 55, (phase+1)/3, txtCyan)
	bottomGlow(out, phase/3)
	return out
}

// ------------------------- serial/Turing protocol ---------------------------
var (
	procCreateFile      = kernel.NewProc("CreateFileW")
	procClose           = kernel.NewProc("CloseHandle")
	procGetCommState    = kernel.NewProc("GetCommState")
	procSetCommState    = kernel.NewProc("SetCommState")
	procSetCommTimeouts = kernel.NewProc("SetCommTimeouts")
	procSetupComm       = kernel.NewProc("SetupComm")
	procPurgeComm       = kernel.NewProc("PurgeComm")
	procWrite           = kernel.NewProc("WriteFile")
	procFlush           = kernel.NewProc("FlushFileBuffers")
)

type dcb struct {
	Length     uint32
	BaudRate   uint32
	Flags      uint32
	WReserved  uint16
	XonLim     uint16
	XoffLim    uint16
	ByteSize   uint8
	Parity     uint8
	StopBits   uint8
	XonChar    uint8
	XoffChar   uint8
	ErrorChar  uint8
	EofChar    uint8
	EvtChar    uint8
	WReserved1 uint16
}
type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

const invalidHandle = ^uintptr(0)

type Serial struct {
	mu   sync.Mutex
	h    uintptr
	port string
}

func (s *Serial) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h != 0 && s.h != invalidHandle {
		procClose.Call(s.h)
	}
	s.h = 0
}
func (s *Serial) connect(p string) error {
	name := `\\.\` + p
	path, _ := syscall.UTF16PtrFromString(name)
	handle, _, e := procCreateFile.Call(uintptr(unsafe.Pointer(path)), 0xC0000000, 0, 0, 3, 0x80, 0)
	if handle == 0 || handle == invalidHandle {
		return fmt.Errorf("open %s: %v", p, e)
	}
	fail := func(err error) error { procClose.Call(handle); return err }
	procSetupComm.Call(handle, 65536, 65536)
	var cfg dcb
	cfg.Length = uint32(unsafe.Sizeof(cfg))
	r, _, e := procGetCommState.Call(handle, uintptr(unsafe.Pointer(&cfg)))
	if r == 0 {
		return fail(fmt.Errorf("GetCommState: %v", e))
	}
	cfg.BaudRate = 230400
	cfg.ByteSize = 8
	cfg.Parity = 0
	cfg.StopBits = 0
	cfg.Flags = 1 | 4 | (1 << 4) | (2 << 12) // fBinary, fOutxCtsFlow, RTS_CONTROL_HANDSHAKE (pyserial rtscts=True).
	r, _, e = procSetCommState.Call(handle, uintptr(unsafe.Pointer(&cfg)))
	if r == 0 {
		return fail(fmt.Errorf("SetCommState: %v", e))
	}
	t := commTimeouts{ReadIntervalTimeout: 0xFFFFFFFF, ReadTotalTimeoutConstant: 500, WriteTotalTimeoutConstant: 3000}
	r, _, e = procSetCommTimeouts.Call(handle, uintptr(unsafe.Pointer(&t)))
	if r == 0 {
		return fail(fmt.Errorf("SetCommTimeouts: %v", e))
	}
	procPurgeComm.Call(handle, 0x0004|0x0008) // PURGE_RXCLEAR | PURGE_TXCLEAR
	s.mu.Lock()
	s.h = handle
	s.port = p
	s.mu.Unlock()
	return nil
}
func (s *Serial) writeData(buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	for off := 0; off < len(buf); {
		end := off + 2048
		if end > len(buf) {
			end = len(buf)
		}
		part := buf[off:end]
		var n uint32
		r, _, e := procWrite.Call(s.h, uintptr(unsafe.Pointer(&part[0])), uintptr(len(part)), uintptr(unsafe.Pointer(&n)), 0)
		runtime.KeepAlive(part)
		if r == 0 {
			return fmt.Errorf("WriteFile: %v", e)
		}
		if n == 0 {
			return errors.New("WriteFile sent zero bytes")
		}
		off += int(n)
	}
	return nil
}
func cmdPacket(x, y, ex, ey, c int) []byte {
	return []byte{byte(x >> 2), byte(((x & 3) << 6) | (y >> 4)), byte(((y & 15) << 4) | (ex >> 6)), byte(((ex & 63) << 2) | (ey >> 8)), byte(ey), byte(c)}
}
func (s *Serial) initScreen() error {
	if e := s.writeData(bytes.Repeat([]byte{69}, 6)); e != nil {
		return e
	}
	time.Sleep(100 * time.Millisecond)
	orientation := append(cmdPacket(0, 0, 0, 0, 121), byte(102), byte(480>>8), byte(480&255), byte(320>>8), byte(320&255))
	if e := s.writeData(orientation); e != nil {
		return e
	}
	time.Sleep(150 * time.Millisecond)
	if e := s.writeData(cmdPacket(0, 0, 0, 0, 102)); e != nil {
		return e
	}
	time.Sleep(200 * time.Millisecond)
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
	x0 &= ^3
	y0 &= ^1
	x1 := (x + w + 6) & ^3
	y1 := (y + h + 3) & ^1
	if x1 > displayW {
		x1 = displayW
	}
	if y1 > displayH {
		y1 = displayH
	}
	x, y, w, h = x0, y0, x1-x0, y1-y0
	if e := s.writeData(cmdPacket(x, y, x+w-1, y+h-1, 197)); e != nil {
		return e
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
	if e := s.writeData(buf); e != nil {
		return e
	}
	r, _, e := procFlush.Call(s.h)
	if r == 0 {
		return fmt.Errorf("FlushFileBuffers: %v", e)
	}
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

func serialWorker() {
	port := findCom()
	appLog.Printf("COM selected=%s baud=230400", port)
	display := &Serial{}
	for {
		select {
		case <-stopCh:
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
		appLog.Printf("COM connected=%s", port)
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
		last["clock"] = dateText(time.Now())
		last["cpu"] = pctText(live.snap().CPU)
		last["gpu"] = pctText(live.snap().GPU)
		last["ram"] = pctText(live.snap().RAM)
		t := time.NewTicker(130 * time.Millisecond)
	inner:
		for {
			select {
			case <-stopCh:
				t.Stop()
				display.close()
				return
			case <-t.C:
			}
			now := time.Now()
			// Protect the last stable V35 autostart during initial testing.
			// Replace it only after this single-process serial renderer survives 90s.
			if now.Sub(stableSince) >= 90*time.Second {
				autostartOnce.Do(func() { go func() { setAutostart(); appLog.Printf("V37 autostart switched after 90s stable COM") }() })
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
			if selected == nil {
				continue
			}
			if selected.key != "glow" {
				sentSinceGlow++
			}
			frame := render(m, now, phase)
			if e := display.sendPatch(frame, selected.x, selected.y, selected.w, selected.h); e != nil {
				appLog.Printf("COM partial failed kind=%s: %v; reconnect/full reload", selected.key, e)
				t.Stop()
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
var pGetModule = kernel.NewProc("GetModuleHandleW")

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
	p := filepath.Join(exeDir(), "app_v37.log")
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
		name := ptr16("Turing35V37NativeTray")
		trayCallback = syscall.NewCallback(trayWndProc)
		wc := wndClassEx{CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: trayCallback, HInstance: hmod, LpszClassName: name}
		if r, _, e := pRegisterClass.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			appLog.Printf("tray RegisterClassEx error=%v", e)
			return
		}
		title := ptr16("Turing 3.5 Neon V37")
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
		put16(nd.SzTip[:], "Turing 3.5 Neon Monitor V37")
		if r, _, e := pShellNotify.Call(0, uintptr(unsafe.Pointer(&nd))); r == 0 {
			appLog.Printf("tray Shell_NotifyIcon add error=%v", e)
		} else {
			appLog.Printf("tray V37 icon added custom=%s", iconFile)
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
		appLog.Printf("tray V37 icon removed")
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
	p := filepath.Join(exeDir(), "app_v37.log")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		appLog = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lmicroseconds)
	} else {
		defer f.Close()
		appLog = log.New(f, "", log.Ldate|log.Ltime|log.Lmicroseconds)
	}
	appLog.Printf("V37 native single-process startup")
	if e := loadGraphics(); e != nil {
		appLog.Printf("graphics load error=%v", e)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--preview" {
		example := Metrics{CPU: 13, GPU: 7, RAM: 49, CPUFreq: 3.5, CPUTemp: 52, CPUPower: 43, GPUTemp: 53, GPUPower: 44, VRAMUsed: 1.7, VRAMTotal: 16, RAMUsed: 13.6, RAMTotal: 32, DiskCUsed: 751, DiskCTotal: 931, DiskZUsed: 738, DiskZTotal: 954, Daily: 0, Weekly: 2, Monthly: 100, Balance: "$53.39", BalanceValid: true, UsageValid: true}
		dst := filepath.Join(exeDir(), "Turing35_V37_preview.png")
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
	go serialWorker()
	<-stopCh
	appLog.Printf("V37 requested shutdown")
	time.Sleep(300 * time.Millisecond)
}
