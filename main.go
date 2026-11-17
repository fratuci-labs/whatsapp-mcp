package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	qrcode "github.com/skip2/go-qrcode"
)

// --- MCP Tool Schemas ---

type StatusToolInput struct {
	Mode  string `json:"mode,omitempty" jsonschema:"Mode: 'status' (default) checks connection; 'qr' generates a QR code (returned inline as PNG image and saved locally); 'pair_phone' generates an 8-character pairing code for a phone number."`
	Phone string `json:"phone,omitempty" jsonschema:"Phone number with country code (digits only, e.g. '5511999999999'), required only when mode is 'pair_phone'."`
}

type ListChatsInput struct {
	Query      string `json:"query,omitempty" jsonschema:"Optional substring to filter chats or groups by name or JID (e.g. 'Engineering')."`
	OnlyGroups bool   `json:"only_groups,omitempty" jsonschema:"If true, return only group chats (@g.us)."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Max number of chats to return (default 50, max 200)."`
}

type ReadMessagesInput struct {
	ChatJID  string `json:"chat_jid" jsonschema:"Target chat or group JID (e.g. '120363000000000000@g.us' or '5511999999999@s.whatsapp.net')."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Number of most recent messages to return in chronological order (default 30, max 200)."`
	AfterISO string `json:"after_iso,omitempty" jsonschema:"Optional local timestamp filter 'YYYY-MM-DD HH:MM:SS' or RFC3339 to read messages at/after this time."`
}

type SearchMessagesInput struct {
	Query   string `json:"query" jsonschema:"Substring to search in message text or sender name."`
	ChatJID string `json:"chat_jid,omitempty" jsonschema:"Optional chat/group JID to restrict the search to a single conversation."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Max matching messages to return (default 30, max 200)."`
}

type FindContactsInput struct {
	Query    string `json:"query,omitempty" jsonschema:"Optional name, phone, or LID substring to filter contacts/participants."`
	GroupJID string `json:"group_jid,omitempty" jsonschema:"Optional group JID (e.g. '120363000000000000@g.us') to fetch live group participants with their exact JID/@lid for mentions."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max contacts to return when not querying a specific group (default 50)."`
}

type DownloadMediaInput struct {
	MsgID   string `json:"msg_id" jsonschema:"ID of the message containing audio, image, video, document, or sticker."`
	ChatJID string `json:"chat_jid,omitempty" jsonschema:"Optional chat/group JID where the message was posted."`
}

type SendMessageInput struct {
	ChatJID   string   `json:"chat_jid" jsonschema:"Recipient chat or group JID (e.g. '120363000000000000@g.us'). Only send when explicitly authorized by the user."`
	Text      string   `json:"text" jsonschema:"Message text to send."`
	ReplyToID string   `json:"reply_to_id,omitempty" jsonschema:"Optional message ID to quote/reply to natively."`
	Mentions  []string `json:"mentions,omitempty" jsonschema:"Optional list of full participant JIDs or LIDs to mention (e.g. ['100000000000001@lid']). Clickable @tags are formatted automatically."`
}

func parseFlexibleTime(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Unix()
		}
	}
	return 0
}

func textResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil, nil
}

func authResultWithQR(out AuthResponse) (*mcp.CallToolResult, any, error) {
	// Hide raw QR payload from MCP JSON output to keep context concise; embed image block instead.
	pngPath := out.QRPngPath
	out.QRCode = ""
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	contents := []mcp.Content{&mcp.TextContent{Text: string(b)}}
	if pngPath != "" {
		if pngBytes, err := os.ReadFile(pngPath); err == nil && len(pngBytes) > 0 {
			contents = append(contents, &mcp.ImageContent{
				Data:     pngBytes,
				MIMEType: "image/png",
			})
		}
	}
	return &mcp.CallToolResult{Content: contents}, nil, nil
}

func renderTerminalQR(code string) string {
	qr, err := qrcode.New(code, qrcode.Medium)
	if err != nil {
		return ""
	}
	return qr.ToSmallString(false)
}

func runMCPServer(cfg Config) error {
	dc := newDaemonClient(cfg)

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "whatsapp",
		Version: Version,
	}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wa_status",
		Description: "Check WhatsApp connection status and local database stats, or pair a device via QR code ('qr') or phone pairing code ('pair_phone').",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in StatusToolInput) (*mcp.CallToolResult, any, error) {
		var out AuthResponse
		if err := dc.call(ctx, http.MethodPost, "/auth", AuthRequest{Mode: in.Mode, Phone: in.Phone}, &out); err != nil {
			return nil, nil, err
		}
		return authResultWithQR(out)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wa_list_chats",
		Description: "List or search synced WhatsApp chats and groups from local SQLite (instant, no timeout).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListChatsInput) (*mcp.CallToolResult, any, error) {
		var out map[string]any
		if err := dc.call(ctx, http.MethodPost, "/chats", ChatsRequest{
			Query:      in.Query,
			OnlyGroups: in.OnlyGroups,
			Limit:      in.Limit,
		}, &out); err != nil {
			return nil, nil, err
		}
		return textResult(out)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wa_read_messages",
		Description: "Read recent messages from a WhatsApp chat or group in chronological order, with sender names resolved automatically (including @lid groups).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ReadMessagesInput) (*mcp.CallToolResult, any, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 30
		}
		var out map[string]any
		if err := dc.call(ctx, http.MethodPost, "/messages", MessagesRequest{
			ChatJID: in.ChatJID,
			AfterTS: parseFlexibleTime(in.AfterISO),
			Limit:   limit,
		}, &out); err != nil {
			return nil, nil, err
		}
		return textResult(out)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wa_search_messages",
		Description: "Search WhatsApp messages by text or sender name across all chats or within a specific chat/group.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchMessagesInput) (*mcp.CallToolResult, any, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 30
		}
		var out map[string]any
		if err := dc.call(ctx, http.MethodPost, "/messages", MessagesRequest{
			ChatJID: in.ChatJID,
			Query:   in.Query,
			Limit:   limit,
		}, &out); err != nil {
			return nil, nil, err
		}
		return textResult(out)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wa_find_contacts",
		Description: "Search WhatsApp contacts by name/JID/LID, or list all participants of a group (pass group_jid) with their exact JID/@lid for clickable mentions.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in FindContactsInput) (*mcp.CallToolResult, any, error) {
		var out map[string]any
		if err := dc.call(ctx, http.MethodPost, "/contacts", ContactsRequest{
			Query:    in.Query,
			GroupJID: in.GroupJID,
			Limit:    in.Limit,
		}, &out); err != nil {
			return nil, nil, err
		}
		return textResult(out)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wa_download_media",
		Description: "Decrypt and download media (voice audio .ogg, image, PDF/document, video) from a WhatsApp message to a local file path.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DownloadMediaInput) (*mcp.CallToolResult, any, error) {
		var out DownloadResponse
		if err := dc.call(ctx, http.MethodPost, "/download", DownloadRequest{
			ChatJID: in.ChatJID,
			MsgID:   in.MsgID,
		}, &out); err != nil {
			return nil, nil, err
		}
		return textResult(out)
	})

	if !cfg.ReadOnly {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        "wa_send_message",
			Description: "Send a WhatsApp message to a contact or group, with optional native quote (reply_to_id) and clickable @lid/JID mentions. NEVER call without explicit user confirmation of recipient and content.",
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in SendMessageInput) (*mcp.CallToolResult, any, error) {
			var out SendResponse
			if err := dc.call(ctx, http.MethodPost, "/send", SendRequest{
				ChatJID:   in.ChatJID,
				Text:      in.Text,
				ReplyToID: in.ReplyToID,
				Mentions:  in.Mentions,
			}, &out); err != nil {
				return nil, nil, err
			}
			return textResult(out)
		})
	}

	return srv.Run(context.Background(), &mcp.StdioTransport{})
}

func main() {
	cfg := loadConfig()
	if len(os.Args) > 1 {
		dc := newDaemonClient(cfg)
		ctx := context.Background()
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("whatsapp-mcp %s\n", Version)
			return
		case "daemon":
			if err := runDaemon(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
				os.Exit(1)
			}
			return
		case "status":
			var out AuthResponse
			if err := dc.call(ctx, http.MethodPost, "/auth", AuthRequest{Mode: "status"}, &out); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			out.QRCode = ""
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return
		case "qr":
			var out AuthResponse
			if err := dc.call(ctx, http.MethodPost, "/auth", AuthRequest{Mode: "qr"}, &out); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if out.QRCode != "" {
				if ascii := renderTerminalQR(out.QRCode); ascii != "" {
					fmt.Fprintln(os.Stderr, ascii)
				}
			}
			out.QRCode = ""
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return
		case "pair":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: whatsapp-mcp pair <phone_with_country_code>")
				os.Exit(1)
			}
			var out AuthResponse
			if err := dc.call(ctx, http.MethodPost, "/auth", AuthRequest{Mode: "pair_phone", Phone: os.Args[2]}, &out); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return
		case "stop":
			stopped, err := dc.stopIfRunning(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error stopping daemon: %v\n", err)
				os.Exit(1)
			}
			if stopped {
				fmt.Println("daemon stopping")
			} else {
				fmt.Println("daemon not running")
			}
			return
		}
	}

	if err := runMCPServer(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "mcp server error: %v\n", err)
		os.Exit(1)
	}
}
