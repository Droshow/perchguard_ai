package compliance

import "testing"

type stubExporter struct{ tag string }

func (s stubExporter) Export(Sources) (Report, error) {
	return Report{Regime: s.tag}, nil
}

func TestRegisterAndGet(t *testing.T) {
	exp := stubExporter{tag: "registered"}
	Register("test-regime", "test-doc", exp)

	got, err := Get("test-regime", "test-doc")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	report, _ := got.Export(Sources{})
	if report.Regime != "registered" {
		t.Errorf("Get returned wrong exporter: report.Regime = %q", report.Regime)
	}
}

func TestGetUnregistered(t *testing.T) {
	_, err := Get("nope", "nope")
	if err == nil {
		t.Fatal("expected an error for an unregistered (regime, doc) pair")
	}
}
