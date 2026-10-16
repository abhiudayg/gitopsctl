package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type viewState int

const (
	appsView viewState = iota
	clustersView
	logsView
)

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	state         viewState
	apps          []AppResponse
	clusters      []ClusterResponse
	appCursor     int
	clusterCursor int
	spinner       spinner.Model
	width, height int
	loading       bool
	err           error
	client        *apiClient
	ctx           context.Context
	cancel        context.CancelFunc
	confirmMsg    string
	confirmAction func()
	statusMsg     string
	statusUntil   time.Time
	filter        string
	isFiltering   bool
	history       []Event
	retryCount    int
}

func NewModel(apiURL, token string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(accent)

	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		loading: true,
		spinner: s,
		client:  newAPIClient(apiURL, token),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// ── Messages ──────────────────────────────────────────────────────────────────

type (
	appsLoadedMsg     []AppResponse
	clustersLoadedMsg []ClusterResponse
	historyLoadedMsg  []Event
	errorMsg          error
	reconnectMsg      struct{}
)

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.fetchApps(),
		m.fetchClusters(),
		m.fetchHistory(),
		m.client.listenForEvents(m.ctx),
	)
}

func (m Model) fetchApps() tea.Cmd {
	return func() tea.Msg {
		apps, err := m.client.getApplications()
		if err != nil {
			return errorMsg(err)
		}
		return appsLoadedMsg(apps)
	}
}

func (m Model) fetchClusters() tea.Cmd {
	return func() tea.Msg {
		clusters, err := m.client.getClusters()
		if err != nil {
			return errorMsg(err)
		}
		return clustersLoadedMsg(clusters)
	}
}

func (m Model) fetchHistory() tea.Cmd {
	return func() tea.Msg {
		history, err := m.client.getHistory()
		if err != nil {
			return errorMsg(err)
		}
		return historyLoadedMsg(history)
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Clear transient status message
	if !m.statusUntil.IsZero() && time.Now().After(m.statusUntil) {
		m.statusMsg = ""
		m.statusUntil = time.Time{}
	}

	switch msg := msg.(type) {
	case appsLoadedMsg:
		m.retryCount = 0
		m.loading = false
		m.err = nil
		m.apps = msg
		if m.appCursor >= len(m.apps) {
			m.appCursor = max(0, len(m.apps)-1)
		}

	case clustersLoadedMsg:
		m.retryCount = 0
		m.loading = false
		m.err = nil
		m.clusters = msg
		if m.clusterCursor >= len(m.clusters) {
			m.clusterCursor = max(0, len(m.clusters)-1)
		}

	case errorMsg:
		m.loading = false
		m.err = msg
		// If it's a connection error, schedule a retry
		errStr := msg.Error()
		if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "no such host") || strings.Contains(errStr, "EOF") {
			m.retryCount++
			backoff := time.Duration(min(m.retryCount, 10)) * time.Second
			return m, tea.Tick(backoff, func(t time.Time) tea.Msg {
				return reconnectMsg{}
			})
		}

	case reconnectMsg:
		m.loading = true
		return m, m.Init()

	case historyLoadedMsg:
		m.retryCount = 0 // Reset on successful event or data
		m.loading = false
		m.err = nil
		m.history = msg

	case eventReceivedMsg:
		if msg.Event.ID != "" {
			m.history = append(m.history, msg.Event)
			if len(m.history) > 100 {
				m.history = m.history[1:]
			}
		}
		// Throttle refetches or just fetch status. For now, just continue listening.
		cmds = append(cmds, m.client.listenForEvents(m.ctx))
		// We could fetch apps/clusters here, but let's see if this stops the flood.
		// Actually, let's keep the fetches but we need a better streaming model later.
		cmds = append(cmds, m.fetchApps(), m.fetchClusters())

	case sseDisconnectedMsg:
		// Add a 1s backoff to avoid tight reconnection loops
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return reconnectMsg{}
		})

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		if m.confirmMsg != "" {
			switch msg.String() {
			case "y", "Y":
				if m.confirmAction != nil {
					m.confirmAction()
					m.statusMsg = "✓ Action dispatched"
					m.statusUntil = time.Now().Add(3 * time.Second)
				}
				m.confirmMsg = ""
				m.confirmAction = nil
			case "n", "N", "esc":
				m.confirmMsg = ""
				m.confirmAction = nil
			}
			return m, tea.Batch(cmds...)
		}

		if m.isFiltering {
			switch msg.String() {
			case "esc":
				m.isFiltering = false
				m.filter = ""
				return m, nil
			case "enter":
				m.isFiltering = false
				return m, nil
			case "backspace":
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
					m.appCursor = 0
					m.clusterCursor = 0
				}
				return m, nil
			case "up", "k":
				// If it's "k" and we are filtering, should it be a character or navigation?
				// Usually in TUIs, if you are in a "search mode", arrow keys navigate,
				// but 'k' might be a character.
				// Let's stick to arrow keys for navigation during filtering for now
				// to avoid ambiguity, OR check if it's a rune.
				if msg.Type == tea.KeyUp {
					if m.state == appsView && m.appCursor > 0 {
						m.appCursor--
					} else if m.state == clustersView && m.clusterCursor > 0 {
						m.clusterCursor--
					}
					return m, nil
				}
			case "down", "j":
				if msg.Type == tea.KeyDown {
					if m.state == appsView && m.appCursor < len(m.filteredApps())-1 {
						m.appCursor++
					} else if m.state == clustersView && m.clusterCursor < len(m.filteredClusters())-1 {
						m.clusterCursor++
					}
					return m, nil
				}
			}

			// Handle character input for filter
			if len(msg.String()) == 1 && msg.Type == tea.KeyRunes {
				m.filter += msg.String()
				m.appCursor = 0
				m.clusterCursor = 0
				return m, nil
			}
		}

		switch msg.String() {
		case "/":
			m.isFiltering = true
			m.filter = ""
			m.appCursor = 0
			m.clusterCursor = 0
		case "esc":
			m.filter = ""
		case "ctrl+c", "q":
			m.cancel()
			return m, tea.Quit
		case "tab", "shift+tab":
			if !m.isFiltering {
				switch m.state {
				case appsView:
					m.state = clustersView
				case clustersView:
					m.state = appsView
				default:
					m.state = appsView
				}
				m.appCursor = 0
				m.clusterCursor = 0
			}
		case "l":
			if !m.isFiltering {
				if m.state == logsView {
					m.state = appsView
				} else {
					m.state = logsView
				}
			}
		case "up", "k":
			if m.state == appsView && m.appCursor > 0 {
				m.appCursor--
			} else if m.state == clustersView && m.clusterCursor > 0 {
				m.clusterCursor--
			}
		case "down", "j":
			if m.state == appsView && m.appCursor < len(m.filteredApps())-1 {
				m.appCursor++
			} else if m.state == clustersView && m.clusterCursor < len(m.filteredClusters())-1 {
				m.clusterCursor++
			}
		case "r":
			m.loading = true
			cmds = append(cmds, m.fetchApps(), m.fetchClusters())
		case "s":
			if m.state == appsView && len(m.filteredApps()) > 0 {
				name := m.filteredApps()[m.appCursor].Name
				m.confirmMsg = fmt.Sprintf("Sync  %q ?  (y/n)", name)
				m.confirmAction = func() { _ = m.client.syncApp(name) }
			}
		case "a":
			if m.state == appsView && len(m.filteredApps()) > 0 {
				app := m.filteredApps()[m.appCursor]
				m.confirmMsg = fmt.Sprintf("Approve sync for %q ?  (y/n)", app.Name)
				hash := app.LatestGitHash
				if hash == "" {
					hash = "manual-override"
				}
				m.confirmAction = func() { _ = m.client.approveApp(app.Name, hash) }
			}
		case "c":
			if m.state == clustersView && len(m.filteredClusters()) > 0 {
				name := m.filteredClusters()[m.clusterCursor].Name
				m.confirmMsg = fmt.Sprintf("Health-check  %q ?  (y/n)", name)
				m.confirmAction = func() { _ = m.client.checkCluster(name) }
			}
		case "u":
			if m.state == appsView && len(m.filteredApps()) > 0 {
				name := m.filteredApps()[m.appCursor].Name
				m.confirmMsg = fmt.Sprintf("Unregister application  %q ?  (y/n)", name)
				m.confirmAction = func() { _ = m.client.unregisterApp(name) }
			} else if m.state == clustersView && len(m.filteredClusters()) > 0 {
				name := m.filteredClusters()[m.clusterCursor].Name
				m.confirmMsg = fmt.Sprintf("Unregister cluster  %q ?  (y/n)", name)
				m.confirmAction = func() { _ = m.client.unregisterCluster(name) }
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, tea.Batch(cmds...)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	var out strings.Builder

	// ── Header ──────────────────────────────────────────────────────────────
	logo := HeaderStyle.Render("⎈ GitOpsCTL")
	spin := ""
	if m.loading {
		spin = "  " + m.spinner.View()
	}
	status := ""
	if m.statusMsg != "" {
		status = "  " + ChipSynced.Render(m.statusMsg)
	}
	header := lipgloss.JoinHorizontal(lipgloss.Bottom,
		logo, spin, status,
		lipgloss.NewStyle().Foreground(subtle).Render(
			lipgloss.PlaceHorizontal(m.width-lipgloss.Width(logo)-lipgloss.Width(spin)-lipgloss.Width(status)-4, lipgloss.Right,
				VersionStyle.Render("gitopsctl · dashboard"),
			),
		),
	)
	out.WriteString(header + "\n")

	// ── Tab bar ─────────────────────────────────────────────────────────────
	appsTab := InactiveTab.Render("Applications")
	clTab := InactiveTab.Render("Clusters")
	logsTab := InactiveTab.Render("Activity")
	switch m.state {
	case appsView:
		appsTab = ActiveTab.Render("Applications")
	case clustersView:
		clTab = ActiveTab.Render("Clusters")
	default:
		logsTab = ActiveTab.Render("Activity")
	}
	out.WriteString(TabBar.Width(m.width-2).Render(appsTab+clTab+logsTab) + "\n\n")

	// ── Error / Confirm ─────────────────────────────────────────────────────
	if m.err != nil {
		errStr := m.err.Error()
		if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "no such host") {
			retryText := ""
			if m.retryCount > 0 {
				retryText = fmt.Sprintf(" (Retry #%d)", m.retryCount)
			}
			out.WriteString("  " + OfflineBanner.Render("OFFLINE") + " " + ErrStyle.Render("Controller API at "+m.client.baseURL+" is unreachable"+retryText) + "\n\n")
		} else {
			out.WriteString(ErrStyle.Render("  ✗ "+errStr) + "\n\n")
		}
	}
	if m.confirmMsg != "" {
		out.WriteString(ConfirmStyle.Render("  ? "+m.confirmMsg) + "\n\n")
	}

	// ── Main columns ────────────────────────────────────────────────────────
	listW := m.width / 2
	detailW := m.width - listW - 4
	listH := m.height - 10 // rows below header/tabs/help

	if m.state == logsView {
		out.WriteString(ListPanelActive.Width(m.width-2).Height(listH).Render(m.renderLogStream(m.width-4, listH)) + "\n")
	} else {
		var listContent, detailContent string
		if m.state == appsView {
			listContent = m.renderAppList(listW-4, listH)
			detailContent = m.renderAppDetail(detailW - 4)
		} else {
			listContent = m.renderClusterList(listW-4, listH)
			detailContent = m.renderClusterDetail(detailW - 4)
		}

		leftPanel := ListPanelActive.Width(listW - 2).Height(listH).Render(listContent)
		rightPanel := DetailPanel.Width(detailW - 2).Height(listH).Render(detailContent)
		columns := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)
		out.WriteString(columns + "\n")
	}

	// ── Search bar ──────────────────────────────────────────────────────────
	if m.isFiltering {
		searchBar := lipgloss.NewStyle().
			Foreground(accent).
			Padding(0, 1).
			Render("FILTER: " + m.filter + "█")
		out.WriteString(searchBar + "\n")
	} else if m.filter != "" {
		searchBar := lipgloss.NewStyle().
			Foreground(subtle).
			Padding(0, 1).
			Render("FILTER: " + m.filter + " (press Esc to clear)")
		out.WriteString(searchBar + "\n")
	} else {
		out.WriteString("\n")
	}

	// ── Help bar ────────────────────────────────────────────────────────────
	out.WriteString(m.renderHelp())

	return out.String()
}

// ── List renderers ────────────────────────────────────────────────────────────

func (m Model) renderAppList(_, h int) string {
	apps := m.filteredApps()
	if len(apps) == 0 {
		return ItemMeta.Render("\n  No applications found.")
	}
	maxVisible := max(1, h-2)
	start := 0
	if m.appCursor >= maxVisible {
		start = m.appCursor - maxVisible + 1
	}
	var rows []string
	for i := start; i < len(apps) && i < start+maxVisible; i++ {
		a := apps[i]
		cl := a.ClusterName
		if cl == "" {
			cl = "—"
		}
		name := ItemName.Render(a.Name)
		meta := ItemMeta.Render(fmt.Sprintf("  %s · %s", cl, a.Interval))
		chip := StatusChip(a.Status)
		line := fmt.Sprintf("%s  %s\n%s", name, chip, meta)
		if i == m.appCursor {
			line = lipgloss.NewStyle().
				Foreground(accent).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(accent).
				PaddingLeft(1).
				Render(line)
		} else {
			line = lipgloss.NewStyle().PaddingLeft(2).Render(line)
		}
		rows = append(rows, line)
	}
	return strings.Join(rows, "\n")
}

func (m Model) renderClusterList(_, h int) string {
	clusters := m.filteredClusters()
	if len(clusters) == 0 {
		return ItemMeta.Render("\n  No clusters found.")
	}
	maxVisible := max(1, h-2)
	start := 0
	if m.clusterCursor >= maxVisible {
		start = m.clusterCursor - maxVisible + 1
	}
	var rows []string
	for i := start; i < len(clusters) && i < start+maxVisible; i++ {
		c := clusters[i]
		name := ItemName.Render(c.Name)
		chip := StatusChip(c.Status)
		reg := ItemMeta.Render("  Registered " + c.RegisteredAt.Format("02 Jan 15:04"))
		line := fmt.Sprintf("%s  %s\n%s", name, chip, reg)
		if i == m.clusterCursor {
			line = lipgloss.NewStyle().
				Foreground(accent).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(accent).
				PaddingLeft(1).
				Render(line)
		} else {
			line = lipgloss.NewStyle().PaddingLeft(2).Render(line)
		}
		rows = append(rows, line)
	}
	return strings.Join(rows, "\n")
}

// ── Detail renderers ──────────────────────────────────────────────────────────

func kv(label, value string) string {
	return DetailLabel.Render(label) + DetailValue.Render(value)
}

func (m Model) renderAppDetail(w int) string {
	apps := m.filteredApps()
	if len(apps) == 0 {
		return lipgloss.NewStyle().Foreground(subtle).Render("No matching applications")
	}
	a := apps[m.appCursor]

	hash := a.LastSyncedGitHash
	if len(hash) > 7 {
		hash = hash[:7]
	}
	if hash == "" {
		hash = "—"
	}
	cl := a.ClusterName
	if cl == "" {
		cl = "—"
	}
	failures := fmt.Sprintf("%d", a.ConsecutiveFailures)
	msg := a.Message
	if msg == "" {
		msg = "—"
	}
	if len(msg) > w-20 && w > 20 {
		msg = msg[:w-20] + "…"
	}

	var b strings.Builder
	b.WriteString(DetailTitle.Render(a.Name) + "\n")
	b.WriteString(StatusChip(a.Status) + "\n\n")
	b.WriteString(kv("Repo", a.RepoURL) + "\n")
	b.WriteString(kv("Branch", a.Branch) + "\n")
	b.WriteString(kv("Path", a.Path) + "\n")
	b.WriteString(kv("Cluster", cl) + "\n")
	b.WriteString(kv("Interval", a.Interval) + "\n")
	b.WriteString(kv("Last Synced", hash) + "\n")
	if a.SyncPolicy == "manual" {
		apprv := a.ApprovedGitHash
		if len(apprv) > 7 {
			apprv = apprv[:7]
		}
		if apprv == "" {
			apprv = "none"
		}
		b.WriteString(kv("Policy", "manual (approved: "+apprv+")") + "\n")
	} else {
		b.WriteString(kv("Policy", "auto") + "\n")
	}
	b.WriteString(kv("Failures", failures) + "\n")
	b.WriteString("\n" + DetailLabel.Render("Message") + "\n")
	b.WriteString(DetailValue.Italic(true).Render(msg))
	return b.String()
}

func (m Model) renderClusterDetail(_ int) string {
	clusters := m.filteredClusters()
	if len(clusters) == 0 {
		return lipgloss.NewStyle().Foreground(subtle).Render("No matching clusters")
	}
	c := clusters[m.clusterCursor]

	reg := c.RegisteredAt.Format("02 Jan 2006 15:04")
	checked := "—"
	if !c.LastCheckedAt.IsZero() {
		checked = c.LastCheckedAt.Format("02 Jan 2006 15:04")
	}
	msg := c.Message
	if msg == "" {
		msg = "—"
	}

	var b strings.Builder
	b.WriteString(DetailTitle.Render(c.Name) + "\n")
	b.WriteString(StatusChip(c.Status) + "\n\n")
	b.WriteString(kv("Kubeconfig", c.KubeconfigPath) + "\n")
	b.WriteString(kv("Registered", reg) + "\n")
	b.WriteString(kv("Last Checked", checked) + "\n")
	b.WriteString("\n" + DetailLabel.Render("Message") + "\n")
	b.WriteString(DetailValue.Italic(true).Render(msg))
	return b.String()
}

func (m Model) renderLogStream(_, h int) string {
	if len(m.history) == 0 {
		return ItemMeta.Render("\n  No activity recorded yet.")
	}
	var rows []string
	start := 0
	if len(m.history) > h {
		start = len(m.history) - h
	}
	for i := start; i < len(m.history); i++ {
		ev := m.history[i]
		ts := ev.Time.Format("15:04:05")
		typ := strings.TrimPrefix(ev.Type, "io.gitopsctl.")

		color := subtle
		if strings.Contains(typ, "failed") || strings.Contains(typ, "error") {
			color = lipgloss.Color("#FF5F87") // pink/red
		} else if strings.Contains(typ, "succeeded") {
			color = lipgloss.Color("#87D787") // light green
		}

		line := fmt.Sprintf("%s %s %s",
			lipgloss.NewStyle().Foreground(subtle).Render(ts),
			lipgloss.NewStyle().Foreground(color).Bold(true).Width(25).Render(typ),
			lipgloss.NewStyle().Foreground(accent).Render(fmt.Sprintf("%v", ev.Data)),
		)
		rows = append(rows, line)
	}
	return strings.Join(rows, "\n")
}

// ── Help ──────────────────────────────────────────────────────────────────────

func (m Model) renderHelp() string {
	type binding struct{ key, desc string }
	bindings := []binding{
		{"↑/↓", "navigate"},
		{"tab", "switch view"},
		{"l", "activity"},
		{"r", "refresh"},
	}
	if m.state == appsView {
		bindings = append(bindings, binding{"s", "sync"}, binding{"a", "approve"}, binding{"u", "unregister"})
	} else {
		bindings = append(bindings, binding{"c", "check"}, binding{"u", "unregister"})
	}
	bindings = append(bindings, binding{"/", "filter"}, binding{"q", "quit"})

	var parts []string
	for _, b := range bindings {
		parts = append(parts, HelpKey.Render(b.key)+" "+HelpDesc.Render(b.desc))
	}
	return HelpSep.Render("  ") + strings.Join(parts, HelpSep.Render("  ·  "))
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (m Model) filteredApps() []AppResponse {
	if m.filter == "" {
		return m.apps
	}
	var filtered []AppResponse
	for _, a := range m.apps {
		if strings.Contains(strings.ToLower(a.Name), strings.ToLower(m.filter)) ||
			strings.Contains(strings.ToLower(a.RepoURL), strings.ToLower(m.filter)) {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func (m Model) filteredClusters() []ClusterResponse {
	if m.filter == "" {
		return m.clusters
	}
	var filtered []ClusterResponse
	for _, c := range m.clusters {
		if strings.Contains(strings.ToLower(c.Name), strings.ToLower(m.filter)) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// ── Run ───────────────────────────────────────────────────────────────────────

func Run(apiURL, token string) error {
	p := tea.NewProgram(NewModel(apiURL, token), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
