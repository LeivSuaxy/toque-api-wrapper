package timeutil

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

const DateLayout = "2006-01-02"

var Havana = mustLoadLocation("America/Havana")

func ParseDate(raw string) (time.Time, error) {
	value, err := time.ParseInLocation(DateLayout, raw, Havana)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date %q: %w", raw, err)
	}
	return value, nil
}

func StartOfDay(value time.Time) time.Time {
	year, month, day := value.In(Havana).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, Havana)
}

func EndOfDayExclusive(value time.Time) time.Time {
	return StartOfDay(value).AddDate(0, 0, 1)
}

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("load location %s: %v", name, err))
	}
	return location
}
