package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

type MsgStore struct {
	db *sql.DB
}

type ChatRecord struct {
	JID           string `json:"jid"`
	Name          string `json:"name"`
	IsGroup       bool   `json:"is_group"`
	LastMessageAt string `json:"last_message_at,omitempty"`
	LastMsgTS     int64  `json:"last_msg_ts"`
	MessageCount  int    `json:"message_count"`
}

type ContactRecord struct {
	JID      string `json:"jid"`
	AltJID   string `json:"alt_jid,omitempty"`
	PushName string `json:"push_name,omitempty"`
	FullName string `json:"full_name,omitempty"`
	IsAdmin  bool   `json:"is_admin,omitempty"`
}

type MessageRecord struct {
	ID            string `json:"id"`
	ChatJID       string `json:"chat_jid"`
	ChatName      string `json:"chat_name,omitempty"`
	Timestamp     int64  `json:"timestamp"`
	DateTime      string `json:"datetime"`
	SenderJID     string `json:"sender_jid"`
	SenderAlt     string `json:"sender_alt,omitempty"`
	SenderName    string `json:"sender_name,omitempty"`
	IsFromMe      bool   `json:"is_from_me"`
	Type          string `json:"type"`
	Text          string `json:"text"`
	ReplyToID     string `json:"reply_to_id,omitempty"`
	ReplyToSender string `json:"reply_to_sender,omitempty"`
	HasMedia      bool   `json:"has_media"`
	MimeType      string `json:"mime_type,omitempty"`
	FileName      string `json:"file_name,omitempty"`
}

func NewMsgStore(dbPath string) (*MsgStore, error) {
	db, err := openSQLiteDB(dbPath)
	if err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS chats (
		jid TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		is_group INTEGER NOT NULL DEFAULT 0,
		last_msg_ts INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS contacts (
		jid TEXT PRIMARY KEY,
		alt_jid TEXT NOT NULL DEFAULT '',
		push_name TEXT NOT NULL DEFAULT '',
		full_name TEXT NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_contacts_alt ON contacts(alt_jid);

	CREATE TABLE IF NOT EXISTS messages (
		chat_jid TEXT NOT NULL,
		msg_id TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		sender_jid TEXT NOT NULL DEFAULT '',
		sender_alt TEXT NOT NULL DEFAULT '',
		sender_name TEXT NOT NULL DEFAULT '',
		is_from_me INTEGER NOT NULL DEFAULT 0,
		msg_type TEXT NOT NULL DEFAULT 'text',
		text TEXT NOT NULL DEFAULT '',
		reply_to_id TEXT NOT NULL DEFAULT '',
		reply_to_sender TEXT NOT NULL DEFAULT '',
		has_media INTEGER NOT NULL DEFAULT 0,
		mime_type TEXT NOT NULL DEFAULT '',
		file_name TEXT NOT NULL DEFAULT '',
		raw_proto BLOB,
		PRIMARY KEY (chat_jid, msg_id)
	);
	CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages(chat_jid, timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_id ON messages(msg_id);
	CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(timestamp DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &MsgStore{db: db}, nil
}

func (s *MsgStore) Close() error {
	return s.db.Close()
}

func (s *MsgStore) UpsertChat(jid, name string, isGroup bool, lastMsgTS int64) error {
	if jid == "" || jid == "status@broadcast" {
		return nil
	}
	ig := 0
	if isGroup || strings.HasSuffix(jid, "@g.us") {
		ig = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO chats (jid, name, is_group, last_msg_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE chats.name END,
			is_group = MAX(chats.is_group, excluded.is_group),
			last_msg_ts = MAX(chats.last_msg_ts, excluded.last_msg_ts)
	`, jid, strings.TrimSpace(name), ig, lastMsgTS)
	return err
}

func (s *MsgStore) UpsertContact(jid, altJID, pushName, fullName string) error {
	if jid == "" {
		return nil
	}
	now := time.Now().Unix()
	cleanAlt := strings.TrimSpace(altJID)
	cleanPush := strings.TrimSpace(pushName)
	cleanFull := strings.TrimSpace(fullName)

	_, err := s.db.Exec(`
		INSERT INTO contacts (jid, alt_jid, push_name, full_name, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			alt_jid = CASE WHEN excluded.alt_jid != '' THEN excluded.alt_jid ELSE contacts.alt_jid END,
			push_name = CASE WHEN excluded.push_name != '' THEN excluded.push_name ELSE contacts.push_name END,
			full_name = CASE WHEN excluded.full_name != '' THEN excluded.full_name ELSE contacts.full_name END,
			updated_at = excluded.updated_at
	`, jid, cleanAlt, cleanPush, cleanFull, now)
	if cleanAlt != "" {
		_, _ = s.db.Exec(`
			INSERT INTO contacts (jid, alt_jid, push_name, full_name, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(jid) DO UPDATE SET
				alt_jid = CASE WHEN excluded.alt_jid != '' THEN excluded.alt_jid ELSE contacts.alt_jid END,
				push_name = CASE WHEN excluded.push_name != '' THEN excluded.push_name ELSE contacts.push_name END,
				full_name = CASE WHEN excluded.full_name != '' THEN excluded.full_name ELSE contacts.full_name END,
				updated_at = excluded.updated_at
		`, cleanAlt, jid, cleanPush, cleanFull, now)
	}
	return err
}

func (s *MsgStore) UpsertMessage(
	chatJID, msgID string,
	ts int64,
	senderJID, senderAlt, senderName string,
	isFromMe bool,
	msgType, text, replyToID, replyToSender string,
	hasMedia bool,
	mimeType, fileName string,
	rawProto []byte,
) error {
	if chatJID == "" || msgID == "" || chatJID == "status@broadcast" {
		return nil
	}
	fm := 0
	if isFromMe {
		fm = 1
	}
	hm := 0
	if hasMedia {
		hm = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO messages (
			chat_jid, msg_id, timestamp, sender_jid, sender_alt, sender_name,
			is_from_me, msg_type, text, reply_to_id, reply_to_sender,
			has_media, mime_type, file_name, raw_proto
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat_jid, msg_id) DO UPDATE SET
			sender_alt = CASE WHEN excluded.sender_alt != '' THEN excluded.sender_alt ELSE messages.sender_alt END,
			sender_name = CASE WHEN excluded.sender_name != '' THEN excluded.sender_name ELSE messages.sender_name END,
			text = CASE WHEN excluded.text != '' THEN excluded.text ELSE messages.text END,
			raw_proto = CASE WHEN length(excluded.raw_proto) > 0 THEN excluded.raw_proto ELSE messages.raw_proto END
	`,
		chatJID, msgID, ts, senderJID, senderAlt, senderName,
		fm, msgType, text, replyToID, replyToSender,
		hm, mimeType, fileName, rawProto,
	)
	if err == nil {
		_ = s.UpsertChat(chatJID, "", strings.HasSuffix(chatJID, "@g.us"), ts)
	}
	return err
}

func (s *MsgStore) Stats() (chats int, messages int, contacts int) {
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&chats)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&messages)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM contacts`).Scan(&contacts)
	return
}

func (s *MsgStore) QueryChats(query string, onlyGroups bool, limit int) ([]ChatRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := strings.TrimSpace(strings.ToLower(query))
	sqlStr := `
		SELECT
			c.jid,
			COALESCE(NULLIF(c.name, ''), NULLIF(ct.full_name, ''), NULLIF(ct.push_name, ''), c.jid) AS display_name,
			c.is_group,
			c.last_msg_ts,
			(SELECT COUNT(*) FROM messages m WHERE m.chat_jid = c.jid) AS msg_cnt
		FROM chats c
		LEFT JOIN contacts ct ON ct.jid = c.jid
		WHERE (? = 0 OR c.is_group = 1)
		  AND (? = '' OR lower(c.jid) LIKE '%' || ? || '%'
		       OR lower(c.name) LIKE '%' || ? || '%'
		       OR lower(COALESCE(ct.full_name, '')) LIKE '%' || ? || '%'
		       OR lower(COALESCE(ct.push_name, '')) LIKE '%' || ? || '%')
		ORDER BY c.last_msg_ts DESC, display_name ASC
		LIMIT ?
	`
	og := 0
	if onlyGroups {
		og = 1
	}
	rows, err := s.db.Query(sqlStr, og, q, q, q, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ChatRecord{}
	for rows.Next() {
		var r ChatRecord
		var ig int
		if err := rows.Scan(&r.JID, &r.Name, &ig, &r.LastMsgTS, &r.MessageCount); err != nil {
			return nil, err
		}
		r.IsGroup = ig == 1
		if r.LastMsgTS > 0 {
			r.LastMessageAt = time.Unix(r.LastMsgTS, 0).Local().Format("2006-01-02 15:04:05")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MsgStore) QueryContacts(query string, limit int) ([]ContactRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := strings.TrimSpace(strings.ToLower(query))
	rows, err := s.db.Query(`
		SELECT jid, alt_jid, push_name, full_name
		FROM contacts
		WHERE (? = '' OR lower(jid) LIKE '%' || ? || '%'
		       OR lower(alt_jid) LIKE '%' || ? || '%'
		       OR lower(push_name) LIKE '%' || ? || '%'
		       OR lower(full_name) LIKE '%' || ? || '%')
		ORDER BY (full_name != '') DESC, (push_name != '') DESC, updated_at DESC
		LIMIT ?
	`, q, q, q, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ContactRecord{}
	for rows.Next() {
		var r ContactRecord
		if err := rows.Scan(&r.JID, &r.AltJID, &r.PushName, &r.FullName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MsgStore) QueryMessages(chatJID, textQuery string, afterTS, beforeTS int64, limit int) ([]MessageRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	q := strings.TrimSpace(strings.ToLower(textQuery))
	rows, err := s.db.Query(`
		SELECT
			m.msg_id,
			m.chat_jid,
			COALESCE(NULLIF(ch.name, ''), m.chat_jid) AS chat_name,
			m.timestamp,
			m.sender_jid,
			COALESCE(NULLIF(m.sender_alt, ''), COALESCE(c1.alt_jid, '')) AS sender_alt,
			COALESCE(
				NULLIF(c1.full_name, ''),
				NULLIF(c1.push_name, ''),
				NULLIF(c2.full_name, ''),
				NULLIF(c2.push_name, ''),
				NULLIF(m.sender_name, ''),
				m.sender_jid
			) AS resolved_sender,
			m.is_from_me,
			m.msg_type,
			m.text,
			m.reply_to_id,
			m.reply_to_sender,
			m.has_media,
			m.mime_type,
			m.file_name
		FROM messages m
		LEFT JOIN chats ch ON ch.jid = m.chat_jid
		LEFT JOIN contacts c1 ON c1.jid = m.sender_jid
		LEFT JOIN contacts c2 ON c2.jid = COALESCE(NULLIF(m.sender_alt, ''), c1.alt_jid)
		WHERE (? = '' OR m.chat_jid = ?)
		  AND (? = 0 OR m.timestamp >= ?)
		  AND (? = 0 OR m.timestamp <= ?)
		  AND (? = ''
		       OR lower(m.text) LIKE '%' || ? || '%'
		       OR lower(m.sender_name) LIKE '%' || ? || '%'
		       OR lower(COALESCE(c1.full_name, '')) LIKE '%' || ? || '%'
		       OR lower(COALESCE(c1.push_name, '')) LIKE '%' || ? || '%'
		       OR lower(COALESCE(c2.full_name, '')) LIKE '%' || ? || '%'
		       OR lower(COALESCE(c2.push_name, '')) LIKE '%' || ? || '%')
		ORDER BY m.timestamp DESC
		LIMIT ?
	`, chatJID, chatJID, afterTS, afterTS, beforeTS, beforeTS, q, q, q, q, q, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rev := []MessageRecord{}
	for rows.Next() {
		var r MessageRecord
		var fm, hm int
		if err := rows.Scan(
			&r.ID, &r.ChatJID, &r.ChatName, &r.Timestamp,
			&r.SenderJID, &r.SenderAlt, &r.SenderName,
			&fm, &r.Type, &r.Text, &r.ReplyToID, &r.ReplyToSender,
			&hm, &r.MimeType, &r.FileName,
		); err != nil {
			return nil, err
		}
		r.IsFromMe = fm == 1
		r.HasMedia = hm == 1
		r.DateTime = time.Unix(r.Timestamp, 0).Local().Format("2006-01-02 15:04:05")
		rev = append(rev, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to chronological order (oldest -> newest) for natural reading
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, nil
}

func (s *MsgStore) GetMessageRaw(chatJID, msgID string) (foundChatJID, senderJID, mimeType, fileName string, rawProto []byte, err error) {
	if chatJID != "" {
		err = s.db.QueryRow(`
			SELECT chat_jid, sender_jid, mime_type, file_name, raw_proto
			FROM messages WHERE chat_jid = ? AND msg_id = ?
		`, chatJID, msgID).Scan(&foundChatJID, &senderJID, &mimeType, &fileName, &rawProto)
		return
	}
	err = s.db.QueryRow(`
		SELECT chat_jid, sender_jid, mime_type, file_name, raw_proto
		FROM messages WHERE msg_id = ? ORDER BY timestamp DESC LIMIT 1
	`, msgID).Scan(&foundChatJID, &senderJID, &mimeType, &fileName, &rawProto)
	return
}

func extractMessageFields(msg *waE2E.Message) (
	msgType, text, replyToID, replyToSender string,
	hasMedia bool, mimeType, fileName string,
	skip bool,
) {
	if msg == nil {
		return "", "", "", "", false, "", "", true
	}
	// Unwrap edited message wrapper if present
	if pm := msg.GetProtocolMessage(); pm != nil {
		if em := pm.GetEditedMessage(); em != nil {
			msg = em
		} else {
			return "", "", "", "", false, "", "", true
		}
	}

	readCtx := func(ci *waE2E.ContextInfo) {
		if ci != nil {
			replyToID = ci.GetStanzaID()
			replyToSender = ci.GetParticipant()
		}
	}

	switch {
	case msg.Conversation != nil:
		return "text", msg.GetConversation(), "", "", false, "", "", false

	case msg.ExtendedTextMessage != nil:
		et := msg.GetExtendedTextMessage()
		readCtx(et.GetContextInfo())
		return "text", et.GetText(), replyToID, replyToSender, false, "", "", false

	case msg.AudioMessage != nil:
		am := msg.GetAudioMessage()
		readCtx(am.GetContextInfo())
		label := fmt.Sprintf("[audio %ds]", am.GetSeconds())
		return "audio", label, replyToID, replyToSender, true, am.GetMimetype(), "", false

	case msg.ImageMessage != nil:
		im := msg.GetImageMessage()
		readCtx(im.GetContextInfo())
		txt := im.GetCaption()
		if txt == "" {
			txt = "[image]"
		} else {
			txt = "[image] " + txt
		}
		return "image", txt, replyToID, replyToSender, true, im.GetMimetype(), "", false

	case msg.VideoMessage != nil:
		vm := msg.GetVideoMessage()
		readCtx(vm.GetContextInfo())
		txt := vm.GetCaption()
		if txt == "" {
			txt = fmt.Sprintf("[video %ds]", vm.GetSeconds())
		} else {
			txt = "[video] " + txt
		}
		return "video", txt, replyToID, replyToSender, true, vm.GetMimetype(), "", false

	case msg.DocumentMessage != nil:
		dm := msg.GetDocumentMessage()
		readCtx(dm.GetContextInfo())
		fn := dm.GetFileName()
		if fn == "" {
			fn = dm.GetTitle()
		}
		txt := dm.GetCaption()
		if txt == "" {
			txt = "[document: " + fn + "]"
		} else {
			txt = "[document: " + fn + "] " + txt
		}
		return "document", txt, replyToID, replyToSender, true, dm.GetMimetype(), fn, false

	case msg.StickerMessage != nil:
		sm := msg.GetStickerMessage()
		readCtx(sm.GetContextInfo())
		return "sticker", "[sticker]", replyToID, replyToSender, true, sm.GetMimetype(), "", false

	case msg.ReactionMessage != nil:
		rm := msg.GetReactionMessage()
		targetID := rm.GetKey().GetID()
		emoji := rm.GetText()
		if emoji == "" {
			emoji = "(removed)"
		}
		return "reaction", fmt.Sprintf("[reaction %s -> %s]", emoji, targetID), targetID, rm.GetKey().GetParticipant(), false, "", "", false

	case msg.PollCreationMessage != nil:
		pm := msg.GetPollCreationMessage()
		readCtx(pm.GetContextInfo())
		return "poll", "[poll] " + pm.GetName(), replyToID, replyToSender, false, "", "", false

	case msg.PollCreationMessageV3 != nil:
		pm := msg.GetPollCreationMessageV3()
		readCtx(pm.GetContextInfo())
		return "poll", "[poll] " + pm.GetName(), replyToID, replyToSender, false, "", "", false

	case msg.ContactMessage != nil:
		cm := msg.GetContactMessage()
		readCtx(cm.GetContextInfo())
		return "contact", "[contact] " + cm.GetDisplayName(), replyToID, replyToSender, false, "", "", false

	case msg.LocationMessage != nil:
		lm := msg.GetLocationMessage()
		readCtx(lm.GetContextInfo())
		return "location", fmt.Sprintf("[location %.5f, %.5f %s]", lm.GetDegreesLatitude(), lm.GetDegreesLongitude(), lm.GetName()), replyToID, replyToSender, false, "", "", false
	}

	return "", "", "", "", false, "", "", true
}
