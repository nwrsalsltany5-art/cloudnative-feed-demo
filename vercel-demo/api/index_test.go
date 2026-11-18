package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVisitorCookieIsSignedAndStable(t *testing.T) {
	app := &application{visitorSecret: []byte(strings.Repeat("x", 32))}
	first := app.signVisitor(strings.Repeat("a", 64))
	visitor, ok := app.parseVisitor(first)
	if !ok || visitor.ID != strings.Repeat("a", 64) || len(visitor.Key) != 64 {
		t.Fatalf("signed visitor did not validate: %#v, %v", visitor, ok)
	}
	again, ok := app.parseVisitor(first)
	if !ok || visitor.Key != again.Key {
		t.Fatalf("visitor key changed between requests")
	}
	last := byte('0')
	if first[len(first)-1] == last {
		last = '1'
	}
	if _, ok := app.parseVisitor(first[:len(first)-1] + string(last)); ok {
		t.Fatal("tampered visitor cookie validated")
	}
	if _, ok := app.parseVisitor("not-a-cookie"); ok {
		t.Fatal("malformed visitor cookie validated")
	}
}

func TestDecodeOneJSONRejectsUnknownAndTrailingValues(t *testing.T) {
	for _, body := range []string{
		`{"message":"valid","role":"admin"}`,
		`{"message":"valid"} {}`,
		`{"message":`,
	} {
		var payload struct {
			Message string `json:"message"`
		}
		if err := decodeOneJSON(strings.NewReader(body), &payload); err == nil {
			t.Errorf("decodeOneJSON accepted invalid input %q", body)
		}
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := decodeOneJSON(strings.NewReader(`{"message":"valid"}`), &payload); err != nil || payload.Message != "valid" {
		t.Fatalf("decode valid JSON: message=%q err=%v", payload.Message, err)
	}
}

func TestPublicDatabaseErrorsMapToSafeStatuses(t *testing.T) {
	tests := []struct {
		body   string
		status int
		want   int
	}{
		{`{"code":"P0002"}`, http.StatusBadGateway, http.StatusNotFound},
		{`{"code":"42501"}`, http.StatusBadGateway, http.StatusForbidden},
		{`{"code":"22023"}`, http.StatusBadGateway, http.StatusBadRequest},
		{`{"code":"23503"}`, http.StatusBadGateway, http.StatusNotFound},
		{`{"code":"XX000"}`, http.StatusBadGateway, http.StatusBadGateway},
	}
	for _, test := range tests {
		got := publicDatabaseErrorStatus([]byte(test.body), test.status, nil)
		if got != test.want {
			t.Errorf("publicDatabaseErrorStatus(%s) = %d, want %d", test.body, got, test.want)
		}
	}
	if got := publicDatabaseErrorStatus(nil, http.StatusBadGateway, http.ErrHandlerTimeout); got != http.StatusBadGateway {
		t.Fatalf("network error mapped to %d, want %d", got, http.StatusBadGateway)
	}
}

func TestFeedHandlerUsesServiceEndpointAndSetsVisitorCookie(t *testing.T) {
	supabase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/v1/rpc/get_feed":
			if r.Method != http.MethodPost || r.Header.Get("apikey") != "service-key" {
				t.Errorf("unexpected feed request: method=%s apikey=%q", r.Method, r.Header.Get("apikey"))
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "title": "Feed item", "image_url": "/static/images/photo-1.jpg", "likes_count": 2, "comments_count": 3, "liked": false},
			})
		case "/rest/v1/rpc/record_app_request":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer supabase.Close()
	t.Setenv("SUPABASE_URL", supabase.URL)
	t.Setenv("APP_BASE_URL", "http://localhost")
	t.Setenv("SUPABASE_ANON_KEY", "anon-key")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "service-key")
	t.Setenv("VISITOR_HMAC_SECRET", strings.Repeat("s", 32))
	t.Setenv("ADMIN_COOKIE_SECURE", "false")
	t.Setenv("VERCEL", "")

	request := httptest.NewRequest(http.MethodGet, "/api/feed", nil)
	response := httptest.NewRecorder()
	Handler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("feed status = %d, body = %s", response.Code, response.Body.String())
	}
	var photos []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &photos); err != nil || len(photos) != 1 {
		t.Fatalf("invalid feed response: photos=%v err=%v", photos, err)
	}
	cookie := response.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != visitorCookieName || !cookie[0].HttpOnly || cookie[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("visitor cookie flags are incorrect: %#v", cookie)
	}
}

func TestNewSupabaseKeysAreConfiguredAndSentWithoutBearer(t *testing.T) {
	requests := make(chan *http.Request, 3)
	supabase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer supabase.Close()

	t.Setenv("SUPABASE_URL", supabase.URL)
	t.Setenv("APP_BASE_URL", "https://demo.example")
	t.Setenv("SUPABASE_PUBLISHABLE_KEY", "sb_publishable_public")
	t.Setenv("SUPABASE_SECRET_KEY", "sb_secret_private")
	t.Setenv("SUPABASE_ANON_KEY", "legacy-anon")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "legacy-service")
	t.Setenv("VISITOR_HMAC_SECRET", strings.Repeat("s", 32))
	t.Setenv("ADMIN_COOKIE_SECURE", "true")

	app, err := newApplication()
	if err != nil {
		t.Fatalf("newApplication returned error: %v", err)
	}
	if app.anonKey != "sb_publishable_public" || app.serviceKey != "sb_secret_private" {
		t.Fatalf("new-format keys not selected: publishable=%q secret=%q", app.anonKey, app.serviceKey)
	}

	if _, _, err := app.supabase(context.Background(), http.MethodGet, "/rest/v1/test", nil, true, ""); err != nil {
		t.Fatalf("service request failed: %v", err)
	}
	if _, _, err := app.authRequest(context.Background(), http.MethodPost, "/auth/v1/token", nil, app.anonKey, app.anonKey, ""); err != nil {
		t.Fatalf("publishable auth request failed: %v", err)
	}
	if _, _, err := app.authRequest(context.Background(), http.MethodGet, "/auth/v1/user", nil, app.anonKey, "user-access-token", ""); err != nil {
		t.Fatalf("user auth request failed: %v", err)
	}

	serviceRequest := <-requests
	if serviceRequest.Header.Get("apikey") != "sb_secret_private" || serviceRequest.Header.Get("Authorization") != "" {
		t.Errorf("new secret key request headers are incorrect: apikey=%q authorization=%q", serviceRequest.Header.Get("apikey"), serviceRequest.Header.Get("Authorization"))
	}
	publishableRequest := <-requests
	if publishableRequest.Header.Get("apikey") != "sb_publishable_public" || publishableRequest.Header.Get("Authorization") != "" {
		t.Errorf("publishable-key request headers are incorrect: apikey=%q authorization=%q", publishableRequest.Header.Get("apikey"), publishableRequest.Header.Get("Authorization"))
	}
	userRequest := <-requests
	if userRequest.Header.Get("apikey") != "sb_publishable_public" || userRequest.Header.Get("Authorization") != "Bearer user-access-token" {
		t.Errorf("user-token request headers are incorrect: apikey=%q authorization=%q", userRequest.Header.Get("apikey"), userRequest.Header.Get("Authorization"))
	}
}

func TestNewSupabaseKeyPairMustBeComplete(t *testing.T) {
	t.Setenv("SUPABASE_URL", "http://127.0.0.1:54321")
	t.Setenv("APP_BASE_URL", "https://demo.example")
	t.Setenv("SUPABASE_PUBLISHABLE_KEY", "sb_publishable_public")
	t.Setenv("SUPABASE_SECRET_KEY", "")
	t.Setenv("SUPABASE_ANON_KEY", "legacy-anon")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "legacy-service")
	t.Setenv("VISITOR_HMAC_SECRET", strings.Repeat("s", 32))
	if _, err := newApplication(); err == nil {
		t.Fatal("incomplete new-format key pair unexpectedly fell back to legacy keys")
	}
}

func TestSameOriginRequiresMatchingSchemeAndHost(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://demo.example/admin/api/access", nil)
	request.Header.Set("Origin", "https://demo.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	app := &application{appOrigin: "https://demo.example"}
	if !app.sameOrigin(request) {
		t.Fatal("same-origin HTTPS request was rejected")
	}
	request.Header.Set("Origin", "https://attacker.example")
	if app.sameOrigin(request) {
		t.Fatal("cross-origin request was accepted")
	}
}

func TestPasswordUpdateRequiresMappedEnabledAdmin(t *testing.T) {
	updateCalls := 0
	enabled := true
	supabase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/auth/v1/user" && r.Method == http.MethodGet:
			if r.Header.Get("Authorization") != "Bearer recovery-token" {
				t.Errorf("password recovery token was not sent as bearer")
			}
			_, _ = w.Write([]byte(`{"id":"d5d9d573-4857-4a98-86df-238d365a2d15"}`))
		case r.URL.Path == "/rest/v1/admin_access":
			_ = json.NewEncoder(w).Encode([]adminProfile{{
				UserID:   "d5d9d573-4857-4a98-86df-238d365a2d15",
				Username: "drsura.dev", Role: "admin", Enabled: enabled,
			}})
		case r.URL.Path == "/auth/v1/user" && r.Method == http.MethodPut:
			updateCalls++
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["password"] != "new-password" {
				t.Errorf("unexpected password update payload")
			}
			if r.Header.Get("Authorization") != "Bearer recovery-token" {
				t.Errorf("password update did not use recovery token")
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer supabase.Close()
	app := &application{
		supabaseURL: supabase.URL, appOrigin: "https://demo.example",
		anonKey: "anon-key", serviceKey: "service-key",
		client: &http.Client{Timeout: time.Second},
	}
	request := httptest.NewRequest(http.MethodPost, "https://demo.example/admin/api/password-update",
		strings.NewReader(`{"access_token":"recovery-token","password":"new-password"}`))
	request.Header.Set("Origin", "https://demo.example")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.handlePasswordUpdate(response, request)
	if response.Code != http.StatusNoContent || updateCalls != 1 {
		t.Fatalf("password update status=%d calls=%d body=%s", response.Code, updateCalls, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "https://demo.example/admin/api/password-update",
		strings.NewReader(`{"access_token":"recovery-token","password":"new-password"}`))
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	app.handlePasswordUpdate(response, request)
	if response.Code != http.StatusForbidden || updateCalls != 1 {
		t.Fatalf("cross-origin update status=%d calls=%d", response.Code, updateCalls)
	}

	enabled = false
	request = httptest.NewRequest(http.MethodPost, "https://demo.example/admin/api/password-update",
		strings.NewReader(`{"access_token":"recovery-token","password":"new-password"}`))
	request.Header.Set("Origin", "https://demo.example")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	app.handlePasswordUpdate(response, request)
	if response.Code != http.StatusForbidden || updateCalls != 1 {
		t.Fatalf("disabled account update status=%d calls=%d", response.Code, updateCalls)
	}
}

func TestPasswordResetUsesConfiguredPasswordUpdateRedirect(t *testing.T) {
	expectedRedirect := "https://demo.example/admin/password-update"
	supabase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/v1/admin_access":
			_ = json.NewEncoder(w).Encode([]adminProfile{{
				UserID: "d5d9d573-4857-4a98-86df-238d365a2d15",
			}})
		case r.URL.Path == "/auth/v1/admin/users/d5d9d573-4857-4a98-86df-238d365a2d15":
			_, _ = w.Write([]byte(`{"email":"admin@example.test"}`))
		case r.URL.Path == "/auth/v1/recover" && r.Method == http.MethodPost:
			if r.URL.Query().Get("redirect_to") != expectedRedirect {
				t.Errorf("recovery redirect = %q", r.URL.Query().Get("redirect_to"))
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["email"] != "admin@example.test" {
				t.Errorf("unexpected recovery request")
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer supabase.Close()
	app := &application{
		supabaseURL: supabase.URL, anonKey: "anon-key", serviceKey: "service-key",
		client: &http.Client{Timeout: time.Second},
	}
	if err := app.sendPasswordReset(context.Background(), "drsura.dev", expectedRedirect); err != nil {
		t.Fatalf("sendPasswordReset returned error: %v", err)
	}
}

func TestSimulatedInfrastructureShowsLoadScaleAndRecovery(t *testing.T) {
	started := time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	run := simulationRun{
		StartedAt:    started,
		FinishedAt:   started.Add(90 * time.Second),
		DurationMS:   90000,
		SimulatedVUs: 10,
	}
	tests := []struct {
		name         string
		at           time.Time
		wantCPU      int
		wantRPS      int
		wantReplicas int
		wantUsers    int
		wantState    string
	}{
		{name: "baseline", at: started.Add(-time.Second), wantCPU: -1, wantReplicas: 1, wantState: "SIMULATED HPA: stable"},
		{name: "scale pressure", at: started.Add(10 * time.Second), wantCPU: 78, wantRPS: 12, wantReplicas: 1, wantUsers: 10, wantState: "SIMULATED HPA: scale-up pending"},
		{name: "scaling out", at: started.Add(30 * time.Second), wantCPU: 69, wantRPS: 28, wantReplicas: 2, wantUsers: 10, wantState: "SIMULATED HPA: scaling out"},
		{name: "capacity reached", at: started.Add(70 * time.Second), wantCPU: 52, wantRPS: 40, wantReplicas: 3, wantUsers: 10, wantState: "SIMULATED HPA: target capacity reached"},
		{name: "cooldown", at: started.Add(100 * time.Second), wantCPU: 31, wantRPS: 4, wantReplicas: 2, wantState: "SIMULATED HPA: cooling down"},
		{name: "recovered", at: started.Add(121 * time.Second), wantCPU: -1, wantReplicas: 1, wantState: "SIMULATED HPA: stable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics := simulatedInfrastructureAt(test.at, []simulationRun{run})
			if (test.wantCPU >= 0 && metrics["cpu_percent"] != test.wantCPU) || metrics["requests_per_second"] != test.wantRPS ||
				metrics["replicas"] != test.wantReplicas || metrics["virtual_users"] != test.wantUsers ||
				metrics["autoscaling"] != test.wantState {
				t.Fatalf("unexpected simulation at %s: %#v", test.at, metrics)
			}
			if metrics["label"] != "SIMULATED — no Vercel or Kubernetes load generated" {
				t.Fatalf("simulation source label is missing: %#v", metrics["label"])
			}
		})
	}
}

func TestSimulatedInfrastructureHistoryHasOneHourOfMinutePoints(t *testing.T) {
	now := time.Date(2026, time.November, 18, 12, 10, 30, 0, time.UTC)
	history := simulatedInfrastructureHistory(now, nil)
	if len(history) != 61 {
		t.Fatalf("simulation history points = %d, want 61", len(history))
	}
	if history[0].Timestamp != now.UTC().Truncate(time.Minute).Add(-time.Hour).UnixMilli() ||
		history[len(history)-1].Timestamp != now.UTC().Truncate(time.Minute).UnixMilli() {
		t.Fatalf("unexpected simulation history boundaries: first=%d last=%d", history[0].Timestamp, history[len(history)-1].Timestamp)
	}
}

func TestSimulationRunStatusReportsDurationAndCooldown(t *testing.T) {
	now := time.Date(2026, time.November, 18, 12, 0, 30, 0, time.UTC)
	run := simulationRun{
		StartedAt:  now.Add(-10 * time.Second),
		FinishedAt: now.Add(80 * time.Second),
	}
	view := simulationRunStatus(run, now)
	if view.State != "running" || view.SecondsRemaining != 80 || view.CooldownRemaining != 110 {
		t.Fatalf("unexpected active simulation status: %+v", view)
	}
	view = simulationRunStatus(run, now.Add(100*time.Second))
	if view.State != "completed" || view.SecondsRemaining != 0 || view.CooldownRemaining != 10 {
		t.Fatalf("unexpected cooldown status: %+v", view)
	}
}

func TestAdminSessionRequiresEnabledMappedUser(t *testing.T) {
	enabled := false
	supabase := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v1/user":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Errorf("unexpected auth bearer: %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"id":"d5d9d573-4857-4a98-86df-238d365a2d15"}`))
		case "/rest/v1/admin_access":
			if r.Header.Get("apikey") != "service-key" {
				t.Errorf("admin lookup did not use server service key")
			}
			_ = json.NewEncoder(w).Encode([]adminProfile{{
				UserID: "d5d9d573-4857-4a98-86df-238d365a2d15", Username: "drsura.dev",
				Role: "admin", Enabled: enabled, CanLoadTest: true,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer supabase.Close()
	app := &application{
		supabaseURL: supabase.URL, anonKey: "anon-key", serviceKey: "service-key",
		client: &http.Client{Timeout: time.Second},
	}
	request := httptest.NewRequest(http.MethodGet, "https://demo.example/admin/api/session", nil)
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: "access-token"})
	if _, err := app.requireAdmin(request); err == nil {
		t.Fatal("disabled user was authorized")
	}
	enabled = true
	session, err := app.requireAdmin(request)
	if err != nil || session.Profile.Username != "drsura.dev" || !session.Profile.CanLoadTest {
		t.Fatalf("enabled admin session = %#v, err=%v", session, err)
	}
	if _, err := app.profileForUser(context.Background(), "not-a-uuid"); err != nil {
		t.Fatalf("profile lookup did not return configured record: %v", err)
	}
}

func TestOnlyExpectedAdminsCanBeChanged(t *testing.T) {
	for _, username := range []string{"drsura.dev", "admin2.dev", "admin3.dev"} {
		if !isSeedAdmin(username) {
			t.Errorf("expected admin %q was rejected", username)
		}
	}
	for _, username := range []string{"g95rr", "other-admin", ""} {
		if isSeedAdmin(username) {
			t.Errorf("unexpected admin %q was accepted", username)
		}
	}
}
