package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type selectorEvent int

const (
	selectorNone selectorEvent = iota
	selectorSubmit
	selectorCancel
)

type selectorModel struct {
	components []Component
	selected   map[string]bool
	cursor     int
	query      string
	filtering  bool
	errorText  string
	width      int
	height     int
}

func newSelector(components []Component) selectorModel {
	selected := make(map[string]bool, len(components))
	for _, component := range components {
		selected[component.ID] = true
	}
	return selectorModel{
		components: append([]Component(nil), components...),
		selected:   selected,
		width:      80,
		height:     24,
	}
}

func (m selectorModel) selectedIDs() []string {
	ids := make([]string, 0, len(m.selected))
	for _, component := range m.components {
		if m.selected[component.ID] {
			ids = append(ids, component.ID)
		}
	}
	return ids
}

func (m selectorModel) filteredIndices() []int {
	query := strings.ToLower(strings.TrimSpace(m.query))
	indices := make([]int, 0, len(m.components))
	for index, component := range m.components {
		haystack := strings.ToLower(component.Title + " " + component.Description + " " + component.Category)
		if query == "" || strings.Contains(haystack, query) {
			indices = append(indices, index)
		}
	}
	return indices
}

func (m *selectorModel) clampCursor() {
	count := len(m.filteredIndices())
	if count == 0 {
		m.cursor = 0
	} else if m.cursor >= count {
		m.cursor = count - 1
	} else if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *selectorModel) handleKey(key string) selectorEvent {
	m.errorText = ""
	if m.filtering {
		switch key {
		case "esc":
			m.filtering = false
			m.query = ""
			m.cursor = 0
		case "enter":
			m.filtering = false
		case "backspace", "ctrl+h":
			if len(m.query) > 0 {
				_, size := utf8.DecodeLastRuneInString(m.query)
				m.query = m.query[:len(m.query)-size]
				m.cursor = 0
			}
		default:
			if utf8.RuneCountInString(key) == 1 {
				m.query += key
				m.cursor = 0
			}
		}
		m.clampCursor()
		return selectorNone
	}

	indices := m.filteredIndices()
	switch key {
	case "q", "ctrl+c":
		return selectorCancel
	case "/":
		m.filtering = true
	case "esc":
		if m.query != "" {
			m.query = ""
			m.cursor = 0
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor+1 < len(indices) {
			m.cursor++
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		if len(indices) > 0 {
			m.cursor = len(indices) - 1
		}
	case "space":
		if len(indices) > 0 {
			id := m.components[indices[m.cursor]].ID
			m.selected[id] = !m.selected[id]
			if !m.selected[id] {
				delete(m.selected, id)
			}
		}
	case "a":
		for _, component := range m.components {
			m.selected[component.ID] = true
		}
	case "n":
		clear(m.selected)
	case "enter":
		if len(m.selected) == 0 {
			m.errorText = "Select at least one component before continuing."
			return selectorNone
		}
		return selectorSubmit
	}
	return selectorNone
}

func (m selectorModel) view(profile string) string {
	indices := m.filteredIndices()
	visibleRows := max(3, m.height-10)
	start := 0
	if m.cursor >= visibleRows {
		start = m.cursor - visibleRows + 1
	}
	end := min(len(indices), start+visibleRows)

	var body strings.Builder
	lastCategory := ""
	for visibleIndex := start; visibleIndex < end; visibleIndex++ {
		component := m.components[indices[visibleIndex]]
		if component.Category != lastCategory {
			if body.Len() > 0 {
				body.WriteByte('\n')
			}
			body.WriteString(categoryStyle.Render(strings.ToUpper(component.Category)))
			body.WriteByte('\n')
			lastCategory = component.Category
		}
		cursor := "  "
		if visibleIndex == m.cursor {
			cursor = accentStyle.Render("› ")
		}
		mark := " "
		if m.selected[component.ID] {
			mark = accentStyle.Render("✓")
		}
		label := component.Title
		if component.Description != "" {
			label += mutedStyle.Render("  " + component.Description)
		}
		body.WriteString(fmt.Sprintf("%s[%s] %s\n", cursor, mark, label))
	}
	if len(indices) == 0 {
		body.WriteString(mutedStyle.Render("No components match this filter."))
		body.WriteByte('\n')
	}

	filter := ""
	if m.filtering {
		filter = "\n" + accentStyle.Render("Filter: /"+m.query+"▌")
	} else if m.query != "" {
		filter = "\n" + mutedStyle.Render("Filter: /"+m.query)
	}
	errorLine := ""
	if m.errorText != "" {
		errorLine = "\n" + errorStyle.Render(m.errorText)
	}
	position := ""
	if len(indices) > visibleRows {
		position = fmt.Sprintf("  %d–%d of %d", start+1, end, len(indices))
	}

	header := titleStyle.Render("Select components") + "\n" +
		fmt.Sprintf("Profile %s · %d of %d selected%s", valueStyle.Render(profile), len(m.selected), len(m.components), position)
	help := "↑/↓ move  space toggle  a all  n none  / filter  enter continue  q cancel"
	return header + "\n\n" + body.String() + filter + errorLine + "\n" + mutedStyle.Render(help)
}
