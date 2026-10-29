package bot

import (
	"context"
	"database/sql"
	"fmt"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := initMessages(db); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestSearchIsolationLiteralQueryAndLimit(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	for i := 0; i < 15; i++ {
		if err := saveMessage(ctx, db, "group", fmt.Sprint(i), "100%_match"); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveMessage(ctx, db, "private", "secret", "private 100%_match"); err != nil {
		t.Fatal(err)
	}
	if err := saveMessage(ctx, db, "group", "wildcard-decoy", "100ABmatch"); err != nil {
		t.Fatal(err)
	}
	results, err := searchMessages(ctx, db, "group", "%_")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 10 {
		t.Fatalf("want 10 results, got %d", len(results))
	}
	for _, result := range results {
		if result != "100%_match" {
			t.Fatalf("unexpected result %q", result)
		}
	}
	results, err = searchMessages(ctx, db, "group", "no_match")
	if err != nil || len(results) != 0 {
		t.Fatalf("results=%v err=%v", results, err)
	}
}

type fakeMessenger struct {
	to        types.JID
	message   *waProto.Message
	sends     int
	downloads int
}

func (f *fakeMessenger) Download(context.Context, whatsmeow.DownloadableMessage) ([]byte, error) {
	f.downloads++
	return []byte("audio"), nil
}
func (f *fakeMessenger) SendMessage(_ context.Context, to types.JID, msg *waProto.Message, _ ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	f.to = to
	f.message = msg
	f.sends++
	return whatsmeow.SendResponse{}, nil
}
func TestGroupAudioReplyAndSearch(t *testing.T) {
	db := testDB(t)
	client := &fakeMessenger{}
	group := types.NewJID("123", types.GroupServer)
	sender := types.NewJID("456", types.DefaultUserServer)
	event := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: group, Sender: sender, IsGroup: true}, ID: "audio-1"}, Message: &waProto.Message{AudioMessage: &waProto.AudioMessage{}}}
	transcribe := func(context.Context, []byte) (string, error) { return "meeting tomorrow", nil }
	if err := handleMessage(context.Background(), client, db, event, transcribe, true); err != nil {
		t.Fatal(err)
	}
	if client.to != group {
		t.Fatalf("reply sent to %s instead of group", client.to)
	}
	if client.message.GetExtendedTextMessage().GetContextInfo().GetStanzaID() != "audio-1" {
		t.Fatal("missing quoted message")
	}
	event.Message = &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{Text: proto.String("!search meeting")}}
	if err := handleMessage(context.Background(), client, db, event, transcribe, true); err != nil {
		t.Fatal(err)
	}
	if client.to != group || !strings.Contains(client.message.GetConversation(), "meeting tomorrow") {
		t.Fatal("search did not return transcription to group")
	}
	event.Info.IsFromMe = true
	if err := handleMessage(context.Background(), client, db, event, transcribe, true); err != nil {
		t.Fatal(err)
	}
	if client.sends != 2 {
		t.Fatal("bot replied to itself")
	}
}
func TestManagerFreshStoreAndConcurrentControls(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(nil, dir, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().Status != "Disconnected" {
		t.Fatal("unexpected initial state")
	}
	if err := saveMessage(context.Background(), m.db, "chat", "id", "persistent message"); err != nil {
		t.Fatal(err)
	}
	// Closed managers must never start a connection, even under concurrent requests.
	m.Close()
	reopened, err := NewManager(nil, dir, false, true)
	if err != nil {
		t.Fatal(err)
	}
	results, err := searchMessages(context.Background(), reopened.db, "chat", "persistent")
	reopened.Close()
	if err != nil || len(results) != 1 {
		t.Fatalf("message did not survive restart: results=%v err=%v", results, err)
	}
	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func() { m.Connect(); m.Snapshot(); m.Disconnect(); done <- struct{}{} }()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	if m.Snapshot().Status != "Disconnected" {
		t.Fatal("closed manager connected")
	}
}

func TestWaitingForQRRejectsDuplicateConnectAndStaleEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := &Manager{active: true, generation: 7, session: ctx, cancel: cancel, state: State{Status: "Waiting for QR Scan", QR: "code"}}
	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func() { m.Connect(); done <- struct{}{} }()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	if m.generation != 7 || m.Snapshot().QR != "code" {
		t.Fatal("duplicate connect replaced active QR flow")
	}
	m.event(ctx, 6, nil, &events.Connected{})
	m.event(ctx, 6, nil, &events.LoggedOut{})
	if m.Snapshot().Status != "Waiting for QR Scan" || ctx.Err() != nil {
		t.Fatal("stale events changed current connection")
	}
	m.event(ctx, 7, nil, &events.Connected{})
	state := m.Snapshot()
	if state.Status != "Connected" || state.Started.IsZero() || state.QR != "" {
		t.Fatal("connected event did not clear QR and start uptime")
	}
}

func TestGroupTranscriptionSetting(t *testing.T) {
	for _, tc := range []struct {
		name           string
		group, enabled bool
		want           int
	}{
		{"group disabled", true, false, 0},
		{"group enabled", true, true, 1},
		{"direct message with groups disabled", false, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			client := &fakeMessenger{}
			calls := 0
			chat := types.NewJID("123", types.DefaultUserServer)
			if tc.group {
				chat = types.NewJID("123", types.GroupServer)
			}
			event := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: types.NewJID("456", types.DefaultUserServer), IsGroup: tc.group}, ID: "audio"}, Message: &waProto.Message{AudioMessage: &waProto.AudioMessage{}}}
			transcribe := func(context.Context, []byte) (string, error) { calls++; return "transcript", nil }
			if err := handleMessage(context.Background(), client, db, event, transcribe, tc.enabled); err != nil {
				t.Fatal(err)
			}
			if client.downloads != tc.want || calls != tc.want || client.sends != tc.want {
				t.Fatalf("downloads=%d transcriptions=%d replies=%d; want %d", client.downloads, calls, client.sends, tc.want)
			}
			results, err := searchMessages(context.Background(), db, chat.String(), "transcript")
			if err != nil || len(results) != tc.want {
				t.Fatalf("stored transcripts=%v err=%v", results, err)
			}
		})
	}
}
