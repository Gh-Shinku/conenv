package conenv

import (
	"fmt"
	"os"
	"os/exec"
)

func Doctor(r *Resolved) []CheckResult {
	results := []CheckResult{}
	plan, err := BuildPlan(r)
	if err != nil {
		results = append(results, CheckResult{Name: "desired state", Healthy: false, Detail: err.Error()})
	} else if len(plan.Actions) == 0 {
		results = append(results, CheckResult{Name: "desired state", Healthy: true, Detail: "environment is in sync"})
	} else {
		results = append(results, CheckResult{Name: "desired state", Healthy: false, Detail: fmt.Sprintf("%d pending action(s)", len(plan.Actions))})
	}
	for _, component := range r.Components {
		for _, check := range component.Checks {
			result := CheckResult{Name: check.Name}
			switch check.Type {
			case "path":
				path, expandErr := expandPath(check.Path, r)
				if expandErr != nil {
					result.Detail = expandErr.Error()
				} else if _, statErr := os.Stat(path); statErr != nil {
					result.Detail = statErr.Error()
				} else {
					result.Healthy = true
					result.Detail = path
				}
			case "command":
				command, expandErr := expandString(check.Command, r.Variables, nil)
				if expandErr != nil {
					result.Detail = expandErr.Error()
					break
				}
				args := make([]string, len(check.Args))
				for i, arg := range check.Args {
					args[i], expandErr = expandString(arg, r.Variables, nil)
					if expandErr != nil {
						result.Detail = expandErr.Error()
						break
					}
				}
				if expandErr != nil {
					break
				}
				cmd := exec.Command(command, args...)
				if output, commandErr := cmd.CombinedOutput(); commandErr != nil {
					result.Detail = fmt.Sprintf("%v: %s", commandErr, output)
				} else {
					result.Healthy = true
					result.Detail = "command succeeded"
				}
			default:
				result.Detail = fmt.Sprintf("unsupported check type %q", check.Type)
			}
			results = append(results, result)
		}
	}
	return results
}
