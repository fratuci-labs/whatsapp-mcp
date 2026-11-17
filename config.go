package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/store"
	_ "modernc.org/sqlite"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "1.0.1"

// Config holds runtime settings loaded from standard MCP environment variables.
type Config struct {
	BaseDir        string
	DataDir        string
	MediaDir       string
	ReadOnly       bool
	LogLevel       string
	RequestTimeout time.Duration
	DeviceName     string
}

func loadConfig() Config {
	baseDir := resolveBaseDir()

	dataDir := strings.TrimSpace(os.Getenv("WHATSAPP_MCP_DATA_DIR"))
	if dataDir == "" {
		dataDir = filepath.Join(baseDir, "data")
	}

	mediaDir := strings.TrimSpace(os.Getenv("WHATSAPP_MCP_MEDIA_DIR"))
	if mediaDir == "" {
		mediaDir = filepath.Join(baseDir, "media")
	}

	readOnly := parseBoolEnv("WHATSAPP_MCP_READ_ONLY", false)

	logLevel := strings.ToUpper(strings.TrimSpace(os.Getenv("WHATSAPP_MCP_LOG_LEVEL")))
	switch logLevel {
	case "DEBUG", "INFO", "WARN", "ERROR":
	default:
		logLevel = "INFO"
	}

	timeoutSec := 45
	if raw := strings.TrimSpace(os.Getenv("WHATSAPP_MCP_REQUEST_TIMEOUT")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 5 && v <= 300 {
			timeoutSec = v
		}
	}

	deviceName := strings.TrimSpace(os.Getenv("WHATSAPP_MCP_DEVICE_NAME"))

	return Config{
		BaseDir:        baseDir,
		DataDir:        dataDir,
		MediaDir:       mediaDir,
		ReadOnly:       readOnly,
		LogLevel:       logLevel,
		RequestTimeout: time.Duration(timeoutSec) * time.Second,
		DeviceName:     deviceName,
	}
}

func parseBoolEnv(key string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func resolveBaseDir() string {
	if env := strings.TrimSpace(os.Getenv("WHATSAPP_MCP_HOME")); env != "" {
		return env
	}

	// Preserve backwards compatibility if an existing session.db sits beside the executable.
	if exe, err := os.Executable(); err == nil {
		if realExe, err := filepath.EvalSymlinks(exe); err == nil {
			exe = realExe
		}
		exeDir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(exeDir, "data", "session.db")); err == nil {
			return exeDir
		}
	}

	return defaultUserDataDir()
}

func defaultUserDataDir() string {
	switch runtime.GOOS {
	case "windows":
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "whatsapp-mcp")
		}
		if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
			return filepath.Join(cfgDir, "whatsapp-mcp")
		}
	case "darwin":
		if cfgDir, err := os.UserConfigDir(); err == nil && cfgDir != "" {
			return filepath.Join(cfgDir, "whatsapp-mcp")
		}
	default:
		if xdgData := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdgData != "" {
			return filepath.Join(xdgData, "whatsapp-mcp")
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".local", "share", "whatsapp-mcp")
		}
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".whatsapp-mcp")
	}
	return filepath.Join(os.TempDir(), "whatsapp-mcp")
}

// openSQLiteDB opens a pure-Go SQLite database (modernc.org/sqlite) with WAL,
// foreign_keys, and busy_timeout enabled across platforms (including Windows paths).
func openSQLiteDB(dbPath string) (*sql.DB, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		absPath = dbPath
	}
	slashPath := filepath.ToSlash(absPath)
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)",
		slashPath,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func configureOSInfo(customName string) {
	if customName != "" {
		store.SetOSInfo(customName, [3]uint32{1, 0, 0})
		return
	}
	switch runtime.GOOS {
	case "darwin":
		store.SetOSInfo("Mac OS", [3]uint32{15, 0, 0})
	case "windows":
		store.SetOSInfo("Windows", [3]uint32{11, 0, 0})
	default:
		store.SetOSInfo("Linux", [3]uint32{6, 0, 0})
	}
}

func pairClientDisplayName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Chrome (Mac OS)"
	case "windows":
		return "Chrome (Windows)"
	default:
		return "Chrome (Linux)"
	}
}
