package conenv

import "runtime"

func InspectStatus(resolved *Resolved) StatusReport {
	report := StatusReport{
		Repository: resolved.Root,
		Profile:    resolved.Profile,
		Scope:      componentNames(resolved.Components),
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		Skipped:    append([]string(nil), resolved.Skipped...),
		InSync:     true,
		Components: make([]ComponentStatus, 0, len(resolved.Components)),
	}
	for _, component := range resolved.Components {
		scoped := *resolved
		scoped.Components = []NamedComponent{component}
		status := ComponentStatus{Name: component.Name, State: "in_sync"}
		if err := ValidateScope(&scoped); err != nil {
			status.State = "error"
			status.Error = err.Error()
			report.InSync = false
		} else {
			plan, err := BuildPlan(&scoped)
			if err != nil {
				status.State = "error"
				status.Error = err.Error()
				report.InSync = false
			} else if len(plan.Actions) > 0 {
				status.State = "drifted"
				status.Pending = len(plan.Actions)
				report.InSync = false
			}
		}
		report.Components = append(report.Components, status)
	}
	return report
}
