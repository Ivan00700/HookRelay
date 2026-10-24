package domain

import "testing"

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from DeliveryStatus
		to   DeliveryStatus
		want bool
	}{
		{"pending to processing", DeliveryStatusPending, DeliveryStatusProcessing, true},
		{"processing to delivered", DeliveryStatusProcessing, DeliveryStatusDelivered, true},
		{"processing to failed", DeliveryStatusProcessing, DeliveryStatusFailed, true},
		{"processing to pending for retry", DeliveryStatusProcessing, DeliveryStatusPending, true},
		{"failed to pending for manual retry", DeliveryStatusFailed, DeliveryStatusPending, true},
		{"pending to delivered", DeliveryStatusPending, DeliveryStatusDelivered, false},
		{"pending to failed", DeliveryStatusPending, DeliveryStatusFailed, false},
		{"delivered to processing", DeliveryStatusDelivered, DeliveryStatusProcessing, false},
		{"failed to processing", DeliveryStatusFailed, DeliveryStatusProcessing, false},
		{"same status", DeliveryStatusPending, DeliveryStatusPending, false},
		{"unknown from", DeliveryStatus("unknown"), DeliveryStatusProcessing, false},
		{"unknown to", DeliveryStatusProcessing, DeliveryStatus("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Fatalf("CanTransition(%q, %q) = %t, want %t", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
