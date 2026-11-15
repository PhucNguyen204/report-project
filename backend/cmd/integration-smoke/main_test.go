package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"dicho/internal/modules/mealai"
	"dicho/internal/modules/notification"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type sendFunc func(context.Context, string, string, string) error

func (f sendFunc) Send(ctx context.Context, to, title, body string) error {
	return f(ctx, to, title, body)
}

type aiFunc func(context.Context, mealai.Prompt) ([]mealai.Choice, error)

func (f aiFunc) Recommend(ctx context.Context, p mealai.Prompt) ([]mealai.Choice, error) {
	return f(ctx, p)
}

func smokeResult(t *testing.T, args []string, env map[string]string, factories providerFactories) (int, map[string]any, string) {
	t.Helper()
	var output bytes.Buffer
	code := run(args, func(key string) string { return env[key] }, factories, &output)
	var report map[string]any
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("invalid report: %v", err)
	}
	return code, report, output.String()
}

func statusOf(t *testing.T, report map[string]any, provider string) string {
	t.Helper()
	providers := report["providers"].(map[string]any)
	return providers[provider].(map[string]any)["status"].(string)
}

func configuredEnvironment() map[string]string {
	return map[string]string{"SMTP_ADDR": "smtp.synthetic.test:587", "SMTP_FROM": "from@synthetic.test", "SMOKE_EMAIL_TO": "to@synthetic.test", "FCM_PROJECT_ID": "synthetic-project", "FCM_CREDENTIALS_FILE": "PRIVATE-credentials.json", "SMOKE_FCM_TOKEN": "PRIVATE-device-token", "OPENAI_API_KEY_FILE": "PRIVATE-api-key", "OPENAI_MODEL": "configured-model"}
}

func TestDefaultAndMissingConfigurationsNeverConstructNetworkProviders(t *testing.T) {
	for _, tc := range []struct {
		args []string
		env  map[string]string
	}{
		{nil, configuredEnvironment()},
		{[]string{"--mail", "--push", "--ai"}, map[string]string{}},
	} {
		factories := providerFactories{
			mail: func(context.Context, func(string) string) (notification.EmailSender, error) {
				t.Fatal("mail was not enabled/configured")
				return nil, nil
			},
			push: func(context.Context, func(string) string) (notification.PushSender, error) {
				t.Fatal("push was not enabled/configured")
				return nil, nil
			},
			ai: func(context.Context, func(string) string) (mealai.Provider, error) {
				t.Fatal("AI was not enabled/configured")
				return nil, nil
			},
		}
		code, report, text := smokeResult(t, tc.args, tc.env, factories)
		if code != 0 || statusOf(t, report, "mail") != "NOT_RUN" || statusOf(t, report, "push") != "NOT_RUN" || statusOf(t, report, "ai") != "NOT_RUN" || strings.Contains(text, "PRIVATE") {
			t.Fatal("missing or disabled provider reported success or exposed configuration")
		}
	}
}

func TestConfiguredFailuresAreRedactedAndDoNotPreventOtherChecks(t *testing.T) {
	env := configuredEnvironment()
	factories := providerFactories{
		mail: func(context.Context, func(string) string) (notification.EmailSender, error) {
			return nil, errors.New("PRIVATE-key-and-destination")
		},
		push: func(context.Context, func(string) string) (notification.PushSender, error) {
			return sendFunc(func(ctx context.Context, token, title, body string) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded push context")
				}
				return errors.New("PRIVATE-provider-response")
			}), nil
		},
		ai: func(context.Context, func(string) string) (mealai.Provider, error) {
			return aiFunc(func(context.Context, mealai.Prompt) ([]mealai.Choice, error) {
				return []mealai.Choice{{Key: "r99", Reason: "PRIVATE-invalid-result"}}, nil
			}), nil
		},
	}
	code, report, text := smokeResult(t, []string{"--mail", "--push", "--ai"}, env, factories)
	if code == 0 || statusOf(t, report, "mail") != "FAIL" || statusOf(t, report, "push") != "FAIL" || statusOf(t, report, "ai") != "FAIL" || strings.Contains(text, "PRIVATE") || strings.Contains(text, env["SMOKE_EMAIL_TO"]) {
		t.Fatal("provider failure was hidden, short-circuited or echoed")
	}
}

func TestSelectedPushAndAIUseRealAdaptersAgainstLocalHTTP(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" {
			t.Error("smoke request was not POST")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case "/v1/projects/synthetic-project/messages:send":
			if body["message"].(map[string]any)["token"] != "PRIVATE-device-token" {
				t.Error("wrong explicit synthetic destination")
			}
			requests <- "push"
			_, _ = fmt.Fprint(w, `{"name":"projects/synthetic-project/messages/fixture"}`)
		case "/v1/responses":
			if r.Header.Get("Authorization") != "Bearer PRIVATE-api-key" || body["model"] != "configured-model" || body["store"] != false {
				t.Error("wrong model/auth/storage configuration")
			}
			var prompt mealai.Prompt
			if err := json.Unmarshal([]byte(body["input"].(string)), &prompt); err != nil {
				t.Error(err)
			}
			if len(prompt.Candidates) != 1 || prompt.Candidates[0].Key != "r01" || prompt.Limit != 1 || prompt.Servings != 1 {
				t.Error("unbounded or non-synthetic smoke prompt")
			}
			requests <- "ai"
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "resp_fixture", "object": "response", "status": "completed", "output": []any{map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": `{"choices":[{"key":"r01","reason":"Món thử nghiệm phù hợp"}]}`}}}}})
		default:
			t.Errorf("unexpected network destination %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	factories := providerFactories{
		mail: func(context.Context, func(string) string) (notification.EmailSender, error) {
			t.Fatal("unrequested mail provider constructed")
			return nil, nil
		},
		push: func(_ context.Context, get func(string) string) (notification.PushSender, error) {
			return notification.FCM{Project: get("FCM_PROJECT_ID"), Client: server.Client(), BaseURL: server.URL}, nil
		},
		ai: func(_ context.Context, get func(string) string) (mealai.Provider, error) {
			return mealai.NewOpenAI("PRIVATE-api-key", get("OPENAI_MODEL"), server.Client(), server.URL+"/v1/")
		},
	}
	code, report, text := smokeResult(t, []string{"--push", "--ai"}, configuredEnvironment(), factories)
	if code != 0 || statusOf(t, report, "mail") != "NOT_RUN" || statusOf(t, report, "push") != "PASS" || statusOf(t, report, "ai") != "PASS" || strings.Contains(text, "PRIVATE") || len(requests) != 2 {
		t.Fatal("local adapter checks failed or leaked provider data")
	}
}

func localSMTP(t *testing.T) (string, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- ""
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		var transcript strings.Builder
		_, _ = fmt.Fprint(conn, "220 synthetic ESMTP\r\n")
		reader := bufio.NewReader(conn)
		defer func() { done <- transcript.String() }()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			transcript.WriteString(line)
			switch {
			case strings.HasPrefix(line, "EHLO"):
				_, _ = fmt.Fprint(conn, "250 synthetic\r\n")
			case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				_, _ = fmt.Fprint(conn, "250 ok\r\n")
			case strings.HasPrefix(line, "DATA"):
				_, _ = fmt.Fprint(conn, "354 continue\r\n")
				for {
					part, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					transcript.WriteString(part)
					if part == ".\r\n" {
						break
					}
				}
				_, _ = fmt.Fprint(conn, "250 accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				_, _ = fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				_, _ = fmt.Fprint(conn, "500 unknown\r\n")
			}
		}
	}()
	return listener.Addr().String(), done
}

func TestSMTPCheckCanRunControlledFixtureAndLiveFactoryRequiresTLS(t *testing.T) {
	for _, tlsRequired := range []bool{false, true} {
		address, done := localSMTP(t)
		env := map[string]string{"SMTP_ADDR": address, "SMTP_FROM": "from@synthetic.test", "SMOKE_EMAIL_TO": "to@synthetic.test"}
		factories := liveFactories()
		if !tlsRequired {
			factories.mail = func(_ context.Context, get func(string) string) (notification.EmailSender, error) {
				return notification.SMTP{Addr: get("SMTP_ADDR"), From: get("SMTP_FROM")}, nil
			}
		}
		code, report, text := smokeResult(t, []string{"--mail"}, env, factories)
		transcript := <-done
		if tlsRequired {
			if code == 0 || statusOf(t, report, "mail") != "FAIL" || strings.Contains(transcript, "MAIL FROM") {
				t.Fatal("live SMTP silently sent without STARTTLS")
			}
		} else if code != 0 || statusOf(t, report, "mail") != "PASS" || !strings.Contains(transcript, "RCPT TO:<to@synthetic.test>") || !strings.Contains(transcript, "synthetic") {
			t.Fatal("controlled SMTP check did not deliver its explicit synthetic envelope")
		}
		if strings.Contains(text, "synthetic.test") {
			t.Fatal("SMTP destination echoed")
		}
	}
}

func TestInvalidArgumentsAreRedactedBeforeAnyProviderCanRun(t *testing.T) {
	code, report, text := smokeResult(t, []string{"--PRIVATE-secret"}, configuredEnvironment(), providerFactories{})
	if code == 0 || report["error"] != "invalid_arguments" || strings.Contains(text, "PRIVATE") {
		t.Fatal("invalid argument was echoed or accepted")
	}
}

func TestEachMissingProviderSettingPreventsDispatchAndNamesOnlyTheSetting(t *testing.T) {
	for _, tc := range []struct {
		provider, flag string
		keys           []string
	}{
		{"mail", "--mail", []string{"SMTP_ADDR", "SMTP_FROM", "SMOKE_EMAIL_TO"}},
		{"push", "--push", []string{"FCM_PROJECT_ID", "FCM_CREDENTIALS_FILE", "SMOKE_FCM_TOKEN"}},
		{"ai", "--ai", []string{"OPENAI_API_KEY_FILE"}},
	} {
		for _, missing := range tc.keys {
			t.Run(missing, func(t *testing.T) {
				env := configuredEnvironment()
				delete(env, missing)
				code, report, text := smokeResult(t, []string{tc.flag}, env, providerFactories{})
				entry := report["providers"].(map[string]any)[tc.provider].(map[string]any)
				keys, ok := entry["missing"].([]any)
				if code != 0 || statusOf(t, report, tc.provider) != "NOT_RUN" || !ok || len(keys) != 1 || keys[0] != missing || strings.Contains(text, "PRIVATE") {
					t.Fatal("missing setting did not prevent dispatch or reported sensitive values")
				}
			})
		}
	}
	for _, missing := range []string{"SMTP_USER", "SMTP_PASSWORD"} {
		env := configuredEnvironment()
		env["SMTP_USER"], env["SMTP_PASSWORD"] = "PRIVATE-user", "PRIVATE-password"
		delete(env, missing)
		code, report, text := smokeResult(t, []string{"--mail"}, env, providerFactories{})
		if code != 0 || statusOf(t, report, "mail") != "NOT_RUN" || strings.Contains(text, "PRIVATE") {
			t.Fatal("partial SMTP authentication was dispatched or echoed")
		}
	}
}

func TestInvalidPrivateCredentialFilesFailWithoutEchoingFileOrKey(t *testing.T) {
	env := configuredEnvironment()
	path := filepath.Join(t.TempDir(), "PRIVATE-api-key")
	if err := os.WriteFile(path, []byte("PRIVATE-invalid-credential-content"), 0644); err != nil {
		t.Fatal(err)
	}
	env["OPENAI_API_KEY_FILE"], env["FCM_CREDENTIALS_FILE"] = path, path
	code, report, text := smokeResult(t, []string{"--push", "--ai"}, env, liveFactories())
	if code != 1 || statusOf(t, report, "push") != "FAIL" || statusOf(t, report, "ai") != "FAIL" || strings.Contains(text, "PRIVATE") {
		t.Fatal("invalid private credential reported success or leaked file details")
	}
}

type smokeTransport func(*http.Request) (*http.Response, error)

func (f smokeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLiveFactoriesLoadPrivateFilesAndSendOnlyToFixedEndpoints(t *testing.T) {
	env := configuredEnvironment()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	credentials, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "fixture@unit-test.iam.gserviceaccount.com", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": "https://oauth2.googleapis.com/token"})
	for name, content := range map[string][]byte{"fcm": credentials, "ai": []byte("PRIVATE-synthetic-api-key-only")} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
		if name == "fcm" {
			env["FCM_CREDENTIALS_FILE"] = path
		} else {
			env["OPENAI_API_KEY_FILE"] = path
		}
	}
	delete(env, "OPENAI_MODEL")
	seen := make(chan string, 3)
	transport := smokeTransport(func(r *http.Request) (*http.Response, error) {
		var text string
		switch r.URL.String() {
		case "https://oauth2.googleapis.com/token":
			if r.Method != "POST" || r.ParseForm() != nil || r.Form.Get("assertion") == "" {
				t.Error("typed FCM credential did not produce token assertion")
			}
			seen <- "oauth"
			text = `{"access_token":"PRIVATE-synthetic-token","token_type":"Bearer","expires_in":3600}`
		case "https://fcm.googleapis.com/v1/projects/synthetic-project/messages:send":
			if r.Header.Get("Authorization") != "Bearer PRIVATE-synthetic-token" {
				t.Error("FCM did not use scoped token")
			}
			seen <- "fcm"
			text = `{"name":"projects/synthetic-project/messages/fixture"}`
		case "https://api.openai.com/v1/responses":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if r.Header.Get("Authorization") != "Bearer PRIVATE-synthetic-api-key-only" || request["model"] != "gpt-5.6-terra" || request["store"] != false {
				t.Error("live AI factory lost explicit key, default model or stateless setting")
			}
			seen <- "ai"
			text = `{"id":"resp_fixture","object":"response","status":"completed","output":[{"id":"msg_fixture","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"{\"choices\":[{\"key\":\"r01\",\"reason\":\"Món thử nghiệm\"}]}"}]}]}`
		default:
			t.Errorf("unexpected destination %s", r.URL.Host)
			return nil, errors.New("unexpected fixture request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(text))}, nil
	})
	original := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	factories := liveFactories()
	livePush := factories.push
	factories.push = func(ctx context.Context, get func(string) string) (notification.PushSender, error) {
		return livePush(context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Transport: transport}), get)
	}
	code, report, text := smokeResult(t, []string{"--push", "--ai"}, env, factories)
	if code != 0 || statusOf(t, report, "push") != "PASS" || statusOf(t, report, "ai") != "PASS" || len(seen) != 3 || strings.Contains(text, "PRIVATE") || strings.Contains(text, "unit-test") {
		t.Fatal("private file factories failed or exposed credentials")
	}
}

type failedOutput struct{}

func (failedOutput) Write([]byte) (int, error) { return 0, errors.New("PRIVATE-output-error") }

func TestCannotReportSuccessfulExitWhenJSONOutputFails(t *testing.T) {
	if code := run(nil, func(string) string { return "" }, providerFactories{}, failedOutput{}); code == 0 {
		t.Fatal("failed report write returned successful exit")
	}
}
