package app

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

	"dev-tool-cli/internal/tunnel"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
		totalPages := m.remoteMaxPage() + 1
		start := m.remotePage * remotePageSize
		end := min(start+remotePageSize, len(m.remoteShareRecords))
		tableWidth := remoteTableWidth(m.width)
		lines = append(lines,
			fmt.Sprintf("Page %d/%d  Total %d", m.remotePage+1, totalPages, len(m.remoteShareRecords)),
			"",
			formatShareRow(tableWidth, "Action", "Endpoint", "Key", "Remote", "Local", "Started At", "Status"),
			strings.Repeat("-", tableWidth),
		)
		for i, record := range m.remoteShareRecords[start:end] {
			row := formatShareRow(
				tableWidth,
				record.Action,
				record.User+"@"+net.JoinHostPort(record.Host, record.Port),
				record.Key,
				record.Remote,
				record.Local,
				record.StartedAt,
				renderShareStatus(record, m.statusBlink),
			)
			if start+i == m.remoteSelected {
				row = selectedRowStyle(row, tableWidth)
			}
			lines = append(lines, row)
		}
		lines = append(lines, strings.Repeat("-", tableWidth))
	}

	content := lipgloss.NewStyle().
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, content)
}

func remoteTableWidth(screenWidth int) int {
	return max(min(screenWidth-4, 150), 20)
}

func (m model) remoteMaxPage() int {
	if len(m.remoteShareRecords) == 0 {
		return 0
	}

	return (len(m.remoteShareRecords) - 1) / remotePageSize
}

func remoteLatencyTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return remoteLatencyTickMsg{}
	})
}

func measureRemoteLatency(record remoteShareRecord) tea.Cmd {
	return func() tea.Msg {
		address := net.JoinHostPort(record.Host, record.Port)
		startedAt := time.Now()
		conn, err := net.DialTimeout("tcp", address, 3*time.Second)
		latency := time.Since(startedAt)
		if err == nil {
			_ = conn.Close()
		}
		return remoteLatencyMeasuredMsg{
			id:      record.ID,
			latency: latency,
			err:     err,
		}
	}
}

func formatLatency(latency time.Duration) string {
	if latency < time.Millisecond {
		return fmt.Sprintf("%.2fms", float64(latency.Microseconds())/1000)
	}
	return fmt.Sprintf("%.1fms", float64(latency.Microseconds())/1000)
}

func (m model) remoteRowIndexAtMouse(y int) (int, bool) {
	if len(m.remoteShareRecords) == 0 {
		return 0, false
	}

	firstRowY := 8
	if m.remoteShareLoading {
		firstRowY++
	}
	if m.remoteShareErr != "" {
		firstRowY += 2
	}

	rowOffset := y - firstRowY
	if rowOffset < 0 {
		return 0, false
	}

	pageStart := m.remotePage * remotePageSize
	pageEnd := min(pageStart+remotePageSize, len(m.remoteShareRecords))
	index := pageStart + rowOffset
	if index < pageStart || index >= pageEnd {
		return 0, false
	}

	return index, true
}

func (m model) updateRemoteDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+d", "q":
		return m.quit()
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
	dialog := m.remoteDialogView()
	boxWidth := lipgloss.Width(dialog)
	boxHeight := lipgloss.Height(dialog)
	left := (m.width - boxWidth) / 2
	top := (m.dialogContentHeight() - boxHeight) / 2
	x := msg.X - left
	y := msg.Y - top
	if x < 0 || y < 0 || x >= boxWidth || y >= boxHeight {
		return m, nil
	}

	switch y {
	case 5:
		m.remoteDialog.focus = 0
	case 7:
		m.remoteDialog.focus = 1
	case 9:
		m.remoteDialog.focus = 2
	case 11:
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

func (m model) updateRemoteProxyDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+d":
		return m.quit()
	case "esc":
		m.remoteProxyDialog = remoteProxyDialog{}
		m.status = "Proxy canceled"
	case "enter":
		return m.confirmRemoteProxy()
	case "tab", "shift+tab":
		m.remoteProxyDialog.focus = (m.remoteProxyDialog.focus + 1) % 2
	case "backspace", "ctrl+h":
		if m.remoteProxyDialog.focus == 0 {
			m.remoteProxyDialog.username = trimLastRune(m.remoteProxyDialog.username)
		} else {
			m.remoteProxyDialog.password = trimLastRune(m.remoteProxyDialog.password)
		}
		m.remoteProxyDialog.err = ""
	default:
		if len(msg.Runes) > 0 {
			if m.remoteProxyDialog.focus == 0 {
				m.remoteProxyDialog.username += string(msg.Runes)
			} else {
				m.remoteProxyDialog.password += string(msg.Runes)
			}
			m.remoteProxyDialog.err = ""
		}
	}

	return m, nil
}

func (m model) updateRemoteProxyDialogMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	dialog := m.remoteProxyDialogView()
	dialogWidth := lipgloss.Width(dialog)
	dialogHeight := lipgloss.Height(dialog)
	left := (m.width - dialogWidth) / 2
	top := (m.dialogContentHeight() - dialogHeight) / 2
	x := msg.X - left
	y := msg.Y - top
	if x < 0 || y < 0 || x >= dialogWidth || y >= dialogHeight {
		return m, nil
	}

	switch y {
	case 5:
		m.remoteProxyDialog.focus = 0
		return m, nil
	case 7:
		m.remoteProxyDialog.focus = 1
		return m, nil
	}

	if y == dialogHeight-3 {
		if x < dialogWidth/2 {
			m.remoteProxyDialog = remoteProxyDialog{}
			m.status = "Proxy canceled"
			return m, nil
		}
		return m.confirmRemoteProxy()
	}

	return m, nil
}

func (m model) confirmRemoteProxy() (tea.Model, tea.Cmd) {
	username := strings.TrimSpace(m.remoteProxyDialog.username)
	if username == "" {
		m.remoteProxyDialog.err = "用户名不能为空"
		return m, nil
	}
	if m.remoteProxyDialog.password == "" {
		m.remoteProxyDialog.err = "密码不能为空"
		return m, nil
	}

	password := m.remoteProxyDialog.password
	record := remoteShareRecord{
		ID:        m.remoteShareSeq + 1,
		Action:    "proxy",
		User:      username,
		Host:      "localhost",
		Port:      remoteSharePort,
		Key:       "password",
		Remote:    "-",
		Local:     "SOCKS5 " + localProxyAddr,
		StartedAt: time.Now().Format(time.DateTime),
		Status:    "connecting",
	}
	m.remoteShareSeq++
	m.remoteShareRecords = append(m.remoteShareRecords, record)
	if m.remoteConfigs == nil {
		m.remoteConfigs = make(map[int]remoteConnectionConfig)
	}
	m.remoteConfigs[record.ID] = remoteConnectionConfig{
		action:   "proxy",
		username: username,
		host:     "localhost",
		port:     remoteSharePort,
		password: password,
	}
	m.remoteProxyDialog = remoteProxyDialog{}
	m.remoteShareLoading = true
	m.remoteShareErr = ""
	m.status = "Starting SSH dynamic proxy..."

	blinkCmd := m.scheduleStatusBlink()
	return m, tea.Batch(startRemoteProxy(record.ID, username, password, false), blinkCmd)
}

func (m model) renderRemoteProxyDialog(base string) string {
	return m.overlayDialog(base, m.remoteProxyDialogView())
}

func (m model) remoteProxyDialogView() string {
	boxWidth := min(max(m.width-8, 50), 64)
	maskedPassword := strings.Repeat("*", len([]rune(m.remoteProxyDialog.password)))
	if maskedPassword == "" {
		maskedPassword = " "
	}

	lines := []string{
		"创建动态代理",
		"",
		"用户名",
		m.proxyDialogField(0, m.remoteProxyDialog.username),
		"请输入密码",
		m.proxyDialogField(1, maskedPassword),
		"",
		"SSH: localhost:" + remoteSharePort,
		"SOCKS5: " + localProxyAddr,
	}
	if m.remoteProxyDialog.err != "" {
		lines = append(lines, "", lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Render(m.remoteProxyDialog.err))
	}

	buttons := m.dialogButton(-1, "取消<Esc>") + "    " + m.dialogButton(-1, "确认<Enter>")
	return lipgloss.NewStyle().
		Width(boxWidth-6).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(strings.Join(append(lines, "", buttons), "\n"))
}

func (m model) proxyDialogField(index int, value string) string {
	style := lipgloss.NewStyle().Width(44).Padding(0, 1).Background(lipgloss.Color("236"))
	if m.remoteProxyDialog.focus == index {
		style = style.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
	}
	return style.Render(value)
}

func (m model) updateRemoteDeleteDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+d", "q":
		return m.quit()
	case "esc":
		m.remoteDeleteDialog.visible = false
		m.status = "Delete canceled"
	case "enter":
		m.deleteRemoteRecord(m.remoteDeleteDialog.id)
		m.remoteDeleteDialog.visible = false
		m.status = "Remote record deleted"
	}

	return m, nil
}

func (m model) updateRemoteDeleteDialogMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	dialog := m.remoteDeleteDialogView()
	dialogWidth := lipgloss.Width(dialog)
	dialogHeight := lipgloss.Height(dialog)
	left := (m.width - dialogWidth) / 2
	top := (m.dialogContentHeight() - dialogHeight) / 2
	x := msg.X - left
	y := msg.Y - top
	if x < 0 || y < 0 || x >= dialogWidth || y >= dialogHeight {
		return m, nil
	}

	if y == dialogHeight-3 {
		cancelButton := m.deleteDialogButton("取消<Esc>")
		confirmButton := m.deleteDialogButton("确认<Enter>")
		cancelStart := 3
		cancelEnd := cancelStart + lipgloss.Width(cancelButton)
		confirmStart := cancelEnd + 4
		confirmEnd := confirmStart + lipgloss.Width(confirmButton)

		if x >= cancelStart && x < cancelEnd {
			m.remoteDeleteDialog.visible = false
			m.status = "Delete canceled"
			return m, nil
		}
		if x >= confirmStart && x < confirmEnd {
			m.deleteRemoteRecord(m.remoteDeleteDialog.id)
			m.remoteDeleteDialog.visible = false
			m.status = "Remote record deleted"
		}
	}

	return m, nil
}

func (m *model) deleteRemoteRecord(id int) {
	if forward := m.remoteForwards[id]; forward != nil {
		forward.Close()
		delete(m.remoteForwards, id)
	}
	delete(m.remoteConfigs, id)

	for i, record := range m.remoteShareRecords {
		if record.ID == id {
			m.remoteShareRecords = append(m.remoteShareRecords[:i], m.remoteShareRecords[i+1:]...)
			break
		}
	}

	if m.remoteSelected >= len(m.remoteShareRecords) {
		m.remoteSelected = max(len(m.remoteShareRecords)-1, 0)
	}
	m.remotePage = m.remoteSelected / remotePageSize
}

func (m model) renderRemoteDeleteDialog(base string) string {
	return m.overlayDialog(base, m.remoteDeleteDialogView())
}

func (m model) remoteDeleteDialogView() string {
	boxWidth, _ := remoteDeleteDialogSize(m.width, m.height)
	record := m.remoteRecordByID(m.remoteDeleteDialog.id)
	message := "Delete selected remote tunnel?"
	if record != nil {
		message = fmt.Sprintf("Delete %s tunnel %s:%s?", record.Action, record.Host, record.Port)
	}

	buttons := m.deleteDialogButton("取消<Esc>") + "    " + m.deleteDialogButton("确认<Enter>")
	return lipgloss.NewStyle().
		Width(boxWidth-6).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Render(strings.Join([]string{
			"Confirm Delete",
			"",
			message,
			"",
			buttons,
		}, "\n"))
}

func (m model) remoteRecordByID(id int) *remoteShareRecord {
	for i := range m.remoteShareRecords {
		if m.remoteShareRecords[i].ID == id {
			return &m.remoteShareRecords[i]
		}
	}

	return nil
}

func (m model) deleteDialogButton(label string) string {
	return lipgloss.NewStyle().Padding(0, 2).Background(lipgloss.Color("236")).Render(label)
}

func remoteDeleteDialogSize(width, height int) (int, int) {
	return min(max(width-8, 46), 64), min(max(height-4, 8), 10)
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
	if m.remoteConfigs == nil {
		m.remoteConfigs = make(map[int]remoteConnectionConfig)
	}
	m.remoteConfigs[record.ID] = remoteConnectionConfig{
		action:   action,
		username: username,
		host:     ip,
		port:     port,
		keyPath:  keyPath,
	}
	m.remoteDialog.visible = false
	m.remoteShareLoading = true
	m.remoteShareErr = ""
	m.status = "Starting SSH share tunnel..."

	if action == "connect" {
		m.status = "Starting SSH connect tunnel..."
		blinkCmd := m.scheduleStatusBlink()
		return m, tea.Batch(startRemoteConnect(record.ID, username, ip, port, keyPath, false), blinkCmd)
	}

	blinkCmd := m.scheduleStatusBlink()
	return m, tea.Batch(startRemoteShare(record.ID, username, ip, port, keyPath, false), blinkCmd)
}

func (m model) renderRemoteDialog(base string) string {
	return m.overlayDialog(base, m.remoteDialogView())
}

func (m model) remoteDialogView() string {
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
	return lipgloss.NewStyle().
		Width(boxWidth-6).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(strings.Join(append(lines, "", buttons), "\n"))
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

func startRemoteShare(id int, username string, ip string, port string, keyPath string, manual bool) tea.Cmd {
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
		return remoteShareStartedMsg{id: id, forward: forward, err: err, manual: manual}
	}
}

func startRemoteConnect(id int, username string, ip string, port string, keyPath string, manual bool) tea.Cmd {
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
		return remoteShareStartedMsg{id: id, forward: forward, err: err, manual: manual}
	}
}

func startRemoteProxy(id int, username string, password string, manual bool) tea.Cmd {
	return func() tea.Msg {
		forward, err := tunnel.StartAutoReconnectPasswordDynamicForward(
			context.Background(),
			username,
			net.JoinHostPort("localhost", remoteSharePort),
			password,
			localProxyAddr,
		)
		return remoteShareStartedMsg{id: id, forward: forward, err: err, manual: manual}
	}
}

func renderShareStatus(record remoteShareRecord, blink bool) string {
	switch record.Status {
	case "connected":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("● connected")
	case "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("● failed")
	case "connecting", "reconnecting":
		status := record.Status
		if record.Status == "reconnecting" && record.RetryIn > 0 {
			status = fmt.Sprintf("%s (%ds)", status, record.RetryIn)
		}
		if blink {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("● " + status)
		}
		return "  " + status
	case "closed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("● closed")
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

func (m *model) updateRemoteRetryCountdown(id int, seconds int) {
	for i := range m.remoteShareRecords {
		if m.remoteShareRecords[i].ID == id {
			m.remoteShareRecords[i].RetryIn = seconds
			return
		}
	}
}

func (m model) retrySelectedRemote() (tea.Model, tea.Cmd) {
	if len(m.remoteShareRecords) == 0 {
		m.status = "No remote connection selected"
		return m, nil
	}

	record := m.remoteShareRecords[m.remoteSelected]
	switch record.Status {
	case "connected", "connecting":
		m.status = "Selected remote connection is already active"
		return m, nil
	case "reconnecting":
		if forward := m.remoteForwards[record.ID]; forward != nil {
			forward.Retry()
			m.updateRemoteRetryCountdown(record.ID, 0)
			m.status = "Retrying remote connection now..."
			return m, nil
		}
	}

	config, ok := m.remoteConfigs[record.ID]
	if !ok {
		m.remoteErrorDialog = remoteErrorDialog{
			visible: true,
			message: "Connection settings are unavailable for retry.",
		}
		return m, nil
	}

	m.updateRemoteShareRecord(record.ID, "connecting", "")
	m.updateRemoteRetryCountdown(record.ID, 0)
	m.remoteShareLoading = true
	m.status = "Retrying remote connection..."
	blinkCmd := m.scheduleStatusBlink()
	return m, tea.Batch(startRemoteFromConfig(record.ID, config, true), blinkCmd)
}

func startRemoteFromConfig(id int, config remoteConnectionConfig, manual bool) tea.Cmd {
	switch config.action {
	case "share":
		return startRemoteShare(id, config.username, config.host, config.port, config.keyPath, manual)
	case "connect":
		return startRemoteConnect(id, config.username, config.host, config.port, config.keyPath, manual)
	case "proxy":
		return startRemoteProxy(id, config.username, config.password, manual)
	default:
		return func() tea.Msg {
			return remoteShareStartedMsg{
				id:     id,
				err:    fmt.Errorf("unsupported remote action %q", config.action),
				manual: manual,
			}
		}
	}
}

func (m model) updateRemoteErrorDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+d", "q":
		return m.quit()
	case "esc", "enter":
		m.remoteErrorDialog = remoteErrorDialog{}
	}
	return m, nil
}

func (m model) updateRemoteErrorDialogMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	dialog := m.remoteErrorDialogView()
	dialogWidth := lipgloss.Width(dialog)
	dialogHeight := lipgloss.Height(dialog)
	left := (m.width - dialogWidth) / 2
	top := (m.dialogContentHeight() - dialogHeight) / 2
	if msg.X >= left && msg.X < left+dialogWidth && msg.Y == top+dialogHeight-3 {
		m.remoteErrorDialog = remoteErrorDialog{}
	}
	return m, nil
}

func (m model) renderRemoteErrorDialog(base string) string {
	return m.overlayDialog(base, m.remoteErrorDialogView())
}

func (m model) remoteErrorDialogView() string {
	boxWidth := min(max(m.width-8, 50), 90)
	button := lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("62")).
		Render("返回<Esc/Enter>")
	return lipgloss.NewStyle().
		Width(boxWidth-6).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Render(strings.Join([]string{
			"连接重试失败",
			"",
			m.remoteErrorDialog.message,
			"",
			button,
		}, "\n"))
}

func (m model) hasActiveBlinkingRemoteShare() bool {
	for _, record := range m.remoteShareRecords {
		if record.Status == "connecting" || record.Status == "reconnecting" {
			return true
		}
	}
	return false
}
