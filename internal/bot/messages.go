package bot

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type messenger interface {
	Download(context.Context, whatsmeow.DownloadableMessage) ([]byte, error)
	SendMessage(context.Context, types.JID, *waProto.Message, ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error)
}
type transcriber func(context.Context, []byte) (string, error)

func initMessages(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS bot_messages (
 chat TEXT NOT NULL, id TEXT NOT NULL, text TEXT NOT NULL,
 created INTEGER NOT NULL DEFAULT (unixepoch()), PRIMARY KEY(chat,id));
 CREATE INDEX IF NOT EXISTS bot_messages_chat_created ON bot_messages(chat,created);`)
	return err
}
func saveMessage(ctx context.Context, db *sql.DB, chat, id, text string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO bot_messages(chat,id,text) VALUES(?,?,?) ON CONFLICT(chat,id) DO UPDATE SET text=excluded.text`, chat, id, text)
	return err
}
func searchMessages(ctx context.Context, db *sql.DB, chat, query string) ([]string, error) {
	query = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := db.QueryContext(ctx, `SELECT substr(text,1,500) FROM bot_messages WHERE chat=? AND text LIKE ? ESCAPE '\' ORDER BY created DESC,id DESC LIMIT 10`, chat, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, err
		}
		result = append(result, text)
	}
	return result, rows.Err()
}
func handleMessage(ctx context.Context, client messenger, db *sql.DB, v *events.Message, transcribe transcriber, transcribeGroups bool) error {
	if v.Message == nil || v.Info.IsFromMe || v.Info.Chat.Server == types.BroadcastServer {
		return nil
	}
	chat := v.Info.Chat.String()
	text := v.Message.GetConversation()
	if text == "" {
		text = v.Message.GetExtendedTextMessage().GetText()
	}
	fields := strings.Fields(text)
	if len(fields) > 0 && fields[0] == "!search" {
		reply := "Please provide a search query. Usage: !search <query>"
		query := strings.TrimSpace(strings.TrimPrefix(text, "!search"))
		if query != "" {
			results, err := searchMessages(ctx, db, chat, query)
			if err != nil {
				return err
			}
			reply = "No results found for your query."
			if len(results) > 0 {
				reply = "Search results:\n\n" + strings.Join(results, "\n---\n")
			}
		}
		_, err := client.SendMessage(ctx, v.Info.Chat, &waProto.Message{Conversation: proto.String(reply)})
		return err
	}
	if audio := v.Message.GetAudioMessage(); audio != nil {
		if !transcribeGroups && (v.Info.IsGroup || v.Info.Chat.Server == types.GroupServer) {
			return nil
		}
		data, err := client.Download(ctx, audio)
		if err != nil {
			return err
		}
		transcript, err := transcribe(ctx, data)
		if err != nil {
			return err
		}
		if err := saveMessage(ctx, db, chat, v.Info.ID, transcript); err != nil {
			return err
		}
		// Keep a single reply comfortably below WhatsApp's text size limit.
		runes := []rune(transcript)
		if len(runes) > 12000 {
			transcript = string(runes[:12000]) + "…"
		}
		_, err = client.SendMessage(ctx, v.Info.Chat, &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text:        proto.String(fmt.Sprintf("*Transcrição do áudio:*\n\n%s", transcript)),
			ContextInfo: &waProto.ContextInfo{QuotedMessage: v.Message, Participant: proto.String(v.Info.Sender.String()), StanzaID: proto.String(v.Info.ID)},
		}})
		return err
	}
	if text != "" {
		return saveMessage(ctx, db, chat, v.Info.ID, text)
	}
	return nil
}
