package main

import (
	"context"
	"dicho/internal/app"
	"dicho/internal/modules/mealai"
	"dicho/internal/modules/notification"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
)

type providerFactories struct {
	mail func(context.Context, func(string) string) (notification.EmailSender, error)
	push func(context.Context, func(string) string) (notification.PushSender, error)
	ai   func(context.Context, func(string) string) (mealai.Provider, error)
}

type checkResult struct {
	Status  string   `json:"status"`
	Reason  string   `json:"reason,omitempty"`
	Missing []string `json:"missing,omitempty"`
}

type smokeReport struct {
	Providers map[string]checkResult `json:"providers"`
	Error     string                 `json:"error,omitempty"`
}

func main() { os.Exit(run(os.Args[1:], os.Getenv, liveFactories(), os.Stdout)) }

func run(args []string, get func(string) string, factories providerFactories, output io.Writer) int {
	report := smokeReport{Providers: map[string]checkResult{
		"mail": {Status: "NOT_RUN", Reason: "not_requested"},
		"push": {Status: "NOT_RUN", Reason: "not_requested"},
		"ai":   {Status: "NOT_RUN", Reason: "not_requested"},
	}}
	flags := flag.NewFlagSet("integration-smoke", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mailEnabled := flags.Bool("mail", false, "Run explicitly configured staging email check")
	pushEnabled := flags.Bool("push", false, "Run explicitly configured staging push check")
	aiEnabled := flags.Bool("ai", false, "Run a synthetic AI request")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		report.Error = "invalid_arguments"
		_ = json.NewEncoder(output).Encode(report)
		return 2
	}
	exitCode := 0
	check := func(name string, enabled bool, required []string, perform func(context.Context) error) {
		if !enabled {
			return
		}
		missing := []string{}
		for _, key := range required {
			if get(key) == "" {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			report.Providers[name] = checkResult{Status: "NOT_RUN", Reason: "missing_configuration", Missing: missing}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := perform(ctx); err != nil {
			report.Providers[name] = checkResult{Status: "FAIL", Reason: "provider_check_failed"}
			exitCode = 1
			return
		}
		report.Providers[name] = checkResult{Status: "PASS", Reason: "provider_accepted"}
	}
	mailRequired := []string{"SMTP_ADDR", "SMTP_FROM", "SMOKE_EMAIL_TO"}
	if *mailEnabled {
		if get("SMTP_USER") != "" || get("SMTP_PASSWORD") != "" {
			mailRequired = append(mailRequired, "SMTP_USER", "SMTP_PASSWORD")
		}
	}
	check("mail", *mailEnabled, mailRequired, func(ctx context.Context) error {
		if factories.mail == nil {
			return errors.New("mail provider unavailable")
		}
		sender, err := factories.mail(ctx, get)
		if err != nil || sender == nil {
			return errors.New("mail provider unavailable")
		}
		return sender.Send(ctx, get("SMOKE_EMAIL_TO"), "Di Cho staging integration check", "Synthetic staging message. No account data is included.")
	})
	check("push", *pushEnabled, []string{"FCM_PROJECT_ID", "FCM_CREDENTIALS_FILE", "SMOKE_FCM_TOKEN"}, func(ctx context.Context) error {
		if factories.push == nil {
			return errors.New("push provider unavailable")
		}
		sender, err := factories.push(ctx, get)
		if err != nil || sender == nil {
			return errors.New("push provider unavailable")
		}
		return sender.Send(ctx, get("SMOKE_FCM_TOKEN"), "Di Cho staging check", "Synthetic notification. No account data is included.")
	})
	check("ai", *aiEnabled, []string{"OPENAI_API_KEY_FILE"}, func(ctx context.Context) error {
		if factories.ai == nil {
			return errors.New("AI provider unavailable")
		}
		provider, err := factories.ai(ctx, get)
		if err != nil || provider == nil {
			return errors.New("AI provider unavailable")
		}
		prompt := mealai.Prompt{Date: time.Now().UTC().Format("2006-01-02"), MealSlot: "dinner", Servings: 1, Limit: 1, Candidates: []mealai.Candidate{{Key: "r01", Name: "Synthetic carrot soup", Coverage: 1, Missing: []string{}, Expiring: []string{"Carrot"}}}}
		choices, err := provider.Recommend(ctx, prompt)
		if err != nil {
			return err
		}
		return mealai.ValidateChoices(prompt, choices)
	})
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return 2
	}
	return exitCode
}

func liveFactories() providerFactories {
	return providerFactories{
		mail: func(_ context.Context, get func(string) string) (notification.EmailSender, error) {
			return notification.SMTP{Addr: get("SMTP_ADDR"), From: get("SMTP_FROM"), User: get("SMTP_USER"), Password: get("SMTP_PASSWORD"), RequireTLS: true}, nil
		},
		push: func(ctx context.Context, get func(string) string) (notification.PushSender, error) {
			creds, err := app.ReadFCMCredentials(ctx, get("FCM_CREDENTIALS_FILE"))
			if err != nil {
				return nil, err
			}
			client := oauth2.NewClient(ctx, creds.TokenSource)
			client.Timeout = 20 * time.Second
			return notification.FCM{Project: get("FCM_PROJECT_ID"), Client: client}, nil
		},
		ai: func(_ context.Context, get func(string) string) (mealai.Provider, error) {
			key, err := app.ReadOpenAIKey(get("OPENAI_API_KEY_FILE"))
			if err != nil {
				return nil, err
			}
			model := get("OPENAI_MODEL")
			if model == "" {
				model = "gpt-5.6-terra"
			}
			return mealai.NewOpenAI(key, model, &http.Client{Timeout: 20 * time.Second}, "")
		},
	}
}
