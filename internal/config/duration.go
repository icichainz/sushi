package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a length of time in the config file, written as Go writes
// durations: 5s, 1m30s, 500ms. A bare 0 is taken too, as it is the same in
// every unit.
type Duration time.Duration

// UnmarshalYAML reads a duration. Anything else, including a negative
// one or a number without a unit, is a type error, which the decoder notes
// while it carries on with the rest of the file, so the setting keeps its
// default and the rest still applies.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		if v, err := time.ParseDuration(node.Value); err == nil && v >= 0 {
			*d = Duration(v)
			return nil
		}
	}
	return &yaml.TypeError{Errors: []string{fmt.Sprintf("line %d: want a duration such as 5s or 2m, or 0 for never", node.Line)}}
}

// MarshalYAML writes the duration as it is read, as in 5s rather than
// 5000000000
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}
