package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func menuItem(t *testing.T, m dashboard, action string) choice {
	t.Helper()
	for _, item := range m.choices() {
		if item.action == action {
			return item
		}
	}
	t.Fatalf("missing %s on %s", action, m.page)
	return choice{}
}

func TestCatalogCoversPublicCommandsAndOptions(t *testing.T) {
	root := NewCommand()
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpCmd()
	m := dashboard{page: "home", commandMenu: commandMenu{root: root}}
	m.acceptTool(menuItem(t, m, "cmd:"))
	var walk func(dashboard, *cobra.Command)
	walk = func(m dashboard, c *cobra.Command) {
		menuItem(t, m, "command-help")
		children := 0
		for _, child := range c.Commands() {
			if child.Name() == "help" || child.Hidden {
				continue
			}
			children++
			v := m
			v.acceptTool(menuItem(t, v, "cmd:"+commandPath(child)))
			walk(v, child)
		}
		if children == 0 {
			menuItem(t, m, "run-command")
			c.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
				if !f.Hidden && f.Name != "help" {
					menuItem(t, m, "flag:"+f.Name)
				}
			})
			menuItem(t, m, "flag:dry-run")
		}
	}
	walk(m, root)
	for _, item := range m.choices() {
		if item.action == "cmd:serve" {
			t.Fatal("internal worker exposed")
		}
	}
}

func TestMenuArgumentsAndUninstallConfirmation(t *testing.T) {
	m := dashboard{page: "home", commandMenu: commandMenu{root: NewCommand()}}
	m.acceptTool(menuItem(t, m, "cmd:"))
	m.acceptTool(menuItem(t, m, "cmd:backup"))
	m.acceptTool(menuItem(t, m, "cmd:backup create"))
	m.acceptTool(menuItem(t, m, "run-command"))
	if m.page != "detail" || !strings.Contains(m.body, "--file") {
		t.Fatal("missing required flag feedback")
	}
	m.pop()
	m.acceptTool(menuItem(t, m, "flag:file"))
	filename := "/tmp/a path with $(literal characters)"
	m.inputTool("", filename)
	m.inputTool("enter", "")
	m.acceptTool(menuItem(t, m, "run-command"))
	if m.page != "tool-confirm" || !slices.Equal(m.pending, []string{"backup", "create", "--file=" + filename, "--yes"}) {
		t.Fatal(m.pending)
	}

	m = dashboard{page: "home", commandMenu: commandMenu{root: NewCommand()}}
	m.acceptTool(menuItem(t, m, "cmd:bbr"))
	m.acceptTool(menuItem(t, m, "cmd:bbr enable"))
	m.acceptTool(menuItem(t, m, "flag:dry-run"))
	m.acceptTool(menuItem(t, m, "run-command"))
	if !slices.Equal(m.pending, []string{"bbr", "enable", "--dry-run=true", "--yes"}) {
		t.Fatal(m.pending)
	}
	m = dashboard{page: "home"}
	m.acceptTool(menuItem(t, m, "remove-options"))
	cmd, _ := m.acceptTool(menuItem(t, m, "uninstall --full --yes"))
	if cmd != nil || m.page != "tool-confirm" {
		t.Fatal("full uninstall executed before confirmation")
	}
}
