package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tgdrive/internal/cache"
	"tgdrive/internal/service"
)

type config struct {
	token     string
	hash      string
	apiID     int
	chatID    int64
	key       string
	listen    string
	httpToken string
	index     string
	session   string
	maxCache  int64
	cacheTTL  time.Duration
	debug     bool
}

func main() {
	cfg, err := parseConfig()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	instance, err := service.Start(ctx, service.Config{
		Token:      cfg.token,
		Hash:       cfg.hash,
		APIID:      cfg.apiID,
		ChatID:     cfg.chatID,
		Key:        cfg.key,
		Listen:     cfg.listen,
		HTTPToken:  cfg.httpToken,
		Index:      cfg.index,
		LocalIndex: true,
		Session:    cfg.session,
		MaxCache:   cfg.maxCache,
		CacheTTL:   cfg.cacheTTL,
		Debug:      cfg.debug,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("HTTP drive API listening on %s", cfg.listen)
	if err := instance.Wait(); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

func parseConfig() (config, error) {
	var cfg config
	defaultMaxCache := envOrDefault("TGDRIVE_MAX_CACHE", "500M")
	defaultCacheTTL := envOrDefault("TGDRIVE_CACHE_TTL", "2h")

	flag.StringVar(&cfg.token, "token", os.Getenv("TGDRIVE_BOT_TOKEN"), "Telegram bot token")
	flag.StringVar(&cfg.hash, "hash", os.Getenv("TGDRIVE_API_HASH"), "Telegram API hash")
	flag.StringVar(&cfg.key, "key", os.Getenv("TGDRIVE_ENCRYPTION_KEY"), "encryption key for files and index backups")
	flag.IntVar(&cfg.apiID, "id", envInt("TGDRIVE_API_ID"), "Telegram API ID")
	flag.IntVar(&cfg.apiID, "api-id", envInt("TGDRIVE_API_ID"), "Telegram API ID")
	flag.Int64Var(&cfg.chatID, "chatid", envInt64("TGDRIVE_CHAT_ID"), "Telegram storage chat ID")
	flag.StringVar(&cfg.listen, "listen", envOrDefault("TGDRIVE_LISTEN", ":8080"), "HTTP listen address")
	flag.StringVar(&cfg.httpToken, "http-token", os.Getenv("TGDRIVE_HTTP_TOKEN"), "HTTP bearer token; empty disables API authentication")
	flag.StringVar(&cfg.index, "index", envOrDefault("TGDRIVE_INDEX", "tgdrive-index.json"), "local metadata index")
	flag.StringVar(&cfg.session, "session", envOrDefault("TGDRIVE_SESSION", "tgdrive.session"), "MTProto session file")
	flag.StringVar(&defaultMaxCache, "max-cache", defaultMaxCache, "runtime cache limit using M or G")
	flag.StringVar(&defaultCacheTTL, "cache-ttl", defaultCacheTTL, "runtime cache TTL using Go duration")
	flag.BoolVar(&cfg.debug, "debug", false, "log HTTP, cache, and Telegram transfer diagnostics")
	flag.Parse()

	if cfg.token == "" || cfg.hash == "" || cfg.key == "" || cfg.apiID <= 0 || cfg.chatID == 0 {
		return cfg, fmt.Errorf("required config: --token, --hash, --key, --id, and --chatid (or TGDRIVE_* environment variables)")
	}
	var err error
	cfg.maxCache, err = cache.ParseMaxSize(defaultMaxCache)
	if err != nil {
		return cfg, fmt.Errorf("invalid --max-cache: %w", err)
	}
	cfg.cacheTTL, err = parseCacheTTL(defaultCacheTTL)
	if err != nil {
		return cfg, fmt.Errorf("invalid --cache-ttl")
	}
	if cfg.httpToken == "" {
		log.Printf("warning: HTTP API authentication is disabled")
	}
	return cfg, nil
}

func parseCacheTTL(value string) (time.Duration, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "0" || value == "never" || value == "永久" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("cache TTL must be a non-negative duration or never")
	}
	return parsed, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envInt(name string) int {
	var value int
	_, _ = fmt.Sscanf(os.Getenv(name), "%d", &value)
	return value
}

func envInt64(name string) int64 {
	var value int64
	_, _ = fmt.Sscanf(os.Getenv(name), "%d", &value)
	return value
}
