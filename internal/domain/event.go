package domain

// CanTransition проверяет допустимый переход между статусами.
func CanTransition(from, to DeliveryStatus) bool {
	switch from {
	case DeliveryStatusPending:
		return to == DeliveryStatusProcessing
	case DeliveryStatusProcessing:
		return to == DeliveryStatusDelivered || to == DeliveryStatusFailed || to == DeliveryStatusPending
	case DeliveryStatusFailed:
		return to == DeliveryStatusPending
	default:
		return false
	}
}
