package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestFormatLatency(t *testing.T) {
	tests := []struct {
		latency time.Duration
		want    string
	}{
		{latency: 450 * time.Microsecond, want: "0.45ms"},
		{latency: 1250 * time.Microsecond, want: "1.2ms"},
		{latency: 25 * time.Millisecond, want: "25.0ms"},
	}

	for _, tt := range tests {
		if got := formatLatency(tt.latency); got != tt.want {
			t.Fatalf("formatLatency(%s) = %q, want %q", tt.latency, got, tt.want)
		}
	}
}

func TestFormatShareRowAdaptsToWidth(t *testing.T) {
	for _, width := range []int{80, 100, 125, 150} {
		row := formatShareRow(
			width,
			"connect",
			"hadoop@example.internal:20022",
			"id_rsa",
			"127.0.0.1:20022",
			"SOCKS5 127.0.0.1:10800",
			"2026-09-06 14:00:00",
			"reconnecting (10s)",
		)
		if got := lipgloss.Width(row); got != width {
			t.Fatalf("formatShareRow(%d) width = %d, want %d", width, got, width)
		}
		if !strings.Contains(row, "reconnecting (10s)") {
			t.Fatalf("formatShareRow(%d) omitted retry countdown: %q", width, row)
		}
	}
}
