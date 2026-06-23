package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	openRouterUsageURL = "https://openrouter.ai/api/v1/key"
	pollSeconds        = 60
	hgWidth            = 13
	chamberRows        = 6
	pinchRows          = 3
	budgetLayers       = 100
)

var chamberWidths = [...]int{11, 9, 7, 5, 3, 1}

type KeyData struct {
	Label      string   `json:"label"`
	Usage      float64  `json:"usage"`
	Limit      *float64 `json:"limit"`
	IsFreeTier bool     `json:"is_free_tier"`
	RateLimit  struct {
		Requests int    `json:"requests"`
		Interval string `json:"interval"`
	} `json:"rate_limit"`
}

type usageClient struct {
	http      *http.Client
	authValue string
}

func newUsageClient(apiKey string) usageClient {
	return usageClient{
		http:      &http.Client{Timeout: 10 * time.Second},
		authValue: "Bearer " + apiKey,
	}
}

func (c usageClient) FetchUsage() (*KeyData, error) {
	req, err := http.NewRequest(http.MethodGet, openRouterUsageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authValue)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter usage request returned %d", resp.StatusCode)
	}

	var body struct {
		Data KeyData `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return &body.Data, nil
}

type particle struct {
	row, col, ttl int
}

func buildHourglass(pct float64, particles []particle, tick int) []string {
	spent := int(math.Round(clamp(pct, 0, 1) * budgetLayers))
	lines := []string{"╔" + strings.Repeat("═", hgWidth-2) + "╗"}

	for row := 0; row < chamberRows*2+pinchRows; row++ {
		if row >= chamberRows && row < chamberRows+pinchRows {
			lines = append(lines, renderPinch(row, particles, tick))
			continue
		}
		lines = append(lines, renderChamberRow(row, budgetLayers-spent, spent, particles, tick))
	}

	return append(lines, "╚"+strings.Repeat("═", hgWidth-2)+"╝")
}

func renderPinch(row int, particles []particle, tick int) string {
	center := hgWidth / 2
	switch row - chamberRows {
	case 0:
		return strings.Repeat(" ", center-1) + `\ /` + strings.Repeat(" ", center-1)
	case 1:
		return strings.Repeat(" ", hgWidth)
	default:
		return strings.Repeat(" ", center-1) + `/ \` + strings.Repeat(" ", center-1)
	}
}

func renderChamberRow(row, topUnits, bottomUnits int, particles []particle, tick int) string {
	left, right := chamberBounds(row)
	top := row < chamberRows
	leftFrame, rightFrame := `/`, `\`
	if top {
		leftFrame, rightFrame = `\`, `/`
	}

	var sb strings.Builder
	for col := 0; col < hgWidth; col++ {
		switch {
		case col == left-1:
			sb.WriteString(leftFrame)
		case col == right+1:
			sb.WriteString(rightFrame)
		case col < left || col > right:
			sb.WriteByte(' ')
		case hasParticle(particles, row, col):
			sb.WriteString(grainChar(tick + col))
		case top:
			sb.WriteString(sandAt(row, col, topUnits, tick))
		default:
			sb.WriteString(sandAt(row, col, bottomUnits, tick))
		}
	}
	return sb.String()
}

func sandAt(row, col, units, tick int) string {
	fullness := sandFullness(cellRank(row, col), units)
	switch {
	case fullness >= 1:
		return sandChar(row, col, tick)
	case fullness >= 0.75:
		return "▓"
	case fullness >= 0.5:
		return "▒"
	case fullness > 0:
		return "░"
	default:
		return " "
	}
}

func cellRank(row, col int) int {
	chamberRow := visualChamberRow(row)
	rank := 0
	for r := chamberRows - 1; r > chamberRow; r-- {
		rank += rowWidth(rowForChamber(r, row >= chamberRows+pinchRows))
	}
	left, _ := chamberBounds(row)
	return rank + centeredColumnRank(col-left, rowWidth(row))
}

func visualChamberRow(row int) int {
	if row >= chamberRows+pinchRows {
		return row - chamberRows - pinchRows
	}
	return row
}

func rowForChamber(chamberRow int, bottom bool) int {
	if bottom {
		return chamberRows + pinchRows + chamberRow
	}
	return chamberRow
}

func chamberBounds(row int) (int, int) {
	width := rowWidth(row)
	left := (hgWidth - width) / 2
	return left, left + width - 1
}

func rowWidth(row int) int {
	if row >= chamberRows+pinchRows {
		return chamberWidths[chamberRows-1-visualChamberRow(row)]
	}
	if row >= chamberRows {
		return 1
	}
	return chamberWidths[row]
}

func centeredColumnRank(offset, width int) int {
	center := width / 2
	distance := abs(offset - center)
	if distance == 0 {
		return 0
	}
	if offset < center {
		return distance*2 - 1
	}
	return distance * 2
}

func chamberCapacity() int {
	total := 0
	for _, width := range chamberWidths {
		total += width
	}
	return total
}

func sandFullness(rank, units int) float64 {
	unitsPerCell := float64(budgetLayers) / float64(chamberCapacity())
	return (float64(units) - float64(rank)*unitsPerCell) / unitsPerCell
}

func hasParticle(particles []particle, row, col int) bool {
	for _, p := range particles {
		if p.row == row && p.col == col {
			return true
		}
	}
	return false
}

func sandChar(row, col, tick int) string {
	chars := [...]string{"░", "▒", "░", "▒", "░"}
	return chars[(row+col+tick/4)%len(chars)]
}

func grainChar(tick int) string {
	chars := [...]string{"·", "•", "∘", "◦", "·"}
	return chars[tick%len(chars)]
}

func spawnInterval(spendRate float64) int {
	if spendRate <= 0 {
		return 60
	}
	return clampInt(int(math.Round(-math.Log10(math.Max(spendRate, 0.00001))*3)), 1, 40)
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#C792EA")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7B61FF")).
			Padding(0, 2)

	panelStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444466")).
			Padding(0, 2)

	hgStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7B61FF")).
		Padding(0, 1)

	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888899")).Width(14)
	valueStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E0E0FF"))

	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#A3E635"))
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#555566"))
	sandStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#D4A853"))
	grainStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FCD34D"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")).Bold(true)
	frameStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#8888BB"))
)

type (
	tickMsg time.Time
	dataMsg struct {
		data *KeyData
		err  error
	}
)

type model struct {
	client    usageClient
	data      *KeyData
	err       error
	tick      int
	lastUsage float64
	spendRate float64
	loading   bool
	termWidth int
	quitting  bool
	particles []particle
}

func initialModel(client usageClient) model {
	return model{client: client, loading: true, termWidth: 100}
}

func fetchCmd(client usageClient) tea.Cmd {
	return func() tea.Msg {
		data, err := client.FetchUsage()
		return dataMsg{data: data, err: err}
	}
}

func animTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(m.client), animTick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r", "R":
			m.loading = true
			return m, fetchCmd(m.client)
		}
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
	case dataMsg:
		m.applyData(msg)
		return m, tea.Tick(pollSeconds*time.Second, func(time.Time) tea.Msg {
			return fetchCmd(m.client)()
		})
	case tickMsg:
		m.tick++
		m.updateParticles()
		return m, animTick()
	}
	return m, nil
}

func (m *model) applyData(msg dataMsg) {
	m.loading = false
	m.err = msg.err
	if msg.data == nil {
		return
	}
	if m.data != nil && msg.data.Usage > m.lastUsage {
		m.spendRate = msg.data.Usage - m.lastUsage
	} else {
		m.spendRate = 0
	}
	m.lastUsage = msg.data.Usage
	m.data = msg.data
}

func (m *model) updateParticles() {
	spent := m.spentLayers()
	next := m.particles[:0]
	for _, p := range m.particles {
		p.row++
		p.ttl--
		if p.ttl > 0 && !particleHitsBottomSand(p, spent) {
			next = append(next, p)
		}
	}
	m.particles = next

	if m.tick%spawnInterval(m.spendRate) == 0 {
		m.particles = append(m.particles, particle{
			row: chamberRows - 1,
			col: hgWidth / 2,
			ttl: chamberRows + pinchRows + 2,
		})
	}
}

func (m model) spentLayers() int {
	if m.data == nil || m.data.Limit == nil || *m.data.Limit <= 0 {
		return 0
	}
	return int(math.Round(clamp(m.data.Usage / *m.data.Limit, 0, 1) * budgetLayers))
}

func particleHitsBottomSand(p particle, spent int) bool {
	if p.row < chamberRows+pinchRows || spent <= 0 {
		return false
	}
	left, right := chamberBounds(p.row)
	return p.col >= left && p.col <= right && sandFullness(cellRank(p.row, p.col), spent) > 0
}

func (m model) View() string {
	switch {
	case m.quitting:
		return "Bye. Watch that budget.\n"
	case m.err != nil:
		return titleStyle.Render("OpenRouter Usage") + "\n\n" +
			errorStyle.Render("Usage fetch failed: "+m.err.Error()) + "\n" +
			dimStyle.Render("Press r to retry, q to quit") + "\n"
	case m.loading && m.data == nil:
		spinner := [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		return titleStyle.Render("OpenRouter Usage") + "\n\n" +
			dimStyle.Render(spinner[m.tick%len(spinner)]+" Fetching usage...") + "\n" +
			dimStyle.Render("\nPress q to quit") + "\n"
	}

	body := m.renderPanels()
	footer := dimStyle.Render(fmt.Sprintf("q quit  r refresh  |  delta %s  |  every %ds", m.deltaLabel(), pollSeconds))
	return titleStyle.Render("OpenRouter Usage") + "\n\n" + body + "\n\n" + footer + "\n"
}

func (m model) renderPanels() string {
	left, right := m.renderUsagePanel(), m.renderHourglassPanel()
	if m.termWidth > 0 && m.termWidth < 82 {
		return left + "\n\n" + right
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right)
}

func (m model) renderUsagePanel() string {
	if m.data == nil {
		return panelStyle.Render(dimStyle.Render("loading..."))
	}
	rows := []string{
		m.renderGauge(),
		"",
		infoRow("Key label:", fallback(m.data.Label, "(unnamed)")),
		infoRow("Burn rate:", m.burnRateLabel()),
		"",
		dimStyle.Render("Last checked: " + time.Now().Format("15:04:05")),
	}
	return panelStyle.Width(44).Render(strings.Join(rows, "\n"))
}

func (m model) renderGauge() string {
	const barWidth = 32
	usage := fmt.Sprintf("$%.4f", m.data.Usage)
	if m.data.Limit == nil || *m.data.Limit <= 0 {
		return valueStyle.Render("Used: "+usage) + "  " + dimStyle.Render("(no limit)") + "\n" +
			dimStyle.Render("["+strings.Repeat("░", barWidth)+"]")
	}

	limit := *m.data.Limit
	pct := clamp(m.data.Usage/limit, 0, 1)
	filled := int(math.Round(barWidth * pct))
	color := percentStyle(pct)

	return infoRow("Used:", usage+dimStyle.Render(" of $"+fmt.Sprintf("%.4f", limit))) + "\n" +
		infoRow("Remaining:", color.Render(fmt.Sprintf("$%.4f", limit-m.data.Usage))) + "\n" +
		"[" + color.Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("░", barWidth-filled)) + "] " +
		color.Render(fmt.Sprintf("%.1f%%", pct*100))
}

func (m model) renderHourglassPanel() string {
	pct := 0.0
	if m.data != nil && m.data.Limit != nil && *m.data.Limit > 0 {
		pct = clamp(m.data.Usage / *m.data.Limit, 0, 1)
	}
	lines := buildHourglass(pct, m.particles, m.tick)
	for i, line := range lines {
		lines[i] = colorizeHourglass(line)
	}
	title := dimStyle.Render("budget remaining")
	contentWidth := lipgloss.Width(title)
	for _, line := range lines {
		if w := lipgloss.Width(line); w > contentWidth {
			contentWidth = w
		}
	}
	for i, line := range lines {
		lines[i] = centerLine(line, contentWidth)
	}
	return hgStyle.Render(centerLine(title, contentWidth) + "\n" + strings.Join(lines, "\n"))
}

func (m model) burnRateLabel() string {
	if m.spendRate <= 0 {
		return "idle"
	}
	return fmt.Sprintf("$%.5f/poll", m.spendRate)
}

func (m model) deltaLabel() string {
	if m.spendRate <= 0 {
		return "idle"
	}
	return fmt.Sprintf("$%.5f/poll", m.spendRate)
}

func colorizeHourglass(line string) string {
	var sb strings.Builder
	for _, ch := range line {
		s := string(ch)
		switch s {
		case `\`, `/`, `╔`, `╗`, `╚`, `╝`, `═`:
			sb.WriteString(frameStyle.Render(s))
		case "░", "▒", "▓":
			sb.WriteString(sandStyle.Render(s))
		case "·", "•", "∘", "◦":
			sb.WriteString(grainStyle.Render(s))
		default:
			sb.WriteString(s)
		}
	}
	return sb.String()
}

func infoRow(label, value string) string {
	return labelStyle.Render(label) + valueStyle.Render(value)
}

func centerLine(line string, width int) string {
	lineWidth := lipgloss.Width(line)
	if width <= lineWidth {
		return line
	}
	left := (width - lineWidth) / 2
	right := width - lineWidth - left
	return strings.Repeat(" ", left) + line + strings.Repeat(" ", right)
}

func percentStyle(pct float64) lipgloss.Style {
	switch {
	case pct < 0.5:
		return greenStyle
	case pct < 0.8:
		return yellowStyle
	default:
		return redStyle
	}
}

func yesNo(ok bool) string {
	if ok {
		return "Yes"
	}
	return "No"
}

func fallback(value, alt string) string {
	if value == "" {
		return alt
	}
	return value
}

func clamp(v, lo, hi float64) float64 {
	return math.Min(math.Max(v, lo), hi)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func main() {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		fmt.Print("Enter your OpenRouter API key: ")
		fmt.Scanln(&apiKey)
	}
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "No API key provided. Set OPENROUTER_API_KEY or enter it when prompted.")
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(newUsageClient(apiKey)), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
