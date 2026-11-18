package swupdate

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

var (
	percentRe = regexp.MustCompile(`(\d{1,3})\s*%`)
	stepRe    = regexp.MustCompile(`(?i)(?:step\s+)?\[?\s*(\d+)\s*(?:/|of)\s*(\d+)\s*\]?`)
	imageRe   = regexp.MustCompile(`(?i)(?:\binstalling|\bimage:?)\s+([^\s\[\]]+)`)
)

// progressOutcome is the terminal state reported by swupdate-progress.
type progressOutcome int

const (
	outcomeNone progressOutcome = iota
	outcomeSuccess
	outcomeFailure
)

// scanProgressLines splits on both \n and \r because swupdate-progress
// redraws its progress bar using carriage returns.
func scanProgressLines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// newProgressScanner returns a scanner yielding one output line at a time.
func newProgressScanner(s *bufio.Scanner) *bufio.Scanner {
	s.Buffer(make([]byte, 0, 4096), 64*1024)
	s.Split(scanProgressLines)
	return s
}

// parseProgressLine updates p from one line of swupdate-progress output and
// returns the terminal outcome if the line reports one. Unknown lines are ignored.
func parseProgressLine(line string, p *Progress) (changed bool, outcome progressOutcome) {
	line = strings.TrimSpace(line)
	if line == "" {
		return false, outcomeNone
	}
	lower := strings.ToLower(line)

	switch {
	case strings.Contains(lower, "failure") || strings.Contains(lower, "failed"):
		p.Status = StatusFailure
		p.Message = line
		return true, outcomeFailure
	case strings.HasPrefix(lower, "swupdate successful") || strings.Contains(lower, "success"):
		p.Status = StatusSuccess
		p.StepPercent = 100
		p.Message = line
		return true, outcomeSuccess
	}

	if m := percentRe.FindStringSubmatch(line); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil && v <= 100 {
			p.StepPercent = v
			changed = true
		}
	}
	if m := stepRe.FindStringSubmatch(line); m != nil {
		cur, _ := strconv.Atoi(m[1])
		total, _ := strconv.Atoi(m[2])
		if total > 0 && cur <= total {
			p.CurrentStep = cur
			p.TotalSteps = total
			changed = true
		}
	}
	if m := imageRe.FindStringSubmatch(line); m != nil {
		p.CurrentImage = m[1]
		changed = true
	}
	if changed {
		p.Status = StatusRun
		p.Message = line
	}
	return changed, outcomeNone
}
