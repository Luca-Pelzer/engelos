package commands_test

import (
	"context"
	"testing"

	"github.com/Luca-Pelzer/engelos/internal/commands"
	"github.com/stretchr/testify/assert"
)

type fakeSongRequester struct {
	track       commands.SongTrack
	requestOut  commands.SongOutcome
	nowOut      commands.SongOutcome
	skipOut     commands.SongOutcome
	lastQuery   string
	lastChannel string
}

func (f *fakeSongRequester) Request(_ context.Context, channel, query string) (commands.SongTrack, commands.SongOutcome) {
	f.lastChannel = channel
	f.lastQuery = query
	return f.track, f.requestOut
}

func (f *fakeSongRequester) NowPlaying(_ context.Context, channel string) (commands.SongTrack, commands.SongOutcome) {
	f.lastChannel = channel
	return f.track, f.nowOut
}

func (f *fakeSongRequester) Skip(_ context.Context, channel string) commands.SongOutcome {
	f.lastChannel = channel
	return f.skipOut
}

func TestSongRequest_NameAndRole(t *testing.T) {
	cmd := commands.NewSongRequestCommand(&fakeSongRequester{})
	assert.Equal(t, "sr", cmd.Name)
	assert.Contains(t, cmd.Aliases, "songrequest")
	assert.Equal(t, commands.RoleEveryone, cmd.MinRole)
}

func TestSongRequest_Happy(t *testing.T) {
	f := &fakeSongRequester{track: commands.SongTrack{Title: "Bohemian Rhapsody", Artist: "Queen"}, requestOut: commands.SongOK}
	cmd := commands.NewSongRequestCommand(f)
	reply := cmd.Handler(context.Background(), msgText("!sr bohemian rhapsody"), []string{"bohemian", "rhapsody"})
	assert.Equal(t, "bohemian rhapsody", f.lastQuery)
	assert.Equal(t, "chan-A", f.lastChannel)
	assert.Contains(t, reply.Text, "queued: Bohemian Rhapsody by Queen")
}

func TestSongRequest_EmptyQuery(t *testing.T) {
	cmd := commands.NewSongRequestCommand(&fakeSongRequester{})
	reply := cmd.Handler(context.Background(), msgText("!sr"), nil)
	assert.Contains(t, reply.Text, "usage:")
}

func TestSongRequest_NotFound(t *testing.T) {
	cmd := commands.NewSongRequestCommand(&fakeSongRequester{requestOut: commands.SongNotFound})
	reply := cmd.Handler(context.Background(), msgText("!sr xyz"), []string{"xyz"})
	assert.Contains(t, reply.Text, "couldn't find")
}

func TestSongRequest_TooLong(t *testing.T) {
	cmd := commands.NewSongRequestCommand(&fakeSongRequester{requestOut: commands.SongTooLong})
	reply := cmd.Handler(context.Background(), msgText("!sr epic"), []string{"epic"})
	assert.Contains(t, reply.Text, "too long")
}

func TestSongRequest_Unavailable(t *testing.T) {
	cmd := commands.NewSongRequestCommand(&fakeSongRequester{requestOut: commands.SongUnavailable})
	reply := cmd.Handler(context.Background(), msgText("!sr x"), []string{"x"})
	assert.Contains(t, reply.Text, "couldn't queue")
}

func TestSongRequest_NilRequester(t *testing.T) {
	cmd := commands.NewSongRequestCommand(nil)
	reply := cmd.Handler(context.Background(), msgText("!sr x"), []string{"x"})
	assert.Contains(t, reply.Text, "unavailable")
}

func TestNowPlaying_Happy(t *testing.T) {
	f := &fakeSongRequester{track: commands.SongTrack{Title: "Song", Artist: "Artist"}, nowOut: commands.SongOK}
	cmd := commands.NewNowPlayingCommand(f)
	assert.Equal(t, "song", cmd.Name)
	reply := cmd.Handler(context.Background(), msgText("!song"), nil)
	assert.Contains(t, reply.Text, "now playing: Song by Artist")
}

func TestNowPlaying_Nothing(t *testing.T) {
	cmd := commands.NewNowPlayingCommand(&fakeSongRequester{nowOut: commands.SongNothingPlaying})
	reply := cmd.Handler(context.Background(), msgText("!song"), nil)
	assert.Contains(t, reply.Text, "nothing is playing")
}

func TestNowPlaying_NoArtist(t *testing.T) {
	f := &fakeSongRequester{track: commands.SongTrack{Title: "Solo"}, nowOut: commands.SongOK}
	cmd := commands.NewNowPlayingCommand(f)
	reply := cmd.Handler(context.Background(), msgText("!song"), nil)
	assert.Contains(t, reply.Text, "now playing: Solo")
	assert.NotContains(t, reply.Text, " by ")
}

func TestSkipSong_RoleAndHappy(t *testing.T) {
	f := &fakeSongRequester{skipOut: commands.SongOK}
	cmd := commands.NewSkipSongCommand(f)
	assert.Equal(t, commands.RoleModerator, cmd.MinRole)
	reply := cmd.Handler(context.Background(), modMsg("!skipsong"), nil)
	assert.Contains(t, reply.Text, "skipped")
}

func TestSkipSong_Nothing(t *testing.T) {
	cmd := commands.NewSkipSongCommand(&fakeSongRequester{skipOut: commands.SongNothingPlaying})
	reply := cmd.Handler(context.Background(), modMsg("!skipsong"), nil)
	assert.Contains(t, reply.Text, "nothing is playing")
}

func TestSkipSong_NilRequester(t *testing.T) {
	cmd := commands.NewSkipSongCommand(nil)
	reply := cmd.Handler(context.Background(), modMsg("!skipsong"), nil)
	assert.Contains(t, reply.Text, "unavailable")
}
