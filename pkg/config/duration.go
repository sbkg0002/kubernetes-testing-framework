package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration wraps time.Duration so it unmarshals from YAML/JSON strings like "10m", "5s".
// sigs.k8s.io/yaml converts YAML → JSON before unmarshalling, so we implement
// UnmarshalJSON (not UnmarshalYAML).
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// Fall back: bare integer treated as nanoseconds
		var n int64
		if err2 := json.Unmarshal(b, &n); err2 != nil {
			return fmt.Errorf("duration must be a string (e.g. \"10m\") or integer nanoseconds: %w", err)
		}
		d.Duration = time.Duration(n)
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}
