package tui

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Shumair99/host-ops/internal/billing"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/evidence"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/charmbracelet/x/ansi"
)

type setupField struct {
	Label, Value string
	Secret       bool
	Cursor       int
	Choices      []string
}

type settingsEditor struct {
	Kind, Step, Provider, Error string
	Selected                    int
	Fields                      []setupField
	Resources                   []billing.Resource
	Busy                        bool
}

type settingsMsg struct {
	version   int
	config    *config.Config
	resources []billing.Resource
	notice    string
	err       error
}

var billingSources = []struct{ ID, Label, Hint string }{
	{"manual", "Monthly budget", "Enter the amount you pay and its currency."},
	{"hetzner", "Hetzner", "Read pricing for a server in your account."},
	{"hostinger", "Hostinger", "Read a subscription in your account."},
	{"self-hosted", "Own hardware", "Optional budget; blank turns cost tracking off."},
}

func (m *dashboard) openSettings() {
	m.settingsVersion++
	m.settings = &settingsEditor{Kind: m.page, Step: "choose"}
	m.menuFocus, m.offset, m.notice = false, 0, ""
	if m.page == "costs" && m.source.Config.Host.Billing != nil {
		for i, source := range billingSources {
			if source.ID == m.source.Config.Host.Billing.Provider {
				m.settings.Selected = i
			}
		}
	}
}

func (m *dashboard) setupFields(provider string) {
	e := m.settings
	e.Provider, e.Step, e.Selected, e.Error = provider, "fields", 0, ""
	h := m.source.Config.Host
	if provider == "manual" || provider == "self-hosted" {
		amount := ""
		if h.Billing != nil && h.Billing.MonthlyAmount != nil {
			amount = strconv.FormatFloat(*h.Billing.MonthlyAmount, 'f', -1, 64)
		} else if h.Billing == nil && h.MonthlyCost > 0 {
			amount = strconv.FormatFloat(h.MonthlyCost, 'f', -1, 64)
		}
		currencies := []string{"GBP", "EUR", "USD"}
		currency := h.Currency
		if currency == "" {
			currency = currencies[0]
		} else if !slices.Contains(currencies, currency) {
			// Preserve currencies already configured through the CLI/settings;
			// opening the form must never silently convert an existing budget.
			currencies = append(currencies, currency)
		}
		e.Fields = []setupField{{Label: "Monthly amount", Value: amount}, {Label: "Currency", Value: currency, Choices: currencies}}
	} else {
		env, resource := billing.TokenName(provider, ""), ""
		if h.Billing != nil && h.Billing.Provider == provider {
			env, resource = billing.TokenName(provider, h.Billing.TokenEnv), h.Billing.ResourceID
		}
		e.Fields = []setupField{{Label: "Token variable", Value: env}, {Label: "API token", Secret: true}, {Label: "Resource ID", Value: resource}}
	}
	for i := range e.Fields {
		e.Fields[i].Cursor = len([]rune(e.Fields[i].Value))
	}
}

func (m dashboard) settingsLines(w int) []string {
	e, t := m.settings, m.theme
	rows := []string{}
	if e.Kind == "costs" {
		rows = append(rows, t.paint("Save local billing settings; monitoring continues.", muted))
		if e.Step == "choose" {
			for i, source := range billingSources {
				line := source.Label
				if i == e.Selected {
					line = t.selection(line, w-4)
				}
				rows = append(rows, line)
			}
			rows = append(rows, "", t.paint(billingSources[e.Selected].Hint, muted))
		} else if e.Step == "resources" {
			rows = append(rows, "Choose the server/subscription for this VPS.")
			limit := max(1, m.bodyHeight()-7)
			start := max(0, e.Selected-limit+1)
			for i := start; i < min(len(e.Resources), start+limit); i++ {
				r := e.Resources[i]
				line := clean(r.ID + "  " + r.Name + " / " + r.Plan + " / " + r.Location)
				if i == e.Selected {
					line = t.selection(line, w-4)
				}
				rows = append(rows, clip(line, w-4))
			}
		} else {
			if e.Provider == "manual" || e.Provider == "self-hosted" {
				rows = append(rows, t.paint("Amount: e.g. 12.50", muted))
				if e.Provider == "self-hosted" {
					rows = append(rows, t.paint("Leave amount blank to turn cost tracking off.", muted))
				}
				if m.source.Config.Host.MonthlyBackupCost > 0 {
					rows = append(rows, t.paint(fmt.Sprintf("Existing backup budget adds %.2f / month.", m.source.Config.Host.MonthlyBackupCost), muted))
				}
			} else {
				rows = append(rows, t.paint("Token: use the variable or enter it here, masked.", muted), t.paint("Entered tokens last for this session; never saved.", muted), t.paint("Leave Resource ID blank to choose from your account.", muted))
			}
			rows = append(rows, m.fieldRows(w)...)
		}
	} else if e.Step == "choose" {
		options := setupOptions(e.Kind)
		for i, option := range options {
			line := option
			if i == e.Selected {
				line = t.selection(line, w-4)
			}
			rows = append(rows, line)
		}
		if e.Kind == "recovery" {
			rows = append(rows, "", t.paint("Existing backups are scanned automatically.", muted), t.paint("Backup/restore jobs can also publish reports under", muted), t.paint("~/.local/state/hostops/recovery/ on the VPS.", muted), t.paint("Import a real job report; a backup file is not a report.", muted))
		} else if e.Kind == "deployments" {
			rows = append(rows, "", t.paint("Import the JSON outcome emitted by your deploy job.", muted))
		} else {
			rows = append(rows, "", t.paint("Import fresh JSON observations from another machine.", muted), t.paint("The separate hostops-watcher checks public addresses.", muted))
		}
	} else {
		rows = append(rows, t.paint("Read an existing JSON report from this computer.", muted))
		if e.Kind == "recovery" {
			if assessments, ok := m.snapshot.Detail.([]recovery.Assessment); ok && len(assessments) > 0 {
				a := assessments[min(m.assessmentSelected, len(assessments)-1)]
				rows = append(rows, t.paint("Selected site ID: "+clean(a.SiteID), muted))
			}
		}
		rows = append(rows, m.fieldRows(w)...)
	}
	if e.Busy {
		rows = append(rows, t.paint("Working…", cyan))
	}
	if e.Error != "" {
		rows = append(rows, wrap([]string{t.paint(clean(e.Error), red)}, w-4)...)
	}
	return t.panel(map[bool]string{true: "Billing settings", false: "Connect evidence"}[e.Kind == "costs"], rows, w)
}

func setupOptions(kind string) []string {
	switch kind {
	case "recovery":
		return []string{"Import backup report", "Import restore test report", "Scan existing backups again"}
	case "deployments":
		return []string{"Import deployment report"}
	default:
		return []string{"Import watcher observations"}
	}
}

func (m dashboard) fieldRows(w int) []string {
	e, t := m.settings, m.theme
	rows := []string{""}
	for i, field := range e.Fields {
		if len(field.Choices) > 0 {
			choices := make([]string, len(field.Choices))
			for j, choice := range field.Choices {
				choices[j] = t.paint(choice, muted)
				if choice == field.Value {
					choices[j] = t.strong("["+choice+"]", cyan)
				}
			}
			line := pad(field.Label, 16) + " " + strings.Join(choices, "  ")
			if ansi.StringWidth(line) > w-6 {
				rows = append(rows, field.Label)
				line = "  " + strings.Join(choices, "  ")
			}
			if i == e.Selected {
				line = t.selection(line, w-4)
			}
			rows = append(rows, line)
			continue
		}
		value := []rune(clean(field.Value))
		if field.Secret {
			value = []rune(strings.Repeat("•", len(value)))
		}
		if i == e.Selected {
			cursor := min(field.Cursor, len(value))
			value = append(append(append([]rune{}, value[:cursor]...), '|'), value[cursor:]...)
			available := max(1, w-23)
			start := max(0, cursor-available+1)
			value = value[start:min(len(value), start+available)]
		}
		line := pad(field.Label, 16) + " " + string(value)
		if field.Value == "" && i != e.Selected {
			line += "—"
		}
		if i == e.Selected {
			line = t.selection(line, w-4)
		}
		rows = append(rows, clip(line, w-4))
	}
	action := "Save settings"
	if e.Kind != "costs" {
		action = "Import report"
	}
	if e.Selected == len(e.Fields) {
		action = t.selection("[ "+action+" ]", w-4)
	} else {
		action = t.paint("[ "+action+" ]", cyan)
	}
	return append(rows, "", action)
}

func (m *dashboard) updateSettings(msg tea.KeyPressMsg) tea.Cmd {
	e, key := m.settings, msg.String()
	if e.Busy {
		return nil
	}
	if key == "esc" {
		if e.Step != "choose" {
			e.Step, e.Selected, e.Error, e.Fields = "choose", 0, "", nil
		} else {
			m.settings = nil
		}
		m.offset = 0
		return nil
	}
	if e.Step == "choose" || e.Step == "resources" {
		count := len(setupOptions(e.Kind))
		if e.Kind == "costs" {
			count = len(billingSources)
		}
		if e.Step == "resources" {
			count = len(e.Resources)
		}
		switch key {
		case "up", "k", "shift+tab":
			e.Selected = wrapIndex(e.Selected, -1, count)
		case "down", "j", "tab":
			e.Selected = wrapIndex(e.Selected, 1, count)
		case "home":
			e.Selected = 0
		case "end":
			e.Selected = max(0, count-1)
		case "enter":
			if e.Step == "resources" && count > 0 {
				e.Fields[2].Value = e.Resources[e.Selected].ID
				e.Fields[2].Cursor = len([]rune(e.Fields[2].Value))
				e.Step, e.Selected = "fields", len(e.Fields)
				return m.saveSettingsCmd()
			}
			if e.Kind == "costs" {
				m.setupFields(billingSources[e.Selected].ID)
			} else if e.Kind == "recovery" && e.Selected == 2 {
				m.settings = nil
				return m.requestScan()
			} else {
				e.Provider = "observations"
				if e.Kind == "recovery" {
					e.Provider = []string{"backup", "restore"}[e.Selected]
				} else if e.Kind == "deployments" {
					e.Provider = "deployment"
				}
				e.Step, e.Selected = "fields", 0
				e.Fields = []setupField{{Label: "Report file"}}
			}
		}
		return nil
	}
	switch key {
	case "tab", "down":
		e.Selected = wrapIndex(e.Selected, 1, len(e.Fields)+1)
	case "shift+tab", "up":
		e.Selected = wrapIndex(e.Selected, -1, len(e.Fields)+1)
	case "ctrl+s":
		return m.saveSettingsCmd()
	case "enter":
		if e.Selected == len(e.Fields) {
			return m.saveSettingsCmd()
		}
		e.Selected++
	default:
		if e.Selected >= len(e.Fields) {
			return nil
		}
		field := &e.Fields[e.Selected]
		if len(field.Choices) > 0 {
			index := max(0, slices.Index(field.Choices, field.Value))
			switch key {
			case "left":
				index = wrapIndex(index, -1, len(field.Choices))
			case "right", "space":
				index = wrapIndex(index, 1, len(field.Choices))
			case "home":
				index = 0
			case "end":
				index = len(field.Choices) - 1
			default:
				return nil // Selectors do not accept typed/pasted free text.
			}
			field.Value = field.Choices[index]
			e.Error = ""
			return nil
		}
		runes := []rune(field.Value)
		field.Cursor = min(field.Cursor, len(runes))
		switch key {
		case "left":
			field.Cursor = max(0, field.Cursor-1)
		case "right":
			field.Cursor = min(len(runes), field.Cursor+1)
		case "home", "ctrl+a":
			field.Cursor = 0
		case "end", "ctrl+e":
			field.Cursor = len(runes)
		case "ctrl+u":
			runes, field.Cursor = nil, 0
		case "backspace":
			if field.Cursor > 0 {
				runes = append(runes[:field.Cursor-1], runes[field.Cursor:]...)
				field.Cursor--
			}
		case "delete":
			if field.Cursor < len(runes) {
				runes = append(runes[:field.Cursor], runes[field.Cursor+1:]...)
			}
		default:
			for _, char := range msg.Text {
				if char >= 32 && char != 127 && len(runes) < 4096 {
					runes = append(runes, 0)
					copy(runes[field.Cursor+1:], runes[field.Cursor:])
					runes[field.Cursor] = char
					field.Cursor++
				}
			}
		}
		field.Value = string(runes)
	}
	e.Error = ""
	// Keep the active field or save action visible in a small terminal.
	m.offset = max(0, len(m.settingsLines(m.bodyWidth()))-m.bodyHeight())
	return nil
}

func (m *dashboard) saveSettingsCmd() tea.Cmd {
	e := m.settings
	if e.Kind != "costs" {
		file := strings.Trim(strings.TrimSpace(e.Fields[0].Value), "\"")
		if file == "" {
			e.Error = "Enter the path to an existing JSON report."
			return nil
		}
		e.Busy = true
		version, kind, source, ctx := m.settingsVersion, e.Provider, m.source, m.ctx
		source.Config.Sites = m.rawSites()
		return m.jobs.command(func() tea.Msg {
			f, err := os.Open(file)
			if err != nil {
				return settingsMsg{version: version, err: fmt.Errorf("cannot open report file")}
			}
			defer f.Close()
			created, err := evidence.Record(ctx, source.Config, source.Store, kind, f)
			notice := "Report imported; evidence refreshed."
			if created == 0 && err == nil {
				notice = "Report already recorded; evidence refreshed."
			}
			return settingsMsg{version: version, notice: notice, err: err}
		})
	}
	b := &config.Billing{Provider: e.Provider}
	currency := m.source.Config.Host.Currency
	if e.Provider == "manual" || e.Provider == "self-hosted" {
		amount := strings.TrimSpace(e.Fields[0].Value)
		currency = strings.ToUpper(strings.TrimSpace(e.Fields[1].Value))
		if amount != "" {
			n, err := strconv.ParseFloat(amount, 64)
			if err != nil {
				e.Error = "Enter a monthly amount such as 12.50."
				return nil
			}
			b.MonthlyAmount = &n
			if !slices.Contains(e.Fields[1].Choices, currency) {
				e.Error = "Select a currency with Left / Right."
				return nil
			}
		}
	} else {
		b.TokenEnv, b.ResourceID = strings.TrimSpace(e.Fields[0].Value), strings.TrimSpace(e.Fields[2].Value)
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(b.TokenEnv) {
			e.Error = "Enter a valid token variable name, such as HOSTOPS_HETZNER_TOKEN."
			return nil
		}
		if token := strings.TrimSpace(e.Fields[1].Value); token != "" {
			m.ctx = billing.WithSessionToken(m.ctx, b.Provider, b.TokenEnv, token)
			e.Fields[1].Value, e.Fields[1].Cursor = "", 0
		}
		if !billing.TokenAvailable(m.ctx, b.Provider, b.TokenEnv) {
			e.Error = "Enter your provider token here, or use a variable that already holds it."
			return nil
		}
		if b.ResourceID == "" {
			e.Busy, e.Error = true, ""
			version, ctx := m.settingsVersion, m.ctx
			return m.jobs.command(func() tea.Msg {
				resources, err := billing.NewClient().Resources(ctx, b.Provider, b.TokenEnv)
				if err == nil && len(resources) == 0 {
					err = fmt.Errorf("no resources found in this account")
				}
				return settingsMsg{version: version, resources: resources, err: err}
			})
		}
	}
	candidate := m.source.Config
	candidate.Host.Billing, candidate.Host.Currency = b, currency
	if err := candidate.Validate(); err != nil {
		e.Error = err.Error()
		return nil
	}
	if m.configPath == "" && !m.source.Demo {
		e.Error = "This dashboard has no settings file; reopen it with --config FILE."
		return nil
	}
	e.Busy, e.Error = true, ""
	version, file, source, ctx := m.settingsVersion, m.configPath, m.source, m.ctx
	return m.jobs.command(func() tea.Msg {
		if !source.Demo {
			var err error
			candidate, err = config.SaveBilling(file, source.Config.Host.ID, b, currency)
			if err != nil {
				return settingsMsg{version: version, err: err}
			}
		}
		notice := "Billing settings saved; estimates updated."
		if source.Demo {
			notice = "Demo billing updated; no settings file changed."
		}
		if b.Provider == "hetzner" || b.Provider == "hostinger" {
			_, err := billing.Resolve(ctx, candidate.Host, source.Store, true)
			if err != nil {
				notice = "Settings saved; pricing could not be read. Open settings to retry."
			}
		}
		return settingsMsg{version: version, config: &candidate, notice: notice}
	})
}

func (m *dashboard) settingsClick(msg tea.MouseClickMsg) tea.Cmd {
	e := m.settings
	if e.Busy || e.Step != "fields" || msg.X < 21 {
		return nil
	}
	lines := wrap(m.settingsLines(m.bodyWidth()), m.bodyWidth())
	offset := min(m.offset, max(0, len(lines)-m.bodyHeight()))
	row := msg.Y - 3 + offset
	if row < 0 || row >= len(lines) {
		return nil
	}
	line, x := ansi.Strip(lines[row]), msg.X-21
	for i := range e.Fields {
		field := &e.Fields[i]
		for _, choice := range field.Choices {
			position := strings.Index(line, choice)
			if position < 0 {
				continue
			}
			column := ansi.StringWidth(line[:position])
			if x >= column-1 && x < column+len(choice)+1 {
				e.Selected, field.Value, e.Error = i, choice, ""
				return nil
			}
		}
		if strings.Contains(line, field.Label) {
			e.Selected = i
			return nil
		}
	}
	if strings.Contains(line, "[ Save settings ]") || strings.Contains(line, "[ Import report ]") {
		e.Selected = len(e.Fields)
		return m.saveSettingsCmd()
	}
	return nil
}

func (m *dashboard) requestScan() tea.Cmd {
	if m.source.Live {
		select {
		case m.source.Refresh <- struct{}{}:
		default:
		}
		m.notice = "Fresh discovery requested; backup evidence updates when it finishes."
		return nil
	}
	if m.source.Demo {
		m.notice = "Demo uses fictional evidence; no VPS is scanned."
		return nil
	}
	if !m.collecting {
		m.collecting = true
		m.notice = "Scanning existing workloads and backup evidence…"
		return m.cycleCmd()
	}
	m.notice = "A discovery scan is already running."
	return nil
}
