package status

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// QueueBlockage captures eligible work that could not be claimed because a
// durable coordination gate is protecting the same conflict domain.
type QueueBlockage struct {
	Stage          string
	Domain         string
	ConflictDomain string
	ConflictKey    string
	Blocked        int
	OldestAge      time.Duration
}

// cloneQueueBlockages normalizes queue-blockage rows into the same priority
// order used by the operator report: biggest and oldest blockers first.
func cloneQueueBlockages(rows []QueueBlockage) []QueueBlockage {
	cloned := make([]QueueBlockage, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Stage) == "" {
			continue
		}
		cloned = append(cloned, row)
	}
	sort.Slice(cloned, func(i, j int) bool {
		if cloned[i].Blocked != cloned[j].Blocked {
			return cloned[i].Blocked > cloned[j].Blocked
		}
		if cloned[i].OldestAge != cloned[j].OldestAge {
			return cloned[i].OldestAge > cloned[j].OldestAge
		}
		if cloned[i].Stage != cloned[j].Stage {
			return cloned[i].Stage < cloned[j].Stage
		}
		if cloned[i].Domain != cloned[j].Domain {
			return cloned[i].Domain < cloned[j].Domain
		}
		return cloned[i].ConflictKey < cloned[j].ConflictKey
	})
	return cloned
}

// renderQueueBlockageLines formats conflict-blocked queue diagnostics for the
// text status report without promoting high-cardinality keys into metrics.
func renderQueueBlockageLines(rows []QueueBlockage) []string {
	if len(rows) == 0 {
		return nil
	}

	lines := []string{"Blocked queue work:"}
	for _, row := range rows {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s domain=%s conflict_domain=%s conflict_key=%s blocked=%d oldest=%s",
				row.Stage,
				row.Domain,
				row.ConflictDomain,
				row.ConflictKey,
				row.Blocked,
				row.OldestAge,
			),
		)
	}
	return lines
}
