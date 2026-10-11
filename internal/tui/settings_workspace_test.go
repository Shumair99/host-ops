package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/costs"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/charmbracelet/x/ansi"
)

func pasteSettings(m dashboard, text string) dashboard {
	model, _ := m.Update(tea.PasteMsg{Content: text})
	return model.(dashboard)
}

func TestBillingPageCanSaveAndImmediatelyUseBudget(t *testing.T) {
	m := testDashboard(t)
	m.source.Demo = false
	m.source.Config.Host.Billing = nil
	m.configPath = filepath.Join(t.TempDir(), "settings.json")
	data, _ := json.Marshal(m.source.Config)
	if err := os.WriteFile(m.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	m.page, m.menuFocus = "costs", false
	m, _ = press(m, "enter")
	m, _ = press(m, "enter") // manual budget
	m.settings.Fields[0].Value, m.settings.Fields[0].Cursor = "", 0
	m = pasteSettings(m, "12.50")
	m, _ = press(m, "enter")
	// The current GBP selection is already visible; no currency typing needed.
	if m.settings.Fields[1].Value != "GBP" {
		t.Fatal("saved currency was not preselected")
	}
	m, _ = press(m, "right") // select EUR
	m, _ = press(m, "enter")
	m, cmd := press(m, "enter") // save
	if cmd == nil {
		t.Fatal("save action did not run")
	}
	msg := cmd()
	updated, reload := m.Update(msg)
	m = updated.(dashboard)
	if m.settings != nil || reload == nil || m.source.Config.Host.Billing == nil || *m.source.Config.Host.Billing.MonthlyAmount != 12.5 {
		t.Fatal("saved budget not applied immediately")
	}
	saved, err := config.Load(m.configPath)
	if err != nil || saved.Host.Currency != "EUR" || saved.Host.Billing == nil || *saved.Host.Billing.MonthlyAmount != 12.5 || len(saved.Sites) != len(m.source.Config.Sites) {
		t.Fatal("saved settings lost budget or unrelated sites", err)
	}
	m = finish(m, reload)
	if report, ok := m.snapshot.Detail.(costs.Report); !ok || report.TotalCost == nil {
		t.Fatal("cost page did not show the saved budget")
	}
}

func TestBillingCurrencyChoicesAreVisibleAndUseKeyboardOrMouse(t *testing.T) {
	m := testDashboard(t)
	m.page, m.menuFocus = "costs", false
	m.source.Config.Host.Currency = ""
	m.openSettings()
	m.setupFields("manual")
	m.settings.Selected = 1
	for _, option := range []string{"[GBP]", "EUR", "USD"} {
		if !strings.Contains(ansi.Strip(m.frame()), option) {
			t.Fatal("missing currency option", option)
		}
	}
	if !strings.Contains(m.keyBar(), "←→ currency") {
		t.Fatal("selector controls missing")
	}
	m, _ = press(m, "left")
	if m.settings.Fields[1].Value != "USD" {
		t.Fatal("selector did not wrap backwards")
	}
	m, _ = press(m, "right")
	if m.settings.Fields[1].Value != "GBP" {
		t.Fatal("selector did not wrap forwards")
	}
	m = pasteSettings(m, "not a currency")
	if m.settings.Fields[1].Value != "GBP" || m.settings.Error != "" {
		t.Fatal("selector accepted free text")
	}
	for _, option := range []string{"EUR", "USD"} {
		clicked := false
		for y, row := range strings.Split(m.frame(), "\n") {
			line := ansi.Strip(row)
			if strings.Contains(line, "Currency") {
				position := strings.Index(line, option)
				if position < 0 {
					t.Fatal("option missing", option)
				}
				x := ansi.StringWidth(line[:position])
				updated, _ := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				m = updated.(dashboard)
				clicked = true
				break
			}
		}
		if !clicked || m.settings.Fields[1].Value != option {
			t.Fatal("currency click missed", option)
		}
	}
	m.settings.Selected = 0
	m.settings.Fields[0].Value, m.settings.Fields[0].Cursor = "", 0
	m = pasteSettings(m, "12.50")
	if m.settings.Fields[0].Value != "12.50" || strings.Contains(m.frame(), "▏") {
		t.Fatal("input value or portable cursor regressed")
	}
}

func TestBillingChoicesFitSmallScreenAndPreserveExistingCurrency(t *testing.T) {
	for _, currency := range []string{"", "EUR", "CAD"} {
		m := testDashboard(t)
		m.width, m.height, m.page, m.menuFocus = 60, 16, "costs", false
		m.source.Config.Host.Currency = currency
		m.openSettings()
		m.setupFields("manual")
		m.settings.Selected = 1
		if currency != "" && m.settings.Fields[1].Value != currency {
			t.Fatal("existing currency changed", currency)
		}
		for _, option := range []string{"GBP", "EUR", "USD"} {
			if !strings.Contains(ansi.Strip(m.frame()), option) {
				t.Fatal("currency option hidden on small screen", option)
			}
		}
		if currency != "" && !strings.Contains(ansi.Strip(m.frame()), "["+currency+"]") {
			t.Fatal("selected currency hidden", currency)
		}
		for _, row := range strings.Split(m.frame(), "\n") {
			if ansi.StringWidth(row) > 60 {
				t.Fatal("billing form overflows", row)
			}
		}
	}
}

func TestBillingCancelAndInvalidBudgetLeaveSettingsUntouched(t *testing.T) {
	m := testDashboard(t)
	m.page, m.menuFocus = "costs", false
	m.openSettings()
	m.setupFields("manual")
	m.settings.Fields[0].Value, m.settings.Fields[1].Value = "-10", "GBP"
	before := m.source.Config.Host.Billing
	if cmd := m.saveSettingsCmd(); cmd != nil || m.settings.Error == "" {
		t.Fatal("invalid budget was accepted")
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "esc")
	if m.settings != nil || m.source.Config.Host.Billing != before || m.page != "costs" {
		t.Fatal("cancel changed billing settings or left the page")
	}
}

func TestProviderTokenIsMaskedAndDoesNotEnterProcessEnvironment(t *testing.T) {
	m := testDashboard(t)
	m.page = "costs"
	m.openSettings()
	m.setupFields("hetzner")
	m.settings.Selected = 1
	t.Setenv("HOSTOPS_HETZNER_TOKEN", "")
	m = pasteSettings(m, "private-test-token")
	if strings.Contains(m.frame(), "private-test-token") {
		t.Fatal("token is visible in rendered settings")
	}
	m.settings.Fields[2].Value = "42"
	cmd := m.saveSettingsCmd()
	if cmd == nil || os.Getenv("HOSTOPS_HETZNER_TOKEN") != "" || m.settings.Fields[1].Value != "" {
		t.Fatal("token was not isolated to the session")
	}
	// Do not execute a real provider request in this fixture.
}

func TestRecoveryImportsAnActualJobReportWithoutInventingRestoreEvidence(t *testing.T) {
	m := testDashboard(t)
	m.page, m.menuFocus = "recovery", false
	m.openSettings()
	m, _ = press(m, "enter")
	file := filepath.Join(t.TempDir(), "backup.json")
	backup := recovery.Backup{ID: "job-1", SiteID: m.rawSites()[0].ID, Status: "succeeded", CompletedAt: time.Now().UTC(), SizeBytes: 4096, Destination: "offsite"}
	data, _ := json.Marshal(backup)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	m = pasteSettings(m, file)
	m, _ = press(m, "enter")
	m, cmd := press(m, "enter")
	updated, reload := m.Update(cmd())
	m = updated.(dashboard)
	if m.settings != nil || reload == nil {
		t.Fatal("report import did not return to Recovery")
	}
	assessment, err := recovery.Assess(m.ctx, m.source.Config, m.rawSites()[0], m.source.Store)
	if err != nil || assessment.BackupState != model.Pass || assessment.RestoreState != model.Unknown {
		t.Fatal("import lost backup evidence or invented restore success", err, assessment)
	}
}

func TestSetupActionsAndFormsFitNarrowTerminal(t *testing.T) {
	m := testDashboard(t)
	m.width, m.height, m.menuFocus = 60, 16, false
	for _, page := range []string{"costs", "recovery", "deployments", "external"} {
		m.page = page
		m.openSettings()
		if !strings.Contains(m.keyBar(), "Esc back") {
			t.Fatal("back instruction missing", page)
		}
		for _, line := range strings.Split(m.frame(), "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatal("narrow setup overflows", page, line)
			}
		}
		if page == "costs" {
			m.setupFields("hetzner")
			for _, line := range m.settingsLines(m.bodyWidth()) {
				if ansi.StringWidth(line) > m.bodyWidth() {
					t.Fatal("provider form overflows", line)
				}
			}
		}
		m.settings = nil
	}
}

func TestSetupPreviewExport(t *testing.T) {
	dir := os.Getenv("HOSTOPS_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set HOSTOPS_PREVIEW_DIR to export setup frames")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m := testDashboard(t)
	m.width, m.height, m.page, m.menuFocus, m.theme.color = 100, 26, "costs", false, true
	m.snapshot.Detail = costs.Report{}
	m.source.Config.Host.Billing = nil
	export := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "demo-"+name+".txt"), []byte(m.frame()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	export("billing-empty")
	m.openSettings()
	export("billing-sources")
	m.setupFields("manual")
	m.settings.Fields[0].Value = "12.50"
	m.settings.Fields[1].Value = "GBP"
	m.settings.Selected = 0
	m.settings.Fields[0].Cursor = len([]rune(m.settings.Fields[0].Value))
	export("billing-amount")
	m.settings.Selected = 1
	export("billing-currencies")
	m.settings.Selected = 2
	export("billing-manual")
	m.width, m.height = 60, 16
	m.setupFields("hetzner")
	m.settings.Selected = len(m.settings.Fields)
	m.offset = max(0, len(m.settingsLines(m.bodyWidth()))-m.bodyHeight())
	export("billing-provider-narrow")
	m.width, m.height, m.page = 100, 26, "recovery"
	m.openSettings()
	export("recovery-setup")
}
