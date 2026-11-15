//go:build integration

package app

import (
	"context"
	"dicho/api"
	"dicho/internal/apicontract"
	"dicho/internal/modules/mealai"
	"dicho/internal/platform"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSystemAIUnavailableChecksSpaceBeforeConfiguration(t *testing.T) {
	sys, mail := systemFixture(t)
	owner, sid, _ := systemUser(t, sys, mail, "ai-owner@example.test")
	other, _, _ := systemUser(t, sys, mail, "ai-other@example.test")
	path := "/it4788/v1/spaces/" + sid + "/ai-recommendations"
	body := map[string]any{"date": sys.DB.Today().Format("2006-01-02"), "mealSlot": "dinner", "servings": 2, "limit": 3}
	status, _ := systemCall(t, sys, "POST", path, body, owner, platform.ID())
	if status != 503 {
		t.Fatalf("unconfigured provider HTTP%d, want503", status)
	}
	status, _ = systemCall(t, sys, "POST", path, body, other, platform.ID())
	if status != 404 {
		t.Fatalf("outsider discovered provider configuration: HTTP%d", status)
	}
}

func TestSystemAIWorkerCompletesWithHTTPProviderAndInbox(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/v1/responses" || body["store"] != false || r.Header.Get("Authorization") != "Bearer local-test-provider-token" || !strings.Contains(body["input"].(string), `"key":"r01"`) {
			t.Error("worker sent an unconstrained provider request")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "resp_fixture", "object": "response", "created_at": 1794643200, "model": "gpt-5.6-terra", "status": "completed", "error": nil, "incomplete_details": nil, "output": []any{map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": `{"choices":[{"key":"r01","reason":"Món rau phù hợp cho bữa tối."}]}`, "annotations": []any{}, "logprobs": []any{}}}}}, "metadata": map[string]any{}, "tools": []any{}, "parallel_tool_calls": false, "store": false})
	}))
	defer server.Close()
	provider, err := mealai.NewOpenAI("local-test-provider-token", "gpt-5.6-terra", server.Client(), server.URL+"/v1/")
	if err != nil {
		t.Fatal(err)
	}
	sys, mail := systemFixture(t, provider)
	contract, err := apicontract.New(api.Document)
	if err != nil {
		t.Fatal(err)
	}
	h := &apiHarness{system: sys, contract: contract}
	token, sid, user := systemUser(t, sys, mail, "ai-flow@example.test")
	status, out := systemCall(t, sys, "POST", "/it4788/v1/spaces/"+sid+"/recipes", map[string]any{"name": "Rau hấp", "servings": 2, "mealSlot": "dinner", "ingredients": []any{map[string]any{"foodId": "00000000-0000-7000-8000-000000000021", "unitId": "00000000-0000-7000-8000-000000000011", "quantity": 100}}, "steps": []string{"Hấp chín"}}, token, platform.ID())
	if status != 201 {
		t.Fatalf("recipe setup HTTP%d %v", status, out)
	}
	recipeID := out["data"].(map[string]any)["id"].(string)
	status, out = systemCall(t, sys, "POST", "/it4788/v1/spaces/"+sid+"/lots", map[string]any{"foodId": "00000000-0000-7000-8000-000000000021", "unitId": "00000000-0000-7000-8000-000000000011", "quantity": 200, "storageLocation": "chill", "purchasedOn": sys.DB.Today().Format("2006-01-02"), "expiresOn": sys.DB.Today().AddDate(0, 0, 3).Format("2006-01-02")}, token, platform.ID())
	if status != 201 {
		t.Fatalf("stock setup HTTP%d %v", status, out)
	}
	path := "/it4788/v1/spaces/" + sid + "/ai-recommendations"
	body := map[string]any{"date": sys.DB.Today().Format("2006-01-02"), "mealSlot": "dinner", "servings": 2, "limit": 1}
	out = h.call(t, "POST", path, body, token, "", 202)
	jobID := out["data"].(map[string]any)["jobId"].(string)
	deadline := time.Now().Add(8 * time.Second)
	for {
		out = h.call(t, "GET", path+"/"+jobID, nil, token, "", 200)
		job := out["data"].(map[string]any)
		var notices int
		if err := sys.DB.Pool.QueryRow(context.Background(), "SELECT count(*) FROM notification.notifications WHERE user_id=$1 AND dedupe_key=$2", user, "notification:ai.completed:"+jobID).Scan(&notices); err != nil {
			t.Fatal(err)
		}
		if job["status"] == "ready" && notices == 1 {
			items := job["items"].([]any)
			if len(items) != 1 || items[0].(map[string]any)["recipe"].(map[string]any)["id"] != recipeID || items[0].(map[string]any)["reason"] != "Món rau phù hợp cho bữa tối." || calls.Load() != 1 {
				t.Fatal("AI result did not preserve the validated recipe and single provider call")
			}
			break
		}
		if time.Now().After(deadline) {
			var states string
			if err := sys.DB.Pool.QueryRow(context.Background(), "SELECT coalesce(json_agg(json_build_object('event',args->>'event','state',state,'attempt',attempt,'errors',errors))::text,'[]') FROM river_job").Scan(&states); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("real worker did not complete AI+inbox: status%v notices%d calls%d events%s", job["status"], notices, calls.Load(), states)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
