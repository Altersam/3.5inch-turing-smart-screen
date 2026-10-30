//go:build windows

// Neon theme and OpenCode metrics for the Turing screen.
package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
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

const (
	version      = "Turing35Neon"
	buildVersion = "V47"
	dispW        = 480
	dispH        = 320
	baud         = 115200
)

var (
	appLog   *log.Logger
	stopCh   = make(chan struct{})
	stopOnce sync.Once
	noGlow   = flag.Bool("no-glow", true, "update stats without animated glow patches")
)

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
		for _, z := range x {
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
		if _, e := os.Stat(p); e != nil {
			p = filepath.Join(a, "opencode", "auth.json")
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
		appLog.Printf("OpenCode usage key found")
	}
	return k
}

var remoteClient = &http.Client{Timeout: 12 * time.Second}

const apiBase = "https://opencode.ai"

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
				if z < 0 {
					return 0
				}
				if z > 100 {
					return 100
				}
				return int(z + 0.5)
			case json.Number:
				if f, e := z.Float64(); e == nil {
					if f < 0 {
						return 0
					}
					if f > 100 {
						return 100
					}
					return int(f + 0.5)
				}
			}
		}
	}
	return -1
}
func readUsage(key string) (int, int, int, error) {
	if key == "" {
		return 0, 0, 0, errors.New("opencode key missing")
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
		return 0, 0, 0, errors.New("usage missing")
	}
	if week < 0 {
		week = 0
	}
	if month < 0 {
		month = 0
	}
	return day, week, month, nil
}
func openCodeLoop() {
	update := func() {
		key := findAPIKey()
		if d, w, m, e := readUsage(key); e == nil {
			writeStats(func(s *stats) { s.Daily, s.Weekly, s.Monthly, s.UsageOK = d, w, m, true })
		} else {
			appLog.Printf("OpenCode usage: %v", e)
		}
	}
	update()
	t := time.NewTicker(5 * time.Minute)
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

type stats struct {
	CPU, GPU, RAM          int
	CPUTemp, GPUTemp       int
	CPUFreq, CPUPower      float64
	GPUPower               float64
	VRAMUsed, VRAMTotal    float64
	RAMUsed, RAMTotal      float64
	DiskCUsed, DiskCTotal  int
	DiskZUsed, DiskZTotal  int
	Daily, Weekly, Monthly int
	Claude, Codex          usageWindows
	UsageOK                bool
	When                   time.Time
}

var (
	statMu sync.RWMutex
	cur    stats
)

func readStats() stats {
	statMu.RLock()
	defer statMu.RUnlock()
	return cur
}
func writeStats(f func(*stats)) {
	statMu.Lock()
	f(&cur)
	statMu.Unlock()
}

var kernel = syscall.NewLazyDLL("kernel32.dll")
var (
	procCreateFile      = kernel.NewProc("CreateFileW")
	procClose           = kernel.NewProc("CloseHandle")
	procGetCommState    = kernel.NewProc("GetCommState")
	procSetCommState    = kernel.NewProc("SetCommState")
	procSetCommTimeouts = kernel.NewProc("SetCommTimeouts")
	procSetupComm       = kernel.NewProc("SetupComm")
	procPurgeComm       = kernel.NewProc("PurgeComm")
	procWrite           = kernel.NewProc("WriteFile")
	procRead            = kernel.NewProc("ReadFile")
	procGetSystemTimes  = kernel.NewProc("GetSystemTimes")
	procMemStatus       = kernel.NewProc("GlobalMemoryStatusEx")
	procDiskFree        = kernel.NewProc("GetDiskFreeSpaceExW")
)

type dcb struct {
	Length, BaudRate, Flags                        uint32
	WReserved, XonLim, XoffLim                     uint16
	ByteSize, Parity, StopBits                     uint8
	XonChar, XoffChar, ErrorChar, EofChar, EvtChar uint8
	WReserved1                                     uint16
}
type commTimeouts struct {
	ReadIntervalTimeout, ReadTotalTimeoutMultiplier, ReadTotalTimeoutConstant uint32
	WriteTotalTimeoutMultiplier, WriteTotalTimeoutConstant                    uint32
}
type filetime struct{ Lo, Hi uint32 }

func ftU64(f filetime) uint64 { return uint64(f.Hi)<<32 | uint64(f.Lo) }

const invalidHandle = ^uintptr(0)

type port struct {
	h             uintptr
	bytesSent     uint64
	regionsSent   uint64
	writeDuration time.Duration
	nextRegion    time.Time
}

func openPort(name string) (*port, error) {
	path, _ := syscall.UTF16PtrFromString(`\\.\` + name)
	h, _, e := procCreateFile.Call(uintptr(unsafe.Pointer(path)), 0xC0000000, 0, 0, 3, 0x80, 0)
	if h == 0 || h == invalidHandle {
		return nil, fmt.Errorf("open %s: %v", name, e)
	}
	fail := func(err error) (*port, error) {
		procClose.Call(h)
		return nil, err
	}
	procSetupComm.Call(h, 8192, 8192)
	var cfg dcb
	cfg.Length = uint32(unsafe.Sizeof(cfg))
	r, _, e := procGetCommState.Call(h, uintptr(unsafe.Pointer(&cfg)))
	if r == 0 {
		return fail(fmt.Errorf("GetCommState: %v", e))
	}
	cfg.BaudRate = baud
	cfg.ByteSize = 8
	cfg.Parity = 0
	cfg.StopBits = 0
	cfg.Flags = 1 | (1 << 4) | (1 << 12)
	r, _, e = procSetCommState.Call(h, uintptr(unsafe.Pointer(&cfg)))
	if r == 0 {
		return fail(fmt.Errorf("SetCommState: %v", e))
	}
	// Writes are bounded to serialChunkSize; a dead endpoint should not block
	// every reconnection attempt for a whole minute.
	t := commTimeouts{ReadIntervalTimeout: 0xFFFFFFFF, ReadTotalTimeoutConstant: 200, WriteTotalTimeoutConstant: 3000}
	r, _, e = procSetCommTimeouts.Call(h, uintptr(unsafe.Pointer(&t)))
	if r == 0 {
		return fail(fmt.Errorf("SetCommTimeouts: %v", e))
	}
	r, _, e = procPurgeComm.Call(h, 0x000F) // abort/clear stale RX and TX queues
	if r == 0 {
		return fail(fmt.Errorf("PurgeComm: %v", e))
	}
	time.Sleep(250 * time.Millisecond)
	return &port{h: h}, nil
}

func (p *port) close() {
	if p == nil || p.h == 0 || p.h == invalidHandle {
		return
	}
	procPurgeComm.Call(p.h, 0x000F)
	procClose.Call(p.h)
	p.h = 0
	time.Sleep(700 * time.Millisecond)
}

func (p *port) write(buf []byte) error {
	w := serialWriter{now: time.Now, wait: waitSerial, write: func(part []byte) (int, error) {
		var n uint32
		r, _, e := procWrite.Call(p.h, uintptr(unsafe.Pointer(&part[0])), uintptr(len(part)), uintptr(unsafe.Pointer(&n)), 0)
		runtime.KeepAlive(part)
		if r == 0 {
			return int(n), e
		}
		return int(n), nil
	}}
	n, duration, err := w.send(buf)
	p.bytesSent += uint64(n)
	p.writeDuration += duration
	return err
}

func cmd(x, y, ex, ey, c int) []byte {
	return []byte{byte(x >> 2), byte(((x & 3) << 6) | (y >> 4)), byte(((y & 15) << 4) | (ex >> 6)), byte(((ex & 63) << 2) | (ey >> 8)), byte(ey), byte(c)}
}

func orientationCommand(orientation, w, h int) []byte {
	packet := make([]byte, 16)
	copy(packet, cmd(0, 0, 0, 0, 121))
	packet[6] = byte(100 + orientation)
	packet[7], packet[8] = byte(w>>8), byte(w)
	packet[9], packet[10] = byte(h>>8), byte(h)
	return packet
}

func (p *port) initScreen() error {
	if e := p.write([]byte{69, 69, 69, 69, 69, 69}); e != nil {
		return e
	}
	var reply [6]byte
	n := 0
	for n < len(reply) {
		var got uint32
		r, _, e := procRead.Call(p.h, uintptr(unsafe.Pointer(&reply[n])), uintptr(len(reply)-n), uintptr(unsafe.Pointer(&got)), 0)
		if r == 0 {
			return fmt.Errorf("HELLO read: %w", e)
		}
		if got == 0 {
			break
		}
		n += int(got)
	}
	if n == 0 {
		// Original Rev. A screens do not implement HELLO replies.
		appLog.Printf("COM HELLO: no reply; using standard Rev. A protocol")
	} else if n == len(reply) && (reply == [6]byte{1, 1, 1, 1, 1, 1}) {
		appLog.Printf("COM HELLO: USBMonitor 3.5 acknowledged")
	} else {
		return fmt.Errorf("unexpected HELLO reply: %x", reply[:n])
	}
	time.Sleep(120 * time.Millisecond)
	orient := orientationCommand(2, dispW, dispH)
	if e := p.write(orient); e != nil {
		return e
	}
	time.Sleep(180 * time.Millisecond)
	if e := p.write(cmd(0, 0, 0, 0, 102)); e != nil {
		return e
	}
	time.Sleep(400 * time.Millisecond)
	return nil
}

func (p *port) region(img *image.RGBA, x, y, w, h int) error {
	if err := waitSerial(time.Until(p.nextRegion)); err != nil {
		return err
	}
	if err := sendRegion(p.write, img, x, y, w, h); err != nil {
		return err
	}
	p.nextRegion = time.Now().Add(serialRegionGap)
	p.regionsSent++
	return nil
}

// Each region is one header followed by exactly w*h*2 RGB565 bytes. The port
// writer splits the payload into paced USB writes without inserting commands.
// serialLoop is the only caller, so transactions cannot be interleaved.
func sendRegion(write func([]byte) error, img *image.RGBA, x, y, w, h int) error {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	x &^= 1
	y &^= 1
	if x+w > dispW {
		w = dispW - x
	}
	if y+h > dispH {
		h = dispH - y
	}
	w &^= 1
	h &^= 1
	if w < 2 || h < 2 {
		return nil
	}
	if e := write(cmd(x, y, x+w-1, y+h-1, 197)); e != nil {
		return e
	}
	buf := make([]byte, w*h*2)
	j := 0
	for yy := y; yy < y+h; yy++ {
		i := img.PixOffset(x, yy)
		for xx := 0; xx < w; xx++ {
			r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
			v := (uint16(r>>3) << 11) | (uint16(g>>2) << 5) | uint16(b>>3)
			buf[j], buf[j+1] = byte(v), byte(v>>8)
			j += 2
			i += 4
		}
	}
	if e := write(buf); e != nil {
		return e
	}
	return nil
}

func (p *port) frame(img *image.RGBA) error {
	return p.region(img, 0, 0, dispW, dispH)
}

func findCom() string {
	if p := strings.TrimSpace(os.Getenv("TURING_COM")); len(p) >= 4 {
		return strings.ToUpper(p)
	}
	ps := `$z=Get-CimInstance Win32_PnPEntity -ErrorAction SilentlyContinue|Where-Object {$_.Name -match '\(COM\d+\)' -and $_.PNPDeviceID -match 'VID_1A86&PID_5722'}|Select-Object -First 1 -ExpandProperty Name;if($z){$z}`
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	o, _ := c.Output()
	up := strings.ToUpper(string(o))
	if i := strings.Index(up, "COM"); i >= 0 {
		s := up[i:]
		n := 3
		for n < len(s) && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		if n > 3 {
			return s[:n]
		}
	}
	return "COM5"
}

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

// Move only the lower edge of the top usage panel down by six pixels.
// The neighbouring CPU/GPU/RAM cards begin at y=108.
func extendUsagePanel(im *image.RGBA) {
	const left, right = 102, 379
	bottom := image.NewRGBA(image.Rect(left, 90, right, 98))
	draw.Draw(bottom, bottom.Bounds(), im, image.Pt(left, 90), draw.Src)
	for y := 90; y < 96; y++ {
		draw.Draw(im, image.Rect(left, y, right, y+1), im, image.Pt(left, 88), draw.Src)
	}
	draw.Draw(im, image.Rect(left, 96, right, 104), bottom, image.Pt(left, 90), draw.Src)
}

func loadGraphics() error {
	b, e := assets.ReadFile("assets/base.png")
	if e != nil {
		return e
	}
	im, e := png.Decode(bytes.NewReader(b))
	if e != nil {
		return e
	}
	baseImage = image.NewRGBA(image.Rect(0, 0, dispW, dispH))
	draw.Draw(baseImage, baseImage.Bounds(), im, image.Point{}, draw.Src)
	extendUsagePanel(baseImage)
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
	txtOrange = rgb(255, 155, 58)
)

func blendDot(im *image.RGBA, x, y int, c color.RGBA, a uint8) {
	if x < 0 || y < 0 || x >= dispW || y >= dispH || a == 0 {
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
			if py < 0 || py >= dispH {
				continue
			}
			for xx := 0; xx < g.W; xx++ {
				px := x + xx
				if px < 0 || px >= dispW {
					continue
				}
				k, _, _, _ := f.image.At(g.X+xx, yy).RGBA()
				if a := uint8(k >> 8); a > 0 {
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
	if x1 > dispW {
		x1 = dispW
	}
	if y1 > dispH {
		y1 = dispH
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
func pctText(n int) string { return fmt.Sprintf("%02d%%", n) }
func diskText(u, t int) string {
	if t == 0 {
		return "--/--G"
	}
	return fmt.Sprintf("%d/%dG", u, t)
}

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
		for _, dy := range []int{-2, -1, 1, 2} {
			glow := a / 3
			if dy == -2 || dy == 2 {
				glow /= 2
			}
			blendDot(im, start+k, y+dy, c, glow)
		}
		col := c
		if dist < 5 {
			col = txtWhite
		}
		blendDot(im, start+k, y, col, a)
	}
}
func bottomGlow(im *image.RGBA, phase int) {
	for i := 0; i < 3; i++ {
		x := 340 + (phase*3+i*7)%17
		y := 286 + (phase*2+i*6)%11
		c := txtPink
		if i == 1 {
			c = txtCyan
		} else if i == 2 {
			c = rgb(188, 112, 255)
		}
		for dy := -5; dy <= 5; dy++ {
			for dx := -5; dx <= 5; dx++ {
				d := dx*dx + dy*dy
				if d < 25 {
					blendDot(im, x+dx, y+dy, c, uint8((25-d)*5))
				}
			}
		}
		blendDot(im, x, y, txtWhite, 130)
	}
}
func glowPatch(phase int) (int, int, int, int, int) {
	old := phase
	phase++
	if phase >= 84 {
		phase = 0
	}
	switch phase % 3 {
	case 1, 2:
		oldStep, newStep, y := (old+2)/3, (phase+2)/3, 6
		if phase%3 == 2 {
			oldStep, newStep, y = (old+1)/3, (phase+1)/3, 52
		}
		x0, x1 := neonStep(oldStep), neonStep(newStep)
		if x1 < x0 {
			x0, x1 = x1, x0
		}
		x0 &^= 1
		w := x1 - x0 + 28
		if x0+w > dispW {
			w = dispW - x0
		}
		w &^= 1
		return phase, x0, y, w, 6
	default:
		return phase, 334, 280, 28, 22
	}
}

func quotaLabel(prefix string, value int, valid bool) string {
	if !valid {
		return prefix + ":--"
	}
	return fmt.Sprintf("%s:%d%%", prefix, value)
}

func render(m stats, phase int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, dispW, dispH))
	copy(out.Pix, baseImage.Pix)
	centerText(out, 26, 111, 370, 14, dateText(m.When), txtWhite)
	if m.UsageOK {
		centerText(out, 10, 108, 167, 58, fmt.Sprintf("Д:%d%%", m.Daily), txtCyan)
		centerText(out, 10, 108, 167, 73, fmt.Sprintf("Н:%d%%", m.Weekly), txtPink)
		centerText(out, 12, 167, 229, 67, fmt.Sprintf("М:%d%%", m.Monthly), txtYellow)
	}
	centerText(out, 12, 229, 299, 58, quotaLabel("К5", m.Claude.Five, m.Claude.FiveOK), txtOrange)
	centerText(out, 12, 229, 299, 73, quotaLabel("КН", m.Claude.Week, m.Claude.WeekOK), txtOrange)
	centerText(out, 12, 299, 373, 58, quotaLabel("G5", m.Codex.Five, m.Codex.FiveOK), txtWhite)
	centerText(out, 12, 299, 373, 73, quotaLabel("GН", m.Codex.Week, m.Codex.WeekOK), txtWhite)
	bar(out, 169, 94, 58, 2, float64(m.Monthly), txtYellow, rgb(45, 32, 55))
	bar(out, 232, 94, 64, 2, float64(m.Claude.Five), txtOrange, rgb(50, 29, 45))
	bar(out, 302, 94, 68, 2, float64(m.Codex.Five), txtWhite, rgb(39, 32, 52))
	textAt(out, 14, 348, 112, "RAM", txtWhite)
	for x := 323; x <= 337; x++ {
		blendDot(out, x, 118, txtPink, 245)
		blendDot(out, x, 133, txtPink, 245)
	}
	for y := 118; y <= 133; y++ {
		blendDot(out, 323, y, txtPink, 245)
		blendDot(out, 337, y, txtPink, 245)
	}
	centerText(out, 34, 31, 157, 124, pctText(m.CPU), txtPink)
	centerText(out, 34, 179, 305, 124, pctText(m.GPU), txtPink)
	centerText(out, 40, 328, 452, 129, pctText(m.RAM), txtPink)
	if m.CPUTemp > 0 {
		textAt(out, 17, 33, 169, fmt.Sprintf("%.0f°C", float64(m.CPUTemp)), txtCyan)
	}
	if m.CPUPower > 0 {
		rightText(out, 12, 155, 174, fmt.Sprintf("%.0fW", m.CPUPower), txtWhite)
	}
	if m.CPUFreq > 0 {
		textAt(out, 10, 33, 194, fmt.Sprintf("%.1fG", m.CPUFreq), txtWhite)
	}
	if m.GPUTemp > 0 {
		textAt(out, 17, 183, 169, fmt.Sprintf("%.0f°C", float64(m.GPUTemp)), txtCyan)
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
	bar(out, 33, 211, 124, 4, float64(m.CPU), txtPink, rgb(54, 30, 90))
	bar(out, 181, 211, 123, 4, float64(m.GPU), txtCyan, rgb(30, 44, 79))
	bar(out, 328, 211, 124, 4, float64(m.RAM), txtPink, rgb(54, 30, 90))
	centerText(out, 22, 30, 170, 236, diskText(m.DiskCUsed, m.DiskCTotal), txtYellow)
	centerText(out, 22, 187, 329, 236, diskText(m.DiskZUsed, m.DiskZTotal), txtYellow)
	if m.DiskCTotal > 0 {
		bar(out, 34, 264, 132, 5, 100*float64(m.DiskCUsed)/float64(m.DiskCTotal), txtYellow, rgb(61, 44, 111))
	}
	if m.DiskZTotal > 0 {
		bar(out, 186, 264, 139, 5, 100*float64(m.DiskZUsed)/float64(m.DiskZTotal), txtYellow, rgb(61, 44, 111))
	}
	lineGlint(out, 8, (phase+2)/3, txtPink)
	lineGlint(out, 55, (phase+1)/3, txtCyan)
	bottomGlow(out, phase/3)
	return out
}

type box struct {
	key        string
	x, y, w, h int
}

var boxes = []box{
	{"cpu", 28, 120, 132, 50},
	{"gpu", 176, 120, 132, 50},
	{"ram", 324, 124, 132, 52},
	{"cpu_d", 28, 166, 136, 52},
	{"gpu_d", 176, 166, 136, 52},
	{"ram_d", 324, 168, 132, 52},
	{"diskc", 28, 230, 144, 44},
	{"diskz", 184, 230, 148, 44},
	{"clock", 108, 10, 264, 42},
	{"op", 108, 62, 268, 36},
}

func boxByKey(k string) box {
	for _, b := range boxes {
		if b.key == k {
			return b
		}
	}
	return boxes[0]
}

func keyValue(s stats, k string) string {
	switch k {
	case "cpu":
		return strconv.Itoa(s.CPU)
	case "gpu":
		return strconv.Itoa(s.GPU)
	case "ram":
		return strconv.Itoa(s.RAM)
	case "cpu_d":
		return fmt.Sprintf("%d/%.1f/%.0f", s.CPUTemp, s.CPUFreq, s.CPUPower)
	case "gpu_d":
		return fmt.Sprintf("%d/%.1f/%.0f/%.1f", s.GPUTemp, s.VRAMUsed, s.GPUPower, s.VRAMTotal)
	case "ram_d":
		return fmt.Sprintf("%.1f/%.0f", s.RAMUsed, s.RAMTotal)
	case "diskc":
		return fmt.Sprintf("%d/%d", s.DiskCUsed, s.DiskCTotal)
	case "diskz":
		return fmt.Sprintf("%d/%d", s.DiskZUsed, s.DiskZTotal)
	case "clock":
		return dateText(s.When)
	case "op":
		return fmt.Sprintf("%d/%d/%d/%t/%v/%v", s.Daily, s.Weekly, s.Monthly, s.UsageOK, s.Claude, s.Codex)
	}
	return ""
}

var fastKeys = []string{"cpu", "gpu", "ram"}
var slowKeys = []string{"op", "cpu_d", "gpu_d", "ram_d", "diskc", "diskz", "clock"}

func nextChanged(keys []string, start int, shown, now stats) string {
	for i := 0; i < len(keys); i++ {
		k := keys[(start+i)%len(keys)]
		if keyValue(shown, k) != keyValue(now, k) {
			return k
		}
	}
	return ""
}

func nextKeyIndex(keys []string, key string) int {
	for i, k := range keys {
		if k == key {
			return (i + 1) % len(keys)
		}
	}
	return 0
}

func pickKey(shown, now stats, fastN, fastStart, slowStart int) string {
	if keyValue(shown, "clock") != keyValue(now, "clock") {
		return "clock"
	}
	if fastN >= 3 {
		if k := nextChanged(slowKeys, slowStart, shown, now); k != "" {
			return k
		}
	}
	if k := nextChanged(fastKeys, fastStart, shown, now); k != "" {
		return k
	}
	return nextChanged(slowKeys, slowStart, shown, now)
}

// changedRects returns even-aligned regions covering all changed pixels.  A
// short horizontal gap is merged so individual glyph strokes stay together.
func changedRects(before, after *image.RGBA) []image.Rectangle {
	const mergeGap = 8
	var rects []image.Rectangle
	start, end, minY, maxY := -1, -1, dispH, -1
	flush := func() {
		if start >= 0 {
			rects = append(rects, image.Rect(start, minY&^1, end, (maxY+2)&^1))
		}
		start, end, minY, maxY = -1, -1, dispH, -1
	}
	for x := 0; x < dispW; x += 2 {
		colMin, colMax := dispH, -1
		for y := 0; y < dispH; y++ {
			for dx := 0; dx < 2; dx++ {
				if before.RGBAAt(x+dx, y) != after.RGBAAt(x+dx, y) {
					if y < colMin {
						colMin = y
					}
					colMax = y
				}
			}
		}
		if colMax < 0 {
			continue
		}
		if start >= 0 && x-end > mergeGap {
			flush()
		}
		if start < 0 {
			start = x
		}
		end = x + 2
		if colMin < minY {
			minY = colMin
		}
		if colMax > maxY {
			maxY = colMax
		}
	}
	flush()
	return rects
}

func (p *port) updateRects(img *image.RGBA, rects []image.Rectangle) error {
	for _, r := range rects {
		if err := p.region(img, r.Min.X, r.Min.Y, r.Dx(), r.Dy()); err != nil {
			return fmt.Errorf("region %v: %w", r, err)
		}
	}
	return nil
}

func applyKey(shown *stats, now stats, k string) {
	switch k {
	case "cpu":
		shown.CPU = now.CPU
	case "gpu":
		shown.GPU = now.GPU
	case "ram":
		shown.RAM = now.RAM
	case "cpu_d":
		shown.CPUTemp, shown.CPUFreq, shown.CPUPower = now.CPUTemp, now.CPUFreq, now.CPUPower
	case "gpu_d":
		shown.GPUTemp, shown.VRAMUsed, shown.GPUPower, shown.VRAMTotal = now.GPUTemp, now.VRAMUsed, now.GPUPower, now.VRAMTotal
	case "ram_d":
		shown.RAMUsed, shown.RAMTotal = now.RAMUsed, now.RAMTotal
	case "diskc":
		shown.DiskCUsed, shown.DiskCTotal = now.DiskCUsed, now.DiskCTotal
	case "diskz":
		shown.DiskZUsed, shown.DiskZTotal = now.DiskZUsed, now.DiskZTotal
	case "clock":
		shown.When = now.When
	case "op":
		shown.Daily, shown.Weekly, shown.Monthly = now.Daily, now.Weekly, now.Monthly
		shown.UsageOK = now.UsageOK
		shown.Claude, shown.Codex = now.Claude, now.Codex
	}
}

var cpuIdle, cpuTotal uint64

func sampleCPU() int {
	var idle, kern, user filetime
	r, _, _ := procGetSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kern)), uintptr(unsafe.Pointer(&user)))
	if r == 0 {
		return 0
	}
	i, tot := ftU64(idle), ftU64(kern)+ftU64(user)
	if cpuTotal == 0 || tot <= cpuTotal {
		cpuIdle, cpuTotal = i, tot
		return 0
	}
	di, dt := i-cpuIdle, tot-cpuTotal
	cpuIdle, cpuTotal = i, tot
	if dt == 0 {
		return 0
	}
	pct := int(100 - (di * 100 / dt))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func sampleRAM() (int, float64, float64) {
	var m struct {
		Length, Load uint32
		Total, Avail uint64
		_            [40]byte
	}
	m.Length = 64
	r, _, _ := procMemStatus.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 || m.Total == 0 {
		return 0, 0, 0
	}
	return int(m.Load), float64(m.Total-m.Avail) / (1 << 30), float64(m.Total) / (1 << 30)
}

func sampleDisk(root string) (int, int) {
	p, _ := syscall.UTF16PtrFromString(root)
	var free, total, totalFree uint64
	r, _, _ := procDiskFree.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
	if r == 0 || total == 0 {
		return 0, 0
	}
	gb := uint64(1 << 30)
	return int((total - totalFree) / gb), int(total / gb)
}

func nvidia() string {
	for _, p := range []string{`C:\Windows\System32\nvidia-smi.exe`, `C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`} {
		if _, e := os.Stat(p); e == nil {
			return p
		}
	}
	return ""
}

func sampleGPU(tool string) (int, int, float64, float64, float64) {
	if tool == "" {
		return -1, -1, 0, 0, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, tool, "--query-gpu=utilization.gpu,temperature.gpu,memory.used,memory.total,power.draw", "--format=csv,noheader,nounits")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := c.Output()
	if e != nil {
		return -1, -1, 0, 0, 0
	}
	a := strings.Split(strings.TrimSpace(strings.Split(string(out), "\n")[0]), ",")
	if len(a) < 5 {
		return -1, -1, 0, 0, 0
	}
	u, _ := strconv.Atoi(strings.TrimSpace(a[0]))
	t, _ := strconv.Atoi(strings.TrimSpace(a[1]))
	used, _ := strconv.ParseFloat(strings.TrimSpace(a[2]), 64)
	total, _ := strconv.ParseFloat(strings.TrimSpace(a[3]), 64)
	pow, _ := strconv.ParseFloat(strings.TrimSpace(a[4]), 64)
	if u < 0 {
		u = 0
	}
	if u > 100 {
		u = 100
	}
	return u, t, used / 1024, total / 1024, pow
}

func sampleCPUExtra() (int, float64, float64) {
	script := `$f=(Get-CimInstance Win32_Processor -ErrorAction SilentlyContinue|Measure-Object CurrentClockSpeed -Average).Average;$t=$null;$p=$null;try{$s=Get-CimInstance -Namespace root/LibreHardwareMonitor -ClassName Sensor -ErrorAction Stop;if($t -eq $null){$a=$s|Where-Object {$_.SensorType -eq 'Temperature' -and $_.Name -match 'CPU Package|Tctl|Tdie'}|Select-Object -First 1;if($a){$t=$a.Value}};if($p -eq $null){$a=$s|Where-Object {$_.SensorType -eq 'Power' -and $_.Name -match 'CPU Package|CPU'}|Select-Object -First 1;if($a){$p=$a.Value}}}catch{};if($t -eq $null){try{$z=Get-CimInstance MSAcpi_ThermalZoneTemperature -Namespace root/wmi -ErrorAction Stop|Select-Object -First 1;if($z){$t=($z.CurrentTemperature/10)-273.15}}catch{}};"$f,$t,$p"`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, e := c.Output()
	if e != nil {
		return -1, 0, 0
	}
	parts := strings.Split(strings.TrimSpace(string(b)), ",")
	var freq, temp, pow float64
	if len(parts) > 0 {
		freq, _ = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	}
	if len(parts) > 1 {
		temp, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	}
	if len(parts) > 2 {
		pow, _ = strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	}
	ti := -1
	if temp > 5 && temp < 120 {
		ti = int(temp + 0.5)
	}
	if freq > 100 {
		freq = freq / 1000
	}
	return ti, freq, pow
}

func sensorLoop() {
	sampleCPU()
	tool := nvidia()
	tFast := time.NewTicker(500 * time.Millisecond)
	tGPU := time.NewTicker(2 * time.Second)
	tExtra := time.NewTicker(12 * time.Second)
	tDisk := time.NewTicker(25 * time.Second)
	defer tFast.Stop()
	defer tGPU.Stop()
	defer tExtra.Stop()
	defer tDisk.Stop()
	go func() {
		if u, tp, used, total, pow := sampleGPU(tool); u >= 0 {
			writeStats(func(s *stats) {
				s.GPU = u
				if tp > 0 {
					s.GPUTemp = tp
				}
				s.VRAMUsed, s.VRAMTotal, s.GPUPower = used, total, pow
			})
		}
		if ti, freq, pow := sampleCPUExtra(); ti > 0 || freq > 0 {
			writeStats(func(s *stats) {
				if ti > 0 {
					s.CPUTemp = ti
				}
				if freq > 0 {
					s.CPUFreq = freq
				}
				if pow > 0 {
					s.CPUPower = pow
				}
			})
		}
		cu, ct := sampleDisk(`C:\`)
		zu, zt := sampleDisk(`Z:\`)
		writeStats(func(s *stats) {
			s.DiskCUsed, s.DiskCTotal, s.DiskZUsed, s.DiskZTotal = cu, ct, zu, zt
		})
	}()
	for {
		select {
		case <-stopCh:
			return
		case <-tFast.C:
			cpu := sampleCPU()
			ram, used, total := sampleRAM()
			writeStats(func(s *stats) {
				s.CPU, s.RAM, s.RAMUsed, s.RAMTotal, s.When = cpu, ram, used, total, time.Now()
			})
		case <-tGPU.C:
			if u, tp, used, total, pow := sampleGPU(tool); u >= 0 {
				writeStats(func(s *stats) {
					s.GPU = u
					if tp > 0 {
						s.GPUTemp = tp
					}
					s.VRAMUsed, s.VRAMTotal, s.GPUPower = used, total, pow
				})
			}
		case <-tExtra.C:
			if ti, freq, pow := sampleCPUExtra(); ti > 0 || freq > 0 {
				writeStats(func(s *stats) {
					if ti > 0 {
						s.CPUTemp = ti
					}
					if freq > 0 {
						s.CPUFreq = freq
					}
					if pow > 0 {
						s.CPUPower = pow
					}
				})
			}
		case <-tDisk.C:
			cu, ct := sampleDisk(`C:\`)
			zu, zt := sampleDisk(`Z:\`)
			writeStats(func(s *stats) {
				s.DiskCUsed, s.DiskCTotal, s.DiskZUsed, s.DiskZTotal = cu, ct, zu, zt
			})
		}
	}
}

func serialLoop() {
	name := findCom()
	appLog.Printf("COM selected=%s baud=%d", name, baud)
	fails := 0
	needsRecovery := false
	var recoveryAt time.Time
	for {
		select {
		case <-stopCh:
			return
		default:
		}
		if fails > 0 {
			name = findCom()
		}
		if needsRecovery && fails >= 2 && time.Since(recoveryAt) >= 5*time.Minute {
			recoveryAt = time.Now()
			if err := restartDisplayDevice(name); err != nil {
				appLog.Printf("COM USB recovery: %v; if writes still fail, reconnect the screen USB cable", err)
			} else {
				appLog.Printf("COM USB device restarted; waiting for enumeration")
				if waitSerial(5*time.Second) != nil {
					return
				}
				name = findCom()
			}
		}
		p, e := openPort(name)
		if e != nil {
			fails++
			wait := 20 * time.Second
			if fails >= 2 {
				wait = 90 * time.Second
			}
			appLog.Printf("COM open: %v; retry in %s", e, wait)
			select {
			case <-stopCh:
				return
			case <-time.After(wait):
			}
			continue
		}
		appLog.Printf("COM opened=%s; checking screen", name)
		if e := p.initScreen(); e != nil {
			appLog.Printf("COM init: %v", e)
			needsRecovery = true
			fails++
			p.close()
			select {
			case <-stopCh:
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		s := readStats()
		s.When = time.Now()
		phase := 0
		img := render(s, phase)
		if e := p.frame(img); e != nil {
			appLog.Printf("COM frame: %v", e)
			needsRecovery = true
			fails++
			p.close()
			select {
			case <-stopCh:
				return
			case <-time.After(60 * time.Second):
			}
			continue
		}
		appLog.Printf("COM frame done; bytes=%d write_time=%s; chunk=%d rate_limit=%dB/s region_gap=%s no_glow=%t; stat updates enabled", p.bytesSent, p.writeDuration, serialChunkSize, serialBytesPerSecond, serialRegionGap, *noGlow)
		fails = 0
		needsRecovery = false
		shown := s
		fastN := 0
		fastStart, slowStart := 0, 0
		var lastGlow time.Time
		reportAt := time.Now()
		reportBytes, reportRegions, reportWrite := p.bytesSent, p.regionsSent, p.writeDuration
		tick := time.NewTicker(50 * time.Millisecond)
	inner:
		for {
			select {
			case <-stopCh:
				tick.Stop()
				p.close()
				return
			case <-tick.C:
			}
			now := readStats()
			now.When = time.Now()
			if time.Since(reportAt) >= time.Minute {
				appLog.Printf("COM activity interval=%s bytes=%d regions=%d write_time=%s", time.Since(reportAt).Round(time.Millisecond), p.bytesSent-reportBytes, p.regionsSent-reportRegions, p.writeDuration-reportWrite)
				reportAt = time.Now()
				reportBytes, reportRegions, reportWrite = p.bytesSent, p.regionsSent, p.writeDuration
			}
			k := pickKey(shown, now, fastN, fastStart, slowStart)
			nextPhase := phase
			nextShown := shown
			var rects []image.Rectangle
			if k == "" {
				if *noGlow {
					continue
				}
				if time.Since(lastGlow) < 150*time.Millisecond {
					continue
				}
				var x, y, w, h int
				nextPhase, x, y, w, h = glowPatch(phase)
				rects = []image.Rectangle{image.Rect(x, y, x+w, y+h)}
				k = "glow"
			} else {
				applyKey(&nextShown, now, k)
				before := render(shown, phase)
				after := render(nextShown, phase)
				rects = changedRects(before, after)
			}
			img = render(nextShown, nextPhase)
			if e := p.updateRects(img, rects); e != nil {
				appLog.Printf("COM patch %s: %v; sent_bytes=%d sent_regions=%d; closing and reconnecting after 30s", k, e, p.bytesSent, p.regionsSent)
				needsRecovery = true
				fails++
				tick.Stop()
				p.close()
				select {
				case <-stopCh:
					return
				case <-time.After(30 * time.Second):
				}
				break inner
			}
			phase = nextPhase
			if k == "glow" {
				lastGlow = time.Now()
				continue
			}
			if k == "cpu" || k == "gpu" || k == "ram" {
				fastN++
				fastStart = nextKeyIndex(fastKeys, k)
			} else {
				fastN = 0
				slowStart = nextKeyIndex(slowKeys, k)
			}
			shown = nextShown
		}
	}
}

func killOld() {
	self, _ := os.Executable()
	self = filepath.Base(self)
	for _, n := range []string{
		"Turing35PC.exe", "Turing35NeonStableV39.exe", "Turing35NeonStableV40.exe", "Turing35NeonStableV41.exe", "Turing35NeonStableV42.exe", "Turing35NeonStableV43.exe", "Turing35NeonStableV44.exe", "Turing35NeonStableV45.exe", "Turing35NeonStableV46.exe",
		"Turing35NeonStableV38.exe", "Turing35NeonStableV38_temp.exe",
	} {
		if strings.EqualFold(n, self) {
			continue
		}
		c := exec.Command("taskkill", "/F", "/IM", n)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = c.Run()
	}
}

func setAutostart() {
	exe, e := os.Executable()
	if e != nil {
		return
	}
	c := exec.Command("reg.exe", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "Turing35Neon", "/t", "REG_SZ", "/d", `"`+exe+`"`, "/f")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = c.Run()
}

var user32 = syscall.NewLazyDLL("user32.dll")
var shell32 = syscall.NewLazyDLL("shell32.dll")
var (
	pRegisterClass = user32.NewProc("RegisterClassExW")
	pCreateWindow  = user32.NewProc("CreateWindowExW")
	pDefWnd        = user32.NewProc("DefWindowProcW")
	pGetMessage    = user32.NewProc("GetMessageW")
	pTranslate     = user32.NewProc("TranslateMessage")
	pDispatch      = user32.NewProc("DispatchMessageW")
	pLoadImage     = user32.NewProc("LoadImageW")
	pGetCursor     = user32.NewProc("GetCursorPos")
	pCreatePopup   = user32.NewProc("CreatePopupMenu")
	pAppendMenu    = user32.NewProc("AppendMenuW")
	pTrackPopup    = user32.NewProc("TrackPopupMenu")
	pDestroyMenu   = user32.NewProc("DestroyMenu")
	pForeground    = user32.NewProc("SetForegroundWindow")
	pPostQuit      = user32.NewProc("PostQuitMessage")
	pShellNotify   = shell32.NewProc("Shell_NotifyIconW")
	pGetModule     = kernel.NewProc("GetModuleHandleW")
)

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

const trayMessage = 0x8000 + 52

var trayCB uintptr

func ptr16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func put16(dst []uint16, s string) {
	a, _ := syscall.UTF16FromString(s)
	if len(a) > len(dst) {
		a = a[:len(dst)]
	}
	copy(dst, a)
}

func trayWndProc(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case trayMessage:
		if uint32(l) == 0x0205 {
			menu, _, _ := pCreatePopup.Call()
			if menu != 0 {
				pAppendMenu.Call(menu, 0, 1002, uintptr(unsafe.Pointer(ptr16("Выход"))))
				var pt winPoint
				pGetCursor.Call(uintptr(unsafe.Pointer(&pt)))
				pForeground.Call(hwnd)
				pTrackPopup.Call(menu, 0x0002|0x0020, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
				pDestroyMenu.Call(menu)
			}
			return 0
		}
	case 0x0111:
		if uint32(w&0xffff) == 1002 {
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

func runTray() {
	runtime.LockOSThread()
	icon, e := assets.ReadFile("assets/tray.ico")
	if e != nil {
		appLog.Printf("tray icon: %v", e)
		return
	}
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), version)
	_ = os.MkdirAll(dir, 0755)
	iconFile := filepath.Join(dir, "tray.ico")
	if e := os.WriteFile(iconFile, icon, 0644); e != nil {
		appLog.Printf("tray write: %v", e)
		return
	}
	hmod, _, _ := pGetModule.Call(0)
	name := ptr16("Turing35NeonTray")
	trayCB = syscall.NewCallback(trayWndProc)
	wc := wndClassEx{CbSize: uint32(unsafe.Sizeof(wndClassEx{})), LpfnWndProc: trayCB, HInstance: hmod, LpszClassName: name}
	if r, _, e := pRegisterClass.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		appLog.Printf("tray register: %v", e)
		return
	}
	title := ptr16("Turing 3.5 Neon")
	hwnd, _, e := pCreateWindow.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 0, 0, 0, 0, hmod, 0)
	if hwnd == 0 {
		appLog.Printf("tray window: %v", e)
		return
	}
	ip := ptr16(iconFile)
	hicon, _, e := pLoadImage.Call(0, uintptr(unsafe.Pointer(ip)), 1, 16, 16, 0x10|0x40)
	if hicon == 0 {
		appLog.Printf("tray load: %v", e)
		return
	}
	var nd notifyIconData
	nd.CbSize = uint32(unsafe.Sizeof(nd))
	nd.HWnd = hwnd
	nd.UID = 42
	nd.UFlags = 1 | 2 | 4
	nd.UCallbackMessage = trayMessage
	nd.HIcon = hicon
	put16(nd.SzTip[:], "Turing 3.5 Neon")
	pShellNotify.Call(0, uintptr(unsafe.Pointer(&nd)))
	var m winMsg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 {
			break
		}
		pTranslate.Call(uintptr(unsafe.Pointer(&m)))
		pDispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
	pShellNotify.Call(2, uintptr(unsafe.Pointer(&nd)))
}

func openLog() {
	p := filepath.Join(exeDir(), "app_neon.log")
	if st, e := os.Stat(p); e == nil && st.Size() > 200*1024 {
		_ = os.Remove(p)
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		appLog = log.New(os.Stderr, "", log.Ltime)
		return
	}
	appLog = log.New(f, "", log.Ldate|log.Ltime|log.Lmicroseconds)
}

func main() {
	preview := flag.Bool("preview", false, "render a preview without opening the display")
	flag.Parse()
	openLog()
	appLog.Printf("%s %s start", version, buildVersion)
	if e := loadGraphics(); e != nil {
		appLog.Printf("graphics: %v", e)
		return
	}
	if *preview {
		s := stats{CPU: 42, GPU: 7, RAM: 51, CPUTemp: 54, GPUTemp: 48, CPUFreq: 4.2, CPUPower: 65, GPUPower: 40, VRAMUsed: 3.2, VRAMTotal: 16, RAMUsed: 16.2, RAMTotal: 32, DiskCUsed: 740, DiskCTotal: 931, DiskZUsed: 819, DiskZTotal: 953, Daily: 5, Weekly: 42, Monthly: 100, Claude: usageWindows{37, 20, true, true}, Codex: usageWindows{76, 59, true, true}, UsageOK: true, When: time.Now()}
		im := render(s, 0)
		f, e := os.Create(filepath.Join(exeDir(), "neon_preview.png"))
		if e == nil {
			_ = png.Encode(f, im)
			f.Close()
		}
		appLog.Printf("preview %v", e)
		return
	}
	killOld()
	time.Sleep(1200 * time.Millisecond)
	setAutostart()
	go sensorLoop()
	go openCodeLoop()
	go providerUsageLoop("Claude", readClaudeUsage, func(s *stats, n usageWindows) { s.Claude = n })
	go providerUsageLoop("Codex", readCodexUsage, func(s *stats, n usageWindows) { s.Codex = n })
	go serialLoop()
	runTray()
	stopApp()
	time.Sleep(400 * time.Millisecond)
}
