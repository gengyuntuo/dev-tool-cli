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
		lines = append(lines,
			fmt.Sprintf("Page %d/%d  Total %d", m.remotePage+1, totalPages, len(m.remoteShareRecords)),
			"",
			formatShareRow("Action", "User", "Host", "Port", "Key", "Remote", "Local", "Started At", "Status"),
			strings.Repeat("-", min(m.width, 150)),
		)
		for i, record := range m.remoteShareRecords[start:end] {
			row := formatShareRow(
				record.Action,
				record.User,
				record.Host,
				record.Port,
				record.Key,
				record.Remote,
				record.Local,
				record.StartedAt,
				renderShareStatus(record, m.statusBlink),
			)
			if start+i == m.remoteSelected {
				row = selectedRowStyle(row, max(lipgloss.Width(row), min(m.width, 150)))
			}
			lines = append(lines, row)
		}
		lines = append(lines, strings.Repeat("-", min(m.width, 150)))
	}

	content := lipgloss.NewStyle().
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, content)
}

func (m model) remoteMaxPage() int {
	if len(m.remoteShareRecords) == 0 {
		return 0
	}

	return (len(m.remoteShareRecords) - 1) / remotePageSize
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

func (m model) updateRemoteDeleteDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "ctrl+d", "q":
		return m, tea.Quit
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

func renderShareStatus(record remoteShareRecord, blink bool) string {
	switch record.Status {
	case "connected":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("● connected")
	case "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("● failed")
	case "connecting", "reconnecting":
		if blink {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("● " + record.Status)
		}
		return "  " + record.Status
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

func (m model) hasActiveBlinkingRemoteShare() bool {
	for _, record := range m.remoteShareRecords {
		if record.Status == "connecting" || record.Status == "reconnecting" {
			return true
		}
	}
	return false
}
