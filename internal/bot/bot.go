package bot

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type State struct {
	Status, QR string
	Started    time.Time
}
type Manager struct {
	mu               sync.Mutex
	op               sync.Mutex
	handlers         sync.Mutex
	workers          sync.WaitGroup
	state            State
	client           *whatsmeow.Client
	container        *sqlstore.Container
	db               *sql.DB
	transcribe       transcriber
	transcribeGroups bool
	terminal         bool
	cancel           context.CancelFunc
	session          context.Context
	generation       uint64
	active, closed   bool
}

func NewManager(transcribe func(context.Context, []byte) (string, error), dir string, terminal, transcribeGroups bool) (*Manager, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(dir, "login-store.db"))
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite3", u.String()+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Manager, error) { db.Close(); return nil, err }
	container := sqlstore.NewWithDB(db, "sqlite3", waLog.Stdout("Database", "WARN", true))
	if err := container.Upgrade(context.Background()); err != nil {
		return fail(err)
	}
	if err := initMessages(db); err != nil {
		return fail(err)
	}
	m := &Manager{db: db, container: container, transcribe: transcribe, transcribeGroups: transcribeGroups, terminal: terminal, state: State{Status: "Disconnected"}}
	return m, nil
}

func (m *Manager) Snapshot() State { m.mu.Lock(); defer m.mu.Unlock(); return m.state }
func (m *Manager) Connect() {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active || m.closed {
		return
	}
	m.session, m.cancel = context.WithCancel(context.Background())
	m.active = true
	m.generation++
	m.state = State{Status: "Connecting..."}
	m.workers.Add(1)
	go m.connect(m.session, m.generation)
}
func (m *Manager) set(gen uint64, state State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active && gen == m.generation {
		if state.Status == "Connecting..." && m.state.Status == "Connected" {
			return
		}
		m.state = state
	}
}
func (m *Manager) connect(ctx context.Context, gen uint64) {
	defer m.workers.Done()
	m.op.Lock()
	if ctx.Err() != nil {
		m.op.Unlock()
		return
	}
	var qr <-chan whatsmeow.QRChannelItem
	device, err := m.container.GetFirstDevice(ctx)
	if err != nil {
		m.op.Unlock()
		m.fail(gen)
		return
	}
	if device == nil {
		device = m.container.NewDevice()
	}
	client := whatsmeow.NewClient(device, waLog.Stdout("Client", "WARN", true))
	m.client = client
	client.AddEventHandler(func(evt interface{}) { m.event(ctx, gen, client, evt) })
	if device.ID == nil {
		qr, err = client.GetQRChannel(ctx)
	}
	if err == nil {
		err = client.Connect()
	}
	m.op.Unlock()
	if err != nil {
		m.fail(gen)
		log.Printf("Connection failed: %v", err)
		return
	}
	if qr == nil {
		return
	} // Connected events own the connection state.
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-qr:
			if !ok {
				if m.Snapshot().Status != "Connected" {
					m.fail(gen)
				}
				return
			}
			switch item.Event {
			case "code":
				png, err := qrcode.Encode(item.Code, qrcode.Medium, 256)
				if err != nil {
					m.fail(gen)
					return
				}
				m.set(gen, State{Status: "Waiting for QR Scan", QR: base64.StdEncoding.EncodeToString(png)})
				if m.terminal {
					code, _ := qrcode.New(item.Code, qrcode.Medium)
					fmt.Println(code.ToString(true))
				}
			case "success":
				m.set(gen, State{Status: "Connecting..."})
				return // The Connected event completes the pairing flow.
			case "err-scanned-without-multidevice":
				continue
			default:
				m.fail(gen)
				return
			}
		}
	}
}
func (m *Manager) fail(gen uint64) {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	if gen == m.generation && m.active {
		m.active = false
		m.cancel()
		m.state = State{Status: "Connection Failed"}
		m.mu.Unlock()
		m.client.Disconnect()
		return
	}
	m.mu.Unlock()
}
func (m *Manager) Disconnect() { m.stop(false) }
func (m *Manager) stop(closeManager bool) {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	if closeManager {
		m.closed = true
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.active = false
	m.generation++
	m.state = State{Status: "Disconnected"}
	m.mu.Unlock()
	m.client.Disconnect()
}
func (m *Manager) Close() {
	m.stop(true)
	m.workers.Wait()
	m.handlers.Lock()
	defer m.handlers.Unlock()
	m.db.Close()
}
func (m *Manager) event(session context.Context, gen uint64, client *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		m.mu.Lock()
		if m.active && gen == m.generation {
			m.state = State{Status: "Connected", Started: time.Now()}
		}
		m.mu.Unlock()
	case *events.Disconnected:
		m.mu.Lock()
		if m.active && gen == m.generation {
			m.state = State{Status: "Connecting..."}
		}
		m.mu.Unlock()
	case *events.LoggedOut, *events.ConnectFailure, *events.ClientOutdated, *events.TemporaryBan:
		m.mu.Lock()
		if m.active && gen == m.generation {
			m.active = false
			m.cancel()
			m.state = State{Status: "Connection Failed"}
		}
		m.mu.Unlock()
	case *events.Message:
		m.handlers.Lock()
		defer m.handlers.Unlock()
		m.mu.Lock()
		ctx, active := session, m.active && !m.closed && gen == m.generation
		m.mu.Unlock()
		if !active {
			return
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := handleMessage(ctx, client, m.db, v, m.transcribe, m.transcribeGroups); err != nil && ctx.Err() == nil {
			log.Printf("Message handling failed: %v", err)
		}
	}
}
