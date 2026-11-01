package audio

import (
	"math"
	"time"
)

// CableProbe is the result of Engine.ProbeCable.
type CableProbe int

const (
	// ProbeUnknown: the test couldn't run or can't tell (no cable output,
	// microphone access blocked, something else plays into the cable).
	ProbeUnknown CableProbe = iota
	// ProbePassed: the tone came out at the cable's far end.
	ProbePassed
	// ProbeSilent: the tone went in and nothing came out.
	ProbeSilent
)

const (
	probeLevel    = 0.03  // about -30 dB: nobody listens yet, and a meter sees it
	probeFloor    = 0.005 // about -46 dB
	probeDuration = 500 * time.Millisecond
	probeSettle   = 150 * time.Millisecond
	probeTimeout  = time.Second
)

// probeTone is a quiet stereo tone, faded at both ends so it doesn't click.
func probeTone(rate int) []float32 {
	const hz, fade = 440.0, 0.02
	frames := int(probeDuration.Seconds() * float64(rate))
	pcm := make([]float32, frames*2)
	for i := range frames {
		t := float64(i) / float64(rate)
		env := min(1, t/fade, (probeDuration.Seconds()-t)/fade)
		v := float32(probeLevel * env * math.Sin(2*math.Pi*hz*t))
		pcm[2*i], pcm[2*i+1] = v, v
	}
	return pcm
}

// ProbeCable checks that the cable really carries audio: a short, quiet tone
// plays on the cable output only (the user doesn't hear it) while captureID,
// the cable's far end, is recorded. Devices that merely show up can still
// need the restart, so this is what decides whether it can be skipped. Call
// it off the UI thread, while nothing else plays.
func (e *Engine) ProbeCable(captureID string) (CableProbe, error) {
	meter, err := OpenInputLevel(captureID)
	if err != nil {
		return ProbeUnknown, err
	}
	defer meter.Close()
	time.Sleep(probeSettle)
	if peak, err := meter.Take(); err != nil || peak >= probeFloor {
		return ProbeUnknown, err // audio already arrives there: the tone proves nothing
	}

	roles := []string{RoleCable}
	p, err := e.prepare(roles, func(rate, n int) ([]*Source, error) {
		out := make([]*Source, n)
		for i := range out {
			out[i] = NewStaticSource("cable test", rate, probeTone(rate))
		}
		return out, nil
	})
	if err != nil {
		return ProbeUnknown, err
	}
	info, err := e.Start(p, PlayOptions{Roles: roles, Volume: map[string]float32{RoleCable: 1}})
	if err != nil {
		return ProbeUnknown, err
	}
	defer e.Stop(info.VoiceID)

	for deadline := time.Now().Add(probeTimeout); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		peak, err := meter.Take()
		if err != nil {
			return ProbeUnknown, err
		}
		if peak >= probeFloor {
			return ProbePassed, nil
		}
	}
	return ProbeSilent, nil
}
