package rules

import "time"

// CellWay is a cell's winning way, code and capability (§4, annex 2 point 2).
func CellWay(c OverviewCell) (code, capID string) { return c.winCode, c.winCap }

// FindingWays are the ways a finding stands on, as code and capability.
func FindingWays(f Finding) map[[2]string]bool {
	out := map[[2]string]bool{}
	for _, leg := range f.legs {
		for _, w := range leg {
			out[[2]string{w.code, w.cap}] = true
		}
	}
	return out
}

// Stale is stale, for the 90-day test (§6).
func Stale(date string, now time.Time) bool { return stale(date, now) }
