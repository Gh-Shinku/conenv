package tui

import (
	"fmt"
	"strings"
)

type SelectionState int

const (
	Unselected SelectionState = iota
	PartiallySelected
	Selected
)

// Tree owns hierarchy and selection semantics independently from rendering.
// Only leaf IDs are returned as selections; parent state is derived from its
// descendant leaves.
type Tree struct {
	items    []Item
	byID     map[string]int
	children map[string][]string
	selected map[string]bool
}

func NewTree(items []Item) (*Tree, error) {
	tree := &Tree{
		items:    append([]Item(nil), items...),
		byID:     make(map[string]int, len(items)),
		children: make(map[string][]string),
		selected: make(map[string]bool),
	}
	for index, item := range tree.items {
		if item.ID == "" {
			return nil, fmt.Errorf("selection item %d has an empty ID", index)
		}
		if _, exists := tree.byID[item.ID]; exists {
			return nil, fmt.Errorf("selection item ID %q is repeated", item.ID)
		}
		if item.ParentID != "" {
			if _, exists := tree.byID[item.ParentID]; !exists {
				return nil, fmt.Errorf("selection item %q references a parent that does not precede it", item.ID)
			}
			tree.children[item.ParentID] = append(tree.children[item.ParentID], item.ID)
		}
		tree.byID[item.ID] = index
	}
	tree.SelectAll()
	return tree, nil
}

func (t *Tree) Items() []Item {
	return append([]Item(nil), t.items...)
}

func (t *Tree) Item(index int) Item {
	return t.items[index]
}

func (t *Tree) SelectAll() {
	clear(t.selected)
	for _, item := range t.items {
		if t.isLeaf(item.ID) {
			t.selected[item.ID] = true
		}
	}
}

func (t *Tree) Clear() {
	clear(t.selected)
}

func (t *Tree) Toggle(id string) {
	leaves := t.leafIDs(id)
	allSelected := len(leaves) > 0
	for _, leaf := range leaves {
		allSelected = allSelected && t.selected[leaf]
	}
	for _, leaf := range leaves {
		if allSelected {
			delete(t.selected, leaf)
		} else {
			t.selected[leaf] = true
		}
	}
}

func (t *Tree) State(id string) SelectionState {
	leaves := t.leafIDs(id)
	selected := 0
	for _, leaf := range leaves {
		if t.selected[leaf] {
			selected++
		}
	}
	if selected == 0 {
		return Unselected
	}
	if selected == len(leaves) {
		return Selected
	}
	return PartiallySelected
}

func (t *Tree) SelectedIDs() []string {
	ids := make([]string, 0, len(t.selected))
	for _, item := range t.items {
		if t.isLeaf(item.ID) && t.selected[item.ID] {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func (t *Tree) SelectedCount() int {
	return len(t.selected)
}

func (t *Tree) SelectableCount() int {
	count := 0
	for _, item := range t.items {
		if t.isLeaf(item.ID) {
			count++
		}
	}
	return count
}

func (t *Tree) Depth(id string) int {
	depth := 0
	item, ok := t.itemByID(id)
	for ok && item.ParentID != "" {
		depth++
		item, ok = t.itemByID(item.ParentID)
	}
	return depth
}

func (t *Tree) FilteredIndices(query string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		indices := make([]int, len(t.items))
		for index := range t.items {
			indices[index] = index
		}
		return indices
	}
	directMatches := make(map[string]bool)
	for _, item := range t.items {
		haystack := strings.ToLower(item.Title + " " + item.Description + " " + item.Category)
		directMatches[item.ID] = strings.Contains(haystack, query)
	}
	indices := make([]int, 0, len(t.items))
	for index, item := range t.items {
		if directMatches[item.ID] || t.hasMatchingAncestor(item.ID, directMatches) || t.hasMatchingDescendant(item.ID, directMatches) {
			indices = append(indices, index)
		}
	}
	return indices
}

func (t *Tree) isLeaf(id string) bool {
	return len(t.children[id]) == 0
}

func (t *Tree) leafIDs(id string) []string {
	if _, exists := t.byID[id]; !exists {
		return nil
	}
	if t.isLeaf(id) {
		return []string{id}
	}
	var leaves []string
	for _, child := range t.children[id] {
		leaves = append(leaves, t.leafIDs(child)...)
	}
	return leaves
}

func (t *Tree) itemByID(id string) (Item, bool) {
	index, ok := t.byID[id]
	if !ok {
		return Item{}, false
	}
	return t.items[index], true
}

func (t *Tree) hasMatchingAncestor(id string, matches map[string]bool) bool {
	item, ok := t.itemByID(id)
	for ok && item.ParentID != "" {
		if matches[item.ParentID] {
			return true
		}
		item, ok = t.itemByID(item.ParentID)
	}
	return false
}

func (t *Tree) hasMatchingDescendant(id string, matches map[string]bool) bool {
	for _, child := range t.children[id] {
		if matches[child] || t.hasMatchingDescendant(child, matches) {
			return true
		}
	}
	return false
}
