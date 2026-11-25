package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultZ is the z value of a two-sided 95% confidence interval.
const defaultZ = 1.96

// wilson returns the Wilson score interval for a proportion of successes out
// of n trials at the given z: (p̂ + z²/2n ± z·sqrt(p̂(1−p̂)/n + z²/4n²)) /
// (1 + z²/n) with p̂ = successes/n. successes may be fractional (draws count
// as half a win). ok is false when n is 0. The bounds are clamped to [0, 1].
func wilson(successes float64, n int, z float64) (lo, hi float64, ok bool) {
	if n <= 0 {
		return 0, 0, false
	}
	nf := float64(n)
	p := successes / nf
	z2 := z * z
	center := p + z2/(2*nf)
	margin := z * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf))
	denom := 1 + z2/nf
	lo = math.Max(0, (center-margin)/denom)
	hi = math.Min(1, (center+margin)/denom)
	// The bounds are exactly 0 for p̂ = 0 and 1 for p̂ = 1; avoid rounding
	// such as 0.9999999999999998.
	if successes <= 0 {
		lo = 0
	}
	if successes >= nf {
		hi = 1
	}
	return lo, hi, true
}

// confidenceLabel returns the two-sided confidence level of z as a percentage
// for the summary, rounded to one decimal: "95" for 1.96, "99" for 2.576.
func confidenceLabel(z float64) string {
	level := math.Round(math.Erf(z/math.Sqrt2)*1000) / 10
	return strconv.FormatFloat(level, 'f', -1, 64)
}

// PlayerSummary is one player's totals in a RunSummary. The fields match the
// run-log entry; Commit and Dirty are null without a commit directory (-pcN)
// or when git failed.
type PlayerSummary struct {
	Name   string  `json:"name"`
	Cmd    string  `json:"cmd"`
	Commit *string `json:"commit"`
	Dirty  *bool   `json:"dirty"`
	W      int     `json:"w"`
	L      int     `json:"l"`
	D      int     `json:"d"`
	// WinRate is (W + D/2) / (W + L + D) and CI its Wilson interval; both
	// null when the player has no win, loss or draw.
	WinRate *float64    `json:"win_rate"`
	CI      *[2]float64 `json:"ci"`
	// Faults is null when no fault pattern was given and old mode is off
	// (faults not measured). In old mode a crash counts as a fault.
	Faults *int `json:"faults"`
}

// RunSummary is the summary of a run, written by -summary-json and appended
// as one line to the -runlog file. Its fields are those of the run-log entry
// (spec section 7), plus z. Date is the time the run ended.
type RunSummary struct {
	Date           string          `json:"date"`
	Cgarena        string          `json:"cgarena"`
	Referee        string          `json:"referee"`
	SeedFrom       int             `json:"seed_from"`
	SeedTo         int             `json:"seed_to"`
	Swap           bool            `json:"swap"`
	OldMode        bool            `json:"old_mode,omitempty"`
	Games          int             `json:"games"`
	Threads        int             `json:"threads"`
	Z              float64         `json:"z"`
	Players        []PlayerSummary `json:"players"`
	DoubleForfeits int             `json:"double_forfeits"`
	Invalid        int             `json:"invalid"`
	InvalidReasons map[string]int  `json:"invalid_reasons"`
	Interrupted    bool            `json:"interrupted"`
	ResultsFile    *string         `json:"results_file"` // null without a results file

	// Used for the text, Markdown and HTML summaries only.
	Maps     int `json:"-"`
	Finished int `json:"-"`
}

// Summary returns the run's summary as of now. Call it after the run.
func (a *Arena) Summary(now time.Time) RunSummary {
	k := len(a.seatOrders())
	z := a.z()
	s := RunSummary{
		Date:           now.UTC().Format(time.RFC3339),
		Cgarena:        VERSION,
		Referee:        a.Referee,
		SeedFrom:       a.Seed,
		SeedTo:         gameSeed(a.Seed, a.Games-1, k),
		Swap:           a.Swap,
		OldMode:        a.OldMode,
		Games:          a.Games,
		Threads:        a.Threads,
		Players:        make([]PlayerSummary, len(a.Players)),
		DoubleForfeits: a.DoubleForfeits,
		Invalid:        a.Invalid,
		InvalidReasons: map[string]int{},
		Interrupted:    a.Interrupted,
		Z:              z,
		Maps:           a.Games / k,
		Finished:       a.Finished(),
	}
	for reason, count := range a.InvalidReasons {
		s.InvalidReasons[reason] = count
	}
	if path := a.resultsPath(); path != "" {
		s.ResultsFile = &path
	}
	for i, p := range a.Players {
		ps := PlayerSummary{Name: p.Name, Cmd: p.Cmd, Commit: p.Commit, Dirty: p.Dirty, W: p.Wins, L: p.Losses, D: p.Ties}
		n := p.Wins + p.Losses + p.Ties
		if lo, hi, ok := wilson(float64(p.Wins)+float64(p.Ties)/2, n, z); ok {
			rate := p.WinPct()
			ps.WinRate = &rate
			ps.CI = &[2]float64{lo, hi}
		}
		if a.FaultPattern != "" || a.OldMode {
			faults := p.Faults
			ps.Faults = &faults
		}
		s.Players[i] = ps
	}
	return s
}

// Header describes the run's seeds and games, for the top of the summary:
// "Summary: seeds 100..149 (50 maps, swap on, 100 games)". For an interrupted
// run it says how many games finished.
func (s RunSummary) Header() string {
	swap := "off"
	if s.Swap {
		swap = "on"
	}
	games := fmt.Sprintf("%d games", s.Games)
	if s.Interrupted {
		games = fmt.Sprintf("%d of %d games finished", s.Finished, s.Games)
	}
	return fmt.Sprintf("Summary: seeds %d..%d (%d maps, swap %s, %s)", s.SeedFrom, s.SeedTo, s.Maps, swap, games)
}

// CILabel is the column label of the confidence interval, e.g. "95% CI".
func (s RunSummary) CILabel() string {
	return confidenceLabel(s.Z) + "% CI"
}

// percent formats a proportion as a percentage with one decimal.
func percent(x float64) string {
	return strconv.FormatFloat(x*100, 'f', 1, 64) + "%"
}

// WinRateText is the player's win rate, or "n/a" without results.
func (p PlayerSummary) WinRateText() string {
	if p.WinRate == nil {
		return "n/a"
	}
	return percent(*p.WinRate)
}

// CIText is the player's confidence interval, or "n/a" without results.
func (p PlayerSummary) CIText() string {
	if p.CI == nil {
		return "n/a"
	}
	return "[" + percent(p.CI[0]) + ", " + percent(p.CI[1]) + "]"
}

// FaultsText is the player's fault count, or "n/a" without a fault pattern.
func (p PlayerSummary) FaultsText() string {
	if p.Faults == nil {
		return "n/a"
	}
	return strconv.Itoa(*p.Faults)
}

// InvalidText is the invalid count with its reasons, e.g.
// "2 (exit_status_2: 1, score_count: 1)".
func (s RunSummary) InvalidText() string {
	text := strconv.Itoa(s.Invalid)
	if s.Invalid == 0 {
		return text
	}
	reasons := make([]string, 0, len(s.InvalidReasons))
	for reason := range s.InvalidReasons {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	parts := make([]string, len(reasons))
	for i, reason := range reasons {
		parts[i] = reason + ": " + strconv.Itoa(s.InvalidReasons[reason])
	}
	return text + " (" + strings.Join(parts, ", ") + ")"
}

// Text is the summary printed at the end of a run:
//
//	Summary: seeds 100..149 (50 maps, swap on, 100 games)
//	A: 61 W / 33 L / 4 D   win rate 64.3%  95% CI [54.6%, 73.0%]   faults 0
//	B: 33 W / 61 L / 4 D   win rate 35.7%  95% CI [27.0%, 45.4%]   faults 2
//	Double forfeits: 1   Invalid: 1 (timeout: 1)
//
// The faults column is left out without a fault pattern.
func (s RunSummary) Text() string {
	var sb strings.Builder
	sb.WriteString(s.Header() + "\n")
	for _, p := range s.Players {
		fmt.Fprintf(&sb, "%s: %d W / %d L / %d D   win rate %s  %s %s", p.Name, p.W, p.L, p.D,
			p.WinRateText(), s.CILabel(), p.CIText())
		if p.Faults != nil {
			fmt.Fprintf(&sb, "   faults %d", *p.Faults)
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "Double forfeits: %d   Invalid: %s\n", s.DoubleForfeits, s.InvalidText())
	return sb.String()
}
