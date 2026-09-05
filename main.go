package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	appemr "dev-tool-cli/internal/emr"
	"dev-tool-cli/internal/tunnel"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const statusBarHeight = 1
const emrMenuIndex = 0
const remoteMenuIndex = 1
const remoteSharePort = "20022"
const localSSHAddr = "127.0.0.1:22"
const localConnectAddr = "127.0.0.1:20022"
const remoteConnectAddr = "127.0.0.1:20022"

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
	remoteDialog       remoteShareDialog
	remoteShareLoading bool
	remoteShare        *tunnel.RemoteForward
	remoteShareRecords []remoteShareRecord
	remoteShareSeq     int
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

type sshKeysLoadedMsg struct {
	keys []string
	err  error
}

type remoteShareStartedMsg struct {
	id      int
	forward *tunnel.RemoteForward
	err     error
}

type blinkStatusMsg struct{}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.remoteDialog.visible {
			return m.updateRemoteDialog(msg)
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
			}
		case "n":
			if m.activeMenu == emrMenuIndex && m.emrPage < m.emrMaxPage() {
				m.emrPage++
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
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.remoteDialog.visible {
				return m.updateRemoteDialogMouse(msg)
			}

			if msg.Y == 0 {
				if index, ok := menuIndexAt(msg.X); ok {
					m.activeMenu = index
					m.status = fmt.Sprintf("Switched to %s", menus[m.activeMenu])
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
		m.emrErr = ""
		m.status = fmt.Sprintf("Loaded %d running EMR clusters", len(msg.clusters))
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
		m.updateRemoteShareRecord(msg.id, "connected", "")
		m.remoteShareErr = ""
		m.status = "Remote tunnel connected"
	case blinkStatusMsg:
		m.statusBlink = !m.statusBlink
		if m.hasConnectingRemoteShare() {
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

	view := strings.Join([]string{
		m.renderMenuBar(),
		m.renderDisplay(displayHeight),
		m.renderStatusBar(),
	}, "\n")

	if m.remoteDialog.visible {
		return m.renderRemoteDialog(view)
	}

	return view
}

func main() {
	if _, err := tea.NewProgram(model{}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to run app: %v\n", err)
		os.Exit(1)
	}
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
		text = " r 刷新  p 前一页  n 下一页  q 退出"
	case remoteMenuIndex:
		text = " s 分享  c 连接  q 退出"
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

func (m model) renderEMRDisplay(height int) string {
	if m.emrLoading {
		return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, "Loading running EMR clusters...")
	}

	if m.emrErr != "" {
		return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, "EMR load failed\n\n"+m.emrErr)
	}

	if len(m.emrClusters) == 0 {
		content := "EMR Panel\n\nPress r to load running EMR clusters."
		if m.status != "" {
			content += "\n\n" + m.status
		}
		return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, content)
	}

	pageSize := m.emrPageSize()
	totalPages := m.emrMaxPage() + 1
	start := m.emrPage * pageSize
	end := min(start+pageSize, len(m.emrClusters))

	lines := []string{
		fmt.Sprintf("Running EMR Clusters  Page %d/%d  Total %d", m.emrPage+1, totalPages, len(m.emrClusters)),
		"",
		formatClusterRow("ID", "Name", "State", "Created At"),
		strings.Repeat("-", min(m.width, 96)),
	}

	for _, cluster := range m.emrClusters[start:end] {
		lines = append(lines, formatClusterRow(cluster.ID, cluster.Name, cluster.State, cluster.CreatedAt))
	}

	content := lipgloss.NewStyle().
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, content)
}

func (m model) emrPageSize() int {
	displayHeight := max(m.height-1-statusBarHeight, 0)
	return max(displayHeight-5, 1)
}

func (m model) emrMaxPage() int {
	if len(m.emrClusters) == 0 {
		return 0
	}

	return (len(m.emrClusters) - 1) / m.emrPageSize()
}

func formatClusterRow(id, name, state, createdAt string) string {
	return fmt.Sprintf("%-22s  %-30s  %-14s  %-19s", truncate(id, 22), truncate(name, 30), truncate(state, 14), truncate(createdAt, 19))
}

func truncate(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}

	if width <= 1 {
		return string(runes[:width])
	}

	return string(runes[:width-1]) + "…"
}

func loadEMRClusters() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		clusters, err := appemr.ListRunningClusters(ctx, "")
		return emrClustersLoadedMsg{clusters: clusters, err: err}
	}
}

func (m model) renderRemoteDisplay(height int) string {
	lines := []string{"Remote Shares", ""}
	if m.remoteShareLoading {
		lines = append(lines, "Starting SSH share tunnel...")
	}
	if m.remoteShareErr != "" {
		lines = append(lines, "", m.remoteShareErr)
	}

	if len(m.remoteShareRecords) == 0 {
		lines = append(lines, "No share records.", "", "Press s to share local SSH service to a remote host.")
	} else {
		lines = append(lines,
			formatShareRow("Action", "User", "Host", "Port", "Key", "Remote", "Local", "Started At", "Status"),
			strings.Repeat("-", min(m.width, 150)),
		)
		for _, record := range m.remoteShareRecords {
			lines = append(lines, formatShareRow(
				record.Action,
				record.User,
				record.Host,
				record.Port,
				record.Key,
				record.Remote,
				record.Local,
				record.StartedAt,
				renderShareStatus(record, m.statusBlink),
			))
		}
		lines = append(lines, strings.Repeat("-", min(m.width, 150)))
	}

	content := lipgloss.NewStyle().
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, content)
}

func (m model) updateRemoteDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+d", "q":
		return m, tea.Quit
	case "esc":
		m.remoteDialog.visible = false
		m.status = "Share canceled"
	case "tab":
		m.remoteDialog.focus = (m.remoteDialog.focus + 1) % 6
	case "shift+tab":
		m.remoteDialog.focus = (m.remoteDialog.focus - 1 + 6) % 6
	case "up":
		if m.remoteDialog.focus == 3 && m.remoteDialog.keyIndex > 0 {
			m.remoteDialog.keyIndex--
		}
	case "down":
		if m.remoteDialog.focus == 3 && m.remoteDialog.keyIndex < len(m.remoteDialog.keys)-1 {
			m.remoteDialog.keyIndex++
		}
	case "backspace", "ctrl+h":
		switch m.remoteDialog.focus {
		case 0:
			m.remoteDialog.ip = trimLastRune(m.remoteDialog.ip)
		case 1:
			m.remoteDialog.port = trimLastRune(m.remoteDialog.port)
		case 2:
			m.remoteDialog.username = trimLastRune(m.remoteDialog.username)
		}
	case "enter":
		switch m.remoteDialog.focus {
		case 4:
			m.remoteDialog.visible = false
			m.status = "Share canceled"
		case 5:
			return m.confirmRemoteShare()
		}
	default:
		if len(msg.Runes) > 0 {
			switch m.remoteDialog.focus {
			case 0:
				m.remoteDialog.ip += string(msg.Runes)
			case 1:
				m.remoteDialog.port += string(msg.Runes)
			case 2:
				m.remoteDialog.username += string(msg.Runes)
			}
		}
	}

	return m, nil
}

func (m model) updateRemoteDialogMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	boxWidth, boxHeight := remoteDialogSize(m.width, m.height)
	left := (m.width - boxWidth) / 2
	top := (m.height - boxHeight) / 2
	x := msg.X - left
	y := msg.Y - top
	if x < 0 || y < 0 || x >= boxWidth || y >= boxHeight {
		return m, nil
	}

	switch y {
	case 3:
		m.remoteDialog.focus = 0
	case 5:
		m.remoteDialog.focus = 1
	case 7:
		m.remoteDialog.focus = 2
	case 9:
		m.remoteDialog.focus = 3
	case boxHeight - 3:
		if x < boxWidth/2 {
			m.remoteDialog.focus = 4
			m.remoteDialog.visible = false
			m.status = "Share canceled"
			return m, nil
		}

		m.remoteDialog.focus = 5
		return m.confirmRemoteShare()
	}

	return m, nil
}

func (m model) confirmRemoteShare() (tea.Model, tea.Cmd) {
	ip := strings.TrimSpace(m.remoteDialog.ip)
	port := strings.TrimSpace(m.remoteDialog.port)
	username := strings.TrimSpace(m.remoteDialog.username)
	if ip == "" {
		m.remoteDialog.err = "IP address is required"
		return m, nil
	}
	if _, err := strconv.Atoi(port); err != nil || port == "" {
		m.remoteDialog.err = "SSH port must be a number"
		return m, nil
	}
	if username == "" {
		m.remoteDialog.err = "Username is required"
		return m, nil
	}
	if len(m.remoteDialog.keys) == 0 {
		m.remoteDialog.err = "No private key found in ~/.ssh"
		return m, nil
	}

	keyPath := m.remoteDialog.keys[m.remoteDialog.keyIndex]
	action := m.remoteDialog.action
	remoteAddr := net.JoinHostPort("0.0.0.0", remoteSharePort)
	localAddr := localSSHAddr
	if action == "connect" {
		remoteAddr = remoteConnectAddr
		localAddr = localConnectAddr
	}

	record := remoteShareRecord{
		ID:        m.remoteShareSeq + 1,
		Action:    action,
		User:      username,
		Host:      ip,
		Port:      port,
		Key:       filepath.Base(keyPath),
		Remote:    remoteAddr,
		Local:     localAddr,
		StartedAt: time.Now().Format(time.DateTime),
		Status:    "connecting",
	}
	m.remoteShareSeq++
	m.remoteShareRecords = append(m.remoteShareRecords, record)
	m.remoteDialog.visible = false
	m.remoteShareLoading = true
	m.remoteShareErr = ""
	m.status = "Starting SSH share tunnel..."

	if action == "connect" {
		m.status = "Starting SSH connect tunnel..."
		return m, tea.Batch(startRemoteConnect(record.ID, username, ip, port, keyPath), blinkRemoteStatus())
	}

	return m, tea.Batch(startRemoteShare(record.ID, username, ip, port, keyPath), blinkRemoteStatus())
}

func (m model) renderRemoteDialog(base string) string {
	_ = base

	boxWidth, _ := remoteDialogSize(m.width, m.height)
	keyName := "No private key found"
	if len(m.remoteDialog.keys) > 0 {
		keyName = filepath.Base(m.remoteDialog.keys[m.remoteDialog.keyIndex])
	}

	lines := []string{
		remoteDialogTitle(m.remoteDialog.action),
		"",
		"IP地址",
		m.dialogField(0, m.remoteDialog.ip),
		"端口",
		m.dialogField(1, m.remoteDialog.port),
		"用户名",
		m.dialogField(2, m.remoteDialog.username),
		"私钥",
		m.dialogField(3, keyName+"  ↑/↓"),
	}
	if m.remoteDialog.err != "" {
		lines = append(lines, "", m.remoteDialog.err)
	}

	buttons := m.dialogButton(4, "取消") + "    " + m.dialogButton(5, "确认")
	content := lipgloss.NewStyle().
		Width(boxWidth-4).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(strings.Join(append(lines, "", buttons), "\n"))

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m model) dialogField(index int, value string) string {
	style := lipgloss.NewStyle().Width(44).Padding(0, 1).Background(lipgloss.Color("236"))
	if m.remoteDialog.focus == index {
		style = style.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
	}
	return style.Render(value)
}

func (m model) dialogButton(index int, label string) string {
	style := lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("236"))
	if m.remoteDialog.focus == index {
		style = style.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
	}
	return style.Render(label)
}

func remoteDialogSize(width, height int) (int, int) {
	return min(max(width-8, 50), 64), min(max(height-4, 16), 20)
}

func loadSSHKeys() tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return sshKeysLoadedMsg{err: fmt.Errorf("resolve user home dir: %w", err)}
		}

		sshDir := filepath.Join(homeDir, ".ssh")
		entries, err := os.ReadDir(sshDir)
		if err != nil {
			return sshKeysLoadedMsg{err: fmt.Errorf("read %s: %w", sshDir, err)}
		}

		keys := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || !entry.Type().IsRegular() {
				continue
			}

			name := entry.Name()
			if strings.HasSuffix(name, ".pub") || name == "known_hosts" || name == "config" || name == "authorized_keys" {
				continue
			}

			keys = append(keys, filepath.Join(sshDir, name))
		}
		sort.Strings(keys)

		return sshKeysLoadedMsg{keys: keys}
	}
}

func startRemoteShare(id int, username string, ip string, port string, keyPath string) tea.Cmd {
	return func() tea.Msg {
		remoteAddr := net.JoinHostPort("0.0.0.0", remoteSharePort)
		forward, err := tunnel.StartAutoReconnectRemoteForward(
			context.Background(),
			username,
			net.JoinHostPort(ip, port),
			keyPath,
			remoteAddr,
			localSSHAddr,
		)
		return remoteShareStartedMsg{id: id, forward: forward, err: err}
	}
}

func startRemoteConnect(id int, username string, ip string, port string, keyPath string) tea.Cmd {
	return func() tea.Msg {
		forward, err := tunnel.StartAutoReconnectLocalForwards(
			context.Background(),
			username,
			net.JoinHostPort(ip, port),
			keyPath,
			[]tunnel.LocalForwardSpec{
				{
					LocalAddr:  localConnectAddr,
					RemoteAddr: remoteConnectAddr,
				},
			},
			"",
		)
		return remoteShareStartedMsg{id: id, forward: forward, err: err}
	}
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return string(runes[:len(runes)-1])
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

func renderShareStatus(record remoteShareRecord, blink bool) string {
	switch record.Status {
	case "connected":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("● connected")
	case "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("● failed")
	case "connecting":
		if blink {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("● connecting")
		}
		return "  connecting"
	default:
		return "-"
	}
}

func (m *model) updateRemoteShareRecord(id int, status string, errText string) {
	for i := range m.remoteShareRecords {
		if m.remoteShareRecords[i].ID == id {
			m.remoteShareRecords[i].Status = status
			m.remoteShareRecords[i].Error = errText
			return
		}
	}
}

func (m model) hasConnectingRemoteShare() bool {
	for _, record := range m.remoteShareRecords {
		if record.Status == "connecting" {
			return true
		}
	}
	return false
}

func blinkRemoteStatus() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return blinkStatusMsg{}
	})
}
