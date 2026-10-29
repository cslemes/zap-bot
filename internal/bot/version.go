package bot

import (
	"context"
	"fmt"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"log"
	"net/http"
)

// RefreshWhatsAppVersion must run before creating any clients: the library's
// version and base login payload are process-wide state.
func RefreshWhatsAppVersion(ctx context.Context, client *http.Client) error {
	version, err := whatsmeow.GetLatestVersion(ctx, client)
	if err != nil {
		return fmt.Errorf("fetch WhatsApp Web version: %w", err)
	}
	if version == nil || version.IsZero() || version[2] == 0 {
		return fmt.Errorf("WhatsApp returned an empty client version")
	}
	store.SetWAVersion(*version)
	log.Printf("Using WhatsApp Web client version %s", version.String())
	return nil
}
