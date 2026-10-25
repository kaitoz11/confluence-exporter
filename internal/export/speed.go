package export

import (
	"runtime"
	"time"
)

// Speed trades export time for load on the computer and on Confluence.
type Speed int

const (
	SpeedGentle Speed = iota
	SpeedNormal
	SpeedFast
)

// Speeds lists the choices in the order shown to the user.
var Speeds = []Speed{SpeedGentle, SpeedNormal, SpeedFast}

func (s Speed) String() string {
	switch s {
	case SpeedGentle:
		return "Gentle"
	case SpeedFast:
		return "Fast"
	}
	return "Normal"
}

// Description explains the choice in plain words.
func (s Speed) Description() string {
	switch s {
	case SpeedGentle:
		return "slowest, lightest on the computer and on Confluence"
	case SpeedFast:
		return "quickest, uses several processor cores"
	}
	return "balanced"
}

// ParseSpeed reads a saved setting, defaulting to Normal.
func ParseSpeed(s string) Speed {
	for _, v := range Speeds {
		if v.String() == s {
			return v
		}
	}
	return SpeedNormal
}

type throttle struct {
	downloads   int           // pages fetched at the same time
	requestRate float64       // Confluence requests per second (0 = unlimited)
	pdfWorkers  int           // PDFs created at the same time
	pdfPause    time.Duration // rest between two PDFs, per worker
}

func (s Speed) throttle() throttle {
	switch s {
	case SpeedGentle:
		return throttle{downloads: 2, requestRate: 4, pdfWorkers: 1, pdfPause: 250 * time.Millisecond}
	case SpeedFast:
		return throttle{downloads: 6, requestRate: 0, pdfWorkers: min(max(runtime.NumCPU()/2, 1), 4)}
	}
	return throttle{downloads: 4, requestRate: 10, pdfWorkers: 1, pdfPause: 50 * time.Millisecond}
}
