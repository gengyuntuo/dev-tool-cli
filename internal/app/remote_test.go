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

func TestFormatYarnRowPreservesApplicationID(t *testing.T) {
	const applicationID = "application_1725623456789_0123"
	row := formatYarnRow(
		128,
		applicationID,
		"daily-data-processing",
		"RUNNING",
		"hadoop",
		"2026-09-06 15:00:00",
		"1时2分3秒",
	)
	if !strings.Contains(row, applicationID) {
		t.Fatalf("formatYarnRow() truncated application ID: %q", row)
	}

	idWidth, nameWidth, _, _, _, _ := emrYarnColumnWidths(128)
	if idWidth != 32 {
		t.Fatalf("ID width = %d, want 32", idWidth)
	}
	if nameWidth < 36 {
		t.Fatalf("Name width = %d, want at least 36", nameWidth)
	}

	_, wideNameWidth, _, _, _, _ := emrYarnColumnWidths(180)
	if wideNameWidth <= nameWidth {
		t.Fatalf("wide Name width = %d, want greater than %d", wideNameWidth, nameWidth)
	}
}

func TestEMRDetailPageSizeUsesAvailableHeight(t *testing.T) {
	tests := []struct {
		height int
		want   int
	}{
		{height: 40, want: 30},
		{height: 24, want: 14},
		{height: 10, want: 1},
	}

	for _, tt := range tests {
		m := model{height: tt.height}
		if got := m.emrDetailPageSize(); got != tt.want {
			t.Fatalf("height %d: emrDetailPageSize() = %d, want %d", tt.height, got, tt.want)
		}
	}
}

func TestStepColumnWidthPriorities(t *testing.T) {
	idWidth, nameWidth, createdWidth, startedWidth, endedWidth, elapsedWidth, stateWidth := emrStepColumnWidths(160)
	if idWidth != 24 {
		t.Fatalf("ID width = %d, want 24", idWidth)
	}
	if nameWidth <= 8 {
		t.Fatalf("Name width = %d, want more than minimum width", nameWidth)
	}
	if createdWidth != 19 || startedWidth != 19 || endedWidth != 19 || elapsedWidth != 26 || stateWidth != 18 {
		t.Fatalf(
			"non-Name widths = %v, want [19 19 19 26 18]",
			[]int{createdWidth, startedWidth, endedWidth, elapsedWidth, stateWidth},
		)
	}

	idWidth, nameWidth, createdWidth, startedWidth, endedWidth, _, _ = emrStepColumnWidths(128)
	if idWidth != 24 || nameWidth != 8 || createdWidth != 19 || startedWidth != 19 || endedWidth != 19 {
		t.Fatalf(
			"128-column priorities = %v, want ID and timestamps preserved with minimum Name",
			[]int{idWidth, nameWidth, createdWidth, startedWidth, endedWidth},
		)
	}
}
