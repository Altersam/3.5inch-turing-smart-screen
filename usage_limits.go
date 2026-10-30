//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type usageWindows struct {
	Five, Week     int
	FiveOK, WeekOK bool
}

// Claude Code's local OAuth usage endpoint is not a public API and may change.
// Read only the current access token; credential refresh remains Claude Code's job.
func readClaudeUsage() (usageWindows, error) {
	b, err := os.ReadFile(filepath.Join(home(), ".claude", ".credentials.json"))
	if err != nil {
		return usageWindows{}, errors.New("Claude Code credentials unavailable")
	}
	var creds struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(b, &creds) != nil || creds.OAuth.AccessToken == "" {
		return usageWindows{}, errors.New("Claude Code OAuth login required")
	}
	if creds.OAuth.ExpiresAt > 0 && creds.OAuth.ExpiresAt <= time.Now().UnixMilli() {
		return usageWindows{}, errors.New("Claude Code login expired; sign in again")
	}
	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return usageWindows{}, err
	}
	req.Header.Set("Authorization", "Bearer "+creds.OAuth.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	resp, err := remoteClient.Do(req)
	if err != nil {
		return usageWindows{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return usageWindows{}, fmt.Errorf("Claude usage HTTP=%d", resp.StatusCode)
	}
	type claudeWindow struct {
		Utilization *float64 `json:"utilization"`
	}
	var payload struct {
		FiveHour *claudeWindow `json:"five_hour"`
		SevenDay *claudeWindow `json:"seven_day"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return usageWindows{}, errors.New("invalid Claude usage response")
	}
	var result usageWindows
	if payload.FiveHour != nil && payload.FiveHour.Utilization != nil {
		result.Five, result.FiveOK = usagePercent(*payload.FiveHour.Utilization), true
	}
	if payload.SevenDay != nil && payload.SevenDay.Utilization != nil {
		result.Week, result.WeekOK = usagePercent(*payload.SevenDay.Utilization), true
	}
	if !result.FiveOK && !result.WeekOK {
		return usageWindows{}, errors.New("Claude usage windows unavailable")
	}
	return result, nil
}

func usagePercent(n float64) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return int(n + 0.5)
}

func findCodex() (string, error) {
	if p, err := exec.LookPath("codex.exe"); err == nil {
		return p, nil
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("Codex executable not found")
	}
	paths, _ := filepath.Glob(filepath.Join(base, "OpenAI", "Codex", "bin", "*", "codex.exe"))
	var newest time.Time
	var selected string
	for _, p := range paths {
		if s, err := os.Stat(p); err == nil && (selected == "" || s.ModTime().After(newest)) {
			selected, newest = p, s.ModTime()
		}
	}
	if selected == "" {
		return "", errors.New("Codex executable not found")
	}
	return selected, nil
}

type codexWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins *int     `json:"windowDurationMins"`
}

type codexBucket struct {
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
}

func codexUsage(data json.RawMessage) (usageWindows, error) {
	var result struct {
		RateLimits          *codexBucket           `json:"rateLimits"`
		RateLimitsByLimitID map[string]codexBucket `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return usageWindows{}, errors.New("invalid Codex usage response")
	}
	bucket := result.RateLimits
	if b, ok := result.RateLimitsByLimitID["codex"]; ok {
		bucket = &b
	}
	var usage usageWindows
	if bucket != nil {
		for _, w := range []*codexWindow{bucket.Primary, bucket.Secondary} {
			if w == nil || w.UsedPercent == nil || w.WindowDurationMins == nil {
				continue
			}
			switch *w.WindowDurationMins {
			case 300:
				usage.Five, usage.FiveOK = usagePercent(*w.UsedPercent), true
			case 10080:
				usage.Week, usage.WeekOK = usagePercent(*w.UsedPercent), true
			}
		}
	}
	if !usage.FiveOK && !usage.WeekOK {
		return usageWindows{}, errors.New("Codex usage windows unavailable")
	}
	return usage, nil
}

type rpcResponse struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func waitRPC(dec *json.Decoder, id int) (json.RawMessage, error) {
	for {
		var msg rpcResponse
		if err := dec.Decode(&msg); err != nil {
			return nil, fmt.Errorf("Codex app-server: %w", err)
		}
		if msg.ID == nil || *msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("Codex app-server: %s", msg.Error.Message)
		}
		return msg.Result, nil
	}
}

func readCodexUsage() (usageWindows, error) {
	exe, err := findCodex()
	if err != nil {
		return usageWindows{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, exe, "app-server")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	in, err := c.StdinPipe()
	if err != nil {
		return usageWindows{}, err
	}
	out, err := c.StdoutPipe()
	if err != nil {
		return usageWindows{}, err
	}
	if err := c.Start(); err != nil {
		return usageWindows{}, err
	}
	defer func() {
		_ = in.Close()
		_ = c.Process.Kill()
		_ = c.Wait()
	}()
	enc, dec := json.NewEncoder(in), json.NewDecoder(out)
	if err := enc.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{
		"clientInfo": map[string]string{"name": "turing_screen", "version": "1.0"},
	}}); err != nil {
		return usageWindows{}, err
	}
	if _, err := waitRPC(dec, 1); err != nil {
		return usageWindows{}, err
	}
	if err := enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return usageWindows{}, err
	}
	if err := enc.Encode(map[string]any{"id": 2, "method": "account/rateLimits/read"}); err != nil {
		return usageWindows{}, err
	}
	data, err := waitRPC(dec, 2)
	if err != nil {
		return usageWindows{}, err
	}
	return codexUsage(data)
}

func providerUsageLoop(name string, read func() (usageWindows, error), set func(*stats, usageWindows)) {
	update := func() {
		value, err := read()
		if err != nil {
			appLog.Printf("%s usage: %v", name, err)
		}
		writeStats(func(s *stats) { set(s, value) })
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
