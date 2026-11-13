//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"dicho/api"
	"dicho/internal/apicontract"
	"dicho/internal/modules/media"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type apiMail struct{ to, body string }
type apiInbox struct{ messages chan apiMail }

func (m *apiInbox) Send(_ context.Context, to, subject, body string) error {
	m.messages <- apiMail{to, body}
	return nil
}

type apiHarness struct {
	system   *System
	contract *apicontract.Contract
	mail     *apiInbox
}

func newAPIHarness(t *testing.T) *apiHarness {
	t.Helper()
	ctx := context.Background()
	db := testutil.NewDB(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("dicho-test", "dicho-local-storage-test", "")}, func(o *s3.Options) { o.BaseEndpoint = aws.String(testutil.S3Endpoint()); o.UsePathStyle = true })
	mediaBucket := "api-media-" + platform.ID()
	exportBucket := "api-exports-" + platform.ID()
	for _, bucket := range []string{mediaBucket, exportBucket} {
		if _, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket}); err != nil {
			t.Fatalf("real S3 required for contract exchanges: %v", err)
		}
	}
	mail := &apiInbox{messages: make(chan apiMail, 20)}
	sys, err := NewSystem(ctx, db, Options{SigningKey: key, QueueKey: bytes.Repeat([]byte{9}, 32), MediaStore: media.NewS3(client, mediaBucket), ExportStore: media.NewS3(client, exportBucket), Email: mail, HTTP: Config{MinimumAppVersion: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := sys.Worker.Stop(stopCtx); err != nil {
			t.Error(err)
		}
		for _, bucket := range []string{mediaBucket, exportBucket} {
			objects, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: &bucket})
			if err != nil {
				t.Error(err)
				continue
			}
			for _, object := range objects.Contents {
				if _, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: object.Key}); err != nil {
					t.Error(err)
				}
			}
			if _, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: &bucket}); err != nil {
				t.Error(err)
			}
		}
	})
	if err = sys.Worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	contract, err := apicontract.New(api.Document)
	if err != nil {
		t.Fatal(err)
	}
	return &apiHarness{sys, contract, mail}
}
func (h *apiHarness) call(t *testing.T, method, path string, body any, token, match string, status int) map[string]any {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.Header.Set("X-App-Version", "1.0.0")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Idempotency-Key", platform.ID())
	if match != "" {
		r.Header.Set("If-Match", match)
	}
	if status < 400 {
		if err = h.contract.Request(r, raw); err != nil {
			t.Fatalf("%s %s request contract: %v", method, path, err)
		}
	}
	w := httptest.NewRecorder()
	h.system.Handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, w.Code, status, w.Body)
	}
	if err = h.contract.Response(method, path, w.Code, w.Header(), w.Body.Bytes()); err != nil {
		t.Fatalf("%s %s response contract: %v\n%s", method, path, err, w.Body)
	}
	var out map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func (h *apiHarness) user(t *testing.T, email string) (string, string, string) {
	t.Helper()
	h.call(t, "POST", "/it4788/v1/auth/register", map[string]any{"fullName": "Nguyễn An", "email": email, "password": "Password123", "acceptTerms": true}, "", "", 201)
	var code string
	select {
	case message := <-h.mail.messages:
		if message.to != email {
			t.Fatalf("unexpected OTP recipient %s", message.to)
		}
		code = regexp.MustCompile(`\b[0-9]{6}\b`).FindString(message.body)
	case <-time.After(8 * time.Second):
		t.Fatal("transactional OTP worker did not deliver")
	}
	out := h.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": email, "code": code, "purpose": "register"}, "", "", 200)["data"].(map[string]any)
	return out["accessToken"].(string), out["spaceId"].(string), out["userId"].(string)
}
func field(out map[string]any, name string) string {
	return out["data"].(map[string]any)[name].(string)
}

func TestOpenAPIOperationInventoryMatchesActualSystemMux(t *testing.T) {
	h := newAPIHarness(t)
	for _, op := range h.contract.Operations() {
		path := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(op.Path, "00000000-0000-7000-8000-000000000001")
		req := httptest.NewRequest(op.Method, path, nil)
		_, pattern := h.system.Mux.Handler(req)
		if pattern != op.Method+" "+op.Path {
			t.Errorf("%s %s documented as %s but mux matched %q", op.Method, op.Path, op.APICode, pattern)
		}
	}
}

func TestOpenAPIRealHTTPExchangesMatchTypedSchemas(t *testing.T) {
	h := newAPIHarness(t)
	ctx := context.Background()
	token, sid, user := h.user(t, "owner@contract.test")
	base := "/it4788/v1/spaces/" + sid
	today := h.system.DB.Today().Format("2006-01-02")
	food := "00000000-0000-7000-8000-000000000021"
	unit := "00000000-0000-7000-8000-000000000012"
	h.call(t, "GET", "/healthz", nil, "", "", 200)
	h.call(t, "GET", "/readyz", nil, "", "", 200)
	h.call(t, "GET", "/it4788/v1/me", nil, token, "", 200)
	h.call(t, "PATCH", "/it4788/v1/me", map[string]any{"fullName": "Nguyễn An mới", "locale": "vi"}, token, "1", 200)
	h.call(t, "GET", "/it4788/v1/me/settings", nil, token, "", 200)
	h.call(t, "PUT", "/it4788/v1/me/settings", map[string]any{"reminderDays": 3, "expiryNotifyOn": true}, token, "", 200)
	device := h.call(t, "POST", "/it4788/v1/me/devices", map[string]any{"fcmToken": "contract-device-token", "platform": "android"}, token, "", 200)
	h.call(t, "DELETE", "/it4788/v1/me/devices/"+field(device, "deviceId"), nil, token, "", 200)
	login := h.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": "owner@contract.test", "password": "Password123", "platform": "android"}, "", "", 200)["data"].(map[string]any)
	h.call(t, "POST", "/it4788/v1/auth/refresh", map[string]any{"refreshToken": login["refreshToken"], "deviceId": login["deviceId"]}, "", "", 200)
	h.call(t, "GET", "/it4788/v1/catalog", nil, token, "", 200)
	h.call(t, "GET", "/it4788/v1/spaces", nil, token, "", 200)
	group := h.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Gia đình hợp đồng"}, token, "", 201)
	gid := field(group, "id")
	h.call(t, "GET", "/it4788/v1/spaces/"+gid, nil, token, "", 200)
	invite := h.call(t, "POST", "/it4788/v1/spaces/"+gid+"/invites", nil, token, "", 201)
	other, _, _ := h.user(t, "member@contract.test")
	h.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": field(invite, "code"), "dryRun": true}, other, "", 200)
	h.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": field(invite, "code")}, other, "", 200)
	noticeID := platform.ID()
	if _, err := h.system.DB.Pool.Exec(ctx, "INSERT INTO notification.notifications(id,user_id,dedupe_key,title,body,type) VALUES($1,$2,$3,'Nhắc thực phẩm','Cà rốt sắp hết hạn','expiry')", noticeID, user, "contract:"+noticeID); err != nil {
		t.Fatal(err)
	}
	h.call(t, "GET", "/it4788/v1/me/notifications", nil, token, "", 200)
	h.call(t, "POST", "/it4788/v1/me/notifications/read", map[string]any{"ids": []string{noticeID}}, token, "", 200)
	lot := h.call(t, "POST", base+"/lots", map[string]any{"foodId": food, "unitId": unit, "quantity": "1", "storageLocation": "chill", "purchasedOn": today, "expiresOn": h.system.DB.Today().AddDate(0, 0, 3).Format("2006-01-02")}, token, "", 201)
	lotID := field(lot, "id")
	h.call(t, "GET", base+"/lots?limit=10&location=chill", nil, token, "", 200)
	h.call(t, "GET", base+"/lots/"+lotID, nil, token, "", 200)
	h.call(t, "GET", base+"/lots/expiring", nil, token, "", 200)
	list := h.call(t, "POST", base+"/shopping-lists", map[string]any{"name": "Đi chợ tuần này", "plannedDate": today}, token, "", 201)
	listID := field(list, "id")
	h.call(t, "PATCH", base+"/shopping-lists/"+listID, map[string]any{"notes": "Mua rau tươi"}, token, "1", 200)
	item := h.call(t, "POST", base+"/shopping-lists/"+listID+"/items", map[string]any{"foodId": food, "unitId": unit, "quantity": .3}, token, "", 201)
	itemID := field(item, "id")
	h.call(t, "GET", base+"/shopping-lists", nil, token, "", 200)
	h.call(t, "GET", base+"/shopping-lists/"+listID, nil, token, "", 200)
	h.call(t, "POST", base+"/shopping-items/purchase", map[string]any{"items": []any{map[string]any{"itemId": itemID, "unitPrice": 12000}}}, token, "", 200)
	h.call(t, "POST", base+"/shopping-items/stock", map[string]any{"items": []any{map[string]any{"itemId": itemID, "storageLocation": "chill", "expiresOn": h.system.DB.Today().AddDate(0, 0, 3).Format("2006-01-02")}}}, token, "", 200)
	recipeIn := map[string]any{"name": "Canh cà rốt", "servings": 1, "ingredients": []any{map[string]any{"foodId": food, "unitId": unit, "quantity": "0.2"}}}
	recipe := h.call(t, "POST", base+"/recipes", recipeIn, token, "", 201)
	rid := field(recipe, "id")
	h.call(t, "GET", base+"/recipes", nil, token, "", 200)
	h.call(t, "GET", base+"/recipes/"+rid+"?servings=2", nil, token, "", 200)
	h.call(t, "PUT", "/it4788/v1/me/favorite-recipes/"+rid, nil, token, "", 200)
	h.call(t, "POST", base+"/recipes/"+rid+"/missing-to-list", map[string]any{"servings": 1, "dryRun": true}, token, "", 200)
	meal := h.call(t, "POST", base+"/meals", map[string]any{"recipeId": rid, "date": today, "mealSlot": "lunch", "servings": 1}, token, "", 201)
	mid := field(meal, "id")
	h.call(t, "PATCH", base+"/meals/"+mid, map[string]any{"servings": 2}, token, "1", 200)
	h.call(t, "GET", base+"/meals?from="+today+"&to="+today, nil, token, "", 200)
	h.call(t, "POST", base+"/shopping-lists/generate", map[string]any{"from": today, "to": today, "dryRun": true}, token, "", 200)
	h.call(t, "GET", base+"/recipe-suggestions?servings=1&limit=3", nil, token, "", 200)
	h.call(t, "POST", base+"/meals/"+mid+"/cook", map[string]any{}, token, "2", 200)
	h.call(t, "GET", base+"/search?q=ca%20rot", nil, token, "", 200)
	h.call(t, "GET", base+"/foods/suggest?q=ca", nil, token, "", 200)
	for _, kind := range []string{"spending", "consumption", "waste"} {
		h.call(t, "GET", base+"/reports/"+kind+"?from="+today+"&to="+today, nil, token, "", 200)
	}
	exported := h.call(t, "POST", base+"/reports/exports", map[string]any{"reportType": "consumption", "from": today, "to": today, "format": "csv"}, token, "", 202)
	h.call(t, "GET", base+"/reports/exports/"+field(exported, "exportId"), nil, token, "", 200)
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	grant := h.call(t, "POST", "/it4788/v1/media/upload-urls", map[string]any{"purpose": "recipe", "targetId": rid, "contentType": "image/png", "size": picture.Len()}, token, "", 201)["data"].(map[string]any)
	put, err := http.NewRequest("PUT", grant["uploadUrl"].(string), bytes.NewReader(picture.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range grant["headers"].(map[string]any) {
		put.Header.Set(k, v.(string))
	}
	uploaded, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, uploaded.Body)
	uploaded.Body.Close()
	if uploaded.StatusCode != 200 {
		t.Fatalf("real signed S3 PUT%d", uploaded.StatusCode)
	}
	objectID := grant["objectId"].(string)
	h.call(t, "POST", "/it4788/v1/media/"+objectID+"/complete", nil, token, "", 200)
	recipeIn["imageObjectId"] = objectID
	h.call(t, "PUT", base+"/recipes/"+rid, recipeIn, token, "1", 200)
	h.call(t, "GET", "/it4788/v1/media/"+objectID, nil, token, "", 200)
	h.call(t, "GET", base+"/lots/"+platform.ID(), nil, other, "", 404)
	h.call(t, "POST", base+"/meals/"+mid+"/cook", map[string]any{}, token, "1", 200)
	// A real response is mutated to prove the schema catches removed domain fields.
	bad := recipe["data"].(map[string]any)
	delete(bad, "id")
	raw, _ := json.Marshal(recipe)
	if err = h.contract.Response("POST", base+"/recipes", 201, nil, raw); err == nil {
		t.Fatal("missing recipe.id escaped response contract")
	}
	admin, _, adminID := h.user(t, "admin@contract.test")
	_ = admin
	if _, err = h.system.DB.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", adminID); err != nil {
		t.Fatal(err)
	}
	adminLogin := h.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": "admin@contract.test", "password": "Password123"}, "", "", 200)
	adminToken := field(adminLogin, "accessToken")
	for _, path := range []string{"/it4788/v1/admin/users", "/it4788/v1/admin/food-categories", "/it4788/v1/admin/units", "/it4788/v1/admin/foods", "/it4788/v1/admin/configs", "/it4788/v1/admin/audit-logs", "/it4788/v1/admin/recipe-templates"} {
		h.call(t, "GET", path, nil, adminToken, "", 200)
	}
	h.call(t, "PUT", "/it4788/v1/admin/configs", map[string]any{"values": map[string]any{"suggest.theta": .7}}, adminToken, "", 200)
	h.call(t, "POST", "/it4788/v1/admin/recipe-templates", recipeInWithoutPhoto(recipeIn), adminToken, "", 201)
	h.call(t, "POST", "/it4788/v1/auth/logout", nil, token, "", 200)
	_ = user
}
func recipeInWithoutPhoto(source map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range source {
		if k != "imageObjectId" {
			out[k] = v
		}
	}
	out["status"] = "published"
	return out
}

func TestOpenAPIPublicDocumentAndErrorResponses(t *testing.T) {
	h := newAPIHarness(t)
	req := httptest.NewRequest("GET", "/openapi.json", nil)
	if err := h.contract.Request(req, nil); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.system.Handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("public contract%d %s", w.Code, w.Body)
	}
	if err := h.contract.Response("GET", "/openapi.json", 200, w.Header(), w.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		headers map[string]string
		status  int
	}{{nil, 400}, {map[string]string{"X-App-Version": "0.1.0"}, 426}, {map[string]string{"X-App-Version": "1.0.0"}, 401}} {
		r := httptest.NewRequest("GET", "/it4788/v1/me", nil)
		for k, v := range tc.headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.system.Handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("gateway status%d want%d", w.Code, tc.status)
		}
		if err := h.contract.Response(r.Method, r.URL.Path, w.Code, w.Header(), w.Body.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
}
