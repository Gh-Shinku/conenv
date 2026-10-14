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
	tree      *Tree
	cursor    int
	query     string
	filtering bool
	errorText string
	width     int
	height    int
}

func newSelector(items []Item) (selectorModel, error) {
	tree, err := NewTree(items)
	if err != nil {
		return selectorModel{}, err
	}
	return selectorModel{tree: tree, width: 80, height: 24}, nil
}

func (m selectorModel) selectedIDs() []string {
	return m.tree.SelectedIDs()
}

func (m selectorModel) filteredIndices() []int {
	return m.tree.FilteredIndices(m.query)
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
			m.tree.Toggle(m.tree.Item(indices[m.cursor]).ID)
		}
	case "a":
		m.tree.SelectAll()
	case "n":
		m.tree.Clear()
	case "enter":
		if m.tree.SelectedCount() == 0 {
			m.errorText = "Select at least one item before continuing."
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
		item := m.tree.Item(indices[visibleIndex])
		depth := m.tree.Depth(item.ID)
		if depth == 0 && item.Category != lastCategory {
			if body.Len() > 0 {
				body.WriteByte('\n')
			}
			body.WriteString(categoryStyle.Render(strings.ToUpper(item.Category)))
			body.WriteByte('\n')
			lastCategory = item.Category
		}
		cursor := strings.Repeat("  ", depth) + "  "
		if visibleIndex == m.cursor {
			cursor = strings.Repeat("  ", depth) + accentStyle.Render("› ")
		}
		mark := " "
		switch m.tree.State(item.ID) {
		case Selected:
			mark = accentStyle.Render("✓")
		case PartiallySelected:
			mark = partialStyle.Render("~")
		}
		label := item.Title
		if depth == 0 && len(m.tree.children[item.ID]) > 0 {
			label = valueStyle.Render(label)
		}
		if item.Description != "" {
			label += mutedStyle.Render("  " + item.Description)
		}
		body.WriteString(fmt.Sprintf("%s[%s] %s\n", cursor, mark, label))
	}
	if len(indices) == 0 {
		body.WriteString(mutedStyle.Render("No items match this filter."))
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

	header := titleStyle.Render("Select environment") + "\n" +
		fmt.Sprintf("Profile %s · %d of %d items selected%s", valueStyle.Render(profile), m.tree.SelectedCount(), m.tree.SelectableCount(), position)
	help := "↑/↓ move  space toggle  a all  n none  / filter  enter continue  q cancel"
	return header + "\n\n" + body.String() + filter + errorLine + "\n" + mutedStyle.Render(help)
}
