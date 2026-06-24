package compliance

import "fmt"

// Exporter produces a Report from Sources for one (regime, doc) pair.
type Exporter interface {
	Export(Sources) (Report, error)
}

var registry = map[string]Exporter{}

// Register associates an Exporter with a (regime, doc) pair. Regime packages call
// this from their init() — the database/sql-driver pattern — so compliance never
// imports its regime subpackages and there's no import cycle.
func Register(regime, doc string, exp Exporter) {
	registry[regime+"/"+doc] = exp
}

// Get looks up the Exporter registered for (regime, doc).
func Get(regime, doc string) (Exporter, error) {
	exp, ok := registry[regime+"/"+doc]
	if !ok {
		return nil, fmt.Errorf("no exporter registered for regime=%q doc=%q", regime, doc)
	}
	return exp, nil
}
