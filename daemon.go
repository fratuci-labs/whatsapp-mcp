package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var (
	nonDigitRe  = regexp.MustCompile(`\D+`)
	unsafeIDRe  = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
)

type Daemon struct {
	cfg       Config
	qrPath    string
	cli       *whatsmeow.Client
	msgStore  *MsgStore
	container *sqlstore.Container

	mu          sync.Mutex
	pairingLoop bool
	lastQRCode  string
}

type AuthRequest struct {
	Mode  string `json:"mode"` // "status", "qr", "pair_phone"
	Phone string `json:"phone,omitempty"`
}

type AuthResponse struct {
	LoggedIn       bool   `json:"logged_in"`
	Connected      bool   `json:"connected"`
	ReadOnly       bool   `json:"read_only,omitempty"`
	JID            string `json:"jid,omitempty"`
	PushName       string `json:"push_name,omitempty"`
	Status         string `json:"status"`
	QRPngPath      string `json:"qr_png_path,omitempty"`
	QRCode         string `json:"qr_code,omitempty"`
	PairingCode    string `json:"pairing_code,omitempty"`
	SyncedChats    int    `json:"synced_chats"`
	SyncedMessages int    `json:"synced_messages"`
	SyncedContacts int    `json:"synced_contacts"`
	Error          string `json:"error,omitempty"`
}

type ChatsRequest struct {
	Query      string `json:"query"`
	OnlyGroups bool   `json:"only_groups"`
	Limit      int    `json:"limit"`
}

type ContactsRequest struct {
	Query    string `json:"query"`
	GroupJID string `json:"group_jid"`
	Limit    int    `json:"limit"`
}

type MessagesRequest struct {
	ChatJID  string `json:"chat_jid"`
	Query    string `json:"query"`
	AfterTS  int64  `json:"after_ts"`
	BeforeTS int64  `json:"before_ts"`
	Limit    int    `json:"limit"`
}

type DownloadRequest struct {
	ChatJID string `json:"chat_jid"`
	MsgID   string `json:"msg_id"`
}

type DownloadResponse struct {
	Path      string `json:"path"`
	MimeType  string `json:"mime_type"`
	SizeBytes int    `json:"size_bytes"`
}

type SendRequest struct {
	ChatJID   string   `json:"chat_jid"`
	Text      string   `json:"text"`
	ReplyToID string   `json:"reply_to_id"`
	Mentions  []string `json:"mentions"`
}

type SendResponse struct {
	ID        string `json:"id"`
	ChatJID   string `json:"chat_jid"`
	Timestamp string `json:"timestamp"`
}

func runDaemon(cfg Config) error {
	applyDaemonUmask()
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(cfg.MediaDir, 0o700); err != nil {
		return fmt.Errorf("create media dir: %w", err)
	}

	if newDaemonClient(cfg).isRunning() {
		return nil // Another daemon instance is already serving
	}

	configureOSInfo(cfg.DeviceName)

	ctx := context.Background()
	sessionDB, err := openSQLiteDB(filepath.Join(cfg.DataDir, "session.db"))
	if err != nil {
		return fmt.Errorf("open session.db: %w", err)
	}
	defer sessionDB.Close()

	dbLogLevel := "WARN"
	if cfg.LogLevel == "DEBUG" || cfg.LogLevel == "ERROR" {
		dbLogLevel = cfg.LogLevel
	}
	dbLog := waLog.Stdout("DB", dbLogLevel, false)
	container := sqlstore.NewWithDB(sessionDB, "sqlite", dbLog)
	if err := container.Upgrade(ctx); err != nil {
		return fmt.Errorf("sqlstore upgrade: %w", err)
	}
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("get first device: %w", err)
	}

	msgStore, err := NewMsgStore(filepath.Join(cfg.DataDir, "messages.db"))
	if err != nil {
		return fmt.Errorf("msgStore init: %w", err)
	}
	defer msgStore.Close()

	cliLog := waLog.Stdout("WA", cfg.LogLevel, false)
	cli := whatsmeow.NewClient(deviceStore, cliLog)

	d := &Daemon{
		cfg:       cfg,
		qrPath:    filepath.Join(cfg.DataDir, "qr.png"),
		cli:       cli,
		msgStore:  msgStore,
		container: container,
	}
	cli.AddEventHandler(d.handleEvent)

	if cli.Store.ID != nil {
		if err := cli.Connect(); err != nil {
			fmt.Fprintf(os.Stderr, "initial connect warning: %v\n", err)
		}
	}

	listener, ep, cleanupIPC, err := listenDaemonIPC(cfg.DataDir)
	if err != nil {
		cli.Disconnect()
		return err
	}
	defer func() {
		cleanupIPC()
		cli.Disconnect()
	}()

	mux := http.NewServeMux()
	srv := &http.Server{
		Handler:           authMiddleware(ep.Token, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"version":   Version,
			"read_only": cfg.ReadOnly,
		})
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"stopping": true})
		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = srv.Shutdown(context.Background())
		}()
	})
	mux.HandleFunc("/auth", d.httpAuth)
	mux.HandleFunc("/chats", d.httpChats)
	mux.HandleFunc("/contacts", d.httpContacts)
	mux.HandleFunc("/messages", d.httpMessages)
	mux.HandleFunc("/download", d.httpDownload)
	mux.HandleFunc("/send", d.httpSend)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, daemonSignals()...)
	go func() {
		<-sigCh
		_ = srv.Shutdown(context.Background())
	}()

	if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (d *Daemon) handleEvent(rawEvt any) {
	switch evt := rawEvt.(type) {
	case *events.Connected:
		d.mu.Lock()
		d.pairingLoop = false
		d.lastQRCode = ""
		d.mu.Unlock()
		_ = os.Remove(d.qrPath)
		go d.syncMetadata(false)

	case *events.PairSuccess:
		d.mu.Lock()
		d.pairingLoop = false
		d.lastQRCode = ""
		d.mu.Unlock()
		_ = os.Remove(d.qrPath)

	case *events.LoggedOut:
		fmt.Fprintf(os.Stderr, "logged out event: %v\n", evt.Reason)

	case *events.Message:
		d.ingestEventMessage(evt)

	case *events.HistorySync:
		go d.handleHistorySync(evt)
	}
}

func (d *Daemon) ingestEventMessage(evt *events.Message) {
	if evt == nil || evt.Message == nil {
		return
	}
	msgType, text, replyToID, replyToSender, hasMedia, mimeType, fileName, skip := extractMessageFields(evt.Message)
	if skip {
		return
	}
	chatJID := evt.Info.Chat.ToNonAD().String()
	senderJID := evt.Info.Sender.ToNonAD().String()
	senderAlt := ""
	if !evt.Info.SenderAlt.IsEmpty() {
		senderAlt = evt.Info.SenderAlt.ToNonAD().String()
	}
	pushName := strings.TrimSpace(evt.Info.PushName)
	fullName := d.lookupStoreName(evt.Info.Sender.ToNonAD(), evt.Info.SenderAlt.ToNonAD())

	if senderJID != "" && (pushName != "" || fullName != "" || senderAlt != "") {
		_ = d.msgStore.UpsertContact(senderJID, senderAlt, pushName, fullName)
	}
	senderDisplay := fullName
	if senderDisplay == "" {
		senderDisplay = pushName
	}

	rawBytes, _ := proto.Marshal(evt.Message)
	ts := evt.Info.Timestamp.Unix()
	_ = d.msgStore.UpsertMessage(
		chatJID, evt.Info.ID, ts,
		senderJID, senderAlt, senderDisplay,
		evt.Info.IsFromMe,
		msgType, text, replyToID, replyToSender,
		hasMedia, mimeType, fileName, rawBytes,
	)
}

func (d *Daemon) lookupStoreName(primary, alt types.JID) string {
	if d.cli == nil || d.cli.Store == nil || d.cli.Store.Contacts == nil {
		return ""
	}
	ctx := context.Background()
	for _, j := range []types.JID{primary, alt} {
		if j.IsEmpty() {
			continue
		}
		if ci, err := d.cli.Store.Contacts.GetContact(ctx, j); err == nil && ci.Found {
			if ci.FullName != "" {
				return ci.FullName
			}
			if ci.FirstName != "" {
				return ci.FirstName
			}
			if ci.PushName != "" {
				return ci.PushName
			}
			if ci.BusinessName != "" {
				return ci.BusinessName
			}
		}
	}
	// Try LID -> PN lookup via whatsmeow's LID store
	if primary.Server == types.HiddenUserServer && d.cli.Store.LIDs != nil {
		if pn, err := d.cli.Store.LIDs.GetPNForLID(ctx, primary); err == nil && !pn.IsEmpty() {
			if ci, err := d.cli.Store.Contacts.GetContact(ctx, pn); err == nil && ci.Found {
				if ci.FullName != "" {
					return ci.FullName
				}
				if ci.PushName != "" {
					return ci.PushName
				}
			}
		}
	}
	return ""
}

func (d *Daemon) handleHistorySync(evt *events.HistorySync) {
	if evt == nil || evt.Data == nil {
		return
	}
	for _, pn := range evt.Data.GetPushnames() {
		if pn.GetID() != "" && pn.GetPushname() != "" {
			_ = d.msgStore.UpsertContact(pn.GetID(), "", pn.GetPushname(), "")
		}
	}
	for _, m := range evt.Data.GetPhoneNumberToLidMappings() {
		pnJID := m.GetPnJID()
		lidJID := m.GetLidJID()
		if pnJID != "" && lidJID != "" {
			_ = d.msgStore.UpsertContact(lidJID, pnJID, "", "")
		}
	}
	for _, conv := range evt.Data.GetConversations() {
		rawID := conv.GetID()
		chatJID, err := types.ParseJID(rawID)
		if err != nil || chatJID.Server == types.BroadcastServer {
			continue
		}
		cleanChat := chatJID.ToNonAD().String()
		name := conv.GetName()
		if name == "" {
			name = conv.GetDisplayName()
		}
		isGroup := chatJID.Server == types.GroupServer
		_ = d.msgStore.UpsertChat(cleanChat, name, isGroup, int64(conv.GetLastMsgTimestamp()))

		if conv.GetLidJID() != "" && conv.GetPnJID() != "" {
			_ = d.msgStore.UpsertContact(conv.GetLidJID(), conv.GetPnJID(), "", name)
		}

		for _, hm := range conv.GetMessages() {
			if hm.GetMessage() == nil {
				continue
			}
			parsed, err := d.cli.ParseWebMessage(chatJID, hm.GetMessage())
			if err != nil {
				continue
			}
			d.ingestEventMessage(parsed)
		}
	}
}

func (d *Daemon) syncMetadata(forceGroups bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if d.cli.Store != nil && d.cli.Store.Contacts != nil {
		if all, err := d.cli.Store.Contacts.GetAllContacts(ctx); err == nil {
			for jid, ci := range all {
				clean := jid.ToNonAD().String()
				alt := ""
				if d.cli.Store.LIDs != nil && jid.Server == types.DefaultUserServer {
					if lid, err := d.cli.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
						alt = lid.ToNonAD().String()
					}
				}
				_ = d.msgStore.UpsertContact(clean, alt, ci.PushName, ci.FullName)
				if !strings.HasSuffix(clean, "@g.us") && (ci.FullName != "" || ci.PushName != "") {
					n := ci.FullName
					if n == "" {
						n = ci.PushName
					}
					_ = d.msgStore.UpsertChat(clean, n, false, 0)
				}
			}
		}
	}

	existingChats, _, _ := d.msgStore.Stats()
	if !forceGroups && existingChats > 0 {
		return
	}

	if groups, err := d.cli.GetJoinedGroups(ctx); err == nil {
		for _, g := range groups {
			gJID := g.JID.ToNonAD().String()
			_ = d.msgStore.UpsertChat(gJID, g.Name, true, 0)
			for _, p := range g.Participants {
				pJID := p.JID.ToNonAD().String()
				alt := ""
				if !p.LID.IsEmpty() && !p.PhoneNumber.IsEmpty() {
					pJID = p.LID.ToNonAD().String()
					alt = p.PhoneNumber.ToNonAD().String()
				}
				if pJID != "" {
					_ = d.msgStore.UpsertContact(pJID, alt, p.DisplayName, "")
				}
			}
		}
	}
}

func (d *Daemon) buildStatusResponse(extraStatus string, pairingCode string, qrCode string, errMsg string) AuthResponse {
	chats, msgs, contacts := d.msgStore.Stats()
	loggedIn := d.cli.Store.ID != nil
	connected := d.cli.IsConnected()
	jidStr := ""
	pushName := ""
	if d.cli.Store.ID != nil {
		jidStr = d.cli.Store.ID.ToNonAD().String()
		pushName = d.cli.Store.PushName
	}
	status := extraStatus
	if status == "" {
		switch {
		case loggedIn && connected:
			status = "connected"
		case loggedIn && !connected:
			status = "reconnecting"
		default:
			status = "unpaired"
		}
	}
	qrFile := ""
	if !loggedIn {
		if _, err := os.Stat(d.qrPath); err == nil {
			qrFile = d.qrPath
		}
	}
	return AuthResponse{
		LoggedIn:       loggedIn,
		Connected:      connected,
		ReadOnly:       d.cfg.ReadOnly,
		JID:            jidStr,
		PushName:       pushName,
		Status:         status,
		QRPngPath:      qrFile,
		QRCode:         qrCode,
		PairingCode:    pairingCode,
		SyncedChats:    chats,
		SyncedMessages: msgs,
		SyncedContacts: contacts,
		Error:          errMsg,
	}
}

func (d *Daemon) httpAuth(w http.ResponseWriter, r *http.Request) {
	var req AuthRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Mode == "" {
		req.Mode = "status"
	}

	if d.cli.Store.ID != nil {
		if !d.cli.IsConnected() {
			_ = d.cli.Connect()
			time.Sleep(500 * time.Millisecond)
		}
		writeJSON(w, http.StatusOK, d.buildStatusResponse("", "", "", ""))
		return
	}

	if req.Mode == "status" {
		writeJSON(w, http.StatusOK, d.buildStatusResponse("", "", "", ""))
		return
	}

	// Reuse active QR pairing loop if already connected and waiting for scan.
	d.mu.Lock()
	if d.pairingLoop && d.cli.IsConnected() && d.lastQRCode != "" {
		activeQR := d.lastQRCode
		d.mu.Unlock()
		if req.Mode == "pair_phone" {
			d.handlePairPhone(w, r, req.Phone)
			return
		}
		writeJSON(w, http.StatusOK, d.buildStatusResponse("scan_qr", "", activeQR, ""))
		return
	}

	if d.cli.IsConnected() {
		d.cli.Disconnect()
		time.Sleep(300 * time.Millisecond)
	}
	qrChan, err := d.cli.GetQRChannel(context.Background())
	if err != nil {
		d.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, d.buildStatusResponse("error", "", "", err.Error()))
		return
	}
	if err := d.cli.Connect(); err != nil {
		d.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, d.buildStatusResponse("error", "", "", err.Error()))
		return
	}
	d.pairingLoop = true
	d.mu.Unlock()

	firstCodeCh := make(chan string, 1)
	firstErrCh := make(chan error, 1)

	go func() {
		sentFirst := false
		for item := range qrChan {
			switch item.Event {
			case "code":
				_ = qrcode.WriteFile(item.Code, qrcode.Medium, 512, d.qrPath)
				_ = os.Chmod(d.qrPath, 0o600)
				d.mu.Lock()
				d.lastQRCode = item.Code
				d.mu.Unlock()
				if !sentFirst {
					sentFirst = true
					firstCodeCh <- item.Code
				}
			case "error":
				if !sentFirst {
					sentFirst = true
					firstErrCh <- item.Error
				}
			case "success":
				_ = os.Remove(d.qrPath)
			}
		}
		d.mu.Lock()
		d.pairingLoop = false
		d.lastQRCode = ""
		d.mu.Unlock()
	}()

	var currentCode string
	select {
	case currentCode = <-firstCodeCh:
	case err := <-firstErrCh:
		writeJSON(w, http.StatusInternalServerError, d.buildStatusResponse("error", "", "", err.Error()))
		return
	case <-time.After(15 * time.Second):
		writeJSON(w, http.StatusGatewayTimeout, d.buildStatusResponse("timeout", "", "", "timed out waiting for WhatsApp QR websocket"))
		return
	}

	if req.Mode == "pair_phone" {
		d.handlePairPhone(w, r, req.Phone)
		return
	}

	writeJSON(w, http.StatusOK, d.buildStatusResponse("scan_qr", "", currentCode, ""))
}

func (d *Daemon) handlePairPhone(w http.ResponseWriter, r *http.Request, phone string) {
	digits := nonDigitRe.ReplaceAllString(phone, "")
	if len(digits) < 10 {
		writeJSON(w, http.StatusBadRequest, d.buildStatusResponse("error", "", "", "invalid phone number; include country code e.g. 5511999999999"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	code, err := d.cli.PairPhone(ctx, digits, true, whatsmeow.PairClientChrome, pairClientDisplayName())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, d.buildStatusResponse("error", "", "", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, d.buildStatusResponse("enter_pairing_code", code, "", ""))
}

func (d *Daemon) httpChats(w http.ResponseWriter, r *http.Request) {
	var req ChatsRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	chats, err := d.msgStore.QueryChats(req.Query, req.OnlyGroups, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(chats) == 0 && d.cli.IsConnected() {
		d.syncMetadata(true)
		chats, _ = d.msgStore.QueryChats(req.Query, req.OnlyGroups, req.Limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{"chats": chats})
}

func (d *Daemon) httpContacts(w http.ResponseWriter, r *http.Request) {
	var req ContactsRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.GroupJID != "" && d.cli.IsConnected() {
		gJID, err := types.ParseJID(req.GroupJID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group_jid: "+err.Error())
			return
		}
		gi, err := d.cli.GetGroupInfo(r.Context(), gJID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GetGroupInfo: "+err.Error())
			return
		}
		_ = d.msgStore.UpsertChat(gJID.ToNonAD().String(), gi.Name, true, 0)
		q := strings.ToLower(strings.TrimSpace(req.Query))
		members := []ContactRecord{}
		for _, p := range gi.Participants {
			primary := p.JID.ToNonAD()
			alt := types.EmptyJID
			if !p.LID.IsEmpty() && !p.PhoneNumber.IsEmpty() {
				primary = p.LID.ToNonAD()
				alt = p.PhoneNumber.ToNonAD()
			}
			fullName := d.lookupStoreName(primary, alt)
			pushName := p.DisplayName
			if local, _ := d.msgStore.QueryContacts(primary.String(), 1); len(local) > 0 {
				if fullName == "" {
					fullName = local[0].FullName
				}
				if pushName == "" {
					pushName = local[0].PushName
				}
			}
			rec := ContactRecord{
				JID:      primary.String(),
				AltJID:   alt.String(),
				PushName: pushName,
				FullName: fullName,
				IsAdmin:  p.IsAdmin || p.IsSuperAdmin,
			}
			_ = d.msgStore.UpsertContact(rec.JID, rec.AltJID, rec.PushName, rec.FullName)
			if q == "" ||
				strings.Contains(strings.ToLower(rec.JID), q) ||
				strings.Contains(strings.ToLower(rec.AltJID), q) ||
				strings.Contains(strings.ToLower(rec.PushName), q) ||
				strings.Contains(strings.ToLower(rec.FullName), q) {
				members = append(members, rec)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"group_jid":  gJID.ToNonAD().String(),
			"group_name": gi.Name,
			"contacts":   members,
		})
		return
	}

	contacts, err := d.msgStore.QueryContacts(req.Query, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": contacts})
}

func (d *Daemon) httpMessages(w http.ResponseWriter, r *http.Request) {
	var req MessagesRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	msgs, err := d.msgStore.QueryMessages(req.ChatJID, req.Query, req.AfterTS, req.BeforeTS, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (d *Daemon) httpDownload(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.MsgID == "" {
		writeError(w, http.StatusBadRequest, "msg_id is required")
		return
	}
	if !d.cli.IsConnected() {
		writeError(w, http.StatusServiceUnavailable, "WhatsApp client is not connected")
		return
	}
	_, _, mimeType, fileName, rawProto, err := d.msgStore.GetMessageRaw(req.ChatJID, req.MsgID)
	if err != nil || len(rawProto) == 0 {
		writeError(w, http.StatusNotFound, "message media metadata not found in local store")
		return
	}
	var msg waE2E.Message
	if err := proto.Unmarshal(rawProto, &msg); err != nil {
		writeError(w, http.StatusInternalServerError, "unmarshal message proto: "+err.Error())
		return
	}

	data, err := downloadFromProto(r.Context(), d.cli, &msg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "download media: "+err.Error())
		return
	}

	ext := guessExtension(mimeType, fileName)
	safeID := unsafeIDRe.ReplaceAllString(req.MsgID, "_")
	outPath := filepath.Join(d.cfg.MediaDir, safeID+ext)
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "write media file: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, DownloadResponse{
		Path:      outPath,
		MimeType:  mimeType,
		SizeBytes: len(data),
	})
}

func downloadFromProto(ctx context.Context, cli *whatsmeow.Client, msg *waE2E.Message) ([]byte, error) {
	switch {
	case msg.AudioMessage != nil:
		return cli.Download(ctx, msg.GetAudioMessage())
	case msg.ImageMessage != nil:
		return cli.Download(ctx, msg.GetImageMessage())
	case msg.VideoMessage != nil:
		return cli.Download(ctx, msg.GetVideoMessage())
	case msg.DocumentMessage != nil:
		return cli.Download(ctx, msg.GetDocumentMessage())
	case msg.StickerMessage != nil:
		return cli.Download(ctx, msg.GetStickerMessage())
	default:
		return nil, errors.New("message does not contain downloadable media")
	}
}

func guessExtension(mimeType, fileName string) string {
	if ext := filepath.Ext(fileName); ext != "" {
		return ext
	}
	baseMime := strings.Split(mimeType, ";")[0]
	switch strings.TrimSpace(strings.ToLower(baseMime)) {
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/aac":
		return ".m4a"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "application/pdf":
		return ".pdf"
	}
	if exts, _ := mime.ExtensionsByType(baseMime); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

func (d *Daemon) httpSend(w http.ResponseWriter, r *http.Request) {
	if d.cfg.ReadOnly {
		writeError(w, http.StatusForbidden, "sending messages is disabled (WHATSAPP_MCP_READ_ONLY=true)")
		return
	}
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ChatJID == "" || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "chat_jid and text are required")
		return
	}
	if !d.cli.IsConnected() {
		writeError(w, http.StatusServiceUnavailable, "WhatsApp client is not connected")
		return
	}
	targetJID, err := types.ParseJID(req.ChatJID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chat_jid: "+err.Error())
		return
	}

	text, ctxInfo := buildOutgoingContext(d.msgStore, targetJID.ToNonAD().String(), req.Text, req.ReplyToID, req.Mentions)

	var waMsg *waE2E.Message
	if ctxInfo != nil {
		waMsg = &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(text),
				ContextInfo: ctxInfo,
			},
		}
	} else {
		waMsg = &waE2E.Message{
			Conversation: proto.String(text),
		}
	}

	resp, err := d.cli.SendMessage(r.Context(), targetJID, waMsg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SendMessage: "+err.Error())
		return
	}

	myJID := ""
	if d.cli.Store.ID != nil {
		myJID = d.cli.Store.ID.ToNonAD().String()
	}
	rawOut, _ := proto.Marshal(waMsg)
	_ = d.msgStore.UpsertMessage(
		targetJID.ToNonAD().String(), resp.ID, resp.Timestamp.Unix(),
		myJID, "", d.cli.Store.PushName, true,
		"text", text, req.ReplyToID, "", false, "", "", rawOut,
	)

	writeJSON(w, http.StatusOK, SendResponse{
		ID:        resp.ID,
		ChatJID:   targetJID.ToNonAD().String(),
		Timestamp: resp.Timestamp.Local().Format("2006-01-02 15:04:05"),
	})
}

func buildOutgoingContext(msgStore *MsgStore, cleanChatJID, rawText, replyToID string, mentions []string) (string, *waE2E.ContextInfo) {
	text := rawText
	var ctxInfo *waE2E.ContextInfo

	if replyToID != "" && msgStore != nil {
		_, origSender, _, _, origRaw, err := msgStore.GetMessageRaw(cleanChatJID, replyToID)
		if err == nil {
			ctxInfo = &waE2E.ContextInfo{
				StanzaID: proto.String(replyToID),
			}
			if origSender != "" {
				ctxInfo.Participant = proto.String(origSender)
			}
			if len(origRaw) > 0 {
				var quoted waE2E.Message
				if proto.Unmarshal(origRaw, &quoted) == nil {
					ctxInfo.QuotedMessage = &quoted
				}
			}
		}
	}

	if len(mentions) > 0 {
		if ctxInfo == nil {
			ctxInfo = &waE2E.ContextInfo{}
		}
		var mentionedJIDs []string
		var missingTags []string
		for _, m := range mentions {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			parsed, err := types.ParseJID(m)
			if err != nil || parsed.Server == "" {
				digits := strings.TrimPrefix(m, "@")
				parsed = types.NewJID(digits, types.DefaultUserServer)
			}
			cleanJID := parsed.ToNonAD().String()
			mentionedJIDs = append(mentionedJIDs, cleanJID)

			// Normalize "@12345@lid" in text to "@12345" so WhatsApp renders clickable name
			text = strings.ReplaceAll(text, "@"+cleanJID, "@"+parsed.User)
			if !strings.Contains(text, "@"+parsed.User) {
				missingTags = append(missingTags, "@"+parsed.User)
			}
		}
		if len(missingTags) > 0 {
			text = strings.Join(missingTags, " ") + " " + text
		}
		ctxInfo.MentionedJID = mentionedJIDs
	}

	return text, ctxInfo
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
