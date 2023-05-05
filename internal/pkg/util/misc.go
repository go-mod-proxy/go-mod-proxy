package util

import (
	"fmt"
)

func FormatIth(i int) string {
	var suffix string
	switch i % 10 {
	case 1:
		suffix = "st"
	case 2:
		suffix = "nd"
	case 3:
		suffix = "rd"
	default:
		suffix = "th"
	}
	return fmt.Sprintf("%d%s", i, suffix)
}
