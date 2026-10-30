//go:build windows

package main

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSerialWriterPreservesPayloadAndLimitsBursts(t *testing.T) {
	for _, maxWrite := range []int{serialChunkSize, 257} {
		t.Run(strconv.Itoa(maxWrite), func(t *testing.T) {
			payload := make([]byte, dispW*dispH*2)
			for i := range payload {
				payload[i] = byte(i)
			}
			clock := time.Unix(0, 0)
			start := clock
			var got bytes.Buffer
			const latency = time.Millisecond
			calls := 0
			w := serialWriter{
				now: func() time.Time { return clock },
				wait: func(d time.Duration) error {
					if d > 0 {
						clock = clock.Add(d)
					}
					return nil
				},
				write: func(p []byte) (int, error) {
					if len(p) > serialChunkSize {
						t.Fatalf("oversized USB write: %d bytes", len(p))
					}
					if len(p) > maxWrite {
						p = p[:maxWrite]
					}
					calls++
					clock = clock.Add(latency)
					return got.Write(p)
				},
			}
			n, writeTime, err := w.send(payload)
			if err != nil || n != len(payload) || !bytes.Equal(got.Bytes(), payload) {
				t.Fatalf("payload corrupted: sent=%d, error=%v", n, err)
			}
			// Integer nanosecond rounding can lose less than one ns per call.
			minimum := time.Duration(len(payload))*time.Second/serialBytesPerSecond - time.Duration(calls)
			if elapsed := clock.Sub(start); elapsed < minimum {
				t.Fatalf("USB burst rate exceeded: elapsed=%s, minimum=%s", elapsed, minimum)
			}
			if writeTime != time.Duration(calls)*latency {
				t.Fatalf("write time includes pacing: %s", writeTime)
			}
		})
	}
}

func TestSerialWriterStopsOnFailedOrEmptyWrites(t *testing.T) {
	wantErr := errors.New("USB endpoint failed")
	for _, tc := range []struct {
		name string
		n    int
		err  error
		want error
	}{
		{"failed", 10, wantErr, wantErr},
		{"empty", 0, nil, io.ErrNoProgress},
		{"invalid count", serialChunkSize + 1, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			w := serialWriter{
				now:  time.Now,
				wait: func(time.Duration) error { return nil },
				write: func([]byte) (int, error) {
					calls++
					return tc.n, tc.err
				},
			}
			n, _, err := w.send(make([]byte, serialChunkSize*2))
			if err == nil || calls != 1 || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("failed transfer continued: calls=%d error=%v", calls, err)
			}
			if tc.name == "failed" && n != tc.n {
				t.Fatalf("lost partial-transfer count: %d", n)
			}
		})
	}
}

func TestSerialWriterCanStopDuringPacing(t *testing.T) {
	wantErr := errors.New("stopped")
	calls := 0
	w := serialWriter{
		now: func() time.Time { return time.Time{} },
		wait: func(d time.Duration) error {
			if d > 0 {
				return wantErr
			}
			return nil
		},
		write: func(p []byte) (int, error) { calls++; return len(p), nil },
	}
	n, _, err := w.send(make([]byte, serialChunkSize*2))
	if !errors.Is(err, wantErr) || calls != 1 || n != serialChunkSize {
		t.Fatalf("stop ignored: sent=%d calls=%d error=%v", n, calls, err)
	}
}

func TestSerialWriterDoesNotDelayAlreadySlowWrites(t *testing.T) {
	clock := time.Time{}
	w := serialWriter{
		now: func() time.Time { return clock },
		wait: func(d time.Duration) error {
			if d > 0 {
				t.Fatalf("extra delay after slow write: %s", d)
			}
			return nil
		},
		write: func(p []byte) (int, error) {
			clock = clock.Add(time.Second)
			return len(p), nil
		},
	}
	if _, _, err := w.send(make([]byte, serialChunkSize)); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryTargetsOnlyMatchingDisplay(t *testing.T) {
	const id = `USB\VID_1A86&PID_5722\USB35INCHIPSV2`
	devices := []displayDevice{
		{"Other serial device (COM5)", `USB\VID_1234&PID_5678\OTHER`},
		{"USB hub (COM5)", `USB\ROOT_HUB30\HUB`},
		{"Other display (COM6)", `USB\VID_1A86&PID_5722\SECOND`},
		{"USB serial display (COM5)", id},
	}
	got, err := recoveryDeviceID(devices, "com5")
	if err != nil || got != id {
		t.Fatalf("wrong USB restart target: %q, %v", got, err)
	}
	if _, err := recoveryDeviceID(devices, "COM7"); err == nil {
		t.Fatal("missing display should not restart a different device")
	}
	devices = append(devices, displayDevice{"Duplicate display (COM5)", id})
	if _, err := recoveryDeviceID(devices, "COM5"); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Fatalf("ambiguous restart target accepted: %v", err)
	}
}
