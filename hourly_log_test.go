//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHourlyLogClearsAtHourBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app_neon.log")
	now := time.Date(2026, 10, 30, 23, 59, 58, 0, time.FixedZone("MSK", 3*60*60))
	w, err := openHourlyLog(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	if _, err := w.Write([]byte("before midnight\n")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := w.Write([]byte("same hour\n")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "before midnight\nsame hour\n" {
		t.Fatalf("messages lost within the same hour: %q, %v", b, err)
	}
	now = now.Add(time.Second)
	if _, err := w.Write([]byte("after midnight\n")); err != nil {
		t.Fatal(err)
	}
	// A delayed timer for the same boundary must not erase the new message.
	if err := w.resetHour(now); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil || string(b) != "after midnight\n" {
		t.Fatalf("hour change did not replace old messages: %q, %v", b, err)
	}
	// The timer also clears the file when no new messages are written.
	if err := w.resetHour(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil || len(b) != 0 {
		t.Fatalf("idle log was not cleared: %q, %v", b, err)
	}
}

func TestHourlyLogDropsPreviousHourOnStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app_neon.log")
	now := time.Now()
	if err := os.WriteFile(path, []byte("previous run\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	w, err := openHourlyLog(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	b, err := os.ReadFile(path)
	if err != nil || len(b) != 0 {
		t.Fatalf("previous hour survived startup: %q, %v", b, err)
	}
}

func TestHourlyLogResetAndConcurrentWritesKeepNewMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app_neon.log")
	now := time.Now().Truncate(time.Hour)
	w, err := openHourlyLog(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	if _, err := w.Write([]byte("old hour\n")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if _, err := w.Write([]byte(fmt.Sprintf("new-%02d\n", i))); err != nil {
				t.Error(err)
			}
		}(i)
		go func() {
			defer wg.Done()
			if err := w.resetHour(now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "old hour") || strings.Count(string(b), "\n") != 20 {
		t.Fatalf("reset lost or retained messages: %q", b)
	}
	for i := 0; i < 20; i++ {
		if !strings.Contains(string(b), fmt.Sprintf("new-%02d\n", i)) {
			t.Fatalf("new message %d lost at hour boundary", i)
		}
	}
}
