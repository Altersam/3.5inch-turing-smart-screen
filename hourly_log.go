//go:build windows

package main

import (
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type hourlyLogFile struct {
	mu   sync.Mutex
	file *os.File
	hour time.Time
	now  func() time.Time
}

func openHourlyLog(path string, now func() time.Time) (*hourlyLogFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, err
	}
	w := &hourlyLogFile{file: f, hour: st.ModTime().Truncate(time.Hour), now: now}
	if err := w.resetHour(now()); err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

// Both the hourly timer and the first write of a new hour may trigger a
// reset. Serialize them so the timer cannot erase a fresh hour's messages.
func (w *hourlyLogFile) resetLocked(now time.Time) error {
	hour := now.Truncate(time.Hour)
	if w.hour.Equal(hour) {
		return nil
	}
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	w.hour = hour
	return nil
}

func (w *hourlyLogFile) resetHour(now time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.resetLocked(now)
}

func (w *hourlyLogFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.resetLocked(w.now()); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *hourlyLogFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

func resetLogHourly(w *hourlyLogFile) {
	for {
		nextHour := time.Now().Truncate(time.Hour).Add(time.Hour)
		t := time.NewTimer(time.Until(nextHour))
		select {
		case <-stopCh:
			t.Stop()
			return
		case <-t.C:
			if err := w.resetHour(time.Now()); err != nil {
				log.Printf("hourly log reset: %v", err)
			}
		}
	}
}
