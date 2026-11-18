package cli

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
)

func TestMenuRenderingAndNavigation(t *testing.T) {
	m := dashboard{width: 35, height: 15, page: "home"}
	v := m.View()
	if !v.AltScreen || strings.Contains(v.Content, "\x1b[36") || !strings.Contains(v.Content, "❯ Server overview\n") {
		t.Fatal(v.Content)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(dashboard)
	if m.cursor != 1 {
		t.Fatal("arrow down")
	}
	m.color = true
	if !strings.Contains(m.View().Content, "\x1b[1;36m❯ Public endpoints") {
		t.Fatal("selection highlight")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if !next.(dashboard).zh {
		t.Fatal("language switch")
	}
}
