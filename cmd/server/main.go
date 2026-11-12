// Command server serves the SPA and game API from a single SQLite-owning process.
// A data-directory lock prevents two schedulers from writing the same rooms.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bhcn/umaguessinggame/internal/admission"
	"github.com/bhcn/umaguessinggame/internal/challenge"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi"
	"github.com/bhcn/umaguessinggame/internal/observability"
	"github.com/bhcn/umaguessinggame/internal/realtime"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
	"github.com/bhcn/umaguessinggame/internal/staticserv"
	"github.com/bhcn/umaguessinggame/internal/storage"
	"github.com/bhcn/umaguessinggame/internal/wiring"
)

// version is overridden at build time with -ldflags.
var version = "dev"

func main() {
	// Dispatch healthcheck and backup as one-shot commands without starting another server.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			if err := runHealthcheck(); err != nil {
				fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
				os.Exit(1)
			}
			return
		case "backup":
			if err := runBackup(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "backup: %v\n", err)
				os.Exit(1)
			}
			return
		case "version":
			fmt.Println(version)
			return
		case "help", "-h", "--help":
			fmt.Fprint(os.Stderr, usage)
			return
		default:
			// Anything starting with "-" is a flag for the default server mode, not a
			// subcommand. Treating it as an unknown subcommand made every ordinary
			// invocation fail: `uma-server -addr :8080` exited 2, and so did the
			// documented `docker run <image> -addr ...` form. Only a bare word that
			// is not a known subcommand is an error.
			if !strings.HasPrefix(os.Args[1], "-") {
				fmt.Fprintf(os.Stderr, "server: unknown subcommand %q\n\n%s", os.Args[1], usage)
				os.Exit(2)
			}
		}
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
}

const usage = `usage: uma-server [subcommand] [flags]

With no subcommand the HTTP server starts and flags are parsed from the command
line and the UMA_* environment variables.

Subcommands:
  healthcheck   probe a running instance and exit 0 when healthy
  backup        write a consistent online backup of the runtime database
  version       print the build version
  help          show this message
`

// runHealthcheck probes a running instance over HTTP without opening the database or acquiring its lock.
func runHealthcheck() error {
	addr := env("UMA_ADDR", "127.0.0.1:8080")
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	// A container healthcheck must not hang; the caller gives it 3 seconds.
	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/health/live", "/health/ready"} {
		resp, err := client.Get("http://" + host + path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}
	return nil
}

// runBackup writes a consistent backup of the runtime database.
//
// It uses the driver's VACUUM INTO, which is safe while the server is running.
// Copying the main file alone is not a backup: with WAL enabled the committed
// state can live entirely in the -wal sidecar .
func runBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dir := fs.String("dir", env("UMA_BACKUP_DIR", "./backups"), "backup destination directory")
	dataDir := fs.String("data", env("UMA_DATA_DIR", "./data/runtime"), "runtime data directory")
	label := fs.String("label", "", "optional label appended to the file name")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dbPath := filepath.Join(*dataDir, "runtime.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no runtime database at %s: %w", dbPath, err)
	}
	if err := os.MkdirAll(*dir, 0o750); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	// Open without migrations: a backup of an old schema must not silently upgrade
	// the source database.
	db, err := storage.Open(context.Background(), storage.Config{Dir: *dataDir})
	if err != nil {
		return fmt.Errorf("open runtime database: %w", err)
	}
	defer db.Close()

	stamp := time.Now().UTC().Format("20060102T150405Z")
	name := "runtime-" + stamp
	if *label != "" {
		name += "-" + *label
	}
	dest := filepath.Join(*dir, name+".db")
	if err := db.Backup(context.Background(), dest); err != nil {
		return err
	}
	info, err := os.Stat(dest)
	if err != nil {
		return fmt.Errorf("stat backup: %w", err)
	}
	// Verify the copy is readable and internally consistent before reporting
	// success; an unverified backup is not a backup.
	if err := verifyBackup(dest); err != nil {
		return fmt.Errorf("backup verification failed: %w", err)
	}
	fmt.Printf("backup written: %s (%d bytes, integrity ok)\n", dest, info.Size())
	return nil
}

// verifyBackup opens a backup file and runs PRAGMA integrity_check on it.
func verifyBackup(path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("integrity_check = %q", result)
	}
	return nil
}

// config is the fully resolved runtime configuration.
type config struct {
	policy         deployment.Policy
	addr           string
	dataDir        string
	contentDir     string
	webDir         string
	logLevel       string
	deployment     string
	secureCookies  bool
	trustProxy     bool
	trustedProxies []netip.Prefix
	allowedOrigin  []string
	metricsLocal   bool
	allowNoOrigin  bool
	sessionTTL     time.Duration
	displayTTL     time.Duration
	busyTimeout    time.Duration
	synchronous    string
	readConns      int
	queueCapacity  int
	shutdownGrace  time.Duration
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.logLevel)
	metrics := observability.NewMetrics()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The data directory is created before the lock file so a first run works.
	if err := os.MkdirAll(cfg.dataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	// Single-instance guard: a second process on the same directory must fail
	// rather than race the first one .
	release, err := acquireLock(filepath.Join(cfg.dataDir, "server.lock"))
	if err != nil {
		return err
	}
	defer release()

	db, err := storage.Open(ctx, storage.Config{
		Dir:         cfg.dataDir,
		ReadConns:   cfg.readConns,
		BusyTimeout: cfg.busyTimeout,
		Synchronous: cfg.synchronous,
	})
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer db.Close()

	sqliteVersion, err := db.SQLiteVersion(ctx)
	if err != nil {
		return fmt.Errorf("read sqlite version: %w", err)
	}
	log.Info("storage ready",
		"path", db.Path(),
		"sqliteVersion", sqliteVersion,
		"readConns", cfg.readConns,
		"busyTimeoutMs", cfg.busyTimeout.Milliseconds(),
		"synchronous", cfg.synchronous,
	)

	sessions := session.New(db, session.Config{
		TTL:                cfg.sessionTTL,
		DisplayTTL:         cfg.displayTTL,
		Secure:             cfg.secureCookies,
		SameSite:           http.SameSiteLaxMode,
		AllowedOrigins:     cfg.allowedOrigin,
		AllowMissingOrigin: cfg.allowNoOrigin,
	})

	// Register every implemented game mode. A mode whose content section did not
	// compile still registers; it reports itself unavailable at round creation
	// instead of disappearing from the lobby without explanation .
	modeSet, err := wiring.BuildModes()
	if err != nil {
		return fmt.Errorf("register game modes: %w", err)
	}
	log.Info("game modes registered", "modes", modeSet.Built)

	// Load the published content pack. A missing or corrupt pack is not fatal for
	// the API process: readiness reports 503 with the reason and content-dependent
	// operations fail closed, which is honest instead of pretending the service is
	// complete .
	//
	// The per-endpoint codes differ and the log line must not overstate them:
	// readiness answers 503, starting a round answers 422 round-preparation-failed,
	// and practice packs answer 501. Claiming every content operation answers 503
	// sends whoever reads the log looking for the wrong status code.
	var contentIndex *wiring.ContentIndex
	contentIndex, err = wiring.OpenContent(ctx, cfg.contentDir)
	if err != nil {
		log.Warn("content pack unavailable; readiness answers 503 and content-dependent operations fail closed",
			"contentDir", cfg.contentDir, "error", err.Error())
		contentIndex = nil
	} else {
		log.Info("content pack loaded",
			"version", contentIndex.Version(),
			"datasetVersion", contentIndex.DatasetVersion(),
			"assetBase", contentIndex.AssetBaseURL(),
		)
	}

	rooms := room.New(db, room.Config{
		QueueCapacity:  cfg.queueCapacity,
		QueuedCommands: cfg.policy.Limits.QueuedCommands,
		Policy:         cfg.policy,
		IdempotencyTTL: 24 * time.Hour,
	}, room.Options{
		Modes:   modeSet.Registry,
		Content: contentIndexAdapter(contentIndex),
		Epoch:   newEpoch(),
	})
	defer rooms.Close()
	if count, err := rooms.PauseInterruptedEvents(ctx); err != nil {
		return fmt.Errorf("pause interrupted activities: %w", err)
	} else if count > 0 {
		log.Info("interrupted activity rounds paused for host confirmation", "rounds", count)
	}

	// The deadline sweeper is what makes a stored deadline actually do something.
	// Without it a round whose players stop submitting stays in collecting forever and
	// the "unsubmitted scores zero" rule never runs. It is tied to the server context so
	// it stops with the process rather than leaking a goroutine per restart.
	roomsCtx, stopSweeper := context.WithCancel(context.Background())
	defer stopSweeper()
	go rooms.RunDeadlineSweeper(roomsCtx)
	go rooms.RunReceiptSweeper(roomsCtx, func(err error) {
		log.Warn("expired receipt cleanup failed; retrying next interval", "error", err.Error())
	})

	health := &observability.Health{BuildVersion: version}
	health.Ready = []observability.Probe{
		{Name: "database", Check: func(ctx context.Context) error {
			return db.Ping(ctx)
		}},
		{Name: "migrations", Check: func(ctx context.Context) error {
			// Migrate is idempotent and verifies checksums, so running it as the
			// readiness probe proves the schema is current and unedited.
			return db.Migrate(ctx)
		}},
		{Name: "content", Check: func(ctx context.Context) error {
			if contentIndex == nil {
				// Readiness must fail loudly rather than report ready while every
				// mode is unusable. The reason stays in the log; the health body
				// stays generic .
				return errors.New("no content pack loaded")
			}
			return nil
		}},
	}

	// The push hub and its outbox dispatcher.
	//
	// Without these the stream endpoints fall back to polling: a query per
	// connection every 150ms, which also delays every notification — including a
	// revocation — by up to one tick. The dispatcher is what turns committed
	// outbox rows into frames.
	streamConfig := realtime.Config{MaxConnections: cfg.policy.Limits.Connections, MaxConnectionsPerRoom: cfg.policy.Limits.ConnectionsPerRoom, MaxConnectionsPerSession: cfg.policy.Limits.ConnectionsPerSession}
	hub, hubErr := realtime.NewHub(streamConfig)
	if hubErr != nil {
		return fmt.Errorf("build realtime hub: %w", hubErr)
	}
	dispatcher, dispErr := realtime.NewDispatcher(db, hub, streamConfig)
	if dispErr != nil {
		hub.Close()
		return fmt.Errorf("build outbox dispatcher: %w", dispErr)
	}
	dispatcher.Start()
	// The dispatcher must stop before the database closes, or a poll in flight
	// reads a closed pool and logs a spurious failure during shutdown.
	defer dispatcher.Stop()

	challengeKeys, err := challenge.LoadKeys(filepath.Join(cfg.dataDir, "verified-keys.json"))
	if err != nil {
		return fmt.Errorf("load challenge keys: %w", err)
	}
	classicAdmitted := false
	if contentIndex != nil {
		admission, ok := contentIndex.ModeAdmission("classic")
		classicAdmitted = ok && admission.Available
	}
	challenges := challenge.New(db, contentIndexAdapter(contentIndex), challengeKeys, cfg.policy, classicAdmitted)
	entryCfg := cfg.policy.Turnstile.Resolved()
	entry, err := admission.New(entryCfg, os.Getenv(entryCfg.SecretEnv), cfg.secureCookies)
	if err != nil {
		return fmt.Errorf("entry validation config: %w", err)
	}
	srv := httpapi.New(httpapi.Deps{
		Admission:  entry,
		Challenges: challenges,
		// Cookie/origin policy is configured on the session service above, and the
		// receipt TTL and queue bound on the room service. Those are the only
		// components that act on them, so they are not repeated here: a copy on
		// httpapi.Config would be settable and inert.
		Config: httpapi.Config{
			Limiter: httpapi.LimiterConfig{
				MaxBuckets: cfg.policy.Limits.RateLimitBuckets,
				SessionCreatePerIP: httpapi.Rate{PerMinute: cfg.policy.Limits.SessionCreatePerIPMinute,
					BurstTokens: cfg.policy.Limits.SessionCreatePerIPBurst},
			},
			APIVersion:        "1",
			RealtimeConfig:    streamConfig,
			BuildVersion:      version,
			SiteName:          "UmaGuessingGame",
			DeploymentMode:    cfg.deployment,
			MaxRoomMembers:    cfg.policy.Limits.PlayersPerRoom,
			MaxRoomSpectators: cfg.policy.Limits.SpectatorsPerRoom,
			DefaultTransport:  "auto",
			TrustProxy:        cfg.trustProxy,
			TrustedProxies:    cfg.trustedProxies,
			MetricsLocalOnly:  cfg.metricsLocal,
			RequestTimeout:    20 * time.Second,
		},
		Logger:   log,
		Metrics:  metrics,
		Health:   health,
		Sessions: sessions,
		Rooms:    rooms,
		Realtime: hub,
		Ready: func(ctx context.Context) (bool, []string) {
			st := health.Check(ctx)
			return st.Ready, st.Failed
		},
	})

	// Static serving wraps the API handler: the SPA shell and content media are
	// served from disk, and every /api/ and health path is delegated untouched so
	// a missing endpoint can never be answered with HTML .
	rootHandler := staticserv.Handler(staticserv.Config{
		SPADir:     cfg.webDir,
		ContentDir: cfg.contentDir,
		// A memory board publishes an opaque token per card, not the stored file name,
		// so that two cards of a pair never share a URL . This resolver is the
		// only place that knows which file a token points at; without it every face
		// request 404'd and a flipped card showed a broken image.
		FaceAlias: staticserv.NewFaceAlias(contentMediaDir(cfg.contentDir)),
	}, srv.Handler())

	httpServer := &http.Server{
		Addr:              cfg.addr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		// A streaming response is long-lived, so WriteTimeout must be zero; the
		// per-request context timeout bounds handler work instead.
		WriteTimeout:   0,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 16 << 10,
		BaseContext:    func(net.Listener) context.Context { return context.Background() },
	}

	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.addr, err)
	}
	log.Info("http server listening", "addr", ln.Addr().String(), "buildVersion", version)

	errCh := make(chan error, 1)
	go func() {
		if serveErr := httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown requested")
	case serveErr := <-errCh:
		if serveErr != nil {
			return fmt.Errorf("serve: %w", serveErr)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err.Error())
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("server stopped")
	return nil
}

// loadConfig resolves configuration from flags and environment.
//
// Every value is validated at startup; a bad value fails the boot instead of
// surfacing later as a runtime surprise .
func loadConfig() (config, error) {
	var cfg config
	path := deploymentConfigPath(os.Args[1:])
	if path != "" {
		loaded, err := deployment.Load(path)
		if err != nil {
			return cfg, fmt.Errorf("deployment config: %w", err)
		}
		cfg.policy = loaded
	}
	cfg.policy.Limits = cfg.policy.ResolvedLimits()
	deploymentDefault := cfg.policy.DeploymentMode
	if deploymentDefault == "" {
		deploymentDefault = "lan"
	}
	flag.String("config", path, "deployment policy JSON path (UMA_CONFIG)")
	flag.StringVar(&cfg.addr, "addr", env("UMA_ADDR", "127.0.0.1:8080"), "listen address")
	flag.StringVar(&cfg.dataDir, "data", env("UMA_DATA_DIR", "./data/runtime"), "runtime data directory")
	flag.StringVar(&cfg.contentDir, "content", env("UMA_CONTENT_DIR", "./content"), "compiled content directory")
	flag.StringVar(&cfg.webDir, "web", env("UMA_WEB_DIR", "./web/dist"), "built SPA directory (served at /)")
	flag.StringVar(&cfg.logLevel, "log-level", env("UMA_LOG_LEVEL", "info"), "log level: debug|info|warn|error")
	flag.StringVar(&cfg.deployment, "deployment", env("UMA_DEPLOYMENT", deploymentDefault), "deployment mode: internet|lan")
	flag.BoolVar(&cfg.secureCookies, "secure-cookies", envBool("UMA_SECURE_COOKIES", false), "mark cookies Secure (required on public HTTPS)")
	proxyDefault, err := trustProxyFromEnv(cfg.policy.Network.TrustProxy)
	if err != nil {
		return cfg, err
	}
	proxyCIDRsDefault := trustedProxyCIDRsFromEnv(cfg.policy.Network.TrustedProxyCIDRs)
	flag.BoolVar(&cfg.trustProxy, "trust-proxy", proxyDefault, "honour forwarding headers only from explicitly trusted proxy CIDRs")
	proxyCIDRs := flag.String("trusted-proxy-cidrs", proxyCIDRsDefault, "comma-separated trusted reverse proxy CIDRs; required with trust-proxy")
	flag.BoolVar(&cfg.metricsLocal, "metrics-local-only", envBool("UMA_METRICS_LOCAL_ONLY", true), "restrict /metrics to loopback")
	flag.BoolVar(&cfg.allowNoOrigin, "allow-missing-origin", envBool("UMA_ALLOW_MISSING_ORIGIN", false), "allow unsafe requests without Origin (private LAN only)")
	origins := flag.String("allowed-origins", env("UMA_ALLOWED_ORIGINS", ""), "comma-separated same-origin allow list")
	flag.DurationVar(&cfg.sessionTTL, "session-ttl", envDuration("UMA_SESSION_TTL", 7*24*time.Hour), "session lifetime")
	flag.DurationVar(&cfg.displayTTL, "display-ttl", envDuration("UMA_DISPLAY_TTL", 12*time.Hour), "display session lifetime")
	flag.DurationVar(&cfg.busyTimeout, "busy-timeout", envDuration("UMA_BUSY_TIMEOUT", time.Second), "SQLite busy_timeout")
	flag.StringVar(&cfg.synchronous, "synchronous", env("UMA_SYNCHRONOUS", "FULL"), "SQLite synchronous mode")
	// The default comes from storage so this value has exactly one definition. It used
	// to be hardcoded as 4 here while storage said 8, and the flag default won — so the
	// baseline was never actually raised on a real deployment.
	flag.IntVar(&cfg.readConns, "read-conns", envInt("UMA_READ_CONNS", storage.DefaultReadConns), "read pool size")
	flag.IntVar(&cfg.queueCapacity, "queue-capacity", envInt("UMA_QUEUE_CAPACITY", 128), "per-room command queue bound")
	flag.DurationVar(&cfg.shutdownGrace, "shutdown-grace", envDuration("UMA_SHUTDOWN_GRACE", 15*time.Second), "graceful shutdown budget")
	flag.Parse()
	var proxyValues []string
	if strings.TrimSpace(*proxyCIDRs) != "" {
		proxyValues = strings.Split(*proxyCIDRs, ",")
	}
	parsedProxies, err := deployment.ParseTrustedProxyCIDRs(proxyValues)
	if err != nil {
		return cfg, err
	}
	cfg.trustedProxies = parsedProxies
	if cfg.trustProxy && len(cfg.trustedProxies) == 0 {
		return cfg, errors.New("trust-proxy requires explicit trusted-proxy-cidrs (UMA_TRUSTED_PROXY_CIDRS)")
	}

	if cfg.deployment != "internet" && cfg.deployment != "lan" {
		return cfg, fmt.Errorf("deployment must be internet or lan, got %q", cfg.deployment)
	}
	// On a public deployment a non-Secure cookie would travel in clear text; that
	// combination is refused rather than silently weakened .
	if cfg.deployment == "internet" && !cfg.secureCookies {
		return cfg, errors.New("deployment=internet requires -secure-cookies=true")
	}
	switch strings.ToUpper(cfg.synchronous) {
	case "FULL", "NORMAL":
	default:
		return cfg, fmt.Errorf("synchronous must be FULL or NORMAL, got %q", cfg.synchronous)
	}
	cfg.synchronous = strings.ToUpper(cfg.synchronous)
	if cfg.readConns < 1 || cfg.readConns > 32 {
		return cfg, fmt.Errorf("read-conns must be 1..32, got %d", cfg.readConns)
	}
	if cfg.queueCapacity < 1 || cfg.queueCapacity > 4096 {
		return cfg, fmt.Errorf("queue-capacity must be 1..4096, got %d", cfg.queueCapacity)
	}
	if cfg.busyTimeout <= 0 || cfg.busyTimeout > 30*time.Second {
		return cfg, fmt.Errorf("busy-timeout must be 1ms..30s, got %s", cfg.busyTimeout)
	}
	// The busy budget must stay below the HTTP request timeout so a busy database
	// reports its own error instead of being cut off by the HTTP layer .
	if cfg.busyTimeout >= 20*time.Second {
		return cfg, errors.New("busy-timeout must be below the 20s request timeout")
	}
	if *origins != "" {
		for _, o := range strings.Split(*origins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.allowedOrigin = append(cfg.allowedOrigin, o)
			}
		}
	}
	return cfg, nil
}

// acquireLock takes an exclusive advisory lock on the data directory.
// The kernel releases the lock on process exit. Keep the lock file in place to prevent concurrent processes from locking different inodes.
func acquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			// Read the holder's pid for the message; a stale file with no live
			// holder can still be locked, so reaching here means a real process.
			holder, _ := os.ReadFile(path)
			return nil, fmt.Errorf(
				"data directory is already in use by another process (lock file %s, holder pid %s)",
				path, strings.TrimSpace(string(holder)))
		}
		return nil, fmt.Errorf("lock data directory: %w", err)
	}
	// Record the pid for diagnostics only; correctness comes from flock.
	if err := f.Truncate(0); err == nil {
		if _, err := f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
			// A failed pid write must not fail startup: the lock is already held.
			_ = err
		}
	}
	return func() {
		// Closing the descriptor releases the flock. The file stays on disk.
		_ = f.Close()
	}, nil
}

// newEpoch returns a per-process identifier used for realtime resync detection.
func newEpoch() string {
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return d
}

// ensure slog is referenced so the import is not dropped if logging changes.
var _ = slog.LevelInfo

// contentIndexAdapter converts a nil *wiring.ContentIndex into a nil room
// ContentIndex interface.
//
// This exists because Go's typed-nil trap would otherwise hand the room service a
// non-nil interface wrapping a nil pointer, and its "no content" branch would
// never trigger.
func contentIndexAdapter(idx *wiring.ContentIndex) room.ContentIndex {
	if idx == nil {
		return nil
	}
	return idx
}

// contentMediaDir returns the public asset directory of the active content pack.
//
// The pack layout is <contentDir>/pack-<hash>/public, and that public tree is the
// media root the static server serves from.
func contentMediaDir(contentDir string) string {
	if contentDir == "" {
		return ""
	}
	cur, err := os.ReadFile(filepath.Join(contentDir, "current"))
	if err != nil {
		return ""
	}
	pack := strings.TrimSpace(string(cur))
	if pack == "" {
		return ""
	}
	return filepath.Join(contentDir, pack, "public")
}
