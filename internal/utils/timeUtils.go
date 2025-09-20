package utils

import (
	"time"
	_ "time/tzdata"
)

func ConvertToDateTime(unixTime float64) time.Time {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return time.Time{}
	}

	return time.Unix(int64(unixTime), 0).In(ist)
}
