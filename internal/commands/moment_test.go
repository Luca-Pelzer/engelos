package commands_test

import (
	"context"
	"testing"
	"time"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
)

type fakeMomentController struct {
	openOutcome commands.MomentOutcome
	joinResult  commands.MomentResult
	joinOutcome commands.MomentOutcome
	endResult   commands.MomentResult
	endOutcome  commands.MomentOutcome
	history     string
	historyOut  commands.MomentOutcome

	lastTitle  string
	lastWindow time.Duration
}

func (f *fakeMomentController) Open(_ context.Context, _, title, _ string, window time.Duration) commands.MomentOutcome {
	f.lastTitle = title
	f.lastWindow = window
	return f.openOutcome
}
func (f *fakeMomentController) Join(_ context.Context, _, _, _ string) (commands.MomentResult, commands.MomentOutcome) {
	return f.joinResult, f.joinOutcome
}
func (f *fakeMomentController) End(_ context.Context, _ string) (commands.MomentResult, commands.MomentOutcome) {
	return f.endResult, f.endOutcome
}
func (f *fakeMomentController) History(_ context.Context, _ string, _ int) (string, commands.MomentOutcome) {
	return f.history, f.historyOut
}

func TestMoment_NameAndRole(t *testing.T) {
	cmd := commands.NewMomentCommand(&fakeMomentController{})
	assert.Equal(t, "moment", cmd.Name)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)
}

func TestMoment_OpenDefaultWindow(t *testing.T) {
	f := &fakeMomentController{openOutcome: commands.MomentOK}
	cmd := commands.NewMomentCommand(f)
	reply := cmd.Handler(context.Background(), modMsg("!moment huge play"), []string{"huge", "play"})
	assert.Equal(t, "huge play", f.lastTitle)
	assert.Equal(t, 60*time.Second, f.lastWindow)
	assert.Contains(t, reply.Text, "MOMENT: huge play")
}

func TestMoment_OpenWithWindow(t *testing.T) {
	f := &fakeMomentController{openOutcome: commands.MomentOK}
	cmd := commands.NewMomentCommand(f)
	reply := cmd.Handler(context.Background(), modMsg("!moment clutch 90s"), []string{"clutch", "90s"})
	assert.Equal(t, "clutch", f.lastTitle)
	assert.Equal(t, 90*time.Second, f.lastWindow)
	assert.Contains(t, reply.Text, "90s")
}

func TestMoment_OpenWindowClampedAndPlainInt(t *testing.T) {
	f := &fakeMomentController{openOutcome: commands.MomentOK}
	cmd := commands.NewMomentCommand(f)
	cmd.Handler(context.Background(), modMsg("!moment x 5"), []string{"x", "5"})
	assert.Equal(t, 10*time.Second, f.lastWindow) // clamped up to min
}

func TestMoment_OpenActiveExists(t *testing.T) {
	cmd := commands.NewMomentCommand(&fakeMomentController{openOutcome: commands.MomentActiveExists})
	reply := cmd.Handler(context.Background(), modMsg("!moment x"), []string{"x"})
	assert.Contains(t, reply.Text, "already running")
}

func TestMoment_NoArgsUsage(t *testing.T) {
	cmd := commands.NewMomentCommand(&fakeMomentController{})
	reply := cmd.Handler(context.Background(), modMsg("!moment"), nil)
	assert.Contains(t, reply.Text, "usage:")
}

func TestMoment_End(t *testing.T) {
	f := &fakeMomentController{endOutcome: commands.MomentOK, endResult: commands.MomentResult{Title: "GG", Rarity: "legendary", Participants: 73}}
	cmd := commands.NewMomentCommand(f)
	reply := cmd.Handler(context.Background(), modMsg("!moment end"), []string{"end"})
	assert.Contains(t, reply.Text, "legendary")
	assert.Contains(t, reply.Text, "73 were there")
}

func TestMoment_EndNone(t *testing.T) {
	cmd := commands.NewMomentCommand(&fakeMomentController{endOutcome: commands.MomentNone})
	reply := cmd.Handler(context.Background(), modMsg("!moment end"), []string{"end"})
	assert.Contains(t, reply.Text, "no moment is running")
}

func TestMoment_History(t *testing.T) {
	f := &fakeMomentController{historyOut: commands.MomentOK, history: "GG (legendary, 73)"}
	cmd := commands.NewMomentCommand(f)
	reply := cmd.Handler(context.Background(), modMsg("!moment history"), []string{"history"})
	assert.Contains(t, reply.Text, "Recent moments:")
	assert.Contains(t, reply.Text, "GG (legendary, 73)")
}

func TestMoment_NilController(t *testing.T) {
	cmd := commands.NewMomentCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!moment x"), []string{"x"})
	assert.Contains(t, reply.Text, "unavailable")
}

func TestHere_NameAndRole(t *testing.T) {
	cmd := commands.NewHereCommand(&fakeMomentController{})
	assert.Equal(t, "here", cmd.Name)
	assert.Equal(t, commands.RoleEveryone, cmd.MinRole)
}

func TestHere_Joins(t *testing.T) {
	f := &fakeMomentController{joinOutcome: commands.MomentOK, joinResult: commands.MomentResult{Participants: 5}}
	cmd := commands.NewHereCommand(f)
	reply := cmd.Handler(context.Background(), msgText("!here"), nil)
	assert.Contains(t, reply.Text, "you were here")
	assert.Contains(t, reply.Text, "5 so far")
}

func TestHere_AlreadyJoined(t *testing.T) {
	cmd := commands.NewHereCommand(&fakeMomentController{joinOutcome: commands.MomentAlreadyJoined})
	reply := cmd.Handler(context.Background(), msgText("!here"), nil)
	assert.Contains(t, reply.Text, "already counted")
}

func TestHere_Closed(t *testing.T) {
	cmd := commands.NewHereCommand(&fakeMomentController{joinOutcome: commands.MomentClosedWindow})
	reply := cmd.Handler(context.Background(), msgText("!here"), nil)
	assert.Contains(t, reply.Text, "too late")
}

func TestHere_None(t *testing.T) {
	cmd := commands.NewHereCommand(&fakeMomentController{joinOutcome: commands.MomentNone})
	reply := cmd.Handler(context.Background(), msgText("!here"), nil)
	assert.Contains(t, reply.Text, "no moment running")
}

func TestHere_NilController(t *testing.T) {
	cmd := commands.NewHereCommand(nil)
	reply := cmd.Handler(context.Background(), msgText("!here"), nil)
	assert.Contains(t, reply.Text, "unavailable")
}
