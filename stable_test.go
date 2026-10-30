//go:build windows

package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlowPatchesCoverChangedPixels(t *testing.T) {
	if err := loadGraphics(); err != nil {
		t.Fatal(err)
	}
	s := stats{When: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	for phase := 0; phase < 84; phase++ {
		next, x, y, w, h := glowPatch(phase)
		patch := image.Rect(x, y, x+w, y+h)
		if !patch.In(image.Rect(0, 0, dispW, dispH)) || x%2 != 0 || y%2 != 0 || w%2 != 0 || h%2 != 0 {
			t.Fatalf("phase %d: invalid patch %v", phase, patch)
		}
		before, after := render(s, phase), render(s, next)
		for py := 0; py < dispH; py++ {
			for px := 0; px < dispW; px++ {
				if before.RGBAAt(px, py) != after.RGBAAt(px, py) && !image.Pt(px, py).In(patch) {
					t.Fatalf("phase %d -> %d: changed pixel (%d,%d) outside patch %v", phase, next, px, py, patch)
				}
			}
		}
	}
}

func TestOpenCodeUpdatesAndPatchBounds(t *testing.T) {
	if err := loadGraphics(); err != nil {
		t.Fatal(err)
	}
	base := stats{When: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	for _, update := range []stats{
		{UsageOK: true},
		{Claude: usageWindows{FiveOK: true}},
		{Codex: usageWindows{WeekOK: true}},
		{UsageOK: true, Daily: 100, Weekly: 100, Monthly: 100, Claude: usageWindows{100, 100, true, true}, Codex: usageWindows{100, 100, true, true}},
	} {
		update.When = base.When
		if pickKey(base, update, 0, 0, 0) != "op" {
			t.Fatalf("OpenCode update not selected: %+v", update)
		}
		b := boxByKey("op")
		patch := image.Rect(b.x, b.y, b.x+b.w, b.y+b.h)
		before, after := render(base, 0), render(update, 0)
		for y := 0; y < dispH; y++ {
			for x := 0; x < dispW; x++ {
				if before.RGBAAt(x, y) != after.RGBAAt(x, y) && !image.Pt(x, y).In(patch) {
					t.Fatalf("OpenCode changed pixel (%d,%d) outside patch %v", x, y, patch)
				}
			}
		}
	}
}

func TestQuotaLabelsFit(t *testing.T) {
	if err := loadGraphics(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		text  string
		size  int
		width int
	}{
		{"Д:100%", 10, 59}, {"Н:100%", 10, 59}, {"М:100%", 12, 62},
		{"К5:100%", 12, 70}, {"КН:100%", 12, 70}, {"G5:100%", 12, 74}, {"GН:100%", 12, 74},
	} {
		if got := fontWidth(tc.size, tc.text); got > tc.width {
			t.Errorf("label %q width %d exceeds %d", tc.text, got, tc.width)
		}
	}
}

func TestCodexWindowSelection(t *testing.T) {
	for _, tc := range []struct {
		json string
		want usageWindows
		err  bool
	}{
		{`{"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":76,"windowDurationMins":300},"secondary":{"usedPercent":59,"windowDurationMins":10080}}}}`, usageWindows{76, 59, true, true}, false},
		{`{"rateLimits":{"primary":{"usedPercent":55,"windowDurationMins":10080},"secondary":{"usedPercent":20.6,"windowDurationMins":300}}}`, usageWindows{21, 55, true, true}, false},
		{`{"rateLimits":{"primary":{"usedPercent":90,"windowDurationMins":10080}}}`, usageWindows{Week: 90, WeekOK: true}, false},
		{`{"rateLimits":{"primary":{"usedPercent":null,"windowDurationMins":300}}}`, usageWindows{}, true},
	} {
		got, err := codexUsage([]byte(tc.json))
		if (err != nil) != tc.err || (!tc.err && got != tc.want) {
			t.Errorf("%s: got %+v, %v; want %+v, error=%t", tc.json, got, err, tc.want, tc.err)
		}
	}
}

func TestInstalledCodexWhenRequested(t *testing.T) {
	if os.Getenv("TURING_TEST_CODEX") != "1" {
		t.Skip("set TURING_TEST_CODEX=1 to check the signed-in local Codex")
	}
	if n, err := readCodexUsage(); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("Codex usage: five-hour=%d%% (available=%t), weekly=%d%% (available=%t)", n.Five, n.FiveOK, n.Week, n.WeekOK)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClaudeUsageFromOAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	cred := `{"claudeAiOauth":{"accessToken":"test-token","expiresAt":4102444800000}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude", ".credentials.json"), []byte(cred), 0600); err != nil {
		t.Fatal(err)
	}
	old := remoteClient
	t.Cleanup(func() { remoteClient = old })
	remoteClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.anthropic.com" || req.URL.Path != "/api/oauth/usage" || req.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected Claude request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"five_hour":{"utilization":37.6},"seven_day":{"utilization":21}}`)), Header: make(http.Header)}, nil
	})}
	got, err := readClaudeUsage()
	if err != nil || got != (usageWindows{38, 21, true, true}) {
		t.Fatalf("got %+v, %v; want both usage windows", got, err)
	}
}

func TestExtendedPanelDoesNotTouchCards(t *testing.T) {
	b, err := assets.ReadFile("assets/base.png")
	if err != nil {
		t.Fatal(err)
	}
	src, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if err := loadGraphics(); err != nil {
		t.Fatal(err)
	}
	for y := 104; y < dispH; y++ {
		for x := 0; x < dispW; x++ {
			if color.RGBAModel.Convert(src.At(x, y)) != baseImage.RGBAAt(x, y) {
				t.Fatalf("panel extension changed pixel (%d,%d) below the panel", x, y)
			}
		}
	}
	if got := baseImage.RGBAAt(150, 101); got != rgb(44, 223, 255) {
		t.Fatalf("extended lower border missing: %v", got)
	}
}

func TestBusyStatsDoNotStarveOtherUpdates(t *testing.T) {
	shown := stats{When: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	fastN, fastStart, slowStart := 0, 0, 0
	seen := map[string]bool{}
	for i := 1; i <= 16; i++ {
		now := shown
		now.CPU, now.GPU, now.RAM = i, i, i
		now.CPUTemp, now.GPUTemp = i, i
		now.UsageOK = true
		k := pickKey(shown, now, fastN, fastStart, slowStart)
		seen[k] = true
		applyKey(&shown, now, k)
		if k == "cpu" || k == "gpu" || k == "ram" {
			fastN++
			fastStart = nextKeyIndex(fastKeys, k)
		} else {
			fastN = 0
			slowStart = nextKeyIndex(slowKeys, k)
		}
	}
	for _, k := range []string{"cpu", "gpu", "ram", "op", "cpu_d", "gpu_d"} {
		if !seen[k] {
			t.Errorf("update %q was starved", k)
		}
	}
}

func TestClockTakesPriorityWhenMinuteChanges(t *testing.T) {
	shown := stats{When: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	now := shown
	now.When = now.When.Add(time.Minute)
	now.CPU = 50
	if got := pickKey(shown, now, 0, 0, 0); got != "clock" {
		t.Fatalf("got %q, want clock", got)
	}
}

func TestOrientationPacket(t *testing.T) {
	want := []byte{0, 0, 0, 0, 0, 121, 102, 1, 224, 1, 64, 0, 0, 0, 0, 0}
	if got := orientationCommand(2, 480, 320); !bytes.Equal(got, want) {
		t.Fatalf("orientation packet %v, want %v", got, want)
	}
}

func TestRegionWritesHeaderAndRGB565Payload(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, dispW, dispH))
	im.SetRGBA(2, 4, color.RGBA{255, 0, 0, 255})
	im.SetRGBA(3, 4, color.RGBA{0, 255, 0, 255})
	im.SetRGBA(2, 5, color.RGBA{0, 0, 255, 255})
	im.SetRGBA(3, 5, color.RGBA{255, 255, 255, 255})
	var writes [][]byte
	err := sendRegion(func(p []byte) error {
		writes = append(writes, append([]byte(nil), p...))
		return nil
	}, im, 2, 4, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || !bytes.Equal(writes[0], cmd(2, 4, 3, 5, 197)) || !bytes.Equal(writes[1], []byte{0, 0xf8, 0xe0, 7, 0x1f, 0, 0xff, 0xff}) {
		t.Fatalf("unexpected writes: %x", writes)
	}
	writes = nil
	wantErr := errors.New("disconnected")
	err = sendRegion(func(p []byte) error {
		writes = append(writes, p)
		return wantErr
	}, im, 2, 4, 2, 2)
	if !errors.Is(err, wantErr) || len(writes) != 1 {
		t.Fatalf("header failure should stop transaction: %v, writes=%d", err, len(writes))
	}
}

func TestChangedRectsReconstructUpdatedScreen(t *testing.T) {
	if err := loadGraphics(); err != nil {
		t.Fatal(err)
	}
	base := stats{CPU: 42, GPU: 7, RAM: 51, When: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	for _, tc := range []struct {
		key string
		set func(*stats)
	}{
		{"cpu", func(s *stats) { s.CPU = 43 }},
		{"cpu", func(s *stats) { s.CPU = 100 }},
		{"gpu", func(s *stats) { s.GPU = 98 }},
		{"ram", func(s *stats) { s.RAM = 0 }},
		{"clock", func(s *stats) { s.When = s.When.Add(time.Minute) }},
		{"op", func(s *stats) { s.UsageOK = true; s.Daily, s.Weekly, s.Monthly = 100, 25, 2 }},
		{"cpu_d", func(s *stats) { s.CPUTemp, s.CPUFreq, s.CPUPower = 54, 4.2, 65 }},
	} {
		now := base
		tc.set(&now)
		shown := base
		applyKey(&shown, now, tc.key)
		before, after := render(base, 17), render(shown, 17)
		rects := changedRects(before, after)
		if len(rects) == 0 {
			t.Fatalf("%s: no changes detected", tc.key)
		}
		got := image.NewRGBA(before.Bounds())
		copy(got.Pix, before.Pix)
		var area int
		for _, r := range rects {
			if !r.In(before.Bounds()) || r.Min.X%2 != 0 || r.Min.Y%2 != 0 || r.Dx()%2 != 0 || r.Dy()%2 != 0 {
				t.Fatalf("%s: invalid rect %v", tc.key, r)
			}
			area += r.Dx() * r.Dy()
			draw.Draw(got, r, after, r.Min, draw.Src)
		}
		if !bytes.Equal(got.Pix, after.Pix) {
			t.Fatalf("%s: changes not completely reconstructed", tc.key)
		}
		if tc.key == "cpu" && now.CPU == 43 && area >= 132*50 {
			t.Fatalf("one-digit CPU update sends too much: %d pixels", area)
		}
	}
}
