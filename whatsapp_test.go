package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func TestSQLStorePureGoUpgrade(t *testing.T) {
	tmp := t.TempDir()
	db, err := openSQLiteDB(filepath.Join(tmp, "session.db"))
	if err != nil {
		t.Fatalf("openSQLiteDB failed: %v", err)
	}
	defer db.Close()

	container := sqlstore.NewWithDB(db, "sqlite", waLog.Noop)
	ctx := context.Background()
	if err := container.Upgrade(ctx); err != nil {
		t.Fatalf("sqlstore.Upgrade with modernc.org/sqlite failed: %v", err)
	}
	dev, err := container.GetFirstDevice(ctx)
	if err != nil {
		t.Fatalf("GetFirstDevice failed: %v", err)
	}
	if dev == nil {
		t.Fatal("expected non-nil device store")
	}
}

func TestMsgStoreLifecycleAndLIDResolution(t *testing.T) {
	tmp := t.TempDir()
	ms, err := NewMsgStore(filepath.Join(tmp, "messages.db"))
	if err != nil {
		t.Fatalf("NewMsgStore: %v", err)
	}
	defer ms.Close()

	// Empty slices should marshal as [] rather than null
	emptyChats, err := ms.QueryChats("", false, 10)
	if err != nil {
		t.Fatalf("QueryChats empty: %v", err)
	}
	if b, _ := json.Marshal(emptyChats); string(b) != "[]" {
		t.Fatalf("expected empty JSON array [], got %s", string(b))
	}

	// Upsert group and contacts with @lid <-> phone JID mapping
	groupJID := "120363000000000000@g.us"
	lidJID := "100000000000001@lid"
	phoneJID := "5511999999999@s.whatsapp.net"

	if err := ms.UpsertChat(groupJID, "Platform Architecture", true, 1700000000); err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}
	if err := ms.UpsertContact(phoneJID, lidJID, "Ana", "Ana Silva"); err != nil {
		t.Fatalf("UpsertContact: %v", err)
	}

	raw, _ := proto.Marshal(&waE2E.Message{Conversation: proto.String("Deploy completed successfully")})
	// Ingest message sent from @lid without inline senderName to verify JOIN resolution
	if err := ms.UpsertMessage(
		groupJID, "MSG-001", 1700000100,
		lidJID, "", "",
		false, "text", "Deploy completed successfully", "", "",
		false, "", "", raw,
	); err != nil {
		t.Fatalf("UpsertMessage 1: %v", err)
	}
	if err := ms.UpsertMessage(
		groupJID, "MSG-002", 1700000200,
		phoneJID, lidJID, "Ana Silva",
		true, "text", "Metrics validated", "MSG-001", lidJID,
		false, "", "", raw,
	); err != nil {
		t.Fatalf("UpsertMessage 2: %v", err)
	}

	chatsCnt, msgsCnt, contactsCnt := ms.Stats()
	if chatsCnt != 1 || msgsCnt != 2 || contactsCnt != 2 {
		t.Fatalf("unexpected stats: chats=%d msgs=%d contacts=%d", chatsCnt, msgsCnt, contactsCnt)
	}

	// Chronological order check (oldest -> newest)
	msgs, err := ms.QueryMessages(groupJID, "", 0, 0, 10)
	if err != nil {
		t.Fatalf("QueryMessages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].ID != "MSG-001" || msgs[1].ID != "MSG-002" {
		t.Fatalf("unexpected message order: %+v", msgs)
	}
	if msgs[0].SenderName != "Ana Silva" {
		t.Fatalf("expected @lid sender name resolved to 'Ana Silva', got %q", msgs[0].SenderName)
	}

	// Search by resolved contact name even when m.sender_name was empty on MSG-001
	bySender, err := ms.QueryMessages("", "ana silva", 0, 0, 10)
	if err != nil {
		t.Fatalf("QueryMessages by sender: %v", err)
	}
	if len(bySender) != 2 {
		t.Fatalf("expected 2 messages matching resolved sender name, got %d", len(bySender))
	}

	// Verify raw proto lookup
	foundChat, sender, _, _, gotProto, err := ms.GetMessageRaw("", "MSG-001")
	if err != nil {
		t.Fatalf("GetMessageRaw: %v", err)
	}
	if foundChat != groupJID || sender != lidJID || len(gotProto) == 0 {
		t.Fatalf("unexpected GetMessageRaw result: chat=%s sender=%s len=%d", foundChat, sender, len(gotProto))
	}
}

func TestExtractMessageFieldsAndOutgoingMentions(t *testing.T) {
	tmp := t.TempDir()
	ms, err := NewMsgStore(filepath.Join(tmp, "messages.db"))
	if err != nil {
		t.Fatalf("NewMsgStore: %v", err)
	}
	defer ms.Close()

	origMsg := &waE2E.Message{Conversation: proto.String("Original alert")}
	origBytes, _ := proto.Marshal(origMsg)
	_ = ms.UpsertMessage(
		"120363000000000000@g.us", "ORIG-1", 1700000000,
		"100000000000001@lid", "", "Carlos", false,
		"text", "Original alert", "", "", false, "", "", origBytes,
	)

	// Unwrap edited message protocol wrapper
	edited := &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			EditedMessage: &waE2E.Message{
				Conversation: proto.String("Updated alert"),
			},
		},
	}
	mType, txt, _, _, _, _, _, skip := extractMessageFields(edited)
	if skip || mType != "text" || txt != "Updated alert" {
		t.Fatalf("edited unwrap failed: skip=%v type=%s txt=%q", skip, mType, txt)
	}

	// Outgoing mention normalization + native quote ContextInfo
	formattedText, ctxInfo := buildOutgoingContext(
		ms,
		"120363000000000000@g.us",
		"Hi @100000000000001@lid please review",
		"ORIG-1",
		[]string{"100000000000001@lid", "5511988887777@s.whatsapp.net"},
	)
	if ctxInfo == nil {
		t.Fatal("expected non-nil ContextInfo")
	}
	if ctxInfo.GetStanzaID() != "ORIG-1" || ctxInfo.GetParticipant() != "100000000000001@lid" {
		t.Fatalf("unexpected quote stanza/participant: %+v", ctxInfo)
	}
	if strings.Contains(formattedText, "@lid") {
		t.Fatalf("expected @lid suffix stripped from visible @tag, got %q", formattedText)
	}
	if !strings.Contains(formattedText, "@100000000000001") || !strings.Contains(formattedText, "@5511988887777") {
		t.Fatalf("expected both @tags in text, got %q", formattedText)
	}
	if len(ctxInfo.MentionedJID) != 2 {
		t.Fatalf("expected 2 MentionedJID entries, got %v", ctxInfo.MentionedJID)
	}
}

func TestDaemonIPCAuthAndReadOnlyEnforcement(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}

	listener, ep, cleanup, err := listenDaemonIPC(dataDir)
	if err != nil {
		t.Fatalf("listenDaemonIPC: %v", err)
	}
	defer cleanup()

	cfg := Config{
		BaseDir:        tmp,
		DataDir:        dataDir,
		MediaDir:       filepath.Join(tmp, "media"),
		ReadOnly:       true,
		LogLevel:       "WARN",
		RequestTimeout: 5 * time.Second,
	}
	d := &Daemon{cfg: cfg}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version})
	})
	mux.HandleFunc("/send", d.httpSend)

	srv := &http.Server{Handler: authMiddleware(ep.Token, mux)}
	go func() { _ = srv.Serve(listener) }()
	defer srv.Shutdown(context.Background())

	dc := newDaemonClient(cfg)
	if !dc.isRunning() {
		t.Fatal("expected daemonClient.isRunning() == true")
	}

	// Tampered token must be rejected with 401
	badEP := ep
	badEP.Token = strings.Repeat("0", 64)
	if err := dc.doWithEndpoint(context.Background(), badEP, http.MethodGet, "/health", nil, nil); err == nil {
		t.Fatal("expected unauthorized error with invalid Bearer token")
	}

	// ReadOnly mode must reject /send with forbidden error
	err = dc.doWithEndpoint(context.Background(), ep, http.MethodPost, "/send", SendRequest{
		ChatJID: "5511999999999@s.whatsapp.net",
		Text:    "blocked",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "WHATSAPP_MCP_READ_ONLY") {
		t.Fatalf("expected read-only rejection error, got %v", err)
	}
}

func TestAuthResultEmbedsInlinePNGAndConfigEnv(t *testing.T) {
	tmp := t.TempDir()
	qrFile := filepath.Join(tmp, "qr.png")
	if err := qrcode.WriteFile("test-qr-payload", qrcode.Medium, 256, qrFile); err != nil {
		t.Fatalf("WriteFile QR: %v", err)
	}

	res, _, err := authResultWithQR(AuthResponse{
		Status:    "scan_qr",
		QRPngPath: qrFile,
		QRCode:    "secret-raw-code",
	})
	if err != nil {
		t.Fatalf("authResultWithQR: %v", err)
	}
	if len(res.Content) != 2 {
		t.Fatalf("expected 2 MCP content blocks (TextContent + ImageContent), got %d", len(res.Content))
	}
	img, ok := res.Content[1].(*mcp.ImageContent)
	if !ok || img.MIMEType != "image/png" || len(img.Data) == 0 {
		t.Fatalf("expected valid image/png ImageContent, got %#v", res.Content[1])
	}

	t.Setenv("WHATSAPP_MCP_HOME", tmp)
	t.Setenv("WHATSAPP_MCP_READ_ONLY", "true")
	t.Setenv("WHATSAPP_MCP_LOG_LEVEL", "debug")
	t.Setenv("WHATSAPP_MCP_REQUEST_TIMEOUT", "60")

	cfg := loadConfig()
	if cfg.BaseDir != tmp || !cfg.ReadOnly || cfg.LogLevel != "DEBUG" || cfg.RequestTimeout != 60*time.Second {
		t.Fatalf("unexpected config from env: %+v", cfg)
	}
}
