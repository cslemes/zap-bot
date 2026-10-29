# zap-bot

A Go WhatsApp bot that transcribes incoming audio using an OpenAI-compatible transcription API and searches text and transcriptions received while connected. The dashboard manages WhatsApp pairing and connection status. Terminal mode uses the same bot implementation.

## Run locally

Requires Go 1.26 or newer and a C compiler (SQLite uses CGO).

1. Copy `.env.example` to `.env`.
2. Set `TRANSCRIPTION_ENDPOINT` to the full transcription URL (including `/audio/transcriptions`), `TRANSCRIPTION_API_KEY`, and `TRANSCRIPTION_MODEL` for your provider. Also set `DASHBOARD_USER` and a strong `DASHBOARD_PASSWORD`.
3. Run `go run .` and open http://127.0.0.1:8080.
4. Sign in, click Connect, and scan the QR using WhatsApp's linked-device screen.

For terminal-only operation, run `go run . -terminal`. Dashboard credentials are not required in this mode. Environment variables work without a `.env` file.

Configuration:

| Variable | Default | Purpose |
| --- | --- | --- |
| `TRANSCRIPTION_ENDPOINT` | required | Full OpenAI-compatible audio transcription URL |
| `TRANSCRIPTION_API_KEY` | required | Provider API credential (Bearer authentication) |
| `TRANSCRIPTION_MODEL` | required | Provider transcription model name |
| `TRANSCRIBE_GROUPS` | `true` | Set `false` to skip group audio downloads, transcription, and replies; direct-message transcription remains enabled |
| `DASHBOARD_USER` | required for web mode | HTTP Basic authentication username |
| `DASHBOARD_PASSWORD` | required for web mode | HTTP Basic authentication password |
| `DATA_DIR` | `data` | Session and message database directory |
| `HTTP_ADDR` | `127.0.0.1:8080` | Dashboard listener |
| `BOT_IMAGE` | `cslemes/zap-bot:latest` | Compose image override |

Audio is sent to your configured provider for transcription. The provider must accept multipart `file` and `model` fields and return JSON with a `text` field. The endpoint is used exactly as configured; no path is appended. Replace the old `GROQ_API_KEY` setting with the three `TRANSCRIPTION_*` settings. Replies go to the original chat, including groups when `TRANSCRIBE_GROUPS=true`. Set `TRANSCRIBE_GROUPS=false` in `.env` to disable group audio transcription, then restart the application (or recreate the Compose container). Group text search remains available. The bot ignores its own messages and broadcast messages. Send `!search <query>` to search messages and audio transcriptions stored for that chat. Search matches literal text, returns at most ten results, and truncates each preview to 500 characters. It does not import old WhatsApp history. The local SQLite database retains incoming text and transcriptions; protect and back up the data directory.

## Containers

With `.env` configured, run `docker compose up --build -d`. The dashboard is published only on the host's loopback interface. Use an SSH tunnel or an HTTPS reverse proxy for remote access; Basic authentication must be protected by HTTPS outside localhost.

The `bot_data` volume is mounted at `/data`; the executable is outside that volume so image updates replace it. Images run as an unprivileged user and exclude environment files and databases.

On startup, the bot refreshes the WhatsApp Web client version before creating clients. The lookup has a 15-second timeout; if it fails, the bot logs a warning and uses the current library default. A `Client outdated (405)` error means WhatsApp rejected the advertised client version: rebuild with the current dependencies and restart. The new dependency versions require Go 1.26, including the Docker build stage.

## Existing installations

The formerly tracked root `login-store.db` is no longer included in images or Git tracking. Its local copy is preserved. Since this file was committed, revoke the old linked device in WhatsApp and pair again using a fresh database. Removing the file from the current tree does not remove it from Git history or existing published images.

Previous Compose deployments stored the entire `/app` directory in `sqlite_bp`. This configuration deliberately uses a new data volume and requires pairing again. Keep the old volume as a backup until migration is confirmed; do not mount it over `/app`.

## Validation and deployment

Run `go test -race ./...`, `go vet ./...`, and `go build .` with CGO enabled. Regression tests cover group reply routing, persisted chat-scoped search, dashboard authentication and mutation protection, fresh-store initialization, and transcription cancellation.

Pull requests run validation. Pushes to `main` deploy only after validation succeeds. Deployment needs Docker Hub and VM secrets plus `DOTENV_FILE` containing the required transcription settings and dashboard credentials above. The workflow updates the remote Compose file, writes `.env` with restricted permissions, pulls the image, and restarts the service. It does not automatically revoke old WhatsApp sessions or remove historical images/data.
