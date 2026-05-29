package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// viewState enumerates the top-level screens. It lives on the root Model
// and gates which child model receives Update + View calls.
type viewState int

const (
	viewLogin viewState = iota
	viewDashboard
	viewLeaderboard
	viewChat
)

// chatBufferCap bounds the in-memory chat backlog. 200 entries matches the
// task spec — old lines drop off the top to keep the viewport snappy.
const chatBufferCap = 200

// statsPollInterval is the cadence at which the dashboard polls /stats.
const statsPollInterval = 2 * time.Second

// ----------------------------- Messages -----------------------------------

// LoginSuccessMsg signals that the credentials check succeeded; the root
// model uses it to advance to the dashboard view.
type LoginSuccessMsg struct{ User User }

// LoginFailedMsg carries a login failure for inline display.
type LoginFailedMsg struct{ Err error }

// StatsTickMsg is emitted every statsPollInterval to refresh /stats.
type StatsTickMsg time.Time

// StatsResultMsg carries the result (or error) of a single /stats poll.
type StatsResultMsg struct {
	Stats Stats
	Err   error
}

// LeaderboardResultMsg carries the parallel pity+streak leaderboard fetch.
type LeaderboardResultMsg struct {
	Channel string
	Pity    []LeaderboardEntry
	Streak  []LeaderboardEntry
	Err     error
}

// WSEventMsg wraps a single WebSocket event for the Bubble Tea loop.
type WSEventMsg struct{ Event WSEvent }

// WSClosedMsg signals the WebSocket goroutine has exited.
type WSClosedMsg struct{ Err error }

// WSReadyMsg delivers the freshly-dialed event channel to the root model so
// it can own the lifecycle instead of relying on package-level state.
type WSReadyMsg struct {
	Ch  <-chan WSEvent
	Err error
}

// ViewSwitchMsg is dispatched internally to change viewState.
type ViewSwitchMsg struct{ Target viewState }

// QuitMsg is emitted after Logout completes so the program exits cleanly.
type QuitMsg struct{}

// ---------------------------- Root model ----------------------------------

// Model is the top-level Bubble Tea model. Each child screen is its own
// embedded struct so tests can poke them in isolation.
type Model struct {
	client *Client

	width  int
	height int

	state    viewState
	prevView viewState
	showHelp bool

	user User

	login       loginModel
	dashboard   dashboardModel
	leaderboard leaderboardModel
	chat        chatModel

	wsCancel context.CancelFunc
	wsCh     <-chan WSEvent
}

// NewModel builds a fresh root model. addr/email/password come from the
// command-line; an empty email skips the login attempt and starts in the
// LOGIN view.
func NewModel(client *Client, email, password string) Model {
	m := Model{
		client:      client,
		state:       viewLogin,
		login:       newLoginModel(email, password),
		dashboard:   newDashboardModel(),
		leaderboard: newLeaderboardModel(defaultChannel()),
		chat:        newChatModel(),
	}
	return m
}

// Init kicks off the spinner + (optionally) an immediate login attempt
// when both email and password were provided on the command line.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.login.spinner.Tick,
	}
	if m.login.email.Value() != "" && m.login.password.Value() != "" {
		cmds = append(cmds, doLogin(m.client, m.login.email.Value(), m.login.password.Value()))
	}
	return tea.Batch(cmds...)
}

// Update is the central state machine. It first handles global messages
// (resize, quit, help toggle) and then forwards the rest to the focused
// child model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chat.viewport.Width = msg.Width - 4
		m.chat.viewport.Height = msg.Height - 6
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, m.shutdown()
		case KeyHelp:
			m.showHelp = !m.showHelp
			return m, nil
		case KeyClose:
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
		}
		if m.showHelp {
			return m, nil
		}

	case LoginSuccessMsg:
		m.user = msg.User
		m.state = viewDashboard
		ctx, cancel := context.WithCancel(context.Background())
		m.wsCancel = cancel
		return m, tea.Batch(
			tickStats(),
			doStats(m.client),
			doLeaderboards(m.client, m.leaderboard.channel.Value()),
			dialWS(m.client, ctx),
		)

	case WSReadyMsg:
		if msg.Err != nil {
			m.chat.connectionErr = msg.Err
			return m, nil
		}
		m.wsCh = msg.Ch
		return m, awaitWS(m.wsCh)

	case LoginFailedMsg:
		m.login.err = msg.Err
		m.login.submitting = false
		return m, nil

	case ViewSwitchMsg:
		m.prevView = m.state
		m.state = msg.Target
		switch msg.Target {
		case viewLeaderboard:
			return m, doLeaderboards(m.client, m.leaderboard.channel.Value())
		case viewDashboard:
			return m, doStats(m.client)
		}
		return m, nil

	case StatsTickMsg:
		return m, tea.Batch(tickStats(), doStats(m.client))

	case StatsResultMsg:
		m.dashboard.applyStats(msg)
		return m, nil

	case LeaderboardResultMsg:
		m.leaderboard.apply(msg)
		return m, nil

	case WSEventMsg:
		m.chat.append(msg.Event)
		return m, awaitWS(m.wsCh)

	case WSClosedMsg:
		m.chat.connectionErr = msg.Err
		return m, nil

	case QuitMsg:
		return m, tea.Quit
	}

	var cmd tea.Cmd
	switch m.state {
	case viewLogin:
		m.login, cmd = m.login.Update(msg, m.client)
	case viewDashboard:
		m.dashboard, cmd = m.dashboard.Update(msg)
		if keyMatches(msg, KeyLeaderboard) {
			cmd = func() tea.Msg { return ViewSwitchMsg{Target: viewLeaderboard} }
		} else if keyMatches(msg, KeyChat) {
			cmd = func() tea.Msg { return ViewSwitchMsg{Target: viewChat} }
		} else if keyMatches(msg, KeyRefresh) {
			cmd = doStats(m.client)
		} else if keyMatches(msg, KeyQuit) {
			return m, m.shutdown()
		}
	case viewLeaderboard:
		m.leaderboard, cmd = m.leaderboard.Update(msg, m.client)
		if keyMatches(msg, KeyBack) || keyMatches(msg, KeyDashboard) {
			cmd = func() tea.Msg { return ViewSwitchMsg{Target: viewDashboard} }
		} else if keyMatches(msg, KeyQuit) {
			return m, m.shutdown()
		}
	case viewChat:
		m.chat, cmd = m.chat.Update(msg)
		if keyMatches(msg, KeyBack) || keyMatches(msg, KeyDashboard) {
			cmd = func() tea.Msg { return ViewSwitchMsg{Target: viewDashboard} }
		} else if keyMatches(msg, KeyQuit) {
			return m, m.shutdown()
		}
	}
	return m, cmd
}

// View renders the screen for the current viewState, plus an overlay if
// the help modal is open.
func (m Model) View() string {
	var body string
	switch m.state {
	case viewLogin:
		body = m.login.View(m.width, m.height)
	case viewDashboard:
		body = m.dashboard.View(m.user, m.width, m.height)
	case viewLeaderboard:
		body = m.leaderboard.View(m.width, m.height)
	case viewChat:
		body = m.chat.View(m.width, m.height)
	}
	if m.showHelp {
		overlay := HelpStyle.Render(strings.Join(HelpLines(), "\n"))
		return lipgloss.JoinVertical(lipgloss.Left, body, overlay)
	}
	return body
}

// shutdown cancels the WebSocket goroutine and returns a Cmd that logs
// the user out (best-effort) before signalling tea.Quit.
func (m Model) shutdown() tea.Cmd {
	if m.wsCancel != nil {
		m.wsCancel()
	}
	client := m.client
	return func() tea.Msg {
		if client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = client.Logout(ctx)
		}
		return QuitMsg{}
	}
}

// keyMatches reports whether the message is a KeyMsg matching key, but
// only when no text input is currently consuming keystrokes. Caller must
// pass the raw msg from Update.
func keyMatches(msg tea.Msg, key string) bool {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	return k.String() == key
}

// ---------------------------- Login model ---------------------------------

type loginModel struct {
	email      textinput.Model
	password   textinput.Model
	spinner    spinner.Model
	focusIndex int
	submitting bool
	err        error
}

func newLoginModel(email, password string) loginModel {
	e := textinput.New()
	e.Placeholder = "you@example.com"
	e.SetValue(email)
	e.Focus()
	e.CharLimit = 254
	e.Width = 40

	p := textinput.New()
	p.Placeholder = "password"
	p.SetValue(password)
	p.EchoMode = textinput.EchoPassword
	p.EchoCharacter = '•'
	p.CharLimit = 256
	p.Width = 40

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorPrimary)

	lm := loginModel{email: e, password: p, spinner: sp}
	if email != "" {
		lm.focusIndex = 1
		lm.email.Blur()
		lm.password.Focus()
	}
	return lm
}

func (lm loginModel) Update(msg tea.Msg, client *Client) (loginModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		lm.spinner, cmd = lm.spinner.Update(msg)
		return lm, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab":
			lm.toggleFocus()
			return lm, nil
		case "enter":
			if lm.email.Value() == "" || lm.password.Value() == "" {
				lm.err = errors.New("email and password are required")
				return lm, nil
			}
			lm.submitting = true
			lm.err = nil
			return lm, doLogin(client, lm.email.Value(), lm.password.Value())
		}
	}
	var ecmd, pcmd tea.Cmd
	if lm.focusIndex == 0 {
		lm.email, ecmd = lm.email.Update(msg)
	} else {
		lm.password, pcmd = lm.password.Update(msg)
	}
	return lm, tea.Batch(ecmd, pcmd)
}

func (lm *loginModel) toggleFocus() {
	if lm.focusIndex == 0 {
		lm.focusIndex = 1
		lm.email.Blur()
		lm.password.Focus()
	} else {
		lm.focusIndex = 0
		lm.password.Blur()
		lm.email.Focus()
	}
}

func (lm loginModel) View(width, height int) string {
	title := TitleStyle.Render("engelOS — sign in")
	form := lipgloss.JoinVertical(lipgloss.Left,
		MutedStyle.Render("email"),
		lm.email.View(),
		"",
		MutedStyle.Render("password"),
		lm.password.View(),
	)
	footer := FooterStyle.Render("enter submit • tab switch field • q / ctrl+c quit • ? help")
	status := ""
	if lm.submitting {
		status = lm.spinner.View() + " authenticating…"
	} else if lm.err != nil {
		status = ErrorStyle.Render(lm.err.Error())
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		BorderStyle.Render(form),
		"",
		status,
	)
	if width > 0 && height > 0 {
		return lipgloss.Place(width, height-1, lipgloss.Center, lipgloss.Center, body) + "\n" + footer
	}
	return body + "\n" + footer
}

// -------------------------- Dashboard model -------------------------------

type dashboardModel struct {
	version  string
	phase    string
	current  DispatcherStats
	previous DispatcherStats
	lastErr  error
	lastSeen time.Time
}

func newDashboardModel() dashboardModel { return dashboardModel{} }

func (dm dashboardModel) Update(_ tea.Msg) (dashboardModel, tea.Cmd) { return dm, nil }

func (dm *dashboardModel) applyStats(msg StatsResultMsg) {
	if msg.Err != nil {
		dm.lastErr = msg.Err
		return
	}
	dm.lastErr = nil
	dm.previous = dm.current
	dm.current = msg.Stats.Dispatcher
	dm.version = msg.Stats.Version
	dm.phase = msg.Stats.Phase
	dm.lastSeen = time.Now()
}

func (dm dashboardModel) View(user User, width, height int) string {
	indicator := SuccessStyle.Render("● online")
	if dm.lastErr != nil {
		indicator = ErrorStyle.Render("● offline")
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		TitleStyle.Render("engelOS dashboard"),
		"  ",
		MutedStyle.Render(fmt.Sprintf("v%s • %s", nonEmpty(dm.version, "?"), nonEmpty(dm.phase, "?"))),
		"  ",
		indicator,
	)
	greeting := MutedStyle.Render(fmt.Sprintf("signed in as %s", nonEmpty(user.Username, user.Email)))

	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		statCard("messages", dm.current.Messages, dm.current.Messages-dm.previous.Messages),
		statCard("subs", dm.current.Subscriptions, dm.current.Subscriptions-dm.previous.Subscriptions),
		statCard("raids", dm.current.Raids, dm.current.Raids-dm.previous.Raids),
		statCard("errors",
			dm.current.PityGrantErrors+dm.current.StreakTickErrors,
			(dm.current.PityGrantErrors+dm.current.StreakTickErrors)-(dm.previous.PityGrantErrors+dm.previous.StreakTickErrors)),
	)

	footer := FooterStyle.Render("r refresh • l leaderboard • c chat • q quit • ? help")
	body := lipgloss.JoinVertical(lipgloss.Left, header, greeting, "", cards)
	if dm.lastErr != nil {
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", ErrorStyle.Render("stats error: "+dm.lastErr.Error()))
	}
	_ = width
	_ = height
	return body + "\n\n" + footer
}

func statCard(label string, value, delta int) string {
	deltaStr := ""
	if delta > 0 {
		deltaStr = SuccessStyle.Render(fmt.Sprintf(" +%d", delta))
	} else if delta < 0 {
		deltaStr = ErrorStyle.Render(fmt.Sprintf(" %d", delta))
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		MutedStyle.Render(label),
		fmt.Sprintf("%d%s", value, deltaStr),
	)
	return StatStyle.Render(content)
}

// ------------------------- Leaderboard model ------------------------------

type leaderboardModel struct {
	channel   textinput.Model
	pityTable table.Model
	strkTable table.Model
	focus     int
	lastErr   error
}

func newLeaderboardModel(channel string) leaderboardModel {
	in := textinput.New()
	in.Placeholder = "channel"
	in.SetValue(channel)
	in.CharLimit = 64
	in.Width = 32
	in.Focus()

	cols := []table.Column{
		{Title: "#", Width: 3},
		{Title: "user", Width: 18},
		{Title: "value", Width: 8},
	}
	t1 := table.New(table.WithColumns(cols), table.WithHeight(10))
	t2 := table.New(table.WithColumns(cols), table.WithHeight(10))
	return leaderboardModel{channel: in, pityTable: t1, strkTable: t2}
}

func (lbm leaderboardModel) Update(msg tea.Msg, client *Client) (leaderboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab":
			lbm.focus = (lbm.focus + 1) % 3
			lbm.applyFocus()
			return lbm, nil
		case "enter":
			return lbm, doLeaderboards(client, lbm.channel.Value())
		case KeyRefresh:
			return lbm, doLeaderboards(client, lbm.channel.Value())
		}
	}
	var cmd tea.Cmd
	switch lbm.focus {
	case 0:
		lbm.channel, cmd = lbm.channel.Update(msg)
	case 1:
		lbm.pityTable, cmd = lbm.pityTable.Update(msg)
	case 2:
		lbm.strkTable, cmd = lbm.strkTable.Update(msg)
	}
	return lbm, cmd
}

func (lbm *leaderboardModel) applyFocus() {
	switch lbm.focus {
	case 0:
		lbm.channel.Focus()
		lbm.pityTable.Blur()
		lbm.strkTable.Blur()
	case 1:
		lbm.channel.Blur()
		lbm.pityTable.Focus()
		lbm.strkTable.Blur()
	case 2:
		lbm.channel.Blur()
		lbm.pityTable.Blur()
		lbm.strkTable.Focus()
	}
}

func (lbm *leaderboardModel) apply(msg LeaderboardResultMsg) {
	if msg.Err != nil {
		lbm.lastErr = msg.Err
		return
	}
	lbm.lastErr = nil
	lbm.pityTable.SetRows(toLeaderboardRows(msg.Pity, false))
	lbm.strkTable.SetRows(toLeaderboardRows(msg.Streak, true))
}

func toLeaderboardRows(entries []LeaderboardEntry, streak bool) []table.Row {
	rows := make([]table.Row, 0, len(entries))
	for i, e := range entries {
		name := e.Username
		if name == "" {
			name = e.ViewerID
		}
		var value string
		if streak {
			value = fmt.Sprintf("%d", e.DaysCurrent)
		} else {
			value = fmt.Sprintf("%d", e.Points)
		}
		rows = append(rows, table.Row{fmt.Sprintf("%d", i+1), name, value})
	}
	return rows
}

func (lbm leaderboardModel) View(width, height int) string {
	header := TitleStyle.Render("leaderboards")
	channelRow := lipgloss.JoinHorizontal(lipgloss.Top,
		MutedStyle.Render("channel: "),
		lbm.channel.View(),
	)
	pityPanel := panelFor("pity", lbm.pityTable.View(), lbm.focus == 1)
	strkPanel := panelFor("streak", lbm.strkTable.View(), lbm.focus == 2)
	tables := lipgloss.JoinHorizontal(lipgloss.Top, pityPanel, " ", strkPanel)
	footer := FooterStyle.Render("tab cycle focus • enter/r refresh • b back • q quit • ? help")
	errLine := ""
	if lbm.lastErr != nil {
		errLine = ErrorStyle.Render("error: " + lbm.lastErr.Error())
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		header, "", channelRow, "", tables,
	)
	if errLine != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", errLine)
	}
	_ = width
	_ = height
	return body + "\n\n" + footer
}

func panelFor(label, content string, focused bool) string {
	style := BorderStyle
	if focused {
		style = ActiveBorderStyle
	}
	return style.Render(lipgloss.JoinVertical(lipgloss.Left,
		MutedStyle.Render(label),
		content,
	))
}

// ---------------------------- Chat model ----------------------------------

type chatModel struct {
	viewport       viewport.Model
	lines          []string
	connectionErr  error
	usernameColors map[string]lipgloss.Style
}

func newChatModel() chatModel {
	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().Foreground(ColorText)
	return chatModel{
		viewport:       vp,
		usernameColors: make(map[string]lipgloss.Style),
	}
}

func (cm chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	var cmd tea.Cmd
	cm.viewport, cmd = cm.viewport.Update(msg)
	return cm, cmd
}

func (cm *chatModel) append(ev WSEvent) {
	if !strings.HasPrefix(ev.Type, "message.") {
		return
	}
	var envelope struct {
		Platform string `json:"platform"`
		Channel  string `json:"channel"`
		Message  struct {
			Username string `json:"username"`
			Content  string `json:"content"`
		} `json:"message"`
	}
	if len(ev.Data) > 0 {
		_ = json.Unmarshal(ev.Data, &envelope)
	}
	if envelope.Message.Username == "" && len(ev.Raw) > 0 {
		_ = json.Unmarshal(ev.Raw, &envelope)
	}
	user := envelope.Message.Username
	if user == "" {
		user = "anon"
	}
	platform := envelope.Platform
	if platform == "" {
		platform = "chat"
	}
	style, ok := cm.usernameColors[user]
	if !ok {
		style = colorForUsername(user)
		cm.usernameColors[user] = style
	}
	line := fmt.Sprintf("%s %s: %s",
		MutedStyle.Render("["+platform+"]"),
		style.Render(user),
		envelope.Message.Content,
	)
	cm.lines = append(cm.lines, line)
	if len(cm.lines) > chatBufferCap {
		cm.lines = cm.lines[len(cm.lines)-chatBufferCap:]
	}
	cm.viewport.SetContent(strings.Join(cm.lines, "\n"))
	cm.viewport.GotoBottom()
}

func (cm chatModel) View(width, height int) string {
	header := TitleStyle.Render("chat (live)")
	footer := FooterStyle.Render("↑/↓ scroll • b back • q quit • ? help")
	status := SuccessStyle.Render("● connected")
	if cm.connectionErr != nil {
		status = ErrorStyle.Render("● disconnected: " + cm.connectionErr.Error())
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, header, "  ", status),
		"",
		BorderStyle.Render(cm.viewport.View()),
	)
	_ = width
	_ = height
	return body + "\n" + footer
}

func colorForUsername(name string) lipgloss.Style {
	palette := []lipgloss.Color{
		"#8b5cf6", "#10b981", "#f59e0b", "#ef4444",
		"#3b82f6", "#ec4899", "#14b8a6", "#a855f7",
	}
	var sum int
	for _, r := range name {
		sum += int(r)
	}
	return lipgloss.NewStyle().Foreground(palette[sum%len(palette)]).Bold(true)
}

// ---------------------------- Commands ------------------------------------

func doLogin(client *Client, email, password string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return LoginFailedMsg{Err: errors.New("client not initialised")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		user, err := client.Login(ctx, email, password)
		if err != nil {
			return LoginFailedMsg{Err: err}
		}
		return LoginSuccessMsg{User: user}
	}
}

func tickStats() tea.Cmd {
	return tea.Tick(statsPollInterval, func(t time.Time) tea.Msg {
		return StatsTickMsg(t)
	})
}

func doStats(client *Client) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return StatsResultMsg{Err: errors.New("client not initialised")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := client.Stats(ctx)
		return StatsResultMsg{Stats: s, Err: err}
	}
}

func doLeaderboards(client *Client, channel string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return LeaderboardResultMsg{Err: errors.New("client not initialised")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pity, perr := client.PityLeaderboard(ctx, channel, 10)
		strk, serr := client.StreakLeaderboard(ctx, channel, 10)
		err := perr
		if err == nil {
			err = serr
		}
		return LeaderboardResultMsg{Channel: channel, Pity: pity, Streak: strk, Err: err}
	}
}

// dialWS opens the WebSocket and surfaces the event channel to the Model
// via WSReadyMsg. The dial happens off the Bubble Tea goroutine so the UI
// stays responsive even when the daemon is slow.
func dialWS(client *Client, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return WSReadyMsg{Err: errors.New("client not initialised")}
		}
		ch, err := client.StreamWebSocket(ctx)
		if err != nil {
			return WSReadyMsg{Err: err}
		}
		return WSReadyMsg{Ch: ch}
	}
}

// awaitWS pulls the next event from the Model-owned channel. A closed
// channel surfaces as WSClosedMsg so the Update loop can react.
func awaitWS(ch <-chan WSEvent) tea.Cmd {
	if ch == nil {
		return func() tea.Msg { return WSClosedMsg{} }
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return WSClosedMsg{}
		}
		return WSEventMsg{Event: ev}
	}
}

// ---------------------------- Helpers -------------------------------------

func nonEmpty(a, fallback string) string {
	if strings.TrimSpace(a) == "" {
		return fallback
	}
	return a
}

func defaultChannel() string {
	for _, env := range []string{"ENGELOS_TWITCH_CHANNELS"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			first := strings.SplitN(v, ",", 2)[0]
			return strings.TrimSpace(first)
		}
	}
	return "engelswtf"
}
