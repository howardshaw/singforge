package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMenuRenderingAndNavigation(t *testing.T) {
	m := dashboard{width: 35, height: 15, page: "home"}
	v := m.View()
	if !v.AltScreen || strings.Contains(v.Content, "\x1b[36") || !strings.Contains(v.Content, "❯  1.  Server overview\n") {
		t.Fatal(v.Content)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(dashboard)
	if m.cursor != 1 {
		t.Fatal("arrow down")
	}
	m.color = true
	if !strings.Contains(m.View().Content, "\x1b[1;36m❯  2.  Public endpoints") {
		t.Fatal("selection highlight")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if !next.(dashboard).zh {
		t.Fatal("language switch")
	}
}

func TestDashboardNumbersAndHostStatus(t *testing.T) {
	m := dashboard{page: "home", width: 80, height: 28, panel: hostPanel{System: "Debian 12 · amd64", Services: []string{"sing-box       active"}}}
	for _, key := range []rune{'1', '0'} {
		next, _ := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		m = next.(dashboard)
	}
	if m.cursor != 9 || !strings.Contains(m.View().Content, "❯ 10.  All commands") || !strings.Contains(m.View().Content, "sing-box       active") {
		t.Fatal(m.View().Content)
	}
	m.digits = ""
	next, _ := m.Update(tea.KeyPressMsg{Code: '0', Text: "0"})
	if next.(dashboard).choices()[next.(dashboard).cursor].action != "quit" {
		t.Fatal("0 should select Exit")
	}
}
