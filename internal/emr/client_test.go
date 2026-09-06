package emr

import (
	"testing"
	"time"
)

func TestFormatClusterDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "seconds", duration: 20 * time.Second, want: "20秒"},
		{name: "single digit seconds", duration: 5 * time.Second, want: "05秒"},
		{name: "hours", duration: 2*time.Hour + time.Minute + 20*time.Second, want: "02时01分20秒"},
		{name: "days", duration: 3*24*time.Hour + 4*time.Hour, want: "03天04时00分00秒"},
		{name: "months", duration: 32 * 24 * time.Hour, want: "01月02天00时00分00秒"},
		{name: "years", duration: 400 * 24 * time.Hour, want: "1年01月05天00时00分00秒"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatClusterDuration(tt.duration.Milliseconds()); got != tt.want {
				t.Fatalf("formatClusterDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatYarnDurationUsesFixedWidthUnits(t *testing.T) {
	duration := 400*24*time.Hour + 2*time.Hour + 3*time.Minute + 4*time.Second
	want := "1年01月05天02时03分04秒"
	if got := formatYarnDuration(duration.Milliseconds()); got != want {
		t.Fatalf("formatYarnDuration() = %q, want %q", got, want)
	}
}
