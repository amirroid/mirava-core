package gradle

// speedRating uses the same thresholds as pkg.getSpeedRating (npm/cargo/pypi/pacman).
func speedRating(speedMBps float64) string {
	switch {
	case speedMBps > 20:
		return "Excellent"
	case speedMBps > 10:
		return "Good"
	case speedMBps > 5:
		return "Average"
	default:
		return "Slow"
	}
}
