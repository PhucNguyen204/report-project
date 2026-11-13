//go:build performance

package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"dicho/internal/modules/media"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"dicho/migrations"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func performancePercentile(samples []time.Duration, percentile float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}
func performanceAllocation(index, users, routes int) (int, int) {
	return (index / routes) % users, index % routes
}

func TestPerformanceHelpersPreserveTailLatencyAndBalanceUsersAcrossRoutes(t *testing.T) {
	samples := []time.Duration{9 * time.Millisecond, time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond, 6 * time.Millisecond, 7 * time.Millisecond, 8 * time.Millisecond}
	for _, tc := range []struct {
		p    float64
		want time.Duration
	}{{0.5, 5 * time.Millisecond}, {0.95, 9 * time.Millisecond}, {1, 9 * time.Millisecond}} {
		if got := performancePercentile(samples, tc.p); got != tc.want {
			t.Fatalf("p%v=%v want%v", tc.p, got, tc.want)
		}
	}
	if performancePercentile(nil, 0.95) != 0 {
		t.Fatal("empty sample produced latency")
	}
	counts := [16][4]int{}
	for i := range 1500 {
		user, route := performanceAllocation(i, 16, 4)
		counts[user][route]++
	}
	for user, routes := range counts {
		total := 5
		for route, n := range routes {
			if n < 23 || n > 24 {
				t.Fatalf("user%d route%d has%d samples; workload coupled", user, route, n)
			}
			total += n
		}
		if total > 120 {
			t.Fatalf("user%d exceeds120/min with warmup: %d", user, total)
		}
	}
}

const performanceUsers = 16
const performanceRecipes = 500
const performanceLots = 200
const performanceRPS = 50
const performanceSeconds = 30
const performanceConcurrency = 128

type performanceIdentity struct{ user, space, token string }
type performanceSample struct {
	route                             int
	status                            int
	failure                           string
	latency, httpLatency, dispatchLag time.Duration
	dropped, degraded                 bool
	bytes                             int
}
type performanceLatency struct {
	P50 float64 `json:"p50_ms"`
	P95 float64 `json:"p95_ms"`
	Max float64 `json:"max_ms"`
}
type performanceRoute struct {
	Name          string             `json:"name"`
	Method        string             `json:"method"`
	Path          string             `json:"path_template"`
	Requests      int                `json:"requests"`
	Successes     int                `json:"successes"`
	Errors        int                `json:"errors"`
	Dropped       int                `json:"dropped"`
	Status        map[string]int     `json:"status_counts"`
	Failures      map[string]int     `json:"failure_counts"`
	Latency       performanceLatency `json:"scheduled_to_body_read"`
	HTTP          performanceLatency `json:"dispatch_to_body_read"`
	Lag           performanceLatency `json:"dispatch_lag"`
	Target        float64            `json:"p95_target_ms"`
	TargetMet     bool               `json:"p95_target_met"`
	SuccessfulRPS float64            `json:"successful_rps_including_drain"`
}
type performanceReport struct {
	Source      map[string]any     `json:"source"`
	Version     int                `json:"version"`
	RunID       string             `json:"run_id"`
	Status      string             `json:"status"`
	Phase       string             `json:"phase"`
	Started     time.Time          `json:"started_at"`
	Finished    time.Time          `json:"finished_at"`
	Hardware    map[string]any     `json:"hardware"`
	Dataset     map[string]any     `json:"dataset"`
	Limits      map[string]any     `json:"limits"`
	Services    map[string]any     `json:"services"`
	Cleanup     map[string]any     `json:"cleanup"`
	Workload    map[string]any     `json:"workload"`
	Routes      []performanceRoute `json:"routes"`
	Limitations []string           `json:"limitations"`
}

func performanceMilliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
func performanceLatencies(samples []time.Duration) performanceLatency {
	return performanceLatency{performanceMilliseconds(performancePercentile(samples, .5)), performanceMilliseconds(performancePercentile(samples, .95)), performanceMilliseconds(performancePercentile(samples, 1))}
}
func performanceHardware() map[string]any {
	out := map[string]any{"go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0)}
	capture := func(name string, args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		b, err := exec.CommandContext(ctx, name, args...).Output()
		if err != nil {
			return "unavailable"
		}
		return strings.TrimSpace(string(b))
	}
	if runtime.GOOS == "darwin" {
		out["model"] = capture("sysctl", "-n", "hw.model")
		out["cpu_model"] = capture("sysctl", "-n", "machdep.cpu.brand_string")
		out["host_memory_bytes"] = capture("sysctl", "-n", "hw.memsize")
	}
	if runtime.GOOS == "linux" {
		if b, err := os.ReadFile("/proc/meminfo"); err == nil {
			out["host_memory"] = strings.SplitN(string(b), "\n", 2)[0]
		}
	}
	out["kernel_release"] = capture("uname", "-r")
	out["docker_limits"] = capture("docker", "info", "--format", `{"cpus":{{.NCPU}},"memory_bytes":{{.MemTotal}},"os":{{json .OperatingSystem}}}`)
	return out
}
func performanceDatabase(t *testing.T, run string, report *performanceReport) *platform.DB {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://dicho:dicho-local-test@127.0.0.1:55432/dicho_test?sslmode=disable"
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("PostgreSQL control connection failed")
	}
	name := "dicho_perf_" + strings.ReplaceAll(run, "-", "")
	if _, err = base.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		base.Close()
		t.Fatalf("isolated PostgreSQL database required: %v", err)
	}
	parsed.Path = "/" + name
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		_, _ = base.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		base.Close()
		t.Fatal("PostgreSQL workload connection failed")
	}
	t.Cleanup(func() {
		pool.Close()
		_, dropErr := base.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		var exists bool
		checkErr := base.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", name).Scan(&exists)
		base.Close()
		report.Cleanup["generated_database_dropped"] = dropErr == nil && checkErr == nil && !exists
		if dropErr != nil || checkErr != nil || exists {
			t.Error("performance database cleanup failed")
		}
	})
	if err = platform.Migrate(ctx, pool, migrations.Files); err != nil {
		t.Fatalf("performance migration: %v", err)
	}
	if err = platform.MigrateQueue(ctx, pool); err != nil {
		t.Fatalf("performance queue migration: %v", err)
	}
	db := platform.NewDB(pool)
	var version string
	if err = pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	report.Services["postgresql_version"] = version
	report.Services["postgresql_pool_max_connections"] = pool.Config().MaxConns
	report.Services["postgresql_isolated_database"] = true
	return db
}
func performanceJWT(t *testing.T, key ed25519.PrivateKey, user, family string, now time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","typ":"JWT","kid":"primary"}`))
	raw, err := json.Marshal(map[string]any{"sub": user, "sid": family, "role": "user", "ver": 0, "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "jti": platform.ID()})
	if err != nil {
		t.Fatal(err)
	}
	body := header + "." + base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(body)))
}
func performanceNumber(n int64) pgtype.Numeric {
	return pgtype.Numeric{Int: big.NewInt(n), Valid: true}
}
func performanceSeed(t *testing.T, db *platform.DB, key ed25519.PrivateKey) []performanceIdentity {
	t.Helper()
	ctx := context.Background()
	now := db.Clock.Now()
	today := db.Today()
	unit := "00000000-0000-7000-8000-000000000011"
	category := "00000000-0000-7000-8000-000000000001"
	foods := make([]string, 250)
	foodRows := [][]any{}
	for i := range foods {
		foods[i] = platform.ID()
		foodRows = append(foodRows, []any{foods[i], fmt.Sprintf("Performance food %03d", i), category, unit})
	}
	identities := make([]performanceIdentity, performanceUsers)
	userRows, settingRows, spaceRows, memberRows, sessionRows := [][]any{}, [][]any{}, [][]any{}, [][]any{}, [][]any{}
	recipeRows, ingredientRows, lotRows, movementRows, favoriteRows := [][]any{}, [][]any{}, [][]any{}, [][]any{}, [][]any{}
	for userIndex := range identities {
		user, space, family := platform.ID(), platform.ID(), platform.ID()
		identities[userIndex] = performanceIdentity{user: user, space: space, token: performanceJWT(t, key, user, family, now)}
		hash := sha256.Sum256([]byte(platform.ID()))
		userRows = append(userRows, []any{user, "perf-" + user + "@example.test", "Performance user", "synthetic-no-login", "active"})
		settingRows = append(settingRows, []any{user})
		spaceRows = append(spaceRows, []any{space, "personal", "Performance scope", user})
		memberRows = append(memberRows, []any{space, user, "leader", "personal"})
		sessionRows = append(sessionRows, []any{platform.ID(), user, family, hash[:], platform.ID(), "android", now.Add(time.Hour)})
		for i := range performanceRecipes {
			recipe := platform.ID()
			recipeRows = append(recipeRows, []any{recipe, space, user, fmt.Sprintf("Performance recipe %03d", i), 2, json.RawMessage(`["Prepare ingredients","Cook","Serve"]`)})
			for j := range 5 {
				food := foods[(i*7+j*13)%len(foods)]
				ingredientRows = append(ingredientRows, []any{platform.ID(), recipe, food, unit, performanceNumber(1), "mass", performanceNumber(10), j == 4})
			}
			if i%25 == 0 {
				favoriteRows = append(favoriteRows, []any{user, recipe})
			}
		}
		for i := range performanceLots {
			lot := platform.ID()
			food := foods[i]
			lotRows = append(lotRows, []any{lot, space, food, unit, performanceNumber(1), "mass", performanceNumber(1000), performanceNumber(1000), performanceNumber(1000), today, today.AddDate(0, 0, 1+i%7), "chill", int64(20000)})
			movementRows = append(movementRows, []any{platform.ID(), space, lot, food, "stock_in", performanceNumber(1000), user, now})
		}
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	batches := []struct {
		table   pgx.Identifier
		columns []string
		rows    [][]any
	}{
		{pgx.Identifier{"catalog", "foods"}, []string{"id", "name", "category_id", "default_unit_id"}, foodRows},
		{pgx.Identifier{"identity", "users"}, []string{"id", "email", "full_name", "password_hash", "status"}, userRows},
		{pgx.Identifier{"identity", "user_settings"}, []string{"user_id"}, settingRows},
		{pgx.Identifier{"household", "spaces"}, []string{"id", "kind", "name", "created_by"}, spaceRows},
		{pgx.Identifier{"household", "space_members"}, []string{"space_id", "user_id", "role", "space_kind"}, memberRows},
		{pgx.Identifier{"identity", "refresh_sessions"}, []string{"id", "user_id", "family_id", "token_hash", "device_id", "platform", "expires_at"}, sessionRows},
		{pgx.Identifier{"recipe", "recipes"}, []string{"id", "space_id", "created_by", "name", "servings", "steps"}, recipeRows},
		{pgx.Identifier{"recipe", "recipe_ingredients"}, []string{"id", "recipe_id", "food_id", "unit_id", "unit_factor", "dimension", "quantity", "optional"}, ingredientRows},
		{pgx.Identifier{"recipe", "favorite_recipes"}, []string{"user_id", "recipe_id"}, favoriteRows},
		{pgx.Identifier{"pantry", "food_lots"}, []string{"id", "space_id", "food_id", "unit_id", "unit_factor", "dimension", "initial_qty", "remaining_qty", "base_remaining", "purchased_on", "expires_on", "storage_location", "total_cost"}, lotRows},
		{pgx.Identifier{"pantry", "stock_movements"}, []string{"id", "space_id", "lot_id", "food_id", "kind", "quantity", "actor_id", "occurred_at"}, movementRows},
	}
	for _, batch := range batches {
		count, err := tx.CopyFrom(ctx, batch.table, batch.columns, pgx.CopyFromRows(batch.rows))
		if err != nil || count != int64(len(batch.rows)) {
			t.Fatalf("performance seed%s count%d: %v", batch.table.Sanitize(), count, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}
	for _, identity := range identities {
		var recipes, lots, ingredients int
		if err = db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM recipe.recipes WHERE space_id=$1),(SELECT count(*) FROM pantry.food_lots WHERE space_id=$1),(SELECT count(*) FROM recipe.recipe_ingredients WHERE recipe_id IN(SELECT id FROM recipe.recipes WHERE space_id=$1))`, identity.space).Scan(&recipes, &lots, &ingredients); err != nil || recipes != 500 || lots != 200 || ingredients != 2500 {
			t.Fatalf("scope dataset recipes%d lots%d ingredients%d: %v", recipes, lots, ingredients, err)
		}
	}
	return identities
}
func performanceRedisKeys(users []performanceIdentity) []string {
	keys := []string{}
	for _, user := range users {
		for _, kind := range []string{"api:", "suggest:"} {
			sum := sha256.Sum256([]byte(kind + user.user))
			keys = append(keys, fmt.Sprintf("dicho:rl:%x", sum))
		}
	}
	return keys
}
func performancePath(route int, space string) string {
	base := "/it4788/v1/spaces/" + space
	switch route {
	case 0:
		return base + "/lots"
	case 1:
		return base + "/recipes?source=space"
	case 2:
		return base + "/recipe-suggestions?servings=2&limit=10"
	default:
		return base + "/foods/suggest?q=Performance%20food&limit=10"
	}
}
func performanceRequest(client *http.Client, base string, user performanceIdentity, route int, scheduled time.Time, limiter *platform.RateLimiter) performanceSample {
	sample := performanceSample{route: route}
	dispatch := time.Now()
	sample.dispatchLag = max(dispatch.Sub(scheduled), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", base+performancePath(route, user.space), nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+user.token)
		req.Header.Set("X-App-Version", "1.0.0")
		req.Header.Set("X-Platform", "android")
	}
	if err != nil {
		sample.failure = "request_construction"
	} else {
		response, err := client.Do(req)
		if err != nil {
			sample.failure = "transport"
			if errors.Is(err, context.DeadlineExceeded) {
				sample.failure = "timeout"
			}
		} else {
			sample.status = response.StatusCode
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
			response.Body.Close()
			sample.bytes = len(body)
			if readErr != nil {
				sample.failure = "body_read"
			} else if sample.status != 200 {
				sample.failure = "http_status"
			} else {
				var envelope struct {
					Code int                        `json:"code"`
					Data map[string]json.RawMessage `json:"data"`
				}
				if json.Unmarshal(body, &envelope) != nil || envelope.Code != 1000 {
					sample.failure = "invalid_envelope"
				} else {
					field, want := "items", 20
					if route == 0 {
						field = "lots"
					}
					if route >= 2 {
						want = 10
					}
					var items []json.RawMessage
					if json.Unmarshal(envelope.Data[field], &items) != nil || len(items) != want {
						sample.failure = "unexpected_dataset_response"
					}
				}
			}
		}
	}
	sample.httpLatency = time.Since(dispatch)
	sample.latency = time.Since(scheduled)
	sample.degraded = limiter.Degraded()
	return sample
}
func performanceSummary(samples []performanceSample, duration time.Duration) []performanceRoute {
	routes := []performanceRoute{{Name: "lots", Method: "GET", Path: "/spaces/{sid}/lots", Target: 1000}, {Name: "recipes", Method: "GET", Path: "/spaces/{sid}/recipes?source=space", Target: 1000}, {Name: "suggestions", Method: "GET", Path: "/spaces/{sid}/recipe-suggestions?servings=2&limit=10", Target: 2000}, {Name: "autocomplete", Method: "GET", Path: "/spaces/{sid}/foods/suggest?q=Performance+food&limit=10", Target: 500}}
	for index := range routes {
		route := &routes[index]
		route.Status = map[string]int{}
		route.Failures = map[string]int{}
		latencies, httpTimes, lags := []time.Duration{}, []time.Duration{}, []time.Duration{}
		for _, sample := range samples {
			if sample.route != index {
				continue
			}
			route.Requests++
			route.Status[strconv.Itoa(sample.status)]++
			latencies = append(latencies, sample.latency)
			httpTimes = append(httpTimes, sample.httpLatency)
			lags = append(lags, sample.dispatchLag)
			if sample.failure != "" {
				route.Errors++
				route.Failures[sample.failure]++
			} else {
				route.Successes++
			}
			if sample.dropped {
				route.Dropped++
			}
		}
		route.Latency = performanceLatencies(latencies)
		route.HTTP = performanceLatencies(httpTimes)
		route.Lag = performanceLatencies(lags)
		route.TargetMet = route.Requests > 0 && route.Latency.P95 <= route.Target
		route.SuccessfulRPS = float64(route.Successes) / duration.Seconds()
	}
	return routes
}
func TestPerformanceRealNetworkFiftyRPSForThirtySeconds(t *testing.T) {
	report := &performanceReport{Version: 1, RunID: platform.ID(), Started: time.Now().UTC(), Phase: "setup", Hardware: performanceHardware(), Services: map[string]any{}, Cleanup: map[string]any{}, Dataset: map[string]any{"users": 16, "measured_scopes": 16, "recipes_per_scope": 500, "lots_per_scope": 200, "ingredients_per_recipe": 5, "optional_ingredients_per_recipe": 1, "global_foods_seeded": 250, "total_recipes": 8000, "total_lots": 3200, "all_scope_counts_verified": false}, Limits: map[string]any{"api_per_user_per_minute": 120, "autocomplete_per_user_per_second": 20, "warmup_requests_per_user": 5, "maximum_requests_per_user_including_warmup": 101, "request_timeout_seconds": 10, "maximum_concurrent_requests": 128}, Workload: map[string]any{"target_rps": 50, "offer_duration_seconds": 30, "target_requests": 1500, "routes": 4, "requests_per_route": 375, "latency_basis": "scheduled send time to body fully read; includes dispatch delay and client/DB waiting", "scheduler": "open-loop 20ms slots with bounded128 workers; capacity drops count as errors"}, Limitations: []string{"Local loopback HTTP/1.1 TCP; no TLS, mobile device or4G latency.", "Synthetic16 independent personal scopes with identical dataset shape; production traffic mix and mutation contention differ.", "S3 is initialized and checked over real network before measurement; these four measured GET routes do not read object storage.", "River worker runs idle; scheduler is not launched for this read-only experiment.", "Host and Docker share CPU, memory and filesystem with other work; this is not a production SLA or availability measurement."}}
	t.Cleanup(func() {
		report.Finished = time.Now().UTC()
		report.Status = "pass"
		if t.Failed() {
			report.Status = "failed"
		}
		path := filepath.Clean(filepath.Join("..", "..", "..", "output", "backend-qa", "performance.json"))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Error(err)
			return
		}
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Error(err)
		} else {
			t.Logf("performance artifact: %s", path)
		}
	})
	head, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	dirty, _ := exec.Command("git", "status", "--short").Output()
	report.Source = map[string]any{"head_at_test_start": strings.TrimSpace(string(head)), "dirty_files_at_test_start": strings.Split(strings.TrimSpace(string(dirty)), "\n"), "verified_head_only": false, "binary": "go test working-tree build; dirty snapshot is included, not a production release"}
	report.Limits["queued_request_capacity"] = performanceConcurrency
	db := performanceDatabase(t, report.RunID, report)
	_, signingKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	users := performanceSeed(t, db, signingKey)
	report.Dataset["all_scope_counts_verified"] = true
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:56379/0"
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal("invalid TEST_REDIS_URL")
	}
	redisOptions.ClientName = "dicho-performance-" + report.RunID
	cache := redis.NewClient(redisOptions)
	ctx := context.Background()
	if err = cache.Ping(ctx).Err(); err != nil {
		cache.Close()
		t.Fatalf("real Redis required: %v", err)
	}
	keys := performanceRedisKeys(users)
	if count, err := cache.Exists(ctx, keys...).Result(); err != nil || count != 0 {
		cache.Close()
		t.Fatal("generated Redis namespace is not empty")
	}
	report.Services["redis_address"] = redisOptions.Addr
	report.Services["redis_namespace"] = "dicho:rl:sha256(api|suggest:unique-generated-user-UUID)"
	report.Services["redis_generated_keys"] = len(keys)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		removed, err := cache.Del(cleanup, keys...).Result()
		remaining, checkErr := cache.Exists(cleanup, keys...).Result()
		report.Cleanup["redis_keys_removed"] = removed
		report.Cleanup["redis_generated_keys_remaining"] = remaining
		cache.Close()
		if err != nil || checkErr != nil || remaining != 0 {
			t.Error("generated Redis namespace cleanup failed")
		}
	})
	clientS3 := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(testutil.S3Endpoint()), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider("dicho-test", "dicho-local-storage-test", "")})
	bucket := "performance-" + report.RunID
	if _, err = clientS3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket}); err != nil {
		t.Fatalf("real S3 fixture required: %v", err)
	}
	store := media.NewS3(clientS3, bucket)
	canary := "performance-canary"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		deleteErr := store.Delete(cleanup, []string{canary})
		_, bucketErr := clientS3.DeleteBucket(cleanup, &s3.DeleteBucketInput{Bucket: &bucket})
		report.Cleanup["s3_bucket_deleted"] = deleteErr == nil && bucketErr == nil
		if deleteErr != nil || bucketErr != nil {
			t.Error("performance S3 cleanup failed")
		}
	})
	if err = store.Put(ctx, canary, "text/plain", []byte("performance fixture")); err != nil {
		t.Fatal(err)
	}
	signed, err := store.DownloadURL(ctx, canary, "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	canaryResponse, err := http.Get(signed)
	if err != nil {
		t.Fatal("S3 signed URL canary failed")
	}
	canaryBody, err := io.ReadAll(canaryResponse.Body)
	canaryResponse.Body.Close()
	if err != nil || canaryResponse.StatusCode != 200 || string(canaryBody) != "performance fixture" {
		t.Fatal("S3 signed URL canary invalid")
	}
	report.Services["s3_real_network_canary_passed"] = true
	limiter := platform.NewRateLimiter(cache, db.Clock)
	queueKey := make([]byte, 32)
	if _, err = rand.Read(queueKey); err != nil {
		t.Fatal(err)
	}
	system, err := NewSystem(ctx, db, Options{SigningKey: signingKey, QueueKey: queueKey, MediaStore: store, ExportStore: store, HTTP: Config{MinimumAppVersion: "1.0.0", Limiter: limiter}})
	if err != nil {
		t.Fatal(err)
	}
	if err = system.Worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := system.Worker.Stop(stop); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(system.Handler)
	t.Cleanup(server.Close)
	transport := &http.Transport{MaxConnsPerHost: performanceConcurrency, MaxIdleConns: performanceConcurrency, MaxIdleConnsPerHost: performanceConcurrency}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	report.Phase = "warmup"
	warmupStarted := time.Now()
	for _, user := range users {
		req, err := http.NewRequest("GET", server.URL+"/it4788/v1/me", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+user.token)
		req.Header.Set("X-App-Version", "1.0.0")
		req.Header.Set("X-Platform", "android")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("authentication warmup transport failed")
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("actual signed JWT rejected%d", response.StatusCode)
		}
		for route := range 4 {
			sample := performanceRequest(client, server.URL, user, route, time.Now(), limiter)
			if sample.failure != "" {
				t.Fatalf("route%d warmup failed: status%d %s", route, sample.status, sample.failure)
			}
		}
	}
	tampered := users[0]
	parts := strings.Split(tampered.token, ".")
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	signature[0] ^= 1
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	tampered.token = strings.Join(parts, ".")
	if sample := performanceRequest(client, server.URL, tampered, 0, time.Now(), limiter); sample.status != 401 {
		t.Fatalf("tampered JWT did not reach real authentication: status%d", sample.status)
	}
	report.Workload["warmup_requests"] = 80
	report.Workload["warmup_seconds"] = time.Since(warmupStarted).Seconds()
	report.Services["real_authentication_verified"] = true
	report.Phase = "measuring"
	t.Log("starting50RPS30s real-network measurement")
	type job struct {
		index int
		at    time.Time
	}
	jobs := make(chan job, performanceConcurrency)
	results := make(chan performanceSample, 1500)
	var workers sync.WaitGroup
	for range performanceConcurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range jobs {
				user, route := performanceAllocation(task.index, len(users), 4)
				results <- performanceRequest(client, server.URL, users[user], route, task.at, limiter)
			}
		}()
	}
	start := time.Now()
	for index := range 1500 {
		scheduled := start.Add(time.Duration(index) * time.Second / performanceRPS)
		if wait := time.Until(scheduled); wait > 0 {
			timer := time.NewTimer(wait)
			<-timer.C
		}
		select {
		case jobs <- job{index, scheduled}:
		default:
			_, route := performanceAllocation(index, len(users), 4)
			results <- performanceSample{route: route, failure: "load_generator_capacity", dropped: true, latency: time.Since(scheduled), dispatchLag: time.Since(scheduled)}
		}
	}
	close(jobs)
	workers.Wait()
	if remaining := time.Until(start.Add(performanceSeconds * time.Second)); remaining > 0 {
		timer := time.NewTimer(remaining)
		<-timer.C
	}
	elapsed := max(time.Since(start), performanceSeconds*time.Second)
	close(results)
	samples := make([]performanceSample, 0, 1500)
	degraded := 0
	totalBytes := 0
	for sample := range results {
		samples = append(samples, sample)
		if sample.degraded {
			degraded++
		}
		totalBytes += sample.bytes
	}
	report.Routes = performanceSummary(samples, elapsed)
	report.Workload["measured_samples"] = len(samples)
	report.Workload["elapsed_including_drain_seconds"] = elapsed.Seconds()
	report.Workload["offered_rps"] = 50
	report.Workload["redis_degraded_samples"] = degraded
	report.Workload["response_bytes"] = totalBytes
	successes := 0
	for _, route := range report.Routes {
		successes += route.Successes
		t.Logf("%s n%d errors%d p50%.2fms p95%.2fms max%.2fms", route.Name, route.Requests, route.Errors, route.Latency.P50, route.Latency.P95, route.Latency.Max)
		if route.Requests != 375 || route.Errors != 0 || !route.TargetMet {
			t.Errorf("%s failed local gate: requests%d errors%d p95%.2fms target%.0fms", route.Name, route.Requests, route.Errors, route.Latency.P95, route.Target)
		}
	}
	report.Workload["successful_rps_including_drain"] = float64(successes) / elapsed.Seconds()
	apiKeys := []string{}
	for index, key := range keys {
		if index%2 == 0 {
			apiKeys = append(apiKeys, key)
		}
	}
	present, err := cache.Exists(ctx, apiKeys...).Result()
	report.Services["redis_api_keys_verified"] = present
	if err != nil || present != 16 || degraded != 0 || limiter.Degraded() {
		t.Error("measurement did not stay on real Redis for all generated user quotas")
	}
	report.Phase = "complete"
}
