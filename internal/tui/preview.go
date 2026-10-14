package tui

import (
	"fmt"
	"strings"
)

type previewEvent int

const (
	previewNone previewEvent = iota
	previewBack
	previewConfirm
	previewCancel
)

type previewModel struct {
	plan         Plan
	selected     int
	scroll       int
	width        int
	height       int
	confirmLabel string
}

func newPreview(plan Plan, selected int, confirmLabel string) previewModel {
	if confirmLabel == "" {
		confirmLabel = "apply"
	}
	return previewModel{plan: plan, selected: selected, width: 80, height: 24, confirmLabel: confirmLabel}
}

func (m *previewModel) handleKey(key string) previewEvent {
	pageSize := max(1, m.height-9)
	maxScroll := max(0, len(m.plan.Actions)-pageSize)
	switch key {
	case "q", "ctrl+c":
		return previewCancel
	case "esc", "left", "b", "n":
		return previewBack
	case "enter", "y":
		return previewConfirm
	case "up", "k":
		m.scroll = max(0, m.scroll-1)
	case "down", "j":
		m.scroll = min(maxScroll, m.scroll+1)
	case "pgup", "ctrl+u":
		m.scroll = max(0, m.scroll-pageSize)
	case "pgdown", "ctrl+d":
		m.scroll = min(maxScroll, m.scroll+pageSize)
	case "home", "g":
		m.scroll = 0
	case "end", "G":
		m.scroll = maxScroll
	}
	return previewNone
}

func (m previewModel) view() string {
	pageSize := max(1, m.height-9)
	end := min(len(m.plan.Actions), m.scroll+pageSize)
	var body strings.Builder
	if len(m.plan.Actions) == 0 {
		body.WriteString(successStyle.Render("✓ This profile is already in sync."))
		body.WriteByte('\n')
	} else {
		for _, action := range m.plan.Actions[m.scroll:end] {
			kind := kindStyle.Render("[" + action.Kind + "]")
			component := ""
			if action.Component != "" {
				component = mutedStyle.Render("  " + action.Component)
			}
			body.WriteString(fmt.Sprintf("%s %s%s\n", kind, action.Summary, component))
		}
	}

	position := ""
	if len(m.plan.Actions) > pageSize {
		position = fmt.Sprintf(" · showing %d–%d", m.scroll+1, end)
	}
	header := titleStyle.Render("Review plan") + "\n" +
		fmt.Sprintf("Profile %s · %d selected items · %d actions%s", valueStyle.Render(m.plan.Profile), m.selected, len(m.plan.Actions), position)
	help := fmt.Sprintf("↑/↓ scroll  esc back  enter %s  q cancel", m.confirmLabel)
	return header + "\n\n" + body.String() + "\n" + mutedStyle.Render(help)
}
