package app

import (
	"testing"
	"time"
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
