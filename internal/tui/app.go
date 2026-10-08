package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/charmbracelet/x/term"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Interval              time.Duration
	NoCollect, Once, Demo bool
	Page                  string
	Width, Height         int
}
type jobs struct {
	mu      sync.Mutex
	closing bool
	wg      sync.WaitGroup
}

func (j *jobs) command(fn func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		j.mu.Lock()
		if j.closing {
			j.mu.Unlock()
			return nil
		}
		j.wg.Add(1)
		j.mu.Unlock()
		defer j.wg.Done()
		return fn()
	}
}
func (j *jobs) close() { j.mu.Lock(); j.closing = true; j.mu.Unlock(); j.wg.Wait() }

type tickMsg struct{}
type collectTickMsg struct{ version int }
type cycleMsg struct{ err error }
type loadedMsg struct {
	generation int
	snapshot   Snapshot
	lines      []string
	err        error
}
type noticeMsg string
type dashboard struct {
	ctx                                  context.Context
	source                               Source
	jobs                                 *jobs
	interval                             time.Duration
	width, height                        int
	page, siteID, target, filter         string
	selected, offset, generation         int
	snapshot                             Snapshot
	lines                                []string
	loading, collecting, noCollect       bool
	collectVersion                       int
	notice, collectErr, inputKind, input string
	cancelRead                           context.CancelFunc
	demoOutage, demoBusy                 bool
}

func Run(parent context.Context, c config.Config, dbPath string, opts Options, input io.Reader, out io.Writer) error {
	if opts.Interval == 0 {
		opts.Interval = 60 * time.Second
	}
	if opts.Interval < 5*time.Second {
		return fmt.Errorf("interval must be at least 5 seconds")
	}
	if opts.Page != "" {
		valid := false
		for _, item := range menu {
			valid = valid || item.Page == opts.Page
		}
		if !valid {
			return fmt.Errorf("unknown dashboard view %q; use --page overview or a view listed in Help", opts.Page)
		}
	}
	if !opts.Once {
		inFile, inOK := input.(*os.File)
		outFile, outOK := out.(*os.File)
		if !inOK || !outOK || !term.IsTerminal(inFile.Fd()) || !term.IsTerminal(outFile.Fd()) {
			return fmt.Errorf("interactive dashboard requires a terminal; use --once for plain text output")
		}
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if opts.Demo {
		c = DemoConfig()
		dbPath = ":memory:"
	}
	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("dashboard history unavailable")
	}
	defer store.Close()
	if opts.Demo {
		if err = SeedDemo(ctx, c, store, false); err != nil {
			return err
		}
	}
	m := newDashboard(ctx, Source{Config: c, Store: store, Demo: opts.Demo, MaxAge: max(3*time.Minute, 3*opts.Interval)}, opts)
	if opts.Once {
		if !opts.NoCollect && !opts.Demo {
			if err = agent.Cycle(ctx, c, store); err != nil {
				return err
			}
		}
		m.snapshot, err = m.source.Base(ctx)
		if err != nil {
			return err
		}
		m.lines, err = m.source.Page(ctx, m.page, "", m.target, m.snapshot)
		if err != nil {
			return err
		}
		m.loading = false
		_, err = fmt.Fprintln(out, m.frame())
		return err
	}
	program := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(out))
	_, err = program.Run()
	cancel()
	m.jobs.close()
	if parent.Err() != nil {
		return nil
	}
	return err
}
func newDashboard(ctx context.Context, source Source, opts Options) dashboard {
	width, height := opts.Width, opts.Height
	if width == 0 {
		width = 100
	}
	if height == 0 {
		height = 30
	}
	page := opts.Page
	if page == "" {
		page = "overview"
	}
	return dashboard{ctx: ctx, source: source, jobs: &jobs{}, interval: opts.Interval, width: width, height: height, page: page, target: "caddy", noCollect: opts.NoCollect, loading: true, collecting: !opts.NoCollect && !opts.Demo && !opts.Once}
}
func (m dashboard) Init() tea.Cmd {
	commands := []tea.Cmd{m.readCmd(m.generation), tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tickMsg{} })}
	if !m.noCollect && !m.source.Demo {
		commands = append(commands, m.cycleCmd())
	}
	return tea.Batch(commands...)
}
func (m dashboard) readCmd(generation int) tea.Cmd {
	return m.jobs.command(func() tea.Msg {
		snapshot, err := m.source.Base(m.ctx)
		if err != nil {
			return loadedMsg{generation: generation, err: err}
		}
		lines, err := m.source.Page(m.ctx, m.page, m.siteID, m.target, snapshot)
		return loadedMsg{generation: generation, snapshot: snapshot, lines: lines, err: err}
	})
}
func (m *dashboard) reload() tea.Cmd {
	if m.cancelRead != nil {
		m.cancelRead()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelRead = cancel
	m.generation++
	m.loading = true
	source, page, siteID, target, generation := m.source, m.page, m.siteID, m.target, m.generation
	return m.jobs.command(func() tea.Msg {
		snapshot, err := source.Base(ctx)
		if err != nil {
			return loadedMsg{generation: generation, err: err}
		}
		lines, err := source.Page(ctx, page, siteID, target, snapshot)
		return loadedMsg{generation: generation, snapshot: snapshot, lines: lines, err: err}
	})
}
func (m dashboard) cycleCmd() tea.Cmd {
	return m.jobs.command(func() tea.Msg { return cycleMsg{err: agent.Cycle(m.ctx, m.source.Config, m.source.Store)} })
}
func (m *dashboard) goPage(page string) tea.Cmd {
	m.page = page
	m.offset = 0
	m.notice = ""
	m.siteID = ""
	return m.reload()
}
func (m dashboard) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.lines = []string{"Evidence unavailable. Check configuration and access.", msg.err.Error()}
			m.notice = "Read failed; no successful result is assumed."
		} else {
			m.snapshot, m.lines = msg.snapshot, msg.lines
		}
		m.selected = min(m.selected, max(0, len(m.filteredSites())-1))
	case cycleMsg:
		m.collecting = false
		m.demoBusy = false
		m.collectErr = ""
		m.notice = "Observation cycle finished."
		if m.source.Demo {
			m.notice = "DEMO: application healthy; incident resolved if previously open."
			if m.demoOutage {
				m.notice = "DEMO: Bravo application stopped; incident opened. Press t to recover."
			}
		}
		if msg.err != nil && m.ctx.Err() == nil {
			m.collectErr = "Collection failed"
		}
		m.collectVersion++
		version := m.collectVersion
		commands := []tea.Cmd{}
		if !m.noCollect && !m.source.Demo {
			commands = append(commands, tea.Tick(m.interval, func(time.Time) tea.Msg { return collectTickMsg{version: version} }))
		}
		if m.page != "disk" && m.page != "security" && m.page != "preflight" && m.page != "logs" {
			commands = append(commands, m.reload())
		}
		return m, tea.Batch(commands...)
	case collectTickMsg:
		if msg.version == m.collectVersion && !m.noCollect && !m.source.Demo && !m.collecting {
			m.collecting = true
			return m, m.cycleCmd()
		}
	case tickMsg:
		tick := tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
		if !m.loading && (m.page == "overview" || m.page == "sites" || m.page == "issues" || m.page == "host") {
			return m, tea.Batch(tick, m.reload())
		}
		return m, tick
	case noticeMsg:
		m.notice = string(msg)
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.inputKind != "" {
			switch key {
			case "esc":
				m.inputKind = ""
				m.input = ""
			case "enter":
				if m.inputKind == "filter" {
					m.filter = m.input
					m.selected = 0
				} else if m.inputKind == "dependency" {
					if strings.TrimSpace(m.input) != "" {
						m.target = strings.TrimSpace(m.input)
					}
				}
				m.inputKind = ""
				m.input = ""
				return m, m.reload()
			case "backspace":
				if len(m.input) > 0 {
					m.input = m.input[:len(m.input)-1]
				}
			default:
				if len(msg.Text) > 0 && len(m.input)+len(msg.Text) < 128 {
					for _, r := range msg.Text {
						if r >= 32 && r < 127 {
							m.input += string(r)
						}
					}
				}
			}
			return m, nil
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "esc":
			if m.siteID != "" || m.page == "site" || m.page == "logs" {
				return m, m.goPage("sites")
			}
			m.filter = ""
			m.selected = 0
			m.offset = 0
		case "tab", "shift+tab", "right", "left":
			index := 0
			for i, item := range menu {
				if item.Page == m.page {
					index = i
				}
			}
			delta := 1
			if key == "shift+tab" || key == "left" {
				delta = -1
			}
			index = (index + delta + len(menu)) % len(menu)
			return m, m.goPage(menu[index].Page)
		case "up", "k":
			if m.page == "sites" {
				m.selected = max(0, m.selected-1)
			} else {
				m.offset = max(0, m.offset-1)
			}
		case "down", "j":
			if m.page == "sites" {
				m.selected = min(max(0, len(m.filteredSites())-1), m.selected+1)
			} else {
				m.offset++
			}
		case "pgup":
			m.offset = max(0, m.offset-max(1, m.height-10))
		case "pgdown":
			m.offset += max(1, m.height-10)
		case "home":
			m.offset = 0
			m.selected = 0
		case "end":
			if m.page == "sites" {
				m.selected = max(0, len(m.filteredSites())-1)
			} else {
				m.offset = max(0, len(wrap(m.liveLines(), max(1, m.width-25)))-max(1, m.height-9))
			}
		case "enter":
			if m.page == "sites" {
				sites := m.filteredSites()
				if len(sites) > 0 {
					m.siteID = sites[min(m.selected, len(sites)-1)].ID
					m.page = "site"
					m.offset = 0
					return m, m.reload()
				}
			}
		case "/":
			if m.page == "sites" {
				m.inputKind = "filter"
				m.input = m.filter
			} else if m.page == "impact" {
				m.inputKind = "dependency"
				m.input = m.target
			}
		case "space":
			m.notice = ""
			return m, m.reload()
		case "f5":
			if m.source.Demo {
				m.notice = "DEMO: use t to simulate an outage or recovery."
			} else if m.noCollect {
				m.notice = "Automatic collection is off; Space reloads this view."
			} else if !m.collecting {
				m.collecting = true
				m.notice = "Collecting fresh read-only observations..."
				return m, m.cycleCmd()
			} else {
				m.notice = "A collection is already running."
			}
		case "l":
			if m.siteID != "" {
				m.page = "logs"
				m.offset = 0
				return m, m.reload()
			}
		case "a":
			if m.siteID != "" {
				m.page = "readiness"
				m.offset = 0
				return m, m.reload()
			}
		case "b":
			if m.siteID != "" {
				m.page = "recovery"
				m.offset = 0
				return m, m.reload()
			}
			return m, m.goPage("recovery")
		case "w":
			if m.page == "incidents" {
				return m, m.exportCmd()
			}
		case "t":
			if m.source.Demo {
				if m.demoBusy {
					return m, nil
				}
				m.demoBusy = true
				m.demoOutage = !m.demoOutage
				outage := m.demoOutage
				source := m.source
				return m, m.jobs.command(func() tea.Msg {
					if err := SeedDemo(m.ctx, source.Config, source.Store, outage); err != nil {
						return cycleMsg{err: err}
					}
					return cycleMsg{}
				})
			}
		default:
			for _, item := range menu {
				if item.Key == key {
					return m, m.goPage(item.Page)
				}
			}
		}
	}
	if m.page != "sites" {
		m.offset = min(m.offset, max(0, len(wrap(m.liveLines(), max(1, m.width-25)))-max(1, m.height-9)))
	}
	return m, nil
}
func (m dashboard) View() tea.View {
	v := tea.NewView(m.frame())
	v.AltScreen = true
	v.WindowTitle = "Amadeus HostOps - Terminal"
	return v
}
func (m dashboard) exportCmd() tea.Cmd {
	return m.jobs.command(func() tea.Msg {
		records, err := incidents.List(m.ctx, m.source.Store, m.source.Config.Host.ID, m.siteID)
		if err != nil || len(records) == 0 {
			return noticeMsg("No incident is available to export.")
		}
		var incident incidents.Incident
		if json.Unmarshal(records[0].Data, &incident) != nil {
			return noticeMsg("Incident evidence unavailable.")
		}
		capsule, err := incidents.Export(m.ctx, m.source.Store, m.source.Config.Host.ID, incident.ID)
		if err != nil {
			return noticeMsg("Incident export unavailable.")
		}
		data, err := json.MarshalIndent(capsule, "", "  ")
		if err != nil {
			return noticeMsg("Incident export unavailable.")
		}
		path := filepath.Join("local", "capsules", incident.ID+".json")
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return noticeMsg("Cannot create capsule directory.")
		}
		if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			return noticeMsg("Cannot save capsule.")
		}
		return noticeMsg("Saved " + path + "; review before sharing.")
	})
}
