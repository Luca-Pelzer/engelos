package commands_test

import (
	"context"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createPredictionCall records the args passed to a fake controller's Create.
type createPredictionCall struct {
	channel       string
	title         string
	outcomes      []string
	windowSeconds int
}

// resolvePredictionCall records the args passed to a fake controller's Resolve.
type resolvePredictionCall struct {
	channel        string
	winningOutcome string
}

// fakePredictionController is a programmable [commands.PredictionController]
// recording the last Create/Resolve call args and returning a configurable
// read view + sentinel for every method.
type fakePredictionController struct {
	info   commands.PredictionInfo
	create commands.PredictionOutcome
	lock   commands.PredictionOutcome
	resolv commands.PredictionOutcome
	cancel commands.PredictionOutcome

	createCalls  []createPredictionCall
	resolveCalls []resolvePredictionCall
	lockCalls    int
	cancelCalls  int
}

func (f *fakePredictionController) Create(_ context.Context, channel, title string, outcomes []string, windowSeconds int) (commands.PredictionInfo, commands.PredictionOutcome) {
	f.createCalls = append(f.createCalls, createPredictionCall{channel, title, outcomes, windowSeconds})
	return f.info, f.create
}

func (f *fakePredictionController) Lock(_ context.Context, _ string) (commands.PredictionInfo, commands.PredictionOutcome) {
	f.lockCalls++
	return f.info, f.lock
}

func (f *fakePredictionController) Resolve(_ context.Context, channel, winningOutcome string) (commands.PredictionInfo, commands.PredictionOutcome) {
	f.resolveCalls = append(f.resolveCalls, resolvePredictionCall{channel, winningOutcome})
	return f.info, f.resolv
}

func (f *fakePredictionController) Cancel(_ context.Context, _ string) (commands.PredictionInfo, commands.PredictionOutcome) {
	f.cancelCalls++
	return f.info, f.cancel
}

func TestPrediction_NamesAndRoles(t *testing.T) {
	ctrl := &fakePredictionController{}
	cases := []struct {
		cmd      commands.Command
		wantName string
	}{
		{commands.NewPredictionCommand(ctrl), "prediction"},
		{commands.NewLockPredictionCommand(ctrl), "lockprediction"},
		{commands.NewResolvePredictionCommand(ctrl), "endprediction"},
		{commands.NewCancelPredictionCommand(ctrl), "cancelprediction"},
	}
	for _, c := range cases {
		assert.Equal(t, c.wantName, c.cmd.Name)
		assert.Equal(t, commands.RoleModerator, c.cmd.MinRole)
	}
}

func TestPrediction_ResolveAlias(t *testing.T) {
	cmd := commands.NewResolvePredictionCommand(&fakePredictionController{})
	assert.Contains(t, cmd.Aliases, "resolveprediction")
}

func TestPrediction_CreateHappyPath(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionOK}
	cmd := commands.NewPredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(),
		modMsg("!prediction Will I win? | Yes | No"),
		[]string{"Will", "I", "win?", "|", "Yes", "|", "No"})

	require.Len(t, ctrl.createCalls, 1)
	call := ctrl.createCalls[0]
	assert.Equal(t, testChannel, call.channel)
	assert.Equal(t, "Will I win?", call.title)
	assert.Equal(t, []string{"Yes", "No"}, call.outcomes)
	assert.Equal(t, 120, call.windowSeconds)
	assert.Contains(t, reply.Text, "prediction opened")
	assert.Contains(t, reply.Text, "Will I win?")
	assert.Contains(t, reply.Text, "Yes / No")
	assert.Contains(t, reply.Text, "(120s to bet)")
	assert.NotContains(t, reply.Text, "\n")
}

func TestPrediction_CreateThreeOutcomes(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionOK}
	cmd := commands.NewPredictionCommand(ctrl)

	cmd.Handler(context.Background(),
		modMsg("!prediction Race | Red | Blue | Green"),
		[]string{"Race", "|", "Red", "|", "Blue", "|", "Green"})

	require.Len(t, ctrl.createCalls, 1)
	assert.Equal(t, "Race", ctrl.createCalls[0].title)
	assert.Equal(t, []string{"Red", "Blue", "Green"}, ctrl.createCalls[0].outcomes)
}

func TestPrediction_CreateTooFewOutcomesUsage(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionOK}
	cmd := commands.NewPredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(),
		modMsg("!prediction Only one | Yes"),
		[]string{"Only", "one", "|", "Yes"})

	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, ctrl.createCalls)
}

func TestPrediction_CreateTitleTooLongUsage(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionOK}
	cmd := commands.NewPredictionCommand(ctrl)

	// 46-char title (exceeds the 45 limit).
	longTitle := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	require.Len(t, longTitle, 46)
	reply := cmd.Handler(context.Background(),
		modMsg("!prediction "+longTitle+" | Yes | No"),
		[]string{longTitle, "|", "Yes", "|", "No"})

	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, ctrl.createCalls)
}

func TestPrediction_CreateOutcomeCappedTo25(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionOK}
	cmd := commands.NewPredictionCommand(ctrl)

	// 30-char outcome, capped to 25.
	longOutcome := "abcdefghijklmnopqrstuvwxyz1234"
	require.Len(t, longOutcome, 30)
	cmd.Handler(context.Background(),
		modMsg("!prediction Title | "+longOutcome+" | No"),
		[]string{"Title", "|", longOutcome, "|", "No"})

	require.Len(t, ctrl.createCalls, 1)
	assert.Equal(t, "abcdefghijklmnopqrstuvwxy", ctrl.createCalls[0].outcomes[0])
	assert.Len(t, ctrl.createCalls[0].outcomes[0], 25)
}

func TestPrediction_CreateActiveExists(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionActiveExists}
	cmd := commands.NewPredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(),
		modMsg("!prediction Title | Yes | No"),
		[]string{"Title", "|", "Yes", "|", "No"})

	assert.Contains(t, reply.Text, "already running")
}

func TestPrediction_CreateNotAffiliate(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionNotAffiliate}
	cmd := commands.NewPredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(),
		modMsg("!prediction Title | Yes | No"),
		[]string{"Title", "|", "Yes", "|", "No"})

	assert.Contains(t, reply.Text, "affiliate/partner")
}

func TestPrediction_CreateInvalidMapsToUsage(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionInvalid}
	cmd := commands.NewPredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(),
		modMsg("!prediction Title | Yes | No"),
		[]string{"Title", "|", "Yes", "|", "No"})

	assert.Contains(t, reply.Text, "usage:")
}

func TestPrediction_CreateUnavailable(t *testing.T) {
	ctrl := &fakePredictionController{create: commands.PredictionUnavailable}
	cmd := commands.NewPredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(),
		modMsg("!prediction Title | Yes | No"),
		[]string{"Title", "|", "Yes", "|", "No"})

	assert.Contains(t, reply.Text, "couldn't open the prediction")
}

func TestPrediction_CreateNilController(t *testing.T) {
	cmd := commands.NewPredictionCommand(nil)
	reply := cmd.Handler(context.Background(),
		modMsg("!prediction Title | Yes | No"),
		[]string{"Title", "|", "Yes", "|", "No"})
	assert.Contains(t, reply.Text, "predictions are unavailable")
}

func TestLockPrediction_HappyPath(t *testing.T) {
	ctrl := &fakePredictionController{
		lock: commands.PredictionOK,
		info: commands.PredictionInfo{Title: "Will I win?"},
	}
	cmd := commands.NewLockPredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(), modMsg("!lockprediction"), nil)
	assert.Equal(t, 1, ctrl.lockCalls)
	assert.Contains(t, reply.Text, "betting locked for: Will I win?")
	assert.NotContains(t, reply.Text, "\n")
}

func TestLockPrediction_None(t *testing.T) {
	ctrl := &fakePredictionController{lock: commands.PredictionNone}
	cmd := commands.NewLockPredictionCommand(ctrl)
	reply := cmd.Handler(context.Background(), modMsg("!lockprediction"), nil)
	assert.Contains(t, reply.Text, "no active prediction")
}

func TestLockPrediction_NilController(t *testing.T) {
	cmd := commands.NewLockPredictionCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!lockprediction"), nil)
	assert.Contains(t, reply.Text, "predictions are unavailable")
}

func TestResolvePrediction_HappyPath(t *testing.T) {
	ctrl := &fakePredictionController{resolv: commands.PredictionOK}
	cmd := commands.NewResolvePredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(),
		modMsg("!endprediction Yes indeed"),
		[]string{"Yes", "indeed"})

	require.Len(t, ctrl.resolveCalls, 1)
	assert.Equal(t, testChannel, ctrl.resolveCalls[0].channel)
	assert.Equal(t, "Yes indeed", ctrl.resolveCalls[0].winningOutcome)
	assert.Contains(t, reply.Text, "prediction resolved: 'Yes indeed' wins!")
	assert.NotContains(t, reply.Text, "\n")
}

func TestResolvePrediction_EmptyUsage(t *testing.T) {
	ctrl := &fakePredictionController{resolv: commands.PredictionOK}
	cmd := commands.NewResolvePredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(), modMsg("!endprediction"), nil)
	assert.Contains(t, reply.Text, "usage:")
	assert.Empty(t, ctrl.resolveCalls)
}

func TestResolvePrediction_None(t *testing.T) {
	ctrl := &fakePredictionController{resolv: commands.PredictionNone}
	cmd := commands.NewResolvePredictionCommand(ctrl)
	reply := cmd.Handler(context.Background(),
		modMsg("!endprediction Yes"), []string{"Yes"})
	assert.Contains(t, reply.Text, "no active prediction")
}

func TestResolvePrediction_InvalidOutcome(t *testing.T) {
	ctrl := &fakePredictionController{resolv: commands.PredictionInvalid}
	cmd := commands.NewResolvePredictionCommand(ctrl)
	reply := cmd.Handler(context.Background(),
		modMsg("!endprediction Maybe"), []string{"Maybe"})
	assert.Contains(t, reply.Text, "couldn't find that outcome")
}

func TestResolvePrediction_NilController(t *testing.T) {
	cmd := commands.NewResolvePredictionCommand(nil)
	reply := cmd.Handler(context.Background(),
		modMsg("!endprediction Yes"), []string{"Yes"})
	assert.Contains(t, reply.Text, "predictions are unavailable")
}

func TestCancelPrediction_HappyPath(t *testing.T) {
	ctrl := &fakePredictionController{cancel: commands.PredictionOK}
	cmd := commands.NewCancelPredictionCommand(ctrl)

	reply := cmd.Handler(context.Background(), modMsg("!cancelprediction"), nil)
	assert.Equal(t, 1, ctrl.cancelCalls)
	assert.Contains(t, reply.Text, "prediction canceled, all points refunded")
	assert.NotContains(t, reply.Text, "\n")
}

func TestCancelPrediction_None(t *testing.T) {
	ctrl := &fakePredictionController{cancel: commands.PredictionNone}
	cmd := commands.NewCancelPredictionCommand(ctrl)
	reply := cmd.Handler(context.Background(), modMsg("!cancelprediction"), nil)
	assert.Contains(t, reply.Text, "no active prediction")
}

func TestCancelPrediction_NilController(t *testing.T) {
	cmd := commands.NewCancelPredictionCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!cancelprediction"), nil)
	assert.Contains(t, reply.Text, "predictions are unavailable")
}
