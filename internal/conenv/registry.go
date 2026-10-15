package conenv

import (
	"fmt"
	"sort"
)

var adapters = map[string]AgentAdapter{}

func RegisterAdapter(adapter AgentAdapter) {
	adapters[adapter.Harness()] = adapter
}

// LookupAdapter resolves a declared harness name. An unknown harness is an
// error rather than a fallback, so a typo fails validation instead of silently
// writing an unmanaged file.
func LookupAdapter(harness string) (AgentAdapter, error) {
	if adapter, ok := adapters[harness]; ok {
		return adapter, nil
	}
	known := make([]string, 0, len(adapters))
	for name := range adapters {
		known = append(known, name)
	}
	sort.Strings(known)
	return nil, fmt.Errorf("unknown harness %q; supported harnesses: %v", harness, known)
}

// Adapters lists the registered adapters in a stable order.
func Adapters() []AgentAdapter {
	names := make([]string, 0, len(adapters))
	for name := range adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	list := make([]AgentAdapter, 0, len(names))
	for _, name := range names {
		list = append(list, adapters[name])
	}
	return list
}

func init() {
	RegisterAdapter(codexAdapter{})
	RegisterAdapter(piAdapter{})
}
