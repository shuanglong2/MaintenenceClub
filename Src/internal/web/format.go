package web

import (
	"fmt"
	"time"
)

var china = time.FixedZone("CST", 8*3600)

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.In(china).Format("2006-01-02 15:04")
}

func formatYuan(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

func parseDay(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, china)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
