package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appemr "dev-tool-cli/internal/emr"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const statusBarHeight = 1
const emrMenuIndex = 0

var menus = []string{"EMR", "Remote", "Help"}

type model struct {
	width       int
	height      int
	activeMenu  int
	status      string
	emrClusters []appemr.Cluster
	emrLoading  bool
	emrErr      string
	emrPage     int
}

type emrClustersLoadedMsg struct {
	clusters []appemr.Cluster
	err      error
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
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
	}

	return m, nil
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing dev-tool-cli...\n"
	}

	displayHeight := max(m.height-1-statusBarHeight, 0)

	return strings.Join([]string{
		m.renderMenuBar(),
		m.renderDisplay(displayHeight),
		m.renderStatusBar(),
	}, "\n")
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
	return lipgloss.NewStyle().
		Width(m.width).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("238")).
		Render(" r 刷新  p 前一页  n 下一页  q 退出")
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
