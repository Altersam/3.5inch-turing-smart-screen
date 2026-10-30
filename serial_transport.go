//go:build windows

package main

import (
	"fmt"
	"io"
	"time"
)

const (
	serialChunkSize      = dispW * 8
	serialBytesPerSecond = 48 * 1024
	serialRegionGap      = 40 * time.Millisecond
)

// Limit both the USB write size and the burst rate. WriteFile completion only
// means the driver accepted bytes, not that the screen has processed them.
type serialWriter struct {
	write func([]byte) (int, error)
	wait  func(time.Duration) error
	now   func() time.Time
}

func (w serialWriter) send(buf []byte) (sent int, writeTime time.Duration, err error) {
	for sent < len(buf) {
		if err := w.wait(0); err != nil {
			return sent, writeTime, err
		}
		end := sent + serialChunkSize
		if end > len(buf) {
			end = len(buf)
		}
		started := w.now()
		n, err := w.write(buf[sent:end])
		elapsed := w.now().Sub(started)
		writeTime += elapsed
		if n < 0 || n > end-sent {
			return sent, writeTime, fmt.Errorf("invalid WriteFile count %d for %d bytes", n, end-sent)
		}
		offset := sent
		sent += n
		if err != nil {
			return sent, writeTime, fmt.Errorf("WriteFile offset=%d/%d transferred=%d elapsed=%s: %w", offset, len(buf), n, elapsed.Round(time.Millisecond), err)
		}
		if n == 0 {
			return sent, writeTime, fmt.Errorf("WriteFile offset=%d/%d: %w", offset, len(buf), io.ErrNoProgress)
		}
		budget := time.Duration(n) * time.Second / serialBytesPerSecond
		if err := w.wait(budget - elapsed); err != nil {
			return sent, writeTime, err
		}
	}
	return sent, writeTime, nil
}

func waitSerial(d time.Duration) error {
	if d <= 0 {
		select {
		case <-stopCh:
			return fmt.Errorf("stopping")
		default:
			return nil
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-stopCh:
		return fmt.Errorf("stopping")
	case <-t.C:
		return nil
	}
}
