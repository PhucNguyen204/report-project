package mealai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureKey = "fixture-key-only"

func finalMessage(text string) map[string]any {
	return map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}, "logprobs": []any{}}}}
}
func responseFixture(output ...any) map[string]any {
	return map[string]any{"id": "resp_fixture", "object": "response", "created_at": 1794643200, "model": "gpt-5.6-terra", "status": "completed", "error": nil, "incomplete_details": nil, "output": output, "metadata": map[string]any{}, "tools": []any{}, "parallel_tool_calls": false, "store": false}
}
func providerFixture(t *testing.T, handler http.HandlerFunc) Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	p, err := NewOpenAI(fixtureKey, "gpt-5.6-terra", server.Client(), server.URL+"/v1/")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenAISendsConstrainedStatelessRequest(t *testing.T) {
	var calls atomic.Int32
	p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer "+fixtureKey {
			t.Errorf("wrong request method/path/auth")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "gpt-5.6-terra" || body["store"] != false {
			t.Errorf("wrong model/store: %v", body)
		}
		if budget, ok := body["max_output_tokens"].(float64); !ok || budget < 1 || budget > 4096 {
			t.Errorf("unbounded output: %v", body["max_output_tokens"])
		}
		for _, forbidden := range []string{"tools", "previous_response_id", "conversation", "user", "metadata", "safety_identifier", "prompt_cache_key"} {
			if _, ok := body[forbidden]; ok {
				t.Errorf("unexpected data field %s", forbidden)
			}
		}
		var prompt map[string]any
		if s, ok := body["input"].(string); !ok {
			t.Error("input is not JSON data")
		} else if err := json.Unmarshal([]byte(s), &prompt); err != nil {
			t.Error(err)
		}
		if len(prompt) != 5 || prompt["date"] != "2026-11-14" || prompt["mealSlot"] != "dinner" || prompt["servings"] != float64(2) || prompt["limit"] != float64(2) {
			t.Errorf("unexpected prompt envelope: %v", prompt)
		}
		candidates, _ := prompt["candidates"].([]any)
		if len(candidates) != 2 {
			t.Errorf("candidate count: %v", candidates)
		} else {
			first, _ := candidates[0].(map[string]any)
			if len(first) != 5 || first["key"] != "r01" || first["name"] != "Canh cải" || first["coverage"] != float64(1) {
				t.Errorf("unexpected candidate: %v", first)
			}
		}
		instructions, _ := body["instructions"].(string)
		if !strings.Contains(instructions, "Vietnamese") || !strings.Contains(instructions, "untrusted") {
			t.Error("missing ranking/language/data-isolation instructions")
		}
		reasoning, _ := body["reasoning"].(map[string]any)
		if reasoning["effort"] != "none" {
			t.Error("reasoning consumes bounded response budget")
		}
		text, _ := body["text"].(map[string]any)
		format, _ := text["format"].(map[string]any)
		if format["type"] != "json_schema" || format["strict"] != true {
			t.Errorf("missing strict schema: %v", format)
		}
		schema, _ := format["schema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		choices, _ := props["choices"].(map[string]any)
		items, _ := choices["items"].(map[string]any)
		itemprops, _ := items["properties"].(map[string]any)
		key, _ := itemprops["key"].(map[string]any)
		if schema["additionalProperties"] != false || items["additionalProperties"] != false || !reflect.DeepEqual(schema["required"], []any{"choices"}) || !reflect.DeepEqual(items["required"], []any{"key", "reason"}) || !reflect.DeepEqual(key["enum"], []any{"r01", "r02"}) || choices["maxItems"] != float64(2) {
			t.Errorf("unsafe schema: %v", schema)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responseFixture(finalMessage(`{"choices":[{"key":"r02","reason":"Bổ sung trứng cho bữa tối"},{"key":"r01","reason":"Ưu tiên cải sắp hết hạn"}]}`)))
	})
	got, err := p.Recommend(context.Background(), validPrompt())
	want := []Choice{{Key: "r02", Reason: "Bổ sung trứng cho bữa tối"}, {Key: "r01", Reason: "Ưu tiên cải sắp hết hạn"}}
	if err != nil || !reflect.DeepEqual(got, want) || calls.Load() != 1 {
		t.Fatalf("got %v %v calls%d", got, err, calls.Load())
	}
}

func TestOpenAIRejectsUntrustedOutput(t *testing.T) {
	valid := `{"choices":[{"key":"r01","reason":"Đủ nguyên liệu"}]}`
	cases := []struct {
		name string
		body any
	}{
		{"unknown field", responseFixture(finalMessage(`{"choices":[{"key":"r01","reason":"A","stock":true}]}`))},
		{"unknown envelope", responseFixture(finalMessage(`{"choices":[{"key":"r01","reason":"A"}],"other":1}`))},
		{"invented alias", responseFixture(finalMessage(`{"choices":[{"key":"r99","reason":"A"}]}`))},
		{"duplicate alias", responseFixture(finalMessage(`{"choices":[{"key":"r01","reason":"A"},{"key":"r01","reason":"B"}]}`))},
		{"missing reason", responseFixture(finalMessage(`{"choices":[{"key":"r01"}]}`))},
		{"null choices", responseFixture(finalMessage(`{"choices":null}`))},
		{"malformed JSON", responseFixture(finalMessage(`{"choices":`))},
		{"trailing JSON", responseFixture(finalMessage(valid + `{}`))},
		{"multiple texts", responseFixture(finalMessage(valid), finalMessage(valid))},
		{"no output", responseFixture()},
		{"refusal", responseFixture(map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "refusal", "refusal": "fixture refusal"}}})},
		{"tool call", responseFixture(map[string]any{"type": "function_call", "id": "call_fixture", "name": "unsafe", "arguments": "{}"}, finalMessage(valid))},
		{"outer malformed", `not-json`},
		{"outer empty", ""},
		{"outer null", "null"},
	}
	incomplete := responseFixture(finalMessage(valid))
	incomplete["status"] = "incomplete"
	cases = append(cases, struct {
		name string
		body any
	}{"incomplete response", incomplete})
	failed := responseFixture(finalMessage(valid))
	failed["error"] = map[string]any{"code": "server_error", "message": "fixture secret"}
	cases = append(cases, struct {
		name string
		body any
	}{"response error", failed})
	message := finalMessage(valid)
	message["status"] = "incomplete"
	cases = append(cases, struct {
		name string
		body any
	}{"incomplete message", responseFixture(message)})
	message = finalMessage(valid)
	message["phase"] = "unrecognized"
	cases = append(cases, struct {
		name string
		body any
	}{"unknown phase", responseFixture(message)})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if s, ok := tc.body.(string); ok {
					_, _ = io.WriteString(w, s)
				} else {
					_ = json.NewEncoder(w).Encode(tc.body)
				}
			})
			if got, err := p.Recommend(context.Background(), validPrompt()); !errors.Is(err, ErrInvalidResponse) || got != nil {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func TestOpenAIUsesOnlyCompletedFinalMessage(t *testing.T) {
	commentary := finalMessage("working on it")
	commentary["phase"] = "commentary"
	final := finalMessage(`{"choices":[{"key":"r01","reason":"Đủ rau"}]}`)
	final["phase"] = "final_answer"
	p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(responseFixture(map[string]any{"type": "reasoning", "id": "rs_fixture", "summary": []any{}}, commentary, final))
	})
	if got, err := p.Recommend(context.Background(), validPrompt()); err != nil || len(got) != 1 || got[0].Reason != "Đủ rau" {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestOpenAIClassifiesFailuresWithoutRetryOrLeaking(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 408, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"fixture-private-prompt-and-key","type":"fixture_error","code":"bad"}}`)
			})
			_, err := p.Recommend(context.Background(), validPrompt())
			want := ErrRejected
			if status == 408 || status == 429 || status >= 500 {
				want = ErrUnavailable
			}
			if !errors.Is(err, want) || calls.Load() != 1 || strings.Contains(err.Error(), "fixture-") {
				t.Fatalf("got %v calls%d", err, calls.Load())
			}
		})
	}
}

func TestOpenAICancellationStopsWithoutRetry(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := p.Recommend(ctx, validPrompt()); result <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrUnavailable) || calls.Load() != 1 {
			t.Fatalf("got %v calls%d", err, calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop request")
	}
}

func TestOpenAIBoundsResponseBody(t *testing.T) {
	p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Repeat(" ", 256*1024)+`{}`)
	})
	if _, err := p.Recommend(context.Background(), validPrompt()); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenAIRejectsInvalidInputBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	p := providerFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) })
	in := validPrompt()
	in.Candidates[0].Key = "actual-recipe-id"
	if _, err := p.Recommend(context.Background(), in); !errors.Is(err, ErrRejected) || calls.Load() != 0 {
		t.Fatalf("got %v calls%d", err, calls.Load())
	}
}

func TestOpenAIRejectsUnsafeConfiguration(t *testing.T) {
	cases := []struct{ key, model, url string }{
		{"", "gpt-5-mini", ""}, {"bad\nkey", "gpt-5-mini", ""}, {"ok", "", ""}, {"ok", "model with spaces", ""},
		{"ok", "gpt-5-mini", "http://example.test/v1"}, {"ok", "gpt-5-mini", "https://user:password@example.test/v1"},
		{"ok", "gpt-5-mini", "https://example.test/v1?key=unsafe"}, {"ok", "gpt-5-mini", "https://example.test/v1#fragment"},
	}
	for i, tc := range cases {
		if p, err := NewOpenAI(tc.key, tc.model, nil, tc.url); p != nil || !errors.Is(err, ErrRejected) {
			t.Fatalf("case%d got %v %v", i, p, err)
		}
	}
}

type providerRoundTrip func(*http.Request) (*http.Response, error)

func (f providerRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failedProviderBody struct{}

func (failedProviderBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failedProviderBody) Close() error             { return nil }

func TestOpenAIIgnoresAmbientProviderConfiguration(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://unexpected.invalid/v1/")
	t.Setenv("OPENAI_API_KEY", "unexpected-key")
	t.Setenv("OPENAI_ORG_ID", "unexpected-org")
	t.Setenv("OPENAI_PROJECT_ID", "unexpected-project")
	t.Setenv("OPENAI_CUSTOM_HEADERS", "X-Unexpected: private-value")
	body, _ := json.Marshal(responseFixture(finalMessage(`{"choices":[{"key":"r01","reason":"Đủ rau"}]}`)))
	client := &http.Client{Transport: providerRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.openai.com/v1/responses" || r.Header.Get("Authorization") != "Bearer "+fixtureKey {
			t.Error("ambient origin/key changed outbound request")
		}
		if r.Header.Get("OpenAI-Organization") != "" || r.Header.Get("OpenAI-Project") != "" || r.Header.Get("X-Unexpected") != "" {
			t.Error("ambient identity/headers leaked")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 20*time.Second || time.Until(deadline) < 19*time.Second {
			t.Error("missing bounded provider deadline")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	p, err := NewOpenAI(fixtureKey, "gpt-5.6-terra", client, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Recommend(context.Background(), validPrompt()); err != nil || len(got) != 1 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestOpenAIRefusesRedirectDespiteCallerPolicy(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1); w.WriteHeader(500) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
	p, err := NewOpenAI(fixtureKey, "gpt-5.6-terra", client, server.URL+"/v1/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Recommend(context.Background(), validPrompt()); !errors.Is(err, ErrRejected) || destinationCalls.Load() != 0 {
		t.Fatalf("got %v destinationcalls%d", err, destinationCalls.Load())
	}
}

func TestOpenAISanitizesTransportAndBodyReadErrors(t *testing.T) {
	for _, readBody := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "body"}[readBody], func(t *testing.T) {
			client := &http.Client{Transport: providerRoundTrip(func(r *http.Request) (*http.Response, error) {
				if readBody {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: failedProviderBody{}}, nil
				}
				return nil, errors.New("fixture-private-network")
			})}
			p, err := NewOpenAI(fixtureKey, "gpt-5.6-terra", client, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Recommend(context.Background(), validPrompt()); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "fixture-") {
				t.Fatalf("got %v", err)
			}
		})
	}
}
