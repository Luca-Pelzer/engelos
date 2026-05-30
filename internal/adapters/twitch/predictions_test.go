package twitch

import (
	"context"
	"errors"
	"testing"

	"github.com/nicklaw5/helix/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func predictionsResp(preds ...helix.Prediction) *helix.PredictionsResponse {
	r := &helix.PredictionsResponse{ResponseCommon: helix.ResponseCommon{StatusCode: 200}}
	r.Data.Predictions = preds
	return r
}

func TestCreatePrediction_MapsParamsAndResponse(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.createPredictionRsp = predictionsResp(helix.Prediction{
		ID:     "pred-1",
		Title:  "Will we win?",
		Status: PredictionStatusActive,
		Outcomes: []helix.Outcomes{
			{ID: "o1", Title: "Yes"},
			{ID: "o2", Title: "No"},
		},
	})
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.CreatePrediction(context.Background(), "#Broadcaster", "Will we win?", []string{"Yes", "No"}, 120)
	require.NoError(t, err)
	assert.Equal(t, "pred-1", view.ID)
	assert.Equal(t, PredictionStatusActive, view.Status)
	require.Len(t, view.Outcomes, 2)
	assert.Equal(t, "Yes", view.Outcomes[0].Title)

	p := hx.snapshotCreatePrediction()
	require.NotNil(t, p)
	assert.Equal(t, "987", p.BroadcasterID)
	assert.Equal(t, "Will we win?", p.Title)
	assert.Equal(t, 120, p.PredictionWindow)
	require.Len(t, p.Outcomes, 2)
	assert.Equal(t, "Yes", p.Outcomes[0].Title)
	assert.Equal(t, "No", p.Outcomes[1].Title)
}

func TestCreatePrediction_AnonymousUnavailable(t *testing.T) {
	a, _, _ := newTestAdapter(t, true)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CreatePrediction(context.Background(), "broadcaster", "t", []string{"a", "b"}, 120)
	assert.ErrorIs(t, err, ErrHelixUnavailable)
}

func TestActivePrediction_ReturnsActiveOrLocked(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.getPredictionsResp = predictionsResp(
		helix.Prediction{ID: "old", Title: "done", Status: PredictionStatusResolved},
		helix.Prediction{ID: "cur", Title: "live", Status: PredictionStatusActive,
			Outcomes: []helix.Outcomes{{ID: "o1", Title: "A"}, {ID: "o2", Title: "B"}}},
	)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.ActivePrediction(context.Background(), "broadcaster")
	require.NoError(t, err)
	assert.Equal(t, "cur", view.ID)
}

func TestActivePrediction_NoneReturnsSentinel(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.getPredictionsResp = predictionsResp(
		helix.Prediction{ID: "old", Title: "done", Status: PredictionStatusResolved},
	)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.ActivePrediction(context.Background(), "broadcaster")
	assert.ErrorIs(t, err, ErrNoActivePrediction)
}

func TestResolvePrediction_MapsTitleToOutcomeID(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.getPredictionsResp = predictionsResp(
		helix.Prediction{ID: "cur", Title: "live", Status: PredictionStatusLocked,
			Outcomes: []helix.Outcomes{{ID: "o1", Title: "Red"}, {ID: "o2", Title: "Blue"}}},
	)
	hx.endPredictionResp = predictionsResp(
		helix.Prediction{ID: "cur", Title: "live", Status: PredictionStatusResolved},
	)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	view, err := a.ResolvePrediction(context.Background(), "broadcaster", "  blue ")
	require.NoError(t, err)
	assert.Equal(t, PredictionStatusResolved, view.Status)

	p := hx.snapshotEndPrediction()
	require.NotNil(t, p)
	assert.Equal(t, "987", p.BroadcasterID)
	assert.Equal(t, "cur", p.ID)
	assert.Equal(t, PredictionStatusResolved, p.Status)
	assert.Equal(t, "o2", p.WinningOutcomeID)
}

func TestResolvePrediction_UnknownOutcome(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.getPredictionsResp = predictionsResp(
		helix.Prediction{ID: "cur", Status: PredictionStatusActive,
			Outcomes: []helix.Outcomes{{ID: "o1", Title: "Red"}, {ID: "o2", Title: "Blue"}}},
	)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.ResolvePrediction(context.Background(), "broadcaster", "Green")
	assert.ErrorIs(t, err, ErrOutcomeNotFound)
}

func TestResolvePrediction_NoActive(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.getPredictionsResp = predictionsResp()
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.ResolvePrediction(context.Background(), "broadcaster", "Red")
	assert.ErrorIs(t, err, ErrNoActivePrediction)
}

func TestCancelPrediction_SendsCanceledStatus(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.getPredictionsResp = predictionsResp(
		helix.Prediction{ID: "cur", Status: PredictionStatusActive,
			Outcomes: []helix.Outcomes{{ID: "o1", Title: "A"}, {ID: "o2", Title: "B"}}},
	)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.CancelPrediction(context.Background(), "broadcaster")
	require.NoError(t, err)
	p := hx.snapshotEndPrediction()
	require.NotNil(t, p)
	assert.Equal(t, PredictionStatusCanceled, p.Status)
	assert.Equal(t, "cur", p.ID)
}

func TestLockPrediction_SendsLockedStatus(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.getPredictionsResp = predictionsResp(
		helix.Prediction{ID: "cur", Status: PredictionStatusActive,
			Outcomes: []helix.Outcomes{{ID: "o1", Title: "A"}, {ID: "o2", Title: "B"}}},
	)
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.LockPrediction(context.Background(), "broadcaster")
	require.NoError(t, err)
	p := hx.snapshotEndPrediction()
	require.NotNil(t, p)
	assert.Equal(t, PredictionStatusLocked, p.Status)
}

func TestLockPrediction_HelixErrorPropagated(t *testing.T) {
	a, _, hx := newTestAdapter(t, false)
	hx.defaultUsersFor = map[string]string{"broadcaster": "987"}
	hx.getPredictionsErr = errors.New("boom")
	require.NoError(t, a.Connect(context.Background()))
	t.Cleanup(func() { _ = a.Disconnect(context.Background()) })

	_, err := a.LockPrediction(context.Background(), "broadcaster")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
