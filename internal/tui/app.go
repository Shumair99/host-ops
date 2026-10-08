package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Interval              time.Duration
	NoCollect, Once, Demo bool
	Page                  string
	Color                 string
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
type diskMsg struct {
	report *metrics.DiskReport
	scan   agent.DiskScan
	err    error
}
type loadedMsg struct {
	generation int
	snapshot   Snapshot
	lines      []string
	err        error
}
type noticeMsg string
type telemetryMsg struct {
	generation int
	snapshot   Snapshot
	err        error
}
type dashboard struct {
	ctx                                     context.Context
	source                                  Source
	jobs                                    *jobs
	interval                                time.Duration
	width, height                           int
	page, siteID, target, filter            string
	selected, offset, generation            int
	navSelected                             int
	toolSelected                            int
	menuFocus                               bool
	viewCache                               map[string][]string
	detailCache                             map[string]any
	theme                                   theme
	telemetryReading                        bool
	diskReading                             bool
	diskPath                                string
	diskSelected                            int
	snapshot                                Snapshot
	lines                                   []string
	loading, reading, collecting, noCollect bool
	collectVersion                          int
	notice, collectErr, inputKind, input    string
	cancelRead                              context.CancelFunc
	demoOutage, demoBusy                    bool
}

func Run(parent context.Context, c config.Config, dbPath string, opts Options, input io.Reader, out io.Writer) error {
	if opts.Color != "" && opts.Color != "auto" && opts.Color != "always" && opts.Color != "never" {
		return fmt.Errorf("color must be auto, always or never")
	}
	if opts.Interval == 0 {
		opts.Interval = 30 * time.Second
	}
	if opts.Interval < 5*time.Second {
		return fmt.Errorf("interval must be at least 5 seconds")
	}
	if opts.Page != "" {
		valid := false
		for _, item := range allViews() {
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
	if err = store.Prune(ctx, c.History); err != nil {
		return fmt.Errorf("dashboard history maintenance: %w", err)
	}
	if opts.Demo {
		if err = SeedDemo(ctx, c, store, false); err != nil {
			return err
		}
	}
	source := Source{Config: c, Store: store, Demo: opts.Demo, MaxAge: max(3*time.Minute, 3*opts.Interval)}
	if !opts.Demo && !opts.NoCollect && !opts.Once && c.Discovering() {
		source.Live = true
		source.Refresh = make(chan struct{}, 1)
		done := make(chan struct{})
		go func() { defer close(done); _ = agent.RunLive(ctx, c, store, opts.Interval, source.Refresh) }()
		defer func() { cancel(); <-done }()
	}
	m := newDashboard(ctx, source, opts)
	if source.Live {
		m.collecting = false
	}
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
		m.lines, err = m.source.Evidence(ctx, m.page, "", m.target, &m.snapshot)
		if opts.Page == "disk" && !opts.NoCollect && !opts.Demo {
			if _, err = agent.InspectDisk(ctx, c, store, true); err != nil {
				return err
			}
			m.snapshot, err = m.source.Base(ctx)
			if err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
		m.loading = false
		_, err = fmt.Fprintln(out, m.frame())
		return err
	}
	programOptions := []tea.ProgramOption{tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(out)}
	if opts.Color == "always" {
		programOptions = append(programOptions, tea.WithColorProfile(colorprofile.ANSI256))
	}
	program := tea.NewProgram(m, programOptions...)
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
	m := dashboard{ctx: ctx, source: source, jobs: &jobs{}, interval: opts.Interval, width: width, height: height, page: page, target: "host", noCollect: opts.NoCollect, loading: true, menuFocus: true, viewCache: map[string][]string{}, detailCache: map[string]any{}, collecting: !opts.NoCollect && !opts.Demo && !opts.Once}
	m.theme.color = opts.Color == "always" || (opts.Color != "never" && !opts.Once && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb")
	m.navSelected = menuIndex(page)
	return m
}
func (m dashboard) Init() tea.Cmd {
	commands := []tea.Cmd{m.readCmd(m.generation), tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })}
	if !m.noCollect && !m.source.Demo && !m.source.Live {
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
		lines, err := m.source.Evidence(m.ctx, m.page, m.siteID, m.target, &snapshot)
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
	m.reading = true
	source, page, siteID, target, generation := m.source, m.page, m.siteID, m.target, m.generation
	return m.jobs.command(func() tea.Msg {
		snapshot, err := source.Base(ctx)
		if err != nil {
			return loadedMsg{generation: generation, err: err}
		}
		lines, err := source.Evidence(ctx, page, siteID, target, &snapshot)
		return loadedMsg{generation: generation, snapshot: snapshot, lines: lines, err: err}
	})
}
func (m dashboard) cycleCmd() tea.Cmd {
	return m.jobs.command(func() tea.Msg { return cycleMsg{err: agent.Cycle(m.ctx, m.source.Config, m.source.Store)} })
}
func (m *dashboard) goPage(page string) tea.Cmd {
	m.page = page
	m.navSelected = menuIndex(page)
	m.menuFocus = false
	m.loading = true
	m.lines = nil
	m.snapshot.Detail = nil
	m.offset = 0
	m.notice = ""
	m.siteID = ""
	return m.reload()
}

func (m *dashboard) backToTools() {
	m.toolSelected = toolIndex(m.page)
	if m.cancelRead != nil {
		m.cancelRead()
		m.cancelRead = nil
	}
	m.generation++
	m.page = "tools"
	m.navSelected = menuIndex("tools")
	m.menuFocus = false
	m.loading, m.reading = false, false
	m.lines = nil
	m.snapshot.Detail = nil
	m.offset = 0
	m.notice = ""
	m.siteID = ""
}

func menuIndex(page string) int {
	for i, item := range menu {
		if item.Page == page {
			return i
		}
	}
	if page == "site" || page == "logs" {
		return 1
	}
	for i, item := range menu {
		if item.Page == "tools" {
			return i
		}
	}
	return 0
}

func demandPage(page string) bool {
	return page == "disk" || page == "security" || page == "preflight"
}

func (m *dashboard) previewMenu(index int) tea.Cmd {
	page := menu[index].Page
	if page == m.page && m.siteID == "" {
		m.navSelected = index
		return nil
	}
	if demandPage(page) && !m.source.Demo {
		if m.cancelRead != nil {
			m.cancelRead()
		}
		m.generation++
		m.page, m.siteID, m.offset = page, "", 0
		m.navSelected, m.menuFocus = index, true
		m.loading, m.reading, m.notice = false, false, ""
		m.lines = m.viewCache[page]
		m.snapshot.Detail = m.detailCache[page]
		if len(m.lines) == 0 {
			m.lines = inspectionPreview(page, m.snapshot)
		}
		return nil
	}
	cmd := m.goPage(page)
	m.menuFocus = true
	if cached := m.viewCache[page]; len(cached) > 0 {
		m.lines, m.loading = cached, false
		m.snapshot.Detail = m.detailCache[page]
	}
	return cmd
}

func (m *dashboard) focusContent() tea.Cmd {
	m.menuFocus = false
	if demandPage(m.page) && !m.source.Demo && len(m.viewCache[m.page]) == 0 && !m.reading && !m.loading {
		m.loading = true
		return m.reload()
	}
	return nil
}

func (m *dashboard) inspectPage(page string) tea.Cmd {
	m.page = page
	m.menuFocus = false
	m.loading = true
	m.lines = nil
	m.offset = 0
	return m.reload()
}
func (m dashboard) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case diskMsg:
		m.diskReading = false
		if msg.err == nil {
			if msg.report != nil && (m.snapshot.Disk == nil || !msg.report.ObservedAt.Before(m.snapshot.Disk.ObservedAt)) {
				m.snapshot.Disk = msg.report
			}
			if !msg.scan.StartedAt.Before(m.snapshot.DiskScan.StartedAt) {
				m.snapshot.DiskScan = msg.scan
			}
		} else if m.ctx.Err() == nil {
			m.notice = "Storage inspection failed; retained results remain visible."
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		initial := m.loading
		m.loading, m.reading = false, false
		if msg.err != nil {
			if initial {
				m.lines = []string{"Evidence unavailable. Check configuration and access.", msg.err.Error()}
				m.notice = "Read failed; no successful result is assumed."
			} else {
				m.notice = "Refresh failed; showing previous evidence: " + msg.err.Error()
			}
		} else {
			// A slow Disk scan must not roll back recent streamed telemetry.
			// An older cached sample must never override a fresh stale-data decision.
			if m.page == "disk" && time.Since(m.snapshot.Host.ObservedAt) < 5*time.Second && m.snapshot.Host.ObservedAt.After(msg.snapshot.Host.ObservedAt) {
				msg.snapshot.Host = m.snapshot.Host
			}
			m.snapshot, m.lines = msg.snapshot, msg.lines
			if m.siteID == "" {
				m.viewCache[m.page] = append([]string(nil), msg.lines...)
				m.detailCache[m.page] = msg.snapshot.Detail
			}
			if strings.HasPrefix(m.notice, "Refresh failed; showing previous evidence:") {
				m.notice = ""
			}
		}
		m.selected = min(m.selected, max(0, len(m.filteredSites())-1))
	case telemetryMsg:
		m.telemetryReading = false
		if msg.generation == m.generation && msg.err == nil {
			msg.snapshot.Detail = m.snapshot.Detail
			if m.snapshot.Disk != nil && (msg.snapshot.Disk == nil || m.snapshot.Disk.ObservedAt.After(msg.snapshot.Disk.ObservedAt)) {
				msg.snapshot.Disk = m.snapshot.Disk
			}
			m.snapshot = msg.snapshot
		}
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
		tick := tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
		commands := []tea.Cmd{tick}
		if !m.noCollect && !m.source.Demo && !m.source.Config.Host.DiskScanDisabled && !m.diskReading && (!m.source.Live || !m.snapshot.Inventory.Snapshot.ObservedAt.IsZero()) && agent.DiskDue(m.snapshot.Disk, m.snapshot.DiskScan, time.Now()) {
			m.diskReading = true
			commands = append(commands, m.diskCmd(false))
		}
		if m.page == "disk" && !m.telemetryReading {
			m.telemetryReading = true
			source, ctx, generation := m.source, m.ctx, m.generation
			return m, tea.Batch(append(commands, m.jobs.command(func() tea.Msg {
				snapshot, err := source.Base(ctx)
				return telemetryMsg{generation: generation, snapshot: snapshot, err: err}
			}))...)
		}
		if !m.loading && !m.reading && (m.page == "overview" || m.page == "sites" || m.page == "issues" || m.page == "host" || m.page == "inventory" || m.page == "site") {
			return m, tea.Batch(append(commands, m.reload())...)
		}
		return m, tea.Batch(commands...)
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
			if toolIndex(m.page) >= 0 && m.siteID == "" {
				m.backToTools()
				return m, nil
			}
			if !m.menuFocus && (m.siteID != "" || m.page == "site" || m.page == "logs") {
				return m, m.goPage("sites")
			}
			if !m.menuFocus && m.page == "sites" && m.filter != "" {
				m.filter = ""
				m.selected = 0
				return m, nil
			}
			m.menuFocus = true
			m.navSelected = menuIndex(m.page)
		case "tab", "shift+tab":
			if m.menuFocus {
				return m, m.focusContent()
			}
			m.menuFocus = true
			m.navSelected = menuIndex(m.page)
		case "left":
			m.menuFocus = true
			m.navSelected = menuIndex(m.page)
		case "right":
			if m.menuFocus {
				return m, m.focusContent()
			}
			if m.page == "disk" {
				m.openDiskDirectory()
				return m, nil
			}
		case "up", "k":
			if m.menuFocus {
				return m, m.previewMenu(max(0, m.navSelected-1))
			} else if m.page == "tools" {
				m.toolSelected = max(0, m.toolSelected-1)
			} else if m.page == "sites" {
				m.selected = max(0, m.selected-1)
			} else if m.page == "disk" {
				m.diskSelected = max(0, m.diskSelected-1)
			} else {
				m.offset = max(0, m.offset-1)
			}
		case "down", "j":
			if m.menuFocus {
				return m, m.previewMenu(min(len(menu)-1, m.navSelected+1))
			} else if m.page == "tools" {
				m.toolSelected = min(len(toolMenu)-1, m.toolSelected+1)
			} else if m.page == "sites" {
				m.selected = min(max(0, len(m.filteredSites())-1), m.selected+1)
			} else if m.page == "disk" {
				m.diskSelected = min(max(0, len(m.diskChildren())-1), m.diskSelected+1)
			} else {
				m.offset++
			}
		case "pgup":
			if m.menuFocus {
				return m, m.previewMenu(max(0, m.navSelected-max(1, m.height-10)))
			} else if m.page == "sites" {
				m.selected = max(0, m.selected-max(1, m.height-10))
			} else {
				m.offset = max(0, m.offset-max(1, m.height-10))
			}
		case "pgdown":
			if m.menuFocus {
				return m, m.previewMenu(min(len(menu)-1, m.navSelected+max(1, m.height-10)))
			} else if m.page == "sites" {
				m.selected = min(max(0, len(m.filteredSites())-1), m.selected+max(1, m.height-10))
			} else {
				m.offset += max(1, m.height-10)
			}
		case "home":
			if m.menuFocus {
				return m, m.previewMenu(0)
			} else if m.page == "tools" {
				m.toolSelected = 0
			} else {
				m.offset = 0
				m.selected = 0
			}
		case "end":
			if m.menuFocus {
				return m, m.previewMenu(len(menu) - 1)
			} else if m.page == "tools" {
				m.toolSelected = len(toolMenu) - 1
			} else if m.page == "sites" {
				m.selected = max(0, len(m.filteredSites())-1)
			} else {
				m.offset = max(0, len(wrap(m.liveLines(), m.bodyWidth()))-m.bodyHeight())
			}
		case "enter":
			if m.menuFocus {
				return m, m.focusContent()
			}
			if m.page == "disk" {
				m.openDiskDirectory()
				return m, nil
			}
			if m.page == "tools" {
				return m, m.goPage(toolMenu[m.toolSelected].Page)
			}
			if m.page == "sites" {
				sites := m.filteredSites()
				if len(sites) > 0 {
					m.siteID = sites[min(m.selected, len(sites)-1)].ID
					return m, m.inspectPage("site")
				}
			}
		case "/":
			m.menuFocus = false
			if m.page == "sites" {
				m.inputKind = "filter"
				m.input = m.filter
			} else if m.page == "impact" {
				m.inputKind = "dependency"
				m.input = m.target
			}
		case "space":
			m.notice = ""
			if m.page == "disk" && !m.source.Demo {
				if !m.diskReading {
					m.diskReading = true
					return m, m.diskCmd(true)
				}
				return m, nil
			}
			return m, m.reload()
		case "backspace":
			if m.page == "disk" && !m.menuFocus {
				m.diskPath = path.Dir(m.currentDiskPath())
				m.diskSelected, m.offset = 0, 0
			}
		case "f5":
			if m.source.Live {
				select {
				case m.source.Refresh <- struct{}{}:
				default:
				}
				m.notice = "Requested discovery and public checks on the existing session."
			} else if m.source.Demo {
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
			if !m.menuFocus && m.siteID != "" {
				return m, m.inspectPage("logs")
			}
		case "a":
			if !m.menuFocus && m.siteID != "" {
				return m, m.inspectPage("readiness")
			}
			return m, m.goPage("tools")
		case "b":
			if !m.menuFocus && m.siteID != "" {
				return m, m.inspectPage("recovery")
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
			for _, item := range allViews() {
				if item.Key == key {
					return m, m.goPage(item.Page)
				}
			}
		}
	}
	if m.page != "sites" {
		m.offset = min(m.offset, max(0, len(wrap(m.liveLines(), m.bodyWidth()))-m.bodyHeight()))
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
