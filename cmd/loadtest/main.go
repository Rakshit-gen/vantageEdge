// Command loadtest drives the real gateway request pipeline (tenant
// resolution -> route match -> auth -> rate limit -> cache -> proxy) end to
// end against real Postgres + Redis and reports throughput, the gateway's
// added latency over a direct origin call, and config-propagation time.
//
// It wires the gateway in-process exactly as cmd/gateway/main.go does (real
// net/http server on a loopback port, real gRPC ConfigService with push
// invalidation, RedisLimiter, Redis response cache), so every hop on the
// hot path is real kernel TCP. Only process isolation is skipped.
//
// Usage:
//
//	docker compose up -d postgres redis   # or local services
//	DB_HOST=localhost DB_PASSWORD=... go run ./cmd/loadtest -conns 96 -duration 20s
//
// Numbers land in README.md / BACKEND_STATUS.md; re-run after any change to
// the request path.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/vantageedge/backend/api/proto/configpb"
	"github.com/vantageedge/backend/internal/auth/apikey"
	authjwt "github.com/vantageedge/backend/internal/auth/jwt"
	redispkg "github.com/vantageedge/backend/internal/cache/redis"
	"github.com/vantageedge/backend/internal/controlplane/grpcserver"
	"github.com/vantageedge/backend/internal/eventbus"
	"github.com/vantageedge/backend/internal/gateway/configclient"
	"github.com/vantageedge/backend/internal/gateway/middleware"
	"github.com/vantageedge/backend/internal/gateway/proxy"
	"github.com/vantageedge/backend/internal/gateway/router"
	"github.com/vantageedge/backend/internal/loadbalancer"
	"github.com/vantageedge/backend/internal/models"
	"github.com/vantageedge/backend/internal/observability"
	"github.com/vantageedge/backend/internal/ratelimit"
	"github.com/vantageedge/backend/internal/repository"
	"github.com/vantageedge/backend/pkg/config"
	"github.com/vantageedge/backend/pkg/database"
	"github.com/vantageedge/backend/pkg/logger"
)

func main() {
	conns := flag.Int("conns", 96, "concurrent connections (closed-loop workers)")
	duration := flag.Duration("duration", 20*time.Second, "measurement window per scenario")
	warmup := flag.Duration("warmup", 3*time.Second, "warmup window (discarded)")
	originDelay := flag.Duration("origin-delay", 0, "artificial per-request latency at the mock origin")
	flag.Parse()

	log := logger.New("error", "console")

	dbCfg := &config.DatabaseConfig{
		Host: envOr("DB_HOST", "localhost"), Port: envOrInt("DB_PORT", 5432),
		User: envOr("DB_USER", "vantageedge"), Password: envOr("DB_PASSWORD", "changeme_db_password"),
		Name: envOr("DB_NAME", "vantageedge"), SSLMode: "disable",
		MaxConnections: 40, MaxIdleConnections: 20, MaxLifetime: 5 * time.Minute,
	}
	db, err := database.New(dbCfg, log)
	must("connect postgres", err)
	defer db.Close()
	repos := repository.New(db)

	redisClient, err := redispkg.NewClient(fmt.Sprintf("redis://%s:%d/%d",
		envOr("REDIS_HOST", "localhost"), envOrInt("REDIS_PORT", 6379), envOrInt("REDIS_DB", 0)))
	must("connect redis", err)
	defer redisClient.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- mock origin: ~220 byte JSON body, optional fixed latency ---
	originBody := []byte(`{"ok":true,"service":"mock-origin","payload":"` +
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" + `"}`)
	var originHits int64
	originMux := http.NewServeMux()
	originMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&originHits, 1)
		if *originDelay > 0 {
			time.Sleep(*originDelay)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Write(originBody)
	})
	originSrv := &http.Server{Handler: originMux}
	originLn := mustListen()
	go originSrv.Serve(originLn)
	defer originSrv.Close()
	originURL := "http://" + originLn.Addr().String()

	// --- in-process control-plane gRPC ConfigService (push invalidation) ---
	hub := eventbus.NewHub()
	grpcLn := mustListen()
	grpcSrv := grpc.NewServer()
	configpb.RegisterConfigServiceServer(grpcSrv, grpcserver.NewConfigServer(repos, hub, log))
	go grpcSrv.Serve(grpcLn)
	defer grpcSrv.Stop()

	configClient, err := configclient.NewClient(grpcLn.Addr().String(), log)
	must("dial config gRPC", err)
	defer configClient.Close()

	// Production TTL. Push invalidation (below) is what makes propagation
	// fast; the TTL is only the fallback ceiling.
	configCache := router.NewConfigCache(router.NewGRPCSource(configClient), 5*time.Second)
	go configClient.WatchInvalidations(ctx, configCache.Invalidate)

	// --- gateway, wired like cmd/gateway/main.go ---
	jwtValidator, err := authjwt.NewJWTValidator(ctx, testJWKS(), "", "")
	must("jwt validator", err)
	gwCfg := &config.Config{
		RateLimit:    config.RateLimitConfig{DefaultRPS: 1_000_000, DefaultBurst: 1_000_000},
		LoadBalancer: config.LoadBalancerConfig{HealthCheckInterval: time.Minute},
	}
	healthChecker := loadbalancer.NewHealthChecker(log, time.Minute)
	gwHandler := router.New(gwCfg, repos, router.Deps{
		ConfigCache:   configCache,
		Authenticator: middleware.NewAuthenticator(jwtValidator, apikey.NewValidator(repos), repos.User),
		Limiter:       ratelimit.NewRedisLimiter(redisClient),
		Cache:         middleware.NewResponseCache(middleware.NewRedisStore(redisClient)),
		Proxy:         proxy.NewReverseProxy(60 * time.Second),
		Metrics:       observability.NewMetrics("loadtest_" + uuid.NewString()[:8]),
		HealthChecker: healthChecker,
	}, log)
	gwSrv := &http.Server{Handler: gwHandler, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second}
	gwLn := mustListen()
	go gwSrv.Serve(gwLn)
	defer gwSrv.Close()
	gwURL := "http://" + gwLn.Addr().String()

	// --- seed tenant + one route per scenario, all pointing at the mock ---
	sub := "lt" + uuid.NewString()[:8]
	tenant := &models.Tenant{Name: "loadtest", Subdomain: sub, Status: "active", Settings: models.JSONB{}}
	must("create tenant", repos.Tenant.Create(ctx, tenant))
	defer func() {
		_ = repos.Tenant.Delete(context.Background(), tenant.ID)
		_, _ = db.ExecContext(context.Background(), "DELETE FROM request_logs WHERE tenant_id = $1", tenant.ID)
	}()

	origin := &models.Origin{TenantID: tenant.ID, Name: "mock", URL: originURL, TimeoutSeconds: 30, Weight: 100}
	must("create origin", repos.Origin.Create(ctx, origin))

	mkRoute := func(name, path string, rl, cache bool) *models.Route {
		rt := &models.Route{
			TenantID: tenant.ID, OriginID: origin.ID, Name: name, PathPattern: path,
			Methods: models.StringArray{"GET"}, AuthMode: "public", IsActive: true,
			RateLimitEnabled: rl, RateLimitRequestsPerSecond: 1_000_000, RateLimitBurst: 1_000_000,
			CacheEnabled: cache, CacheTTLSeconds: 60, CacheKeyPattern: "path+query",
			LoadBalancing: "weighted", Metadata: models.JSONB{},
		}
		must("create route "+name, repos.Route.Create(ctx, rt))
		return rt
	}
	mkRoute("passthrough", "/pass/*", false, false)
	mkRoute("ratelimited", "/rl/*", true, false)
	mkRoute("cached", "/cache/*", false, true)

	client := newClient(*conns)
	hdr := http.Header{"X-Tenant-Subdomain": {sub}}

	// prime config cache + health checker
	warm(client, gwURL+"/pass/x", hdr)
	warm(client, gwURL+"/rl/x", hdr)
	warm(client, gwURL+"/cache/x", hdr)
	time.Sleep(500 * time.Millisecond)

	fmt.Printf("\nVantageEdge gateway load test\n")
	fmt.Printf("conns=%d  duration=%s  origin-delay=%s  origin=in-process mock\n", *conns, *duration, *originDelay)
	fmt.Printf("go %s  %s\n\n", goVersion(), time.Now().Format(time.RFC3339))

	base := run("origin direct (baseline)", client, originURL+"/x", nil, *conns, *warmup, *duration)
	pass := run("gateway: passthrough", client, gwURL+"/pass/x", hdr, *conns, *warmup, *duration)
	rl := run("gateway: + rate limit (Redis token bucket)", client, gwURL+"/rl/x", hdr, *conns, *warmup, *duration)
	ch := run("gateway: cache hit (Redis)", client, gwURL+"/cache/x", hdr, *conns, *warmup, *duration)

	fmt.Printf("\n%-42s %10s %10s %10s %10s %10s\n", "scenario", "req/s", "p50", "p90", "p99", "max")
	for _, r := range []result{base, pass, rl, ch} {
		fmt.Printf("%-42s %10.0f %10s %10s %10s %10s\n", r.name, r.rps,
			d(r.p50), d(r.p90), d(r.p99), d(r.max))
	}
	fmt.Printf("\nadded latency (gateway passthrough - origin direct):\n")
	fmt.Printf("  p50 +%s   p90 +%s   p99 +%s\n", d(pass.p50-base.p50), d(pass.p90-base.p90), d(pass.p99-base.p99))
	if base.errs+pass.errs+rl.errs+ch.errs > 0 {
		fmt.Printf("  errors: baseline=%d passthrough=%d ratelimit=%d cache=%d\n", base.errs, pass.errs, rl.errs, ch.errs)
	}

	// --- config propagation: flip the origin URL and time until the gateway serves it ---
	fmt.Printf("\nconfig propagation (control-plane write -> gateway serving new config):\n")
	alt := mustListen()
	altSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ALT")) })}
	go altSrv.Serve(alt)
	defer altSrv.Close()
	altURL := "http://" + alt.Addr().String()

	var samples []time.Duration
	for i := 0; i < 25; i++ {
		want := originURL
		if i%2 == 0 {
			want = altURL
		}
		origin.URL = want
		must("update origin", repos.Origin.Update(ctx, origin))
		t0 := time.Now()
		hub.Publish(tenant.ID.String()) // what a control-plane handler does after a write
		for {
			body := get(client, gwURL+"/pass/x", hdr)
			isAlt := body == "ALT"
			if isAlt == (want == altURL) {
				break
			}
			if time.Since(t0) > 5*time.Second {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if i > 0 { // discard first (cache already warm on wrong value)
			samples = append(samples, time.Since(t0))
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	fmt.Printf("  n=%d  p50 %s  p90 %s  max %s\n", len(samples),
		d(pct(samples, 50)), d(pct(samples, 90)), d(samples[len(samples)-1]))

	// restore + let async request-log writes drain before the deferred cleanup
	origin.URL = originURL
	_ = repos.Origin.Update(ctx, origin)
	time.Sleep(2 * time.Second)
	fmt.Println()
}

type result struct {
	name              string
	rps               float64
	p50, p90, p99, max time.Duration
	errs              int64
}

func run(name string, client *http.Client, url string, hdr http.Header, conns int, warmup, dur time.Duration) result {
	// warmup
	deadline := time.Now().Add(warmup)
	var wg sync.WaitGroup
	for i := 0; i < conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				get(client, url, hdr)
			}
		}()
	}
	wg.Wait()

	// measure
	var (
		total   int64
		errs    int64
		latMu   sync.Mutex
		lat     = make([]time.Duration, 0, 1<<20)
	)
	stop := time.Now().Add(dur)
	start := time.Now()
	for i := 0; i < conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]time.Duration, 0, 1<<16)
			for time.Now().Before(stop) {
				t0 := time.Now()
				body, code := getFull(client, url, hdr)
				el := time.Since(t0)
				if code != 200 || body == "" {
					atomic.AddInt64(&errs, 1)
				}
				atomic.AddInt64(&total, 1)
				local = append(local, el)
			}
			latMu.Lock()
			lat = append(lat, local...)
			latMu.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	return result{
		name: name,
		rps:  float64(total) / elapsed.Seconds(),
		p50:  pct(lat, 50), p90: pct(lat, 90), p99: pct(lat, 99), max: lat[len(lat)-1],
		errs: errs,
	}
}

func pct(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := p * len(sorted) / 100
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func newClient(conns int) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        conns * 2,
			MaxIdleConnsPerHost: conns * 2,
			MaxConnsPerHost:     conns * 2,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
		},
	}
}

func get(c *http.Client, url string, hdr http.Header) string {
	b, _ := getFull(c, url, hdr)
	return b
}

func getFull(c *http.Client, url string, hdr http.Header) (string, int) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", 0
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), resp.StatusCode
}

func warm(c *http.Client, url string, hdr http.Header) {
	for i := 0; i < 5; i++ {
		getFull(c, url, hdr)
	}
}

func d(v time.Duration) string {
	switch {
	case v < time.Microsecond:
		return fmt.Sprintf("%dns", v.Nanoseconds())
	case v < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(v.Nanoseconds())/1e3)
	default:
		return fmt.Sprintf("%.2fms", float64(v.Nanoseconds())/1e6)
	}
}

func mustListen() net.Listener {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must("listen", err)
	return ln
}

func must(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "loadtest: %s: %v\n", what, err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envOrInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func goVersion() string { return envOr("GOVERSION", "1.25") }

// testJWKS serves an empty JWKS — the load test only exercises public
// routes, but NewJWTValidator needs a reachable key set to initialise.
func testJWKS() string {
	ln := mustListen()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"keys":[]}`))
	})}
	go srv.Serve(ln)
	return "http://" + ln.Addr().String()
}
