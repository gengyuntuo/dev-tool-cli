package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	appemr "dev-tool-cli/internal/emr"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
		formatCell(createdAt, createdAtWidth),
		formatCell(state, stateWidth),
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
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(value, width, "…")
}

func formatCell(value string, width int) string {
	if lipgloss.Width(value) > width {
		value = truncate(value, width)
	}

	return padRight(value, width)
}

func formatCellRight(value string, width int) string {
	if lipgloss.Width(value) > width {
		value = truncate(value, width)
	}

	padding := max(width-lipgloss.Width(value), 0)
	return strings.Repeat(" ", padding) + value
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

func emrStepColumnWidths(tableWidth int) (int, int, int, int, int, int, int) {
	innerWidth := max(tableWidth-2, 0)
	availableWidth := max(innerWidth-12, 7)
	widths := []int{24, 8, 19, 19, 19, 16, 18}
	totalWidth := 0
	for _, width := range widths {
		totalWidth += width
	}

	shrink := func(index, minimum int) {
		if totalWidth <= availableWidth {
			return
		}
		reduction := min(widths[index]-minimum, totalWidth-availableWidth)
		widths[index] -= reduction
		totalWidth -= reduction
	}
	shrink(5, 10)
	shrink(6, 12)
	shrink(4, 12)
	shrink(3, 12)
	shrink(2, 12)
	shrink(1, 4)
	shrink(0, 8)
	for _, index := range []int{1, 5, 6, 4, 3, 2, 0} {
		shrink(index, 1)
	}

	if totalWidth < availableWidth {
		widths[1] += availableWidth - totalWidth
	}

	return widths[0], widths[1], widths[2], widths[3], widths[4], widths[5], widths[6]
}

func emrYarnColumnWidths(tableWidth int) (int, int, int, int, int, int) {
	innerWidth := max(tableWidth-2, 0)
	gapWidth := 10
	availableWidth := max(innerWidth-gapWidth, 6)
	widths := []int{16, 12, 8, 10, 10, 10}
	minimumWidth := 0
	for _, width := range widths {
		minimumWidth += width
	}
	if availableWidth < minimumWidth {
		widths = []int{8, 8, 3, 3, 3, 3}
	}

	remaining := availableWidth
	for _, width := range widths {
		remaining -= width
	}
	grow := func(index, target int) {
		increase := min(max(target-widths[index], 0), max(remaining, 0))
		widths[index] += increase
		remaining -= increase
	}

	grow(0, 32)
	grow(1, 36)
	grow(2, 12)
	grow(3, 19)
	grow(4, 16)
	grow(5, 12)
	if remaining > 0 {
		widths[1] += remaining
	}

	return widths[0], widths[1], widths[2], widths[3], widths[4], widths[5]
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

	content := m.renderEMRDetailContent(m.emrDetailTableWidth())
	body := lipgloss.NewStyle().Padding(1, 2).Render(content)
	return m.renderEMRDetailTabs(m.width) + "\n" +
		lipgloss.Place(m.width, max(height-1, 0), lipgloss.Left, lipgloss.Top, body)
}

func (m model) emrDetailTableWidth() int {
	if m.emrDetail.activeTab == "yarn" {
		return max(min(m.width-4, 180), 40)
	}
	if m.emrDetail.activeTab == "steps" {
		return max(min(m.width-4, 200), 40)
	}
	return tableWidth(m.width)
}

func (m model) renderEMRDetailContent(tableWidth int) string {
	detail := m.emrDetail.detail

	switch m.emrDetail.activeTab {
	case "overview":
		lines := strings.Split(m.renderEMROverviewPanel(tableWidth, detail), "\n")
		start := min(m.emrDetail.overviewScroll, max(len(lines)-1, 0))
		end := min(start+m.emrOverviewVisibleLines(), len(lines))
		return strings.Join(lines[start:end], "\n")
	case "yarn":
		return m.renderYarnDetailContent(tableWidth, detail)
	case "instances":
		return m.renderEMRInstancesPanel(tableWidth, detail)
	default:
		return m.renderEMRStepsPanel(tableWidth)
	}
}

func (m model) emrOverviewVisibleLines() int {
	return max(m.height-statusBarHeight-4, 1)
}

func (m model) emrOverviewMaxScroll() int {
	lines := strings.Split(m.renderEMROverviewPanel(tableWidth(m.width), m.emrDetail.detail), "\n")
	return max(len(lines)-m.emrOverviewVisibleLines(), 0)
}

func (m model) renderEMRDetailTabs(tableWidth int) string {
	sections := []struct {
		key   string
		label string
	}{
		{key: "overview", label: "Overview"},
		{key: "steps", label: "Steps"},
		{key: "yarn", label: "YARN"},
		{key: "instances", label: "Instances"},
	}

	items := make([]string, 0, len(sections))
	for _, section := range sections {
		style := lipgloss.NewStyle().Padding(0, 1)
		if m.emrDetail.activeTab == section.key {
			style = style.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Bold(true)
		}
		items = append(items, style.Render(section.label))
	}

	return lipgloss.NewStyle().
		Width(tableWidth).
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Render(strings.Join(items, ""))
}

func (m model) renderEMROverviewPanel(tableWidth int, detail appemr.ClusterDetail) string {
	lines := []string{
		"Cluster Overview",
		boxTop(tableWidth),
		boxRow(overviewField("ID", detail.ID), tableWidth),
		boxRow(overviewField("Name", detail.Name), tableWidth),
		boxRow(overviewField("Release", detail.ReleaseLabel), tableWidth),
		boxRow(overviewField("S3 Log URI", detail.LogURI), tableWidth),
		boxRow(overviewField("Primary node private DNS", detail.PrimaryNodePrivateDNS), tableWidth),
		boxRow(overviewField("Step Concurrency", detail.StepConcurrency), tableWidth),
		boxRow(overviewField("Service Role", detail.ServiceRole), tableWidth),
		boxRow(overviewField("Applications", strings.Join(detail.Applications, ", ")), tableWidth),
		boxBottom(tableWidth),
		"",
		"Status and time",
		boxTop(tableWidth),
		boxRow(overviewField("State", detail.State), tableWidth),
		boxRow(overviewField("State change reason", detail.StateChangeReason), tableWidth),
		boxRow(overviewField("Created At", detail.CreatedAt), tableWidth),
		boxRow(overviewField("Ready At", detail.ReadyAt), tableWidth),
		boxRow(overviewField("Ended At", detail.EndedAt), tableWidth),
		boxRow(overviewField("Elapsed", detail.Elapsed), tableWidth),
		boxBottom(tableWidth),
	}

	uiLinks := applicationUILinks(detail)
	if len(uiLinks) > 0 {
		lines = append(lines,
			"",
			"Application UIs on the primary node",
			boxTop(tableWidth),
		)
		for _, link := range uiLinks {
			label, url, _ := strings.Cut(link, ": ")
			lines = append(lines, boxRow(overviewField(label, url), tableWidth))
		}
		lines = append(lines, boxBottom(tableWidth))
	}

	return strings.Join(lines, "\n")
}

func overviewField(label, value string) string {
	labelText := lipgloss.NewStyle().
		Width(26).
		Bold(true).
		Foreground(lipgloss.Color("75")).
		Render(label + ":")
	return labelText + " " + value
}

func applicationUILinks(detail appemr.ClusterDetail) []string {
	host := detail.PrimaryNodePrivateDNS
	if host == "" || host == "-" {
		return nil
	}

	links := []string{
		"YARN ResourceManager: http://" + host + ":8088/",
	}
	if emrReleaseMajor(detail.ReleaseLabel) >= 6 {
		links = append(links, "HDFS NameNode: http://"+host+":9870/")
	} else {
		links = append(links, "HDFS NameNode: http://"+host+":50070/")
	}

	applicationLinks := []struct {
		application string
		label       string
		url         string
	}{
		{application: "Flink", label: "Flink History Server", url: "http://" + host + ":8082/"},
		{application: "Ganglia", label: "Ganglia", url: "http://" + host + "/ganglia/"},
		{application: "HBase", label: "HBase", url: "http://" + host + ":16010/"},
		{application: "Hue", label: "Hue", url: "http://" + host + ":8888/"},
		{application: "JupyterHub", label: "JupyterHub", url: "https://" + host + ":9443/"},
		{application: "Livy", label: "Livy", url: "http://" + host + ":8998/"},
		{application: "Spark", label: "Spark HistoryServer", url: "http://" + host + ":18080/"},
		{application: "Tez", label: "Tez", url: "http://" + host + ":8080/tez-ui"},
		{application: "Zeppelin", label: "Zeppelin", url: "http://" + host + ":8890/"},
	}
	for _, candidate := range applicationLinks {
		if clusterHasApplication(detail.Applications, candidate.application) {
			links = append(links, candidate.label+": "+candidate.url)
		}
	}

	return links
}

func clusterHasApplication(applications []string, name string) bool {
	for _, application := range applications {
		fields := strings.Fields(application)
		if len(fields) > 0 && strings.EqualFold(fields[0], name) {
			return true
		}
	}
	return false
}

func emrReleaseMajor(releaseLabel string) int {
	var major int
	if _, err := fmt.Sscanf(releaseLabel, "emr-%d", &major); err != nil {
		return 6
	}
	return major
}

func (m model) renderEMRInstancesPanel(tableWidth int, detail appemr.ClusterDetail) string {
	lines := []string{
		"Instances",
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
	lines = append(lines, boxBottom(tableWidth))
	return strings.Join(lines, "\n")
}

func (m model) renderEMRStepsPanel(tableWidth int) string {
	detail := m.emrDetail.detail
	pageSize := m.emrDetailPageSize()
	stepStart := m.emrDetail.stepPage * pageSize
	stepEnd := min(stepStart+pageSize, len(m.emrDetail.steps))
	totalStepPages := m.emrDetailMaxStepPage() + 1
	if m.emrDetail.stepMarker != "" {
		totalStepPagesLabel := fmt.Sprintf("%d+", totalStepPages)
		lines := []string{
			"Steps",
			fmt.Sprintf("Page %d/%s  Loaded %d", m.emrDetail.stepPage+1, totalStepPagesLabel, len(m.emrDetail.steps)),
			boxTop(tableWidth),
			boxRow(formatStepRow(tableWidth, "ID", "Name", "Created At", "Started At", "Ended At", "Elapsed", "State"), tableWidth),
			boxSeparator(tableWidth),
		}
		if m.emrDetail.stepLoading {
			lines = append(lines, boxRow("Loading more steps...", tableWidth))
		} else if m.emrDetail.stepErr != "" {
			lines = append(lines, boxRow("Step load failed: "+m.emrDetail.stepErr, tableWidth))
		} else if len(m.emrDetail.steps) == 0 {
			lines = append(lines, boxRow("No steps found.", tableWidth))
		} else {
			for i, step := range m.emrDetail.steps[stepStart:stepEnd] {
				row := boxRow(formatStepRow(tableWidth, step.ID, step.Name, step.CreatedAt, step.StartedAt, step.EndedAt, step.Elapsed, renderStepState(step.State, m.statusBlink)), tableWidth)
				if stepStart+i == m.emrDetail.stepSelected {
					row = selectedRowStyle(row, tableWidth)
				}
				lines = append(lines, row)
			}
		}
		lines = append(lines, boxBottom(tableWidth))
		return strings.Join(lines, "\n")
	}

	lines := []string{
		"Steps",
		fmt.Sprintf("Page %d/%d  Loaded %d", m.emrDetail.stepPage+1, totalStepPages, len(m.emrDetail.steps)),
		boxTop(tableWidth),
		boxRow(formatStepRow(tableWidth, "ID", "Name", "Created At", "Started At", "Ended At", "Elapsed", "State"), tableWidth),
		boxSeparator(tableWidth),
	}
	if m.emrDetail.stepLoading {
		lines = append(lines, boxRow("Loading steps...", tableWidth))
	} else if m.emrDetail.stepErr != "" {
		lines = append(lines, boxRow("Step load failed: "+m.emrDetail.stepErr, tableWidth))
	} else if len(m.emrDetail.steps) == 0 {
		lines = append(lines, boxRow("No steps found.", tableWidth))
	} else {
		for i, step := range m.emrDetail.steps[stepStart:stepEnd] {
			row := boxRow(formatStepRow(tableWidth, step.ID, step.Name, step.CreatedAt, step.StartedAt, step.EndedAt, step.Elapsed, renderStepState(step.State, m.statusBlink)), tableWidth)
			if stepStart+i == m.emrDetail.stepSelected {
				row = selectedRowStyle(row, tableWidth)
			}
			lines = append(lines, row)
		}
	}
	lines = append(lines, boxBottom(tableWidth))
	_ = detail
	return strings.Join(lines, "\n")
}

func (m model) renderYarnDetailContent(tableWidth int, detail appemr.ClusterDetail) string {
	pageSize := m.emrDetailPageSize()
	appStart := m.emrDetail.yarnPage * pageSize
	appEnd := min(appStart+pageSize, len(m.emrDetail.yarnApps))
	lines := []string{
		"YARN Applications",
		fmt.Sprintf("Page %d/%d  Loaded %d", m.emrDetail.yarnPage+1, m.emrDetailYarnMaxPage()+1, len(m.emrDetail.yarnApps)),
		boxTop(tableWidth),
		boxRow(formatYarnRow(tableWidth, "ID", "Name", "State", "User", "Started At", "Elapsed"), tableWidth),
		boxSeparator(tableWidth),
	}
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
	_ = detail
	return strings.Join(lines, "\n")
}

func (m *model) openSelectedEMRItemDialog() bool {
	switch m.emrDetail.activeTab {
	case "steps":
		if m.emrDetail.stepSelected < 0 || m.emrDetail.stepSelected >= len(m.emrDetail.steps) {
			return false
		}
		m.emrItemDialog = emrItemDialog{
			visible: true,
			kind:    "step",
			index:   m.emrDetail.stepSelected,
		}
		return true
	case "yarn":
		if m.emrDetail.yarnSelected < 0 || m.emrDetail.yarnSelected >= len(m.emrDetail.yarnApps) {
			return false
		}
		m.emrItemDialog = emrItemDialog{
			visible: true,
			kind:    "yarn",
			index:   m.emrDetail.yarnSelected,
		}
		return true
	default:
		return false
	}
}

func (m model) updateEMRDetailMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Y == 0 {
		sections := []string{"overview", "steps", "yarn", "instances"}
		if index, ok := emrDetailTabIndexAt(msg.X); ok {
			m.emrDetail.activeTab = sections[index]
			if m.emrDetail.activeTab == "yarn" && m.emrDetail.yarnApps == nil && !m.emrDetail.yarnLoading {
				m.emrDetail.yarnLoading = true
				m.emrDetail.yarnErr = ""
				return m, loadYarnApps(m.emrDetail.detail.PrimaryNodePrivateDNS)
			}
		}
		return m, nil
	}

	const firstDataRowY = 7

	if msg.X < 2 || msg.X >= 2+m.emrDetailTableWidth() || msg.Y < firstDataRowY {
		return m, nil
	}

	rowOffset := msg.Y - firstDataRowY
	pageSize := m.emrDetailPageSize()
	switch m.emrDetail.activeTab {
	case "steps":
		pageStart := m.emrDetail.stepPage * pageSize
		index := pageStart + rowOffset
		pageEnd := min(pageStart+pageSize, len(m.emrDetail.steps))
		if index < pageStart || index >= pageEnd {
			return m, nil
		}
		if index == m.emrDetail.stepSelected {
			m.openSelectedEMRItemDialog()
		} else {
			m.emrDetail.stepSelected = index
		}
	case "yarn":
		pageStart := m.emrDetail.yarnPage * pageSize
		index := pageStart + rowOffset
		pageEnd := min(pageStart+pageSize, len(m.emrDetail.yarnApps))
		if index < pageStart || index >= pageEnd {
			return m, nil
		}
		if index == m.emrDetail.yarnSelected {
			m.openSelectedEMRItemDialog()
		} else {
			m.emrDetail.yarnSelected = index
		}
	}

	return m, nil
}

func emrDetailTabIndexAt(x int) (int, bool) {
	labels := []string{"Overview", "Steps", "YARN", "Instances"}
	offset := 0
	for i, label := range labels {
		width := lipgloss.Width(label) + 2
		if x >= offset && x < offset+width {
			return i, true
		}
		offset += width
	}

	return 0, false
}

func (m model) renderEMRItemDialog(base string) string {
	dialog := m.emrItemDialogView()
	return m.overlayDialog(base, dialog)
}

func (m model) emrItemDialogView() string {
	boxWidth := min(max(m.width-8, 56), 100)
	boxWidth = min(boxWidth, max(m.width-2, 20))
	contentWidth := max(boxWidth-6, 1)
	fields := wrapDetailLines(m.emrItemDialogFields(), contentWidth)
	visibleLines := m.emrItemDialogVisibleLines()
	scroll := min(m.emrItemDialog.scroll, max(len(fields)-visibleLines, 0))
	end := min(scroll+visibleLines, len(fields))

	lines := []string{m.emrItemDialogTitle(), ""}
	if len(fields) > 0 {
		lines = append(lines, fields[scroll:end]...)
	}
	lines = append(lines, fmt.Sprintf("Details %d-%d/%d", min(scroll+1, len(fields)), end, len(fields)))

	button := lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("62")).
		Render("返回<Esc>")
	lines = append(lines, "", button)

	return lipgloss.NewStyle().
		Width(boxWidth-6).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Render(strings.Join(lines, "\n"))
}

func (m model) emrItemDialogTitle() string {
	if m.emrItemDialog.kind == "yarn" {
		return "YARN Application Detail"
	}
	return "Step Detail"
}

func (m model) emrItemDialogFields() []string {
	switch m.emrItemDialog.kind {
	case "step":
		if m.emrItemDialog.index < 0 || m.emrItemDialog.index >= len(m.emrDetail.steps) {
			return nil
		}
		step := m.emrDetail.steps[m.emrItemDialog.index]
		fields := []string{
			detailField("ID", step.ID),
			detailField("Name", step.Name),
			detailField("Created At", step.CreatedAt),
			detailField("Started At", step.StartedAt),
			detailField("Ended At", step.EndedAt),
			detailField("Elapsed", step.Elapsed),
			detailField("State", step.State),
		}
		for _, logLink := range stepLogLinks(step, m.emrDetail.detail) {
			label, url, _ := strings.Cut(logLink, ": ")
			fields = append(fields, detailField(label, url))
		}
		return fields
	case "yarn":
		if m.emrItemDialog.index < 0 || m.emrItemDialog.index >= len(m.emrDetail.yarnApps) {
			return nil
		}
		app := m.emrDetail.yarnApps[m.emrItemDialog.index]
		return []string{
			detailField("ID", app.ID),
			detailField("Name", app.Name),
			detailField("User", app.User),
			detailField("Queue", app.Queue),
			detailField("Application Type", app.ApplicationType),
			detailField("Application Tags", app.ApplicationTags),
			detailField("Priority", app.Priority),
			detailField("State", app.State),
			detailField("Final Status", app.FinalStatus),
			detailField("Progress", app.Progress),
			detailField("Started At", app.StartedAt),
			detailField("Finished At", app.FinishedAt),
			detailField("Elapsed", app.Elapsed),
			detailField("Tracking URL", app.TrackingURL),
			detailField("Diagnostics", app.Diagnostics),
			detailField("AM Container Logs", app.AMContainerLogs),
			detailField("AM Host HTTP Address", app.AMHostHTTPAddress),
			detailField("Allocated Memory", app.AllocatedMB+" MB"),
			detailField("Allocated vCores", app.AllocatedVCores),
			detailField("Reserved Memory", app.ReservedMB+" MB"),
			detailField("Reserved vCores", app.ReservedVCores),
			detailField("Running Containers", app.RunningContainers),
			detailField("Memory Seconds", app.MemorySeconds),
			detailField("vCore Seconds", app.VCoreSeconds),
			detailField("Queue Usage", app.QueueUsagePercentage),
			detailField("Cluster Usage", app.ClusterUsagePercentage),
			detailField("Preempted Memory", app.PreemptedResourceMB+" MB"),
			detailField("Preempted vCores", app.PreemptedResourceVCores),
			detailField("Non-AM Containers Preempted", app.NonAMContainersPreempted),
			detailField("AM Containers Preempted", app.AMContainersPreempted),
			detailField("Log Aggregation Status", app.LogAggregationStatus),
			detailField("Unmanaged Application", app.UnmanagedApplication),
			detailField("App Node Label", app.AppNodeLabelExpression),
			detailField("AM Node Label", app.AMNodeLabelExpression),
		}
	default:
		return nil
	}
}

func detailField(label, value string) string {
	labelText := lipgloss.NewStyle().
		Width(30).
		Bold(true).
		Foreground(lipgloss.Color("75")).
		Render(label + ":")
	return labelText + " " + value
}

func (m model) emrItemDialogVisibleLines() int {
	return max(m.dialogContentHeight()-9, 1)
}

func (m model) emrItemDialogMaxScroll() int {
	boxWidth := min(max(m.width-8, 56), 100)
	boxWidth = min(boxWidth, max(m.width-2, 20))
	fields := wrapDetailLines(m.emrItemDialogFields(), max(boxWidth-6, 1))
	return max(len(fields)-m.emrItemDialogVisibleLines(), 0)
}

func wrapDetailLines(values []string, width int) []string {
	lines := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			lines = append(lines, "")
			continue
		}

		for lipgloss.Width(value) > width {
			lines = append(lines, ansi.Cut(value, 0, width))
			value = ansi.Cut(value, width, lipgloss.Width(value))
		}
		if value != "" {
			lines = append(lines, value)
		}
	}
	return lines
}

func (m model) updateEMRItemDialogMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	dialog := m.emrItemDialogView()
	dialogWidth := lipgloss.Width(dialog)
	dialogHeight := lipgloss.Height(dialog)
	left := (m.width - dialogWidth) / 2
	top := (m.dialogContentHeight() - dialogHeight) / 2

	if msg.X >= left && msg.X < left+dialogWidth && msg.Y == top+dialogHeight-3 {
		m.emrItemDialog.visible = false
	}

	return m, nil
}

func stepLogLinks(step appemr.Step, detail appemr.ClusterDetail) []string {
	logURI := step.LogURI
	if logURI == "" || logURI == "-" {
		logURI = detail.LogURI
	}
	if logURI == "" || logURI == "-" || detail.ID == "" || step.ID == "" {
		return nil
	}

	baseURI := strings.TrimRight(logURI, "/")
	stepPath := "/steps/" + step.ID
	if !strings.Contains(baseURI, stepPath) {
		baseURI += "/" + detail.ID + stepPath
	}

	return []string{
		"controller: " + baseURI + "/controller.gz",
		"syslog: " + baseURI + "/syslog.gz",
		"stderr: " + baseURI + "/stderr.gz",
		"stdout: " + baseURI + "/stdout.gz",
	}
}

func formatInstanceRow(kind, instanceType, count string) string {
	return strings.Join([]string{
		formatCell(kind, 12),
		formatCell(instanceType, 40),
		formatCell(count, 8),
	}, "  ")
}

func formatStepRow(tableWidth int, id, name, createdAt, startedAt, endedAt, elapsed, state string) string {
	idWidth, nameWidth, createdAtWidth, startedAtWidth, endedAtWidth, elapsedWidth, stateWidth := emrStepColumnWidths(tableWidth)
	return strings.Join([]string{
		formatCell(id, idWidth),
		formatCell(name, nameWidth),
		formatCell(createdAt, createdAtWidth),
		formatCell(startedAt, startedAtWidth),
		formatCell(endedAt, endedAtWidth),
		formatCellRight(elapsed, elapsedWidth),
		formatCell(state, stateWidth),
	}, "  ")
}

func formatYarnRow(tableWidth int, id, name, state, user, startedAt, elapsed string) string {
	idWidth, nameWidth, userWidth, startedAtWidth, elapsedWidth, stateWidth := emrYarnColumnWidths(tableWidth)
	return strings.Join([]string{
		formatCell(id, idWidth),
		formatCell(name, nameWidth),
		formatCell(user, userWidth),
		formatCell(startedAt, startedAtWidth),
		formatCellRight(elapsed, elapsedWidth),
		formatCell(renderYarnState(state), stateWidth),
	}, "  ")
}

func renderYarnState(state string) string {
	switch state {
	case "RUNNING":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Render("● RUNNING")
	case "FINISHED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render("● FINISHED")
	case "FAILED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("● FAILED")
	case "KILLED":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("● KILLED")
	case "ACCEPTED", "NEW":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("● " + state)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render("● " + state)
	}
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

	return (len(m.emrDetail.steps) - 1) / m.emrDetailPageSize()
}

func (m model) emrDetailYarnMaxPage() int {
	if len(m.emrDetail.yarnApps) == 0 {
		return 0
	}

	return (len(m.emrDetail.yarnApps) - 1) / m.emrDetailPageSize()
}

func (m model) emrDetailPageSize() int {
	return max(m.height-10, 1)
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
