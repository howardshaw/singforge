package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandMenu struct {
	root               *cobra.Command
	values             map[string]string
	flagName, flagHelp string
}

func (m dashboard) selectedCommand() *cobra.Command {
	path := strings.TrimPrefix(m.page, "cmd:")
	c, _, err := m.root.Find(strings.Fields(path))
	if err != nil {
		return nil
	}
	return c
}

func commandPath(c *cobra.Command) string {
	if !c.HasParent() {
		return ""
	}
	return strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
}

func (m dashboard) commandChoices() []choice {
	c := m.selectedCommand()
	var items []choice
	for _, sub := range c.Commands() {
		if sub.Hidden || sub.Name() == "help" {
			continue
		}
		path := commandPath(sub)
		zh := zhHelp[path]
		if zh == "" {
			zh = sub.Short
		}
		items = append(items, choice{sub.Name() + "  " + sub.Short, sub.Name() + "  " + zh, "cmd:" + path})
	}
	if len(items) == 0 && c.Runnable() {
		c.InitDefaultHelpFlag() // Merge inherited options with this command’s local flags.
		items = append(items, choice{"Review and run", "核对并执行", "run-command"})
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden || f.Name == "help" || f.Name == "workspace" || f.Name == "core" || f.Name == "yes" {
				return
			}
			label := "--" + f.Name
			value, set := m.values[f.Name]
			if !set {
				value = f.Value.String()
			}
			if value != "" {
				label += " = " + value
			}
			if _, required := f.Annotations[cobra.BashCompOneRequiredFlag]; required {
				label += " *"
			}
			items = append(items, choice{label, label, "flag:" + f.Name})
		})
	}
	return append(items, choice{"Command help", "命令帮助", "command-help"})
}

func (m *dashboard) acceptCommand(item choice) (tea.Cmd, bool) {
	if strings.HasPrefix(item.action, "cmd:") {
		m.values = map[string]string{}
		title := strings.TrimPrefix(item.action, "cmd:")
		if title == "" {
			title = m.t("All commands", "全部命令")
		}
		m.push(item.action, title)
		return nil, true
	}
	if !strings.HasPrefix(m.page, "cmd:") {
		return nil, false
	}
	c := m.selectedCommand()
	args := strings.Fields(commandPath(c))
	switch item.action {
	case "command-help":
		m.push("busy", m.t("Command help", "命令帮助"))
		return m.command(append(args, "--help")...), true
	case "run-command":
		var missing []string
		c.Flags().VisitAll(func(f *pflag.Flag) {
			value, set := m.values[f.Name]
			if set {
				args = append(args, fmt.Sprintf("--%s=%s", f.Name, value))
			}
			if _, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; ok && !set {
				missing = append(missing, "--"+f.Name)
			}
		})
		if len(missing) > 0 {
			m.push("detail", commandPath(c))
			m.body = m.t("Required options: ", "请先填写必填参数：") + strings.Join(missing, ", ")
			return nil, true
		}
		m.push("tool-confirm", commandPath(c))
		m.resultKind = "catalog"
		m.pending = append(args, "--yes")
		return nil, true
	default:
		name := strings.TrimPrefix(item.action, "flag:")
		f := c.Flags().Lookup(name)
		if f == nil {
			return nil, false
		}
		if f.Value.Type() == "bool" {
			value, set := m.values[name]
			if !set {
				value = f.Value.String()
			}
			m.values[name] = fmt.Sprint(value != "true")
			return nil, true
		}
		m.push("flag-value", commandPath(c))
		m.flagName, m.flagHelp, m.entry = name, f.Usage, m.values[name]
		return nil, true
	}
}
