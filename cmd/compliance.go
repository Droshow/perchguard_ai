package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/compliance"
	_ "github.com/Droshow/PerchGuard/perchguard/pkg/compliance/euaiact" // registers eu-ai-act exporters
	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// complianceConfig holds the parameters for compliance export mode.
type complianceConfig struct {
	regime       string
	doc          string
	policyPath   string
	dbPath       string
	manifestPath string
	since, until string
	format       string
	out          string
}

// runCompliance loads the policy profile and audit store directly (no running server
// required) and renders the requested regime/doc export to stdout or --out.
func runCompliance(cfg complianceConfig) {
	pol, err := policy.Load(cfg.policyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perchguard] load policy %q: %v\n", cfg.policyPath, err)
		os.Exit(1)
	}

	sink, err := store.NewSQLiteAuditSink(cfg.dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perchguard] open audit db %q: %v\n", cfg.dbPath, err)
		os.Exit(1)
	}
	defer sink.Close()

	var man *manifest.AgentManifest
	if cfg.manifestPath != "" {
		data, err := os.ReadFile(cfg.manifestPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[perchguard] read manifest %q: %v\n", cfg.manifestPath, err)
			os.Exit(1)
		}
		m, err := manifest.Parse(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[perchguard] parse manifest %q: %v\n", cfg.manifestPath, err)
			os.Exit(1)
		}
		man = m
	}

	since, err := parseComplianceTime(cfg.since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perchguard] --since: %v\n", err)
		os.Exit(1)
	}
	until, err := parseComplianceTime(cfg.until)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perchguard] --until: %v\n", err)
		os.Exit(1)
	}

	exporter, err := compliance.Get(cfg.regime, cfg.doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perchguard] %v\n", err)
		os.Exit(1)
	}

	report, err := exporter.Export(compliance.Sources{
		Policy:   pol,
		Audit:    sink,
		Manifest: man,
		Since:    since,
		Until:    until,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[perchguard] export: %v\n", err)
		os.Exit(1)
	}

	var output []byte
	switch cfg.format {
	case "json":
		output, err = compliance.RenderJSON(report)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[perchguard] render json: %v\n", err)
			os.Exit(1)
		}
	default:
		output = []byte(compliance.RenderMarkdown(report))
	}

	if cfg.out == "" {
		os.Stdout.Write(output)
		return
	}
	if err := os.WriteFile(cfg.out, output, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[perchguard] write %q: %v\n", cfg.out, err)
		os.Exit(1)
	}
}

// parseComplianceTime accepts an empty string (unbounded) or an RFC3339 timestamp.
func parseComplianceTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, v)
}
