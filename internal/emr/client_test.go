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
		{name: "hours", duration: 2*time.Hour + time.Minute + 20*time.Second, want: "2时1分20秒"},
		{name: "days", duration: 3*24*time.Hour + 4*time.Hour, want: "3天4时0分0秒"},
		{name: "months", duration: 32 * 24 * time.Hour, want: "1月2天0时0分0秒"},
		{name: "years", duration: 400 * 24 * time.Hour, want: "1年1月5天0时0分0秒"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatClusterDuration(tt.duration.Milliseconds()); got != tt.want {
				t.Fatalf("formatClusterDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}
