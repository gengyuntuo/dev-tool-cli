package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appemr "dev-tool-cli/internal/emr"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	tableWidth := tableWidth(m.width)

	lines := []string{
		fmt.Sprintf("Running EMR Clusters  Page %d/%d  Total %d", m.emrPage+1, totalPages, len(m.emrClusters)),
		"",
		boxTop(tableWidth),
		boxRow(formatClusterRow(tableWidth, "ID", "Name", "State", "Created At"), tableWidth),
		boxSeparator(tableWidth),
	}

	for i, cluster := range m.emrClusters[start:end] {
		row := boxRow(formatClusterRow(tableWidth, cluster.ID, cluster.Name, renderEMRState(cluster.State), cluster.CreatedAt), tableWidth)
		if start+i == m.emrSelected {
			row = selectedRowStyle(row, tableWidth)
		}
		lines = append(lines, row)
	}
	lines = append(lines, boxBottom(tableWidth))

	content := lipgloss.NewStyle().
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, content)
}

func (m model) emrPageSize() int {
	displayHeight := max(m.height-1-statusBarHeight, 0)
	return max(displayHeight-7, 1)
}

func (m model) emrMaxPage() int {
	if len(m.emrClusters) == 0 {
		return 0
	}

	return (len(m.emrClusters) - 1) / m.emrPageSize()
}

func formatClusterRow(tableWidth int, id, name, state, createdAt string) string {
	idWidth, nameWidth, stateWidth, createdAtWidth := emrClusterColumnWidths(tableWidth)
	return strings.Join([]string{
		formatCell(id, idWidth),
		formatCell(name, nameWidth),
		formatCell(state, stateWidth),
		formatCell(createdAt, createdAtWidth),
	}, "  ")
}

func renderEMRState(state string) string {
	switch state {
	case "WAITING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("● WAITING")
	case "RUNNING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("● RUNNING")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("● " + state)
	}
}

func renderStepState(state string, blink bool) string {
	switch state {
	case "COMPLETED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("● COMPLETED")
	case "FAILED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("● FAILED")
	case "CANCELLED", "CANCEL_PENDING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("● " + state)
	case "PENDING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render("● PENDING")
	case "RUNNING":
		if blink {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("● RUNNING")
		}
		return "  RUNNING"
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("● " + state)
	}
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

func formatCell(value string, width int) string {
	if lipgloss.Width(value) > width {
		value = truncate(value, width)
	}

	return padRight(value, width)
}

func emrClusterColumnWidths(tableWidth int) (int, int, int, int) {
	innerWidth := max(tableWidth-2, 0)
	gapWidth := 6
	idWidth := 22
	stateWidth := 16
	createdAtWidth := 19
	nameWidth := max(innerWidth-gapWidth-idWidth-stateWidth-createdAtWidth, 10)

	return idWidth, nameWidth, stateWidth, createdAtWidth
}

func emrStepColumnWidths(tableWidth int) (int, int, int, int, int, int, int, int) {
	innerWidth := max(tableWidth-2, 0)
	gapWidth := 14
	idWidth := 18
	stateWidth := 18
	createdAtWidth := 19
	startedAtWidth := 19
	endedAtWidth := 19
	yarnAppIDWidth := 20
	elapsedWidth := 18
	nameWidth := innerWidth - gapWidth - idWidth - stateWidth - createdAtWidth - startedAtWidth - endedAtWidth - yarnAppIDWidth - elapsedWidth
	if nameWidth < 12 {
		nameWidth = 12
		idWidth = max(innerWidth-gapWidth-nameWidth-stateWidth-createdAtWidth-startedAtWidth-endedAtWidth-yarnAppIDWidth-elapsedWidth, 8)
	}

	return idWidth, nameWidth, stateWidth, createdAtWidth, startedAtWidth, endedAtWidth, yarnAppIDWidth, elapsedWidth
}

func emrYarnColumnWidths(tableWidth int) (int, int, int, int, int, int) {
	innerWidth := max(tableWidth-2, 0)
	gapWidth := 12
	idWidth := 22
	stateWidth := 16
	userWidth := 14
	startedAtWidth := 19
	elapsedWidth := 18
	nameWidth := innerWidth - gapWidth - idWidth - stateWidth - userWidth - startedAtWidth - elapsedWidth
	if nameWidth < 12 {
		nameWidth = 12
		idWidth = max(innerWidth-gapWidth-nameWidth-stateWidth-userWidth-startedAtWidth-elapsedWidth, 8)
	}

	return idWidth, nameWidth, stateWidth, userWidth, startedAtWidth, elapsedWidth
}

func loadEMRClusters() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		clusters, err := appemr.ListRunningClusters(ctx, "")
		return emrClustersLoadedMsg{clusters: clusters, err: err}
	}
}

func loadEMRClusterDetail(clusterID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		detail, err := appemr.GetClusterDetail(ctx, "", clusterID)
		return emrClusterDetailLoadedMsg{detail: detail, err: err}
	}
}

func loadEMRSteps(clusterID string, marker string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		page, err := appemr.ListStepsPage(ctx, "", clusterID, marker)
		return emrStepsLoadedMsg{page: page, err: err}
	}
}

func loadYarnApps(host string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		apps, err := appemr.ListYarnApplications(ctx, host)
		return yarnAppsLoadedMsg{apps: apps, err: err}
	}
}

func (m model) renderEMRDetailPage(height int) string {
	if m.emrDetail.loading {
		return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, "Loading EMR cluster detail...")
	}
	if m.emrDetail.err != "" {
		return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, "EMR cluster detail failed\n\n"+m.emrDetail.err)
	}

	content := m.renderEMRDetailContent(tableWidth(m.width))
	return lipgloss.NewStyle().
		Width(m.width).
		Height(height).
		Padding(1, 2).
		Render(content)
}

func (m model) renderEMRDetailContent(tableWidth int) string {
	detail := m.emrDetail.detail
	if m.emrDetail.activeTab == "yarn" {
		return m.renderYarnDetailContent(tableWidth, detail)
	}

	stepStart := m.emrDetail.stepPage * emrDetailStepPageSize
	stepEnd := min(stepStart+emrDetailStepPageSize, len(m.emrDetail.steps))
	totalStepPages := m.emrDetailMaxStepPage() + 1
	if m.emrDetail.stepMarker != "" {
		totalStepPagesLabel := fmt.Sprintf("%d+", totalStepPages)
		return m.renderEMRDetailContentWithStepPageLabel(tableWidth, stepStart, stepEnd, totalStepPagesLabel)
	}

	lines := []string{
		"EMR Cluster Detail",
		"",
		"基本信息",
		boxTop(tableWidth),
		boxRow("ID: "+detail.ID, tableWidth),
		boxRow("Name: "+detail.Name, tableWidth),
		boxRow("State: "+detail.State, tableWidth),
		boxRow("Release: "+detail.ReleaseLabel, tableWidth),
		boxRow("S3 Log URI: "+detail.LogURI, tableWidth),
		boxRow("Applications: "+strings.Join(detail.Applications, ", "), tableWidth),
		boxRow("Primary node private DNS: "+detail.PrimaryNodePrivateDNS, tableWidth),
		boxRow("Created At: "+detail.CreatedAt, tableWidth),
		boxRow("Step Concurrency: "+detail.StepConcurrency, tableWidth),
		boxRow("Service Role: "+detail.ServiceRole, tableWidth),
		boxBottom(tableWidth),
		"",
		"实例种类和数量",
		boxTop(tableWidth),
		boxRow(formatInstanceRow("Kind", "Type", "Count"), tableWidth),
		boxSeparator(tableWidth),
	}

	if len(detail.Instances) == 0 {
		lines = append(lines, boxRow("No instances found.", tableWidth))
	} else {
		for _, instance := range detail.Instances {
			lines = append(lines, boxRow(formatInstanceRow(instance.Kind, instance.Type, instance.Count), tableWidth))
		}
	}

	lines = append(lines,
		boxBottom(tableWidth),
		"",
		fmt.Sprintf("Step  Page %d/%d  Loaded %d", m.emrDetail.stepPage+1, totalStepPages, len(m.emrDetail.steps)),
		boxTop(tableWidth),
		boxRow(formatStepRow(tableWidth, "ID", "Name", "State", "Created At", "Started At", "Ended At", "YARN App ID", "Elapsed"), tableWidth),
		boxSeparator(tableWidth),
	)

	if m.emrDetail.stepLoading {
		lines = append(lines, boxRow("Loading steps...", tableWidth))
	} else if m.emrDetail.stepErr != "" {
		lines = append(lines, boxRow("Step load failed: "+m.emrDetail.stepErr, tableWidth))
	} else if len(m.emrDetail.steps) == 0 {
		lines = append(lines, boxRow("No steps found.", tableWidth))
	} else {
		for i, step := range m.emrDetail.steps[stepStart:stepEnd] {
			row := boxRow(formatStepRow(tableWidth, step.ID, step.Name, renderStepState(step.State, m.statusBlink), step.CreatedAt, step.StartedAt, step.EndedAt, "-", "-"), tableWidth)
			if stepStart+i == m.emrDetail.stepSelected {
				row = selectedRowStyle(row, tableWidth)
			}
			lines = append(lines, row)
		}
	}

	lines = append(lines, boxBottom(tableWidth))
	return strings.Join(lines, "\n")
}

func (m model) renderEMRDetailContentWithStepPageLabel(tableWidth, stepStart, stepEnd int, totalStepPagesLabel string) string {
	detail := m.emrDetail.detail

	lines := []string{
		"EMR Cluster Detail",
		"",
		"基本信息",
		boxTop(tableWidth),
		boxRow("ID: "+detail.ID, tableWidth),
		boxRow("Name: "+detail.Name, tableWidth),
		boxRow("State: "+detail.State, tableWidth),
		boxRow("Release: "+detail.ReleaseLabel, tableWidth),
		boxRow("S3 Log URI: "+detail.LogURI, tableWidth),
		boxRow("Applications: "+strings.Join(detail.Applications, ", "), tableWidth),
		boxRow("Primary node private DNS: "+detail.PrimaryNodePrivateDNS, tableWidth),
		boxRow("Created At: "+detail.CreatedAt, tableWidth),
		boxRow("Step Concurrency: "+detail.StepConcurrency, tableWidth),
		boxRow("Service Role: "+detail.ServiceRole, tableWidth),
		boxBottom(tableWidth),
		"",
		"实例种类和数量",
		boxTop(tableWidth),
		boxRow(formatInstanceRow("Kind", "Type", "Count"), tableWidth),
		boxSeparator(tableWidth),
	}

	if len(detail.Instances) == 0 {
		lines = append(lines, boxRow("No instances found.", tableWidth))
	} else {
		for _, instance := range detail.Instances {
			lines = append(lines, boxRow(formatInstanceRow(instance.Kind, instance.Type, instance.Count), tableWidth))
		}
	}

	lines = append(lines,
		boxBottom(tableWidth),
		"",
		fmt.Sprintf("Step  Page %d/%s  Loaded %d", m.emrDetail.stepPage+1, totalStepPagesLabel, len(m.emrDetail.steps)),
		boxTop(tableWidth),
		boxRow(formatStepRow(tableWidth, "ID", "Name", "State", "Created At", "Started At", "Ended At", "YARN App ID", "Elapsed"), tableWidth),
		boxSeparator(tableWidth),
	)

	if m.emrDetail.stepLoading {
		lines = append(lines, boxRow("Loading more steps...", tableWidth))
	} else if m.emrDetail.stepErr != "" {
		lines = append(lines, boxRow("Step load failed: "+m.emrDetail.stepErr, tableWidth))
	} else if len(m.emrDetail.steps) == 0 {
		lines = append(lines, boxRow("No steps found.", tableWidth))
	} else {
		for i, step := range m.emrDetail.steps[stepStart:stepEnd] {
			row := boxRow(formatStepRow(tableWidth, step.ID, step.Name, renderStepState(step.State, m.statusBlink), step.CreatedAt, step.StartedAt, step.EndedAt, "-", "-"), tableWidth)
			if stepStart+i == m.emrDetail.stepSelected {
				row = selectedRowStyle(row, tableWidth)
			}
			lines = append(lines, row)
		}
	}

	lines = append(lines, boxBottom(tableWidth))
	return strings.Join(lines, "\n")
}

func (m model) renderYarnDetailContent(tableWidth int, detail appemr.ClusterDetail) string {
	appStart := m.emrDetail.yarnPage * emrDetailStepPageSize
	appEnd := min(appStart+emrDetailStepPageSize, len(m.emrDetail.yarnApps))
	lines := []string{
		"EMR Cluster Detail",
		"",
		"基本信息",
		boxTop(tableWidth),
		boxRow("ID: "+detail.ID, tableWidth),
		boxRow("Name: "+detail.Name, tableWidth),
		boxRow("State: "+detail.State, tableWidth),
		boxRow("Release: "+detail.ReleaseLabel, tableWidth),
		boxRow("S3 Log URI: "+detail.LogURI, tableWidth),
		boxRow("Applications: "+strings.Join(detail.Applications, ", "), tableWidth),
		boxRow("Primary node private DNS: "+detail.PrimaryNodePrivateDNS, tableWidth),
		boxRow("Created At: "+detail.CreatedAt, tableWidth),
		boxRow("Step Concurrency: "+detail.StepConcurrency, tableWidth),
		boxRow("Service Role: "+detail.ServiceRole, tableWidth),
		boxBottom(tableWidth),
		"",
		"实例种类和数量",
		boxTop(tableWidth),
		boxRow(formatInstanceRow("Kind", "Type", "Count"), tableWidth),
		boxSeparator(tableWidth),
	}
	if len(detail.Instances) == 0 {
		lines = append(lines, boxRow("No instances found.", tableWidth))
	} else {
		for _, instance := range detail.Instances {
			lines = append(lines, boxRow(formatInstanceRow(instance.Kind, instance.Type, instance.Count), tableWidth))
		}
	}
	lines = append(lines,
		boxBottom(tableWidth),
		"",
		fmt.Sprintf("YARN Applications  Page %d/%d  Loaded %d", m.emrDetail.yarnPage+1, m.emrDetailYarnMaxPage()+1, len(m.emrDetail.yarnApps)),
		boxTop(tableWidth),
		boxRow(formatYarnRow(tableWidth, "ID", "Name", "State", "User", "Started At", "Elapsed"), tableWidth),
		boxSeparator(tableWidth),
	)
	if m.emrDetail.yarnLoading {
		lines = append(lines, boxRow("Loading YARN applications...", tableWidth))
	} else if m.emrDetail.yarnErr != "" {
		lines = append(lines, boxRow("YARN load failed: "+m.emrDetail.yarnErr, tableWidth))
	} else if len(m.emrDetail.yarnApps) == 0 {
		lines = append(lines, boxRow("No YARN applications found.", tableWidth))
	} else {
		for i, app := range m.emrDetail.yarnApps[appStart:appEnd] {
			row := boxRow(formatYarnRow(tableWidth, app.ID, app.Name, app.State, app.User, app.StartedAt, app.Elapsed), tableWidth)
			if appStart+i == m.emrDetail.yarnSelected {
				row = selectedRowStyle(row, tableWidth)
			}
			lines = append(lines, row)
		}
	}
	lines = append(lines, boxBottom(tableWidth))
	return strings.Join(lines, "\n")
}

func formatInstanceRow(kind, instanceType, count string) string {
	return strings.Join([]string{
		formatCell(kind, 12),
		formatCell(instanceType, 40),
		formatCell(count, 8),
	}, "  ")
}

func formatStepRow(tableWidth int, id, name, state, createdAt, startedAt, endedAt, yarnAppID, elapsed string) string {
	idWidth, nameWidth, stateWidth, createdAtWidth, startedAtWidth, endedAtWidth, yarnAppIDWidth, elapsedWidth := emrStepColumnWidths(tableWidth)
	return strings.Join([]string{
		formatCell(id, idWidth),
		formatCell(name, nameWidth),
		formatCell(state, stateWidth),
		formatCell(createdAt, createdAtWidth),
		formatCell(startedAt, startedAtWidth),
		formatCell(endedAt, endedAtWidth),
		formatCell(yarnAppID, yarnAppIDWidth),
		formatCell(elapsed, elapsedWidth),
	}, "  ")
}

func formatYarnRow(tableWidth int, id, name, state, user, startedAt, elapsed string) string {
	idWidth, nameWidth, stateWidth, userWidth, startedAtWidth, elapsedWidth := emrYarnColumnWidths(tableWidth)
	return strings.Join([]string{
		formatCell(id, idWidth),
		formatCell(name, nameWidth),
		formatCell(state, stateWidth),
		formatCell(user, userWidth),
		formatCell(startedAt, startedAtWidth),
		formatCell(elapsed, elapsedWidth),
	}, "  ")
}

func tableWidth(screenWidth int) int {
	return max(min(screenWidth-4, 128), 40)
}

func boxTop(width int) string {
	return "┌" + strings.Repeat("─", max(width-2, 0)) + "┐"
}

func boxSeparator(width int) string {
	return "├" + strings.Repeat("─", max(width-2, 0)) + "┤"
}

func boxBottom(width int) string {
	return "└" + strings.Repeat("─", max(width-2, 0)) + "┘"
}

func boxRow(content string, width int) string {
	innerWidth := max(width-2, 0)
	return "│" + formatCell(content, innerWidth) + "│"
}

func padRight(value string, width int) string {
	length := lipgloss.Width(value)
	if length >= width {
		return value
	}

	return value + strings.Repeat(" ", width-length)
}

func (m model) emrDetailMaxStepPage() int {
	if len(m.emrDetail.steps) == 0 {
		return 0
	}

	return (len(m.emrDetail.steps) - 1) / emrDetailStepPageSize
}

func (m model) emrDetailYarnMaxPage() int {
	if len(m.emrDetail.yarnApps) == 0 {
		return 0
	}

	return (len(m.emrDetail.yarnApps) - 1) / emrDetailStepPageSize
}

func hasRunningStep(steps []appemr.Step) bool {
	for _, step := range steps {
		if step.State == "RUNNING" {
			return true
		}
	}

	return false
}

func formatDuration(durationMS int64) string {
	if durationMS <= 0 {
		return "0秒"
	}
	seconds := int(durationMS / 1000)
	hours := seconds / 3600
	seconds %= 3600
	minutes := seconds / 60
	seconds %= 60
	parts := []string{}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d时", hours))
	}
	if minutes > 0 || len(parts) > 0 {
		parts = append(parts, fmt.Sprintf("%d分", minutes))
	}
	parts = append(parts, fmt.Sprintf("%d秒", seconds))
	return strings.Join(parts, "")
}
