package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appemr "dev-tool-cli/internal/emr"
	"dev-tool-cli/internal/tunnel"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const statusBarHeight = 1
const emrDetailStepPageSize = 10
const emrMenuIndex = 0
const remoteMenuIndex = 1
const remoteSharePort = "20022"
const localSSHAddr = "127.0.0.1:22"
const localConnectAddr = "127.0.0.1:20022"
const remoteConnectAddr = "127.0.0.1:20022"
const localProxyAddr = "127.0.0.1:10800"
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
	emrMouseSelected   int
	emrDetail          emrDetailState
	emrItemDialog      emrItemDialog
	remoteDialog       remoteShareDialog
	remoteProxyDialog  remoteProxyDialog
	remoteDeleteDialog remoteDeleteDialog
	remoteErrorDialog  remoteErrorDialog
	remoteShareLoading bool
	remoteForwards     map[int]*tunnel.RemoteForward
	remoteConfigs      map[int]remoteConnectionConfig
	remoteShareRecords []remoteShareRecord
	remoteShareSeq     int
	remotePage         int
	remoteSelected     int
	remoteLatency      string
	remoteLatencyID    int
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

type remoteProxyDialog struct {
	visible  bool
	username string
	password string
	focus    int
	err      string
}

type remoteErrorDialog struct {
	visible bool
	message string
}

type remoteConnectionConfig struct {
	action   string
	username string
	host     string
	port     string
	keyPath  string
	password string
}

type emrDetailState struct {
	visible        bool
	loading        bool
	err            string
	detail         appemr.ClusterDetail
	steps          []appemr.Step
	stepLoading    bool
	stepErr        string
	stepMarker     string
	stepPage       int
	stepSelected   int
	activeTab      string
	overviewScroll int
	yarnApps       []appemr.YarnApplication
	yarnLoading    bool
	yarnErr        string
	yarnPage       int
	yarnSelected   int
}

type emrItemDialog struct {
	visible bool
	kind    string
	index   int
	scroll  int
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
	RetryIn   int
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
	manual  bool
}

type remoteForwardEventMsg struct {
	id    int
	event tunnel.ForwardEvent
}

type blinkStatusMsg struct{}

type remoteLatencyTickMsg struct{}

type remoteLatencyMeasuredMsg struct {
	id      int
	latency time.Duration
	err     error
}

func NewModel() tea.Model {
	return model{emrMouseSelected: -1}
}

func (m model) Init() tea.Cmd {
	return remoteLatencyTick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.remoteDialog.visible {
			return m.updateRemoteDialog(msg)
		}
		if m.remoteProxyDialog.visible {
			return m.updateRemoteProxyDialog(msg)
		}
		if m.remoteErrorDialog.visible {
			return m.updateRemoteErrorDialog(msg)
		}
		if m.remoteDeleteDialog.visible {
			return m.updateRemoteDeleteDialog(msg)
		}
		if m.emrItemDialog.visible {
			switch msg.String() {
			case "ctrl+c", "ctrl+d", "q":
				return m.quit()
			case "esc":
				m.emrItemDialog.visible = false
			case "up":
				if m.emrItemDialog.scroll > 0 {
					m.emrItemDialog.scroll--
				}
			case "down":
				if m.emrItemDialog.scroll < m.emrItemDialogMaxScroll() {
					m.emrItemDialog.scroll++
				}
			case "pgup":
				m.emrItemDialog.scroll = max(m.emrItemDialog.scroll-5, 0)
			case "pgdown":
				m.emrItemDialog.scroll = min(m.emrItemDialog.scroll+5, m.emrItemDialogMaxScroll())
			}
			return m, nil
		}
		if m.emrDetail.visible {
			switch msg.String() {
			case "ctrl+c", "ctrl+d", "q":
				return m.quit()
			case "esc":
				m.emrDetail.visible = false
				m.emrMouseSelected = -1
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
				} else if m.emrDetail.activeTab == "overview" {
					m.emrDetail.overviewScroll = max(m.emrDetail.overviewScroll-m.emrOverviewVisibleLines(), 0)
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
				} else if m.emrDetail.activeTab == "overview" {
					m.emrDetail.overviewScroll = min(
						m.emrDetail.overviewScroll+m.emrOverviewVisibleLines(),
						m.emrOverviewMaxScroll(),
					)
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
				} else if m.emrDetail.activeTab == "overview" && m.emrDetail.overviewScroll > 0 {
					m.emrDetail.overviewScroll--
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
				} else if m.emrDetail.activeTab == "overview" && m.emrDetail.overviewScroll < m.emrOverviewMaxScroll() {
					m.emrDetail.overviewScroll++
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+c", "ctrl+d":
			return m.quit()
		case "q":
			return m.quit()
		case "r":
			if m.activeMenu == emrMenuIndex {
				m.emrLoading = true
				m.emrErr = ""
				m.status = "Loading EMR clusters..."
				return m, loadEMRClusters()
			}
			if m.activeMenu == remoteMenuIndex {
				return m.retrySelectedRemote()
			}
			m.status = fmt.Sprintf("Refreshed %s", menus[m.activeMenu])
		case "p":
			if m.activeMenu == emrMenuIndex && m.emrPage > 0 {
				m.emrPage--
				m.emrSelected = m.emrPage * m.emrPageSize()
				m.emrMouseSelected = -1
			} else if m.activeMenu == remoteMenuIndex && m.remotePage > 0 {
				m.remotePage--
				m.remoteSelected = m.remotePage * remotePageSize
			}
		case "n":
			if m.activeMenu == emrMenuIndex && m.emrPage < m.emrMaxPage() {
				m.emrPage++
				m.emrSelected = m.emrPage * m.emrPageSize()
				m.emrMouseSelected = -1
			} else if m.activeMenu == remoteMenuIndex && m.remotePage < m.remoteMaxPage() {
				m.remotePage++
				m.remoteSelected = m.remotePage * remotePageSize
			}
		case "up":
			if m.activeMenu == emrMenuIndex && m.emrSelected > 0 {
				m.emrSelected--
				m.emrPage = m.emrSelected / m.emrPageSize()
				m.emrMouseSelected = -1
			} else if m.activeMenu == remoteMenuIndex && m.remoteSelected > 0 {
				m.remoteSelected--
				m.remotePage = m.remoteSelected / remotePageSize
			}
		case "down":
			if m.activeMenu == emrMenuIndex && m.emrSelected < len(m.emrClusters)-1 {
				m.emrSelected++
				m.emrPage = m.emrSelected / m.emrPageSize()
				m.emrMouseSelected = -1
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
		case "t":
			if m.activeMenu == remoteMenuIndex {
				m.remoteProxyDialog = remoteProxyDialog{visible: true, username: "hadoop", focus: 1}
				m.status = "Enter SSH password for localhost:20022"
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
		if m.emrItemDialog.visible {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				if m.emrItemDialog.scroll > 0 {
					m.emrItemDialog.scroll--
				}
				return m, nil
			case tea.MouseButtonWheelDown:
				if m.emrItemDialog.scroll < m.emrItemDialogMaxScroll() {
					m.emrItemDialog.scroll++
				}
				return m, nil
			}
		}
		if m.emrDetail.visible && m.emrDetail.activeTab == "overview" {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.emrDetail.overviewScroll = max(m.emrDetail.overviewScroll-1, 0)
				return m, nil
			case tea.MouseButtonWheelDown:
				m.emrDetail.overviewScroll = min(m.emrDetail.overviewScroll+1, m.emrOverviewMaxScroll())
				return m, nil
			}
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.remoteErrorDialog.visible {
				return m.updateRemoteErrorDialogMouse(msg)
			}
			if m.remoteDeleteDialog.visible {
				return m.updateRemoteDeleteDialogMouse(msg)
			}
			if m.remoteProxyDialog.visible {
				return m.updateRemoteProxyDialogMouse(msg)
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
					if m.emrMouseSelected != index {
						m.emrMouseSelected = index
						return m, nil
					}
					cluster := m.emrClusters[index]
					m.emrDetail = emrDetailState{visible: true, loading: true}
					m.emrMouseSelected = -1
					m.status = "Loading EMR cluster detail..."
					return m, loadEMRClusterDetail(cluster.ID)
				}
			}
			if m.activeMenu == remoteMenuIndex {
				if index, ok := m.remoteRowIndexAtMouse(msg.Y); ok {
					m.remoteSelected = index
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
		m.emrMouseSelected = -1
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
			if msg.manual {
				m.remoteErrorDialog = remoteErrorDialog{visible: true, message: msg.err.Error()}
			}
			break
		}

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
			m.updateRemoteRetryCountdown(msg.id, 0)
			m.status = "Remote tunnel reconnected"
		case tunnel.ForwardStatusReconnecting:
			errText := ""
			if msg.event.Err != nil {
				errText = msg.event.Err.Error()
			}
			m.updateRemoteShareRecord(msg.id, "reconnecting", errText)
			m.updateRemoteRetryCountdown(msg.id, int(msg.event.RetryIn/time.Second))
			m.status = "Remote tunnel reconnecting"
			if msg.event.ManualRetryFailed {
				m.remoteErrorDialog = remoteErrorDialog{visible: true, message: errText}
			}
		case tunnel.ForwardStatusClosed:
			m.updateRemoteShareRecord(msg.id, "closed", "")
			m.updateRemoteRetryCountdown(msg.id, 0)
			return m, nil
		}
		if forward := m.remoteForwards[msg.id]; forward != nil {
			return m, waitRemoteForwardEvent(msg.id, forward)
		}
	case remoteLatencyTickMsg:
		nextTick := remoteLatencyTick()
		if m.activeMenu != remoteMenuIndex || m.emrDetail.visible || len(m.remoteShareRecords) == 0 {
			return m, nextTick
		}
		record := m.remoteShareRecords[m.remoteSelected]
		return m, tea.Batch(nextTick, measureRemoteLatency(record))
	case remoteLatencyMeasuredMsg:
		if m.activeMenu != remoteMenuIndex || m.emrDetail.visible || len(m.remoteShareRecords) == 0 {
			break
		}
		if m.remoteShareRecords[m.remoteSelected].ID != msg.id {
			break
		}
		m.remoteLatencyID = msg.id
		if msg.err != nil {
			m.remoteLatency = "不可达"
		} else {
			m.remoteLatency = formatLatency(msg.latency)
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
		detailHeight := max(m.height-statusBarHeight, 0)
		view := strings.Join([]string{
			m.renderEMRDetailPage(detailHeight),
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
	if m.remoteProxyDialog.visible {
		return m.renderRemoteProxyDialog(view)
	}
	if m.remoteErrorDialog.visible {
		return m.renderRemoteErrorDialog(view)
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
		text = " s 分享  c 连接  t 代理  r 重试  ↑/↓ 选择  p 前一页  n 下一页  d 删除  q 退出"
	}
	if m.remoteErrorDialog.visible {
		text = " 返回<Esc/Enter>  q 退出"
	}
	if m.remoteProxyDialog.visible {
		text = " Tab 切换输入框  确认<Enter>  取消<Esc>"
	}
	if m.remoteDeleteDialog.visible {
		text = " 确认<Enter>  取消<Esc>  q 退出"
	}
	if m.emrDetail.visible {
		text = " Tab 切换 section  ↑/↓ 选择  p 前一页  n 下一页  Esc 返回  q 退出"
		if m.emrDetail.activeTab == "overview" {
			text = fmt.Sprintf(
				" Tab 切换 section  ↑/↓/滚轮 滚动  p/n 翻页  位置 %d/%d  Esc 返回  q 退出",
				m.emrDetail.overviewScroll+1,
				m.emrOverviewMaxScroll()+1,
			)
		}
	}
	if m.emrItemDialog.visible {
		text = " ↑/↓ 滚动  PgUp/PgDn 翻页  返回<Esc>  q 退出"
	}

	right := ""
	if m.activeMenu == remoteMenuIndex && !m.emrDetail.visible && len(m.remoteShareRecords) > 0 {
		latency := "--"
		if m.remoteLatencyID == m.remoteShareRecords[m.remoteSelected].ID && m.remoteLatency != "" {
			latency = m.remoteLatency
		}
		right = "延迟: " + latency + " "
	}
	if right != "" {
		leftWidth := max(m.width-lipgloss.Width(right), 0)
		text = ansi.Truncate(text, leftWidth, "")
		text = padRight(text, leftWidth) + right
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("238")).
		Render(text)
}

func (m model) dialogContentHeight() int {
	return max(m.height-statusBarHeight, 0)
}

func (m model) quit() (tea.Model, tea.Cmd) {
	forwards := make([]*tunnel.RemoteForward, 0, len(m.remoteForwards))
	for _, forward := range m.remoteForwards {
		if forward != nil {
			forward.Close()
			forwards = append(forwards, forward)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, forward := range forwards {
		_ = forward.Wait(ctx)
	}

	return m, tea.Quit
}

func (m model) overlayDialog(base, dialog string) string {
	baseLines := strings.Split(base, "\n")
	dialogLines := strings.Split(dialog, "\n")
	contentHeight := m.dialogContentHeight()
	dialogWidth := lipgloss.Width(dialog)
	dialogHeight := lipgloss.Height(dialog)
	left := max((m.width-dialogWidth)/2, 0)
	top := max((contentHeight-dialogHeight)/2, 0)
	visibleWidth := min(dialogWidth, max(m.width-left, 0))

	for i, dialogLine := range dialogLines {
		y := top + i
		if y >= contentHeight || y >= len(baseLines) || visibleWidth == 0 {
			break
		}

		baseLine := baseLines[y]
		prefix := ansi.Cut(baseLine, 0, left)
		overlay := ansi.Cut(dialogLine, 0, visibleWidth)
		suffix := ansi.Cut(baseLine, left+visibleWidth, m.width)
		baseLines[y] = prefix + overlay + suffix
	}

	return strings.Join(baseLines, "\n")
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

func formatShareRow(action, endpoint, key, remote, local, startedAt, status string) string {
	return fmt.Sprintf(
		"%-8s  %-40s  %-18s  %-18s  %-16s  %-19s  %s",
		truncate(action, 8),
		truncate(endpoint, 40),
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
