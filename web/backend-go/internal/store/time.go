package store

import (
	"database/sql"
	"time"
)

// NaiveTime serialises the way Python's datetime.isoformat() does: no timezone
// suffix.
//
// Timestamps are stored as naive UTC+7 wall-clock values. Go's default
// marshalling appends "Z", which tells the browser the value is UTC, and
// `new Date(...)` then shifts every timestamp in the UI by the viewer's offset.
type NaiveTime struct{ time.Time }

const jsonLayout = "2006-01-02T15:04:05.000000"

func (t NaiveTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	out := make([]byte, 0, len(jsonLayout)+2)
	out = append(out, '"')
	out = t.AppendFormat(out, jsonLayout)
	return append(out, '"'), nil
}

func naive(t sql.NullTime) NaiveTime {
	if !t.Valid {
		return NaiveTime{}
	}
	return NaiveTime{t.Time}
}

func naivePtr(t sql.NullTime) *NaiveTime {
	if !t.Valid {
		return nil
	}
	return &NaiveTime{t.Time}
}
