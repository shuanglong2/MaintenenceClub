package web

import (
	"testing"
	"time"
)

func TestFormatYuan(t *testing.T) {
	if got := formatYuan(1234); got != "12.34" {
		t.Fatal(got)
	}
	if got := formatYuan(-5); got != "-0.05" {
		t.Fatal(got)
	}
}

func TestFormatTime(t *testing.T) {
	value := time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC)
	if got := formatTime(value); got != "2026-11-16 10:30" {
		t.Fatal(got)
	}
}
