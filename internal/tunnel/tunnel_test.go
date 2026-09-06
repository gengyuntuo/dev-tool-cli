package tunnel

import (
	"context"
	"testing"
	"time"
)

func TestWaitBeforeReconnectManualRetry(t *testing.T) {
	retry := make(chan struct{}, 1)
	retry <- struct{}{}

	var notifications []time.Duration
	manual, err := waitBeforeReconnect(context.Background(), retry, func(remaining time.Duration) {
		notifications = append(notifications, remaining)
	})
	if err != nil {
		t.Fatalf("waitBeforeReconnect() error = %v", err)
	}
	if !manual {
		t.Fatal("waitBeforeReconnect() manual = false, want true")
	}
	if len(notifications) != 2 || notifications[0] != reconnectInterval || notifications[1] != 0 {
		t.Fatalf("notifications = %v, want [%v 0s]", notifications, reconnectInterval)
	}
}

func TestWaitBeforeReconnectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	manual, err := waitBeforeReconnect(ctx, make(chan struct{}), func(time.Duration) {})
	if err == nil {
		t.Fatal("waitBeforeReconnect() error = nil, want context cancellation")
	}
	if manual {
		t.Fatal("waitBeforeReconnect() manual = true, want false")
	}
}
