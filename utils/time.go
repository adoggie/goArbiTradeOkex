package utils

import (
	"time"
)

func FormatTimeYmdHms(t time.Time) string {
	format := "2006-01-02 15:04:05"
	return t.Format(format)
}
