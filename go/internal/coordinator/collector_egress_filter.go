package coordinator

import (
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) filterCollectorInstancesByEgress(instances []workflow.CollectorInstance) []workflow.CollectorInstance {
	if len(instances) == 0 {
		return nil
	}
	filtered := make([]workflow.CollectorInstance, 0, len(instances))
	for _, instance := range instances {
		decision := s.Config.CollectorEgressPolicy.Decide(instance.CollectorKind)
		if decision.Action == CollectorEgressActionDeny && instance.Enabled && instance.ClaimsEnabled {
			if s.Logger != nil {
				s.Logger.Info(
					"workflow coordinator skipped collector scheduling by egress policy",
					"collector_kind", instance.CollectorKind,
					"reason", decision.Reason,
				)
			}
			continue
		}
		filtered = append(filtered, instance)
	}
	return filtered
}
