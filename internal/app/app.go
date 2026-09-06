package app

import (
	"fmt"
	"strings"
	"time"

	appemr "dev-tool-cli/internal/emr"
	"dev-tool-cli/internal/tunnel"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const statusBarHeight = 1
const emrDetailStepPageSize = 10
const emrMenuIndex = 0
const remoteMenuIndex = 1
const remoteSharePort = "20022"
const localSSHAddr = "127.0.0.1:22"
const localConnectAddr = "127.0.0.1:20022"
const remoteConnectAddr = "127.0.0.1:20022"
const remotePageSize = 10

var menus = []string{"EMR", "Remote", "Help"}

type model struct {
	width              int
	height             int
	activeMenu         int
	status             string
	emrClusters        []appemr.Cluster
	emrLoading         bool
	emrErr             string
	emrPage            int
	emrSelected        int
	emrDetail          emrDetailState
	emrItemDialog      emrItemDialog
	remoteDialog       remoteShareDialog
	remoteDeleteDialog remoteDeleteDialog
	remoteShareLoading bool
	remoteShare        *tunnel.RemoteForward
	remoteForwards     map[int]*tunnel.RemoteForward
	remoteShareRecords []remoteShareRecord
	remoteShareSeq     int
	remotePage         int
	remoteSelected     int
	statusBlink        bool
	remoteShareErr     string
}

type remoteShareDialog struct {
	visible  bool
	action   string
	ip       string
	port     string
	username string
	focus    int
	keys     []string
	keyIndex int
	err      string
}

type remoteDeleteDialog struct {
	visible bool
	id      int
}

type emrDetailState struct {
	visible      bool
	loading      bool
	err          string
	detail       appemr.ClusterDetail
	steps        []appemr.Step
	stepLoading  bool
	stepErr      string
	stepMarker   string
	stepPage     int
	stepSelected int
	activeTab    string
	yarnApps     []appemr.YarnApplication
	yarnLoading  bool
	yarnErr      string
	yarnPage     int
	yarnSelected int
}

type emrItemDialog struct {
	visible bool
	kind    string
	index   int
}

type remoteShareRecord struct {
	ID        int
	Action    string
	User      string
	Host      string
	Port      string
	Key       string
	Remote    string
	Local     string
	StartedAt string
	Status    string
	Error     string
}

type emrClustersLoadedMsg struct {
	clusters []appemr.Cluster
	err      error
}

type emrClusterDetailLoadedMsg struct {
	detail appemr.ClusterDetail
	err    error
}

type emrStepsLoadedMsg struct {
	page appemr.StepPage
	err  error
}

type yarnAppsLoadedMsg struct {
	apps []appemr.YarnApplication
	err  error
}

type sshKeysLoadedMsg struct {
	keys []string
	err  error
}

type remoteShareStartedMsg struct {
	id      int
	forward *tunnel.RemoteForward
	err     error
}

type remoteForwardEventMsg struct {
	id    int
	event tunnel.ForwardEvent
}

type blinkStatusMsg struct{}

func NewModel() tea.Model {
	return model{}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.remoteDialog.visible {
			return m.updateRemoteDialog(msg)
		}
		if m.remoteDeleteDialog.visible {
			return m.updateRemoteDeleteDialog(msg)
		}
		if m.emrItemDialog.visible {
			switch msg.String() {
			case "ctrl+c", "ctrl+d", "q":
				return m, tea.Quit
			case "esc":
				m.emrItemDialog.visible = false
			}
			return m, nil
		}
		if m.emrDetail.visible {
			switch msg.String() {
			case "ctrl+c", "ctrl+d", "q":
				return m, tea.Quit
			case "esc":
				m.emrDetail.visible = false
				return m, nil
			case "enter":
				if m.openSelectedEMRItemDialog() {
					return m, nil
				}
			case "tab":
				sections := []string{"overview", "steps", "yarn", "instances"}
				current := 0
				for i, section := range sections {
					if section == m.emrDetail.activeTab {
						current = i
						break
					}
				}
				m.emrDetail.activeTab = sections[(current+1)%len(sections)]
				if m.emrDetail.activeTab == "yarn" && m.emrDetail.yarnApps == nil && !m.emrDetail.yarnLoading {
					m.emrDetail.yarnLoading = true
					m.emrDetail.yarnErr = ""
					return m, loadYarnApps(m.emrDetail.detail.PrimaryNodePrivateDNS)
				}
				return m, nil
			case "p":
				if m.emrDetail.activeTab == "yarn" {
					if m.emrDetail.yarnPage > 0 {
						m.emrDetail.yarnPage--
						m.emrDetail.yarnSelected = m.emrDetail.yarnPage * emrDetailStepPageSize
					}
				} else if m.emrDetail.activeTab == "steps" && m.emrDetail.stepPage > 0 {
					m.emrDetail.stepPage--
					m.emrDetail.stepSelected = m.emrDetail.stepPage * emrDetailStepPageSize
				}
				return m, nil
			case "n":
				if m.emrDetail.activeTab == "yarn" {
					if m.emrDetail.yarnPage < m.emrDetailYarnMaxPage() {
						m.emrDetail.yarnPage++
						m.emrDetail.yarnSelected = m.emrDetail.yarnPage * emrDetailStepPageSize
					}
				} else if m.emrDetail.activeTab == "steps" {
					if m.emrDetail.stepPage < m.emrDetailMaxStepPage() {
						m.emrDetail.stepPage++
						m.emrDetail.stepSelected = m.emrDetail.stepPage * emrDetailStepPageSize
					} else if m.emrDetail.stepMarker != "" && !m.emrDetail.stepLoading {
						m.emrDetail.stepLoading = true
						m.emrDetail.stepErr = ""
						return m, loadEMRSteps(m.emrDetail.detail.ID, m.emrDetail.stepMarker)
					}
				}
				return m, nil
			case "up":
				if m.emrDetail.activeTab == "yarn" {
					if m.emrDetail.yarnSelected > 0 {
						m.emrDetail.yarnSelected--
						m.emrDetail.yarnPage = m.emrDetail.yarnSelected / emrDetailStepPageSize
					}
				} else if m.emrDetail.activeTab == "steps" && m.emrDetail.stepSelected > 0 {
					m.emrDetail.stepSelected--
					m.emrDetail.stepPage = m.emrDetail.stepSelected / emrDetailStepPageSize
				}
				return m, nil
			case "down":
				if m.emrDetail.activeTab == "yarn" {
					if m.emrDetail.yarnSelected < len(m.emrDetail.yarnApps)-1 {
						m.emrDetail.yarnSelected++
						m.emrDetail.yarnPage = m.emrDetail.yarnSelected / emrDetailStepPageSize
					}
				} else if m.emrDetail.activeTab == "steps" {
					if m.emrDetail.stepSelected < len(m.emrDetail.steps)-1 {
						m.emrDetail.stepSelected++
						m.emrDetail.stepPage = m.emrDetail.stepSelected / emrDetailStepPageSize
					} else if m.emrDetail.stepMarker != "" && !m.emrDetail.stepLoading {
						m.emrDetail.stepLoading = true
						m.emrDetail.stepErr = ""
						return m, loadEMRSteps(m.emrDetail.detail.ID, m.emrDetail.stepMarker)
					}
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+c", "ctrl+d":
			return m, tea.Quit
		case "q":
			return m, tea.Quit
		case "r":
			if m.activeMenu == emrMenuIndex {
				m.emrLoading = true
				m.emrErr = ""
				m.status = "Loading EMR clusters..."
				return m, loadEMRClusters()
			}
			m.status = fmt.Sprintf("Refreshed %s", menus[m.activeMenu])
		case "p":
			if m.activeMenu == emrMenuIndex && m.emrPage > 0 {
				m.emrPage--
				m.emrSelected = m.emrPage * m.emrPageSize()
			} else if m.activeMenu == remoteMenuIndex && m.remotePage > 0 {
				m.remotePage--
				m.remoteSelected = m.remotePage * remotePageSize
			}
		case "n":
			if m.activeMenu == emrMenuIndex && m.emrPage < m.emrMaxPage() {
				m.emrPage++
				m.emrSelected = m.emrPage * m.emrPageSize()
			} else if m.activeMenu == remoteMenuIndex && m.remotePage < m.remoteMaxPage() {
				m.remotePage++
				m.remoteSelected = m.remotePage * remotePageSize
			}
		case "up":
			if m.activeMenu == emrMenuIndex && m.emrSelected > 0 {
				m.emrSelected--
				m.emrPage = m.emrSelected / m.emrPageSize()
			} else if m.activeMenu == remoteMenuIndex && m.remoteSelected > 0 {
				m.remoteSelected--
				m.remotePage = m.remoteSelected / remotePageSize
			}
		case "down":
			if m.activeMenu == emrMenuIndex && m.emrSelected < len(m.emrClusters)-1 {
				m.emrSelected++
				m.emrPage = m.emrSelected / m.emrPageSize()
			} else if m.activeMenu == remoteMenuIndex && m.remoteSelected < len(m.remoteShareRecords)-1 {
				m.remoteSelected++
				m.remotePage = m.remoteSelected / remotePageSize
			}
		case "enter":
			if m.activeMenu == emrMenuIndex && len(m.emrClusters) > 0 {
				cluster := m.emrClusters[m.emrSelected]
				m.emrDetail = emrDetailState{visible: true, loading: true}
				m.status = "Loading EMR cluster detail..."
				return m, loadEMRClusterDetail(cluster.ID)
			}
		case "tab":
			m.activeMenu = (m.activeMenu + 1) % len(menus)
			m.status = fmt.Sprintf("Switched to %s", menus[m.activeMenu])
		case "shift+tab":
			m.activeMenu = (m.activeMenu - 1 + len(menus)) % len(menus)
			m.status = fmt.Sprintf("Switched to %s", menus[m.activeMenu])
		case "esc":
			m.status = ""
		case "s":
			if m.activeMenu == remoteMenuIndex {
				m.remoteDialog = remoteShareDialog{visible: true, action: "share", port: "22", username: "hadoop"}
				m.status = "Loading SSH private keys..."
				return m, loadSSHKeys()
			}
		case "c":
			if m.activeMenu == remoteMenuIndex {
				m.remoteDialog = remoteShareDialog{visible: true, action: "connect", port: "22", username: "hadoop"}
				m.status = "Loading SSH private keys..."
				return m, loadSSHKeys()
			}
		case "d":
			if m.activeMenu == remoteMenuIndex && len(m.remoteShareRecords) > 0 {
				m.remoteDeleteDialog = remoteDeleteDialog{
					visible: true,
					id:      m.remoteShareRecords[m.remoteSelected].ID,
				}
			}
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.remoteDeleteDialog.visible {
				return m.updateRemoteDeleteDialogMouse(msg)
			}
			if m.remoteDialog.visible {
				return m.updateRemoteDialogMouse(msg)
			}
			if m.emrItemDialog.visible {
				return m.updateEMRItemDialogMouse(msg)
			}
			if m.emrDetail.visible {
				return m.updateEMRDetailMouse(msg)
			}

			if msg.Y == 0 {
				if index, ok := menuIndexAt(msg.X); ok {
					m.activeMenu = index
					m.status = fmt.Sprintf("Switched to %s", menus[m.activeMenu])
				}
				return m, nil
			}

			if m.activeMenu == emrMenuIndex {
				if index, ok := m.emrRowIndexAtMouse(msg.Y); ok {
					m.emrSelected = index
					cluster := m.emrClusters[index]
					m.emrDetail = emrDetailState{visible: true, loading: true}
					m.status = "Loading EMR cluster detail..."
					return m, loadEMRClusterDetail(cluster.ID)
				}
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case emrClustersLoadedMsg:
		m.emrLoading = false
		if msg.err != nil {
			m.emrErr = msg.err.Error()
			m.status = "Failed to load EMR clusters"
			break
		}

		m.emrClusters = msg.clusters
		m.emrPage = 0
		m.emrSelected = 0
		m.emrErr = ""
		m.status = fmt.Sprintf("Loaded %d running EMR clusters", len(msg.clusters))
	case emrClusterDetailLoadedMsg:
		m.emrDetail.loading = false
		if msg.err != nil {
			m.emrDetail.err = msg.err.Error()
			m.status = "Failed to load EMR cluster detail"
			break
		}

		m.emrDetail.detail = msg.detail
		m.emrDetail.steps = nil
		m.emrDetail.stepLoading = true
		m.emrDetail.stepErr = ""
		m.emrDetail.stepMarker = ""
		m.emrDetail.stepPage = 0
		m.emrDetail.stepSelected = 0
		m.emrDetail.activeTab = "overview"
		m.emrDetail.yarnApps = nil
		m.emrDetail.yarnSelected = 0
		m.emrDetail.yarnPage = 0
		m.emrDetail.err = ""
		m.status = "Loaded EMR cluster detail"
		return m, tea.Batch(loadEMRSteps(msg.detail.ID, ""), loadYarnApps(msg.detail.PrimaryNodePrivateDNS))
	case emrStepsLoadedMsg:
		m.emrDetail.stepLoading = false
		if msg.err != nil {
			m.emrDetail.stepErr = msg.err.Error()
			m.status = "Failed to load EMR steps"
			break
		}

		m.emrDetail.steps = append(m.emrDetail.steps, msg.page.Steps...)
		m.emrDetail.stepMarker = msg.page.NextMarker
		m.emrDetail.stepErr = ""
		m.status = fmt.Sprintf("Loaded %d EMR steps", len(m.emrDetail.steps))
		if hasRunningStep(m.emrDetail.steps) {
			return m, blinkRemoteStatus()
		}
	case yarnAppsLoadedMsg:
		m.emrDetail.yarnLoading = false
		if msg.err != nil {
			m.emrDetail.yarnErr = msg.err.Error()
			m.status = "Failed to load YARN applications"
			break
		}
		m.emrDetail.yarnApps = msg.apps
		m.emrDetail.yarnErr = ""
		m.status = fmt.Sprintf("Loaded %d YARN applications", len(msg.apps))
	case sshKeysLoadedMsg:
		if msg.err != nil {
			m.remoteDialog.err = msg.err.Error()
			m.status = "Failed to load SSH private keys"
			break
		}

		m.remoteDialog.keys = msg.keys
		m.remoteDialog.keyIndex = 0
		m.status = "Fill share dialog and confirm"
	case remoteShareStartedMsg:
		m.remoteShareLoading = false
		if msg.err != nil {
			m.remoteShareErr = msg.err.Error()
			m.updateRemoteShareRecord(msg.id, "failed", msg.err.Error())
			m.status = "Failed to start remote share"
			break
		}

		if m.remoteShare != nil {
			m.remoteShare.Close()
		}
		m.remoteShare = msg.forward
		if m.remoteForwards == nil {
			m.remoteForwards = make(map[int]*tunnel.RemoteForward)
		}
		m.remoteForwards[msg.id] = msg.forward
		m.updateRemoteShareRecord(msg.id, "connected", "")
		m.remoteShareErr = ""
		m.status = "Remote tunnel connected"
		return m, waitRemoteForwardEvent(msg.id, msg.forward)
	case remoteForwardEventMsg:
		switch msg.event.Status {
		case tunnel.ForwardStatusConnected:
			m.updateRemoteShareRecord(msg.id, "connected", "")
			m.status = "Remote tunnel reconnected"
		case tunnel.ForwardStatusReconnecting:
			errText := ""
			if msg.event.Err != nil {
				errText = msg.event.Err.Error()
			}
			m.updateRemoteShareRecord(msg.id, "reconnecting", errText)
			m.status = "Remote tunnel reconnecting"
		case tunnel.ForwardStatusClosed:
			m.updateRemoteShareRecord(msg.id, "closed", "")
			return m, nil
		}
		if forward := m.remoteForwards[msg.id]; forward != nil {
			return m, waitRemoteForwardEvent(msg.id, forward)
		}
	case blinkStatusMsg:
		m.statusBlink = !m.statusBlink
		if m.hasActiveBlinkingRemoteShare() || (m.emrDetail.visible && hasRunningStep(m.emrDetail.steps)) {
			return m, blinkRemoteStatus()
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing dev-tool-cli...\n"
	}

	displayHeight := max(m.height-1-statusBarHeight, 0)

	if m.emrDetail.visible {
		view := strings.Join([]string{
			m.renderEMRDetailPage(displayHeight),
			m.renderStatusBar(),
		}, "\n")
		if m.emrItemDialog.visible {
			return m.renderEMRItemDialog(view)
		}
		return view
	}

	view := strings.Join([]string{
		m.renderMenuBar(),
		m.renderDisplay(displayHeight),
		m.renderStatusBar(),
	}, "\n")

	if m.remoteDialog.visible {
		return m.renderRemoteDialog(view)
	}
	if m.remoteDeleteDialog.visible {
		return m.renderRemoteDeleteDialog(view)
	}

	return view
}

func (m model) renderMenuBar() string {
	menuItems := make([]string, 0, len(menus))
	for i, menu := range menus {
		style := lipgloss.NewStyle().Padding(0, 1)
		if i == m.activeMenu {
			style = style.Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
		}
		menuItems = append(menuItems, style.Render(menu))
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Render(strings.Join(menuItems, ""))
}

func (m model) renderDisplay(height int) string {
	if m.activeMenu == emrMenuIndex {
		return m.renderEMRDisplay(height)
	}
	if m.activeMenu == remoteMenuIndex {
		return m.renderRemoteDisplay(height)
	}

	title := fmt.Sprintf("%s Panel", menus[m.activeMenu])
	body := "Use Tab / Shift+Tab or click the top bar to switch menus."
	if m.status != "" {
		body += "\n\n" + m.status
	}

	content := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Render(title + "\n\n" + body)

	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m model) renderStatusBar() string {
	text := " r 刷新  q 退出"
	switch m.activeMenu {
	case emrMenuIndex:
		text = " r 刷新  ↑/↓ 选择  p 前一页  n 下一页  Enter 详情  q 退出"
	case remoteMenuIndex:
		text = " s 分享  c 连接  ↑/↓ 选择  p 前一页  n 下一页  d 删除  q 退出"
	}
	if m.remoteDeleteDialog.visible {
		text = " 确认<Enter>  取消<Esc>  q 退出"
	}
	if m.emrDetail.visible {
		text = " Tab 切换 section  ↑/↓ 选择  p 前一页  n 下一页  Esc 返回  q 退出"
	}
	if m.emrItemDialog.visible {
		text = " 返回<Esc>  q 退出"
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("238")).
		Render(text)
}

func menuIndexAt(x int) (int, bool) {
	offset := 0
	for i, menu := range menus {
		width := len(menu) + 2
		if x >= offset && x < offset+width {
			return i, true
		}
		offset += width
	}

	return 0, false
}

func (m model) emrRowIndexAtMouse(y int) (int, bool) {
	if y < 6 {
		return 0, false
	}

	pageSize := m.emrPageSize()
	index := m.emrPage*pageSize + (y - 6)
	if index < 0 || index >= len(m.emrClusters) || y >= 6+pageSize {
		return 0, false
	}

	return index, true
}

func selectedRowStyle(row string, width int) string {
	if width <= 0 {
		width = lipgloss.Width(row)
	}
	return lipgloss.NewStyle().
		Width(width).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("62")).
		Render(row)
}

func formatShareRow(action, userName, host, port, key, remote, local, startedAt, status string) string {
	return fmt.Sprintf(
		"%-8s  %-12s  %-18s  %-6s  %-18s  %-18s  %-16s  %-19s  %s",
		truncate(action, 8),
		truncate(userName, 12),
		truncate(host, 18),
		truncate(port, 6),
		truncate(key, 18),
		truncate(remote, 18),
		truncate(local, 16),
		truncate(startedAt, 19),
		status,
	)
}

func remoteDialogTitle(action string) string {
	if action == "connect" {
		return "Connect Remote SSH"
	}

	return "Share Local SSH"
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return string(runes[:len(runes)-1])
}

func blinkRemoteStatus() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return blinkStatusMsg{}
	})
}

func waitRemoteForwardEvent(id int, forward *tunnel.RemoteForward) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-forward.Events
		if !ok {
			return remoteForwardEventMsg{
				id: id,
				event: tunnel.ForwardEvent{
					Status: tunnel.ForwardStatusClosed,
				},
			}
		}

		return remoteForwardEventMsg{id: id, event: event}
	}
}
