package ui

import (
	"fmt"
	"log"
	"os"
	"runtime/metrics"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/internal/pacing"
)

// Frame statistics: with MYGO_FRAME_STATS set, the engine logs each frame
// slower than a threshold, with how long each part took, which path drew
// it, what the process allocated meanwhile and whether the garbage
// collector ran. Unset, frames measure nothing. With MYGO_UNCAPPED set
// too, frames wait for no refresh, so a frame's present is its own work,
// and the engine logs once a second how many frames came: the rate a scene
// draws at, which the display's no longer hides.

// frameStatsThreshold is how long a frame takes before it is logged, from
// MYGO_FRAME_STATS: 1 (or true, on) logs those over 8 ms, another number
// those over that many milliseconds, and all every frame. frameStatsOn
// tells whether it is set.
var frameStatsThreshold, frameStatsOn = frameStatsSetting(os.Getenv("MYGO_FRAME_STATS"))

// frameLog writes the log of a frame; tests replace it.
var frameLog = log.Print

func frameStatsSetting(v string) (threshold time.Duration, on bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "off", "no":
		return 0, false
	case "1", "true", "on", "yes":
		return 8 * time.Millisecond, true
	case "all":
		return 0, true
	}
	ms, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || ms < 0 {
		log.Printf("mygo: MYGO_FRAME_STATS=%q is not a number of milliseconds: logging frames over 8 ms", v)
		return 8 * time.Millisecond, true
	}
	return time.Duration(ms * float64(time.Millisecond)), true
}

// The parts of a frame.
const (
	phaseBuild = iota
	phaseLayout
	phasePaint
	phasePresent
	phases
)

// The runtime's counters a frame reads before and after.
const (
	statObjects = iota // allocations, but tiny ones
	statTiny           // tiny allocations, combined in blocks
	statBytes
	statCycles // GC cycles ended
	statAssist // CPU seconds goroutines helped the GC mark
	stats
)

// frameStats measures a frame for MYGO_FRAME_STATS.
type frameStats struct {
	threshold   time.Duration
	start, last time.Time
	laps        [phases]time.Duration
	passes      int
	samples     [stats]metrics.Sample
	before      [stats]float64
	layouts     uint64 // the text system's, before the frame
	// rateFrom is when the frames counted for the rate began (rate),
	// rateFrames how many ended since, the last at rateLast.
	rateFrom, rateLast time.Time
	rateFrames         int
}

// rateGap is a pause between frames that starts the count of the frame
// rate anew: the time a window idled tells nothing of how fast it draws.
const rateGap = 250 * time.Millisecond

func newFrameStats(threshold time.Duration) *frameStats {
	f := &frameStats{threshold: threshold}
	for i, name := range [stats]string{
		"/gc/heap/allocs:objects",
		"/gc/heap/tiny/allocs:objects",
		"/gc/heap/allocs:bytes",
		"/gc/cycles/total:gc-cycles",
		"/cpu/classes/gc/mark/assist:cpu-seconds",
	} {
		f.samples[i].Name = name
	}
	return f
}

// read returns the runtime's counters.
func (f *frameStats) read() (v [stats]float64) {
	metrics.Read(f.samples[:])
	for i, s := range f.samples {
		switch s.Value.Kind() {
		case metrics.KindUint64:
			v[i] = float64(s.Value.Uint64())
		case metrics.KindFloat64:
			v[i] = s.Value.Float64()
		}
	}
	return v
}

// begin starts measuring a frame. The methods of a nil frameStats do
// nothing, as frames go while MYGO_FRAME_STATS is unset.
func (f *frameStats) begin(rt *engine) {
	if f == nil {
		return
	}
	f.before, f.layouts = f.read(), rt.text.LayoutsMade()
	f.laps, f.passes = [phases]time.Duration{}, 0
	f.start = time.Now()
	f.last = f.start
}

// lap ends a part of the frame.
func (f *frameStats) lap(phase int) {
	if f == nil {
		return
	}
	now := time.Now()
	f.laps[phase] += now.Sub(f.last)
	f.last = now
}

// end logs the frame if it took longer than the threshold.
func (f *frameStats) end(rt *engine) {
	if f == nil {
		return
	}
	end := time.Now()
	total := end.Sub(f.start)
	if pacing.Uncapped {
		f.rate(f.start, end)
	}
	if total < f.threshold {
		return
	}
	after := f.read()
	var d [stats]float64
	for i := range d {
		d[i] = after[i] - f.before[i]
	}
	ms := func(t time.Duration) string { return strconv.FormatFloat(t.Seconds()*1000, 'f', 1, 64) }
	other := total
	for _, t := range f.laps {
		other -= t
	}
	path := "not shown"
	if p, ok := rt.host.(interface{ framePath() string }); ok {
		if fp := p.framePath(); fp != "" {
			path = fp
		}
	}
	if pacing.Uncapped {
		// Its present waited for no refresh.
		path += ", uncapped"
	}
	passes := ""
	switch {
	case f.passes == 0:
		passes = " (painted again)" // engine.repaintFrame
	case f.passes > 1:
		passes = fmt.Sprintf(" (%d passes)", f.passes)
	}
	gc := "no GC"
	if d[statCycles] > 0 || d[statAssist] > 0 {
		gc = fmt.Sprintf("GC: %.0f cycles ended, %s ms of assists", d[statCycles], ms(time.Duration(d[statAssist]*float64(time.Second))))
	}
	frameLog(fmt.Sprintf("mygo: frame %d took %s ms: build %s%s, layout %s, paint %s, present %s (%s), other %s; meanwhile the process made about %.0f allocations, %.1f KB, %d text layouts; %s",
		rt.frame, ms(total), ms(f.laps[phaseBuild]), passes, ms(f.laps[phaseLayout]), ms(f.laps[phasePaint]), ms(f.laps[phasePresent]), path, ms(other),
		d[statObjects]+d[statTiny], d[statBytes]/1024, rt.text.LayoutsMade()-f.layouts, gc))
}

// rate counts a frame that ran from start to end while frames are uncapped
// (MYGO_UNCAPPED), and once they followed each other for a second, logs how
// many came a second and counts anew: the frame rate, which the display's
// refreshes no longer cap, tells how much of a refresh a scene leaves. A
// pause longer than rateGap starts the count anew, and so does a frame
// that took longer, as one that waited on the system, which the frame's
// own line tells: counted, it would have the second it ends in log a few
// frames a second, as if the frames after it still came that slowly.
func (f *frameStats) rate(start, end time.Time) {
	if end.Sub(start) > rateGap {
		f.rateFrom, f.rateFrames, f.rateLast = end, 0, end
		return
	}
	if f.rateFrom.IsZero() || start.Sub(f.rateLast) > rateGap {
		f.rateFrom, f.rateFrames = start, 0
	}
	f.rateFrames++
	f.rateLast = end
	if d := end.Sub(f.rateFrom); d >= time.Second {
		frameLog(fmt.Sprintf("mygo: %.0f frames a second, uncapped: %.2f ms a frame", float64(f.rateFrames)/d.Seconds(), d.Seconds()*1000/float64(f.rateFrames)))
		f.rateFrom, f.rateFrames = end, 0
	}
}
