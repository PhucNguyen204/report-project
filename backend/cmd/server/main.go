package main

import (
	"context"
	"dicho/internal/app"
	"dicho/internal/modules/media"
	"dicho/internal/modules/notification"
	"dicho/internal/platform"
	"dicho/migrations"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("backend stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := app.LoadRuntimeConfig(os.Getenv)
	if err != nil {
		return err
	}
	key, queueKey, err := app.ReadRuntimeKeys(cfg.SigningKeyFile, cfg.QueueKeyFile)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer pool.Close()
	initCtx, initCancel := context.WithTimeout(ctx, 30*time.Second)
	defer initCancel()
	if err = pool.Ping(initCtx); err != nil {
		return errors.New("database connection failed")
	}
	if cfg.AutoMigrate {
		if err = platform.Migrate(initCtx, pool, migrations.Files); err != nil {
			return err
		}
		if err = platform.MigrateQueue(initCtx, pool); err != nil {
			return err
		}
	}
	if err = platform.CheckMigrations(initCtx, pool, migrations.Files); err != nil {
		return err
	}
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return errors.New("invalid Redis configuration")
	}
	redisOpts.DialTimeout = 200 * time.Millisecond
	redisOpts.ReadTimeout = 100 * time.Millisecond
	redisOpts.MaxRetries = -1
	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()
	if err = redisClient.Ping(initCtx).Err(); err != nil {
		return errors.New("Redis unavailable at startup")
	}
	httpClient := &http.Client{Timeout: 20 * time.Second}
	s3client := s3.NewFromConfig(aws.Config{Region: cfg.S3Region, Credentials: credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""), HTTPClient: httpClient}, func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.S3Endpoint); o.UsePathStyle = true })
	publicClient := s3.NewFromConfig(aws.Config{Region: cfg.S3Region, Credentials: credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""), HTTPClient: httpClient}, func(o *s3.Options) { o.BaseEndpoint = aws.String(cfg.S3PublicEndpoint); o.UsePathStyle = true })
	for _, bucket := range []string{cfg.MediaBucket, cfg.ExportBucket} {
		if _, err = s3client.HeadBucket(initCtx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err != nil {
			if !cfg.Development {
				return errors.New("object storage bucket unavailable")
			}
			if _, err = s3client.CreateBucket(initCtx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
				return errors.New("cannot prepare development storage bucket")
			}
		}
	}
	var push notification.PushSender
	if cfg.FCMProject != "" {
		var creds *google.Credentials
		if cfg.FCMCredentialFile != "" {
			raw, readErr := os.ReadFile(cfg.FCMCredentialFile)
			if readErr != nil {
				return errors.New("cannot read FCM credentials")
			}
			creds, err = google.CredentialsFromJSON(ctx, raw, "https://www.googleapis.com/auth/firebase.messaging")
		} else {
			creds, err = google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/firebase.messaging")
		}
		if err != nil {
			return errors.New("invalid FCM credentials")
		}
		client := oauth2.NewClient(ctx, creds.TokenSource)
		client.Timeout = 20 * time.Second
		push = notification.FCM{Project: cfg.FCMProject, Client: client}
	}
	db := platform.NewDB(pool)
	system, err := app.NewSystem(initCtx, db, app.Options{SigningKey: key, QueueKey: queueKey, MediaStore: media.NewS3(s3client, cfg.MediaBucket).WithPublicClient(publicClient), ExportStore: media.NewS3(s3client, cfg.ExportBucket).WithPublicClient(publicClient), Email: notification.SMTP{Addr: cfg.SMTPAddr, From: cfg.SMTPFrom, User: cfg.SMTPUser, Password: cfg.SMTPPassword, RequireTLS: !cfg.Development}, Push: push, HTTP: app.Config{MinimumAppVersion: cfg.MinimumAppVersion, Limiter: platform.NewRateLimiter(redisClient, db.Clock), TrustedProxies: cfg.TrustedProxies}})
	if err != nil {
		return err
	}
	if cfg.Mode != "api" {
		if err = system.Worker.Start(ctx); err != nil {
			return err
		}
		go system.RunScheduler(ctx, func(err error) { slog.Error("scheduled maintenance failed", "error", err) })
	}
	server := &http.Server{Addr: cfg.ListenAddr, Handler: system.Handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	failed := make(chan error, 1)
	if cfg.Mode != "worker" {
		go func() {
			slog.Info("backend listening", "address", cfg.ListenAddr, "mode", cfg.Mode)
			failed <- server.ListenAndServe()
		}()
	}
	select {
	case <-ctx.Done():
	case err = <-failed:
		if !errors.Is(err, http.ErrServerClosed) {
			cancel()
		}
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer stopCancel()
	shutdownErr := server.Shutdown(stopCtx)
	if cfg.Mode != "api" {
		shutdownErr = errors.Join(shutdownErr, system.Worker.Stop(stopCtx))
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}
