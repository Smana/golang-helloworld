// Package loadgen drives the image-gallery API with open-loop scenarios, from
// a laptop or in-cluster, instrumented so that traces start at the client.
package loadgen

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// MaxRate is the default cap (req/s); --force lifts it. It is also the soak
// target of success criterion 5.
const MaxRate = 25

// Options configures one run.
type Options struct {
	Target      string
	Scenario    string
	Rate        float64
	Duration    time.Duration
	Concurrency int
	Force       bool
	Seed        int64
	PhaseScale  float64 // incident phase-length multiplier (tests shrink it)
}

func parseOptions(args []string, stderr io.Writer) (Options, error) {
	fs := flag.NewFlagSet("loadgen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o Options
	fs.StringVar(&o.Target, "target", "", "base URL of the app (required), e.g. https://image-gallery.priv.gcp.ogenki.io")
	fs.StringVar(&o.Scenario, "scenario", "mixed", "browse | upload | mixed | steady | incident")
	fs.Float64Var(&o.Rate, "rate", 0, "arrival rate in req/s (default 10; 1 for steady)")
	fs.DurationVar(&o.Duration, "duration", 5*time.Minute, "run length (incident follows its own ~10 min timeline)")
	fs.IntVar(&o.Concurrency, "concurrency", 20, "cap on requests in flight; arrivals beyond it are shed, not delayed")
	fs.BoolVar(&o.Force, "force", false, fmt.Sprintf("allow --rate above %d req/s", MaxRate))
	fs.Int64Var(&o.Seed, "seed", 0, "random seed (0 = time-based)")
	fs.Float64Var(&o.PhaseScale, "phase-scale", 1, "incident phase-length multiplier")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	err := o.validate()
	return o, err
}

// validate checks Target and Scenario, fills in defaults for the rest (Rate,
// Duration, Concurrency, PhaseScale, Seed) and reports any hard failure. Split
// into per-concern helpers to keep each one easy to follow.
func (o *Options) validate() error {
	if err := o.validateTarget(); err != nil {
		return err
	}
	if _, ok := scenarios[o.Scenario]; !ok && o.Scenario != scenarioIncident {
		return fmt.Errorf("unknown scenario %q (browse, upload, mixed, steady, incident)", o.Scenario)
	}
	o.applyRateDefault()
	if err := o.validateRate(); err != nil {
		return err
	}
	if o.Concurrency < 1 {
		return errors.New("--concurrency must be at least 1")
	}
	if o.Duration <= 0 {
		o.Duration = 5 * time.Minute
	}
	if o.PhaseScale <= 0 {
		o.PhaseScale = 1
	}
	if o.Seed == 0 {
		o.Seed = time.Now().UnixNano()
	}
	return nil
}

func (o *Options) validateTarget() error {
	if o.Target == "" {
		return errors.New("--target is required")
	}
	u, err := url.Parse(o.Target)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("--target %q is not an absolute URL", o.Target)
	}
	o.Target = strings.TrimRight(o.Target, "/")
	return nil
}

// applyRateDefault fills in the default rate (10, or 1 for steady) when --rate was not passed.
func (o *Options) applyRateDefault() {
	if o.Rate != 0 {
		return
	}
	o.Rate = 10
	if o.Scenario == scenarioSteady {
		o.Rate = 1
	}
}

func (o *Options) validateRate() error {
	if o.Rate < 0 {
		return errors.New("--rate must be positive")
	}
	if o.Rate > MaxRate && !o.Force {
		return fmt.Errorf("--rate %v exceeds the %d req/s cap; pass --force to exceed it", o.Rate, MaxRate)
	}
	return nil
}
