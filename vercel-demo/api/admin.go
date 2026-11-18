package handler

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

//go:embed admin.html admin-login.html admin.js password-update.html password-update.js
var adminFiles embed.FS

var errAdminProfileNotFound = errors.New("admin profile not found")
var errAdminSessionRequired = errors.New("admin session is missing or invalid")

type adminBackendError struct {
	err error
}

func (e *adminBackendError) Error() string {
	return e.err.Error()
}

func (e *adminBackendError) Unwrap() error {
	return e.err
}

type authUser struct {
	ID string `json:"id"`
}

type adminProfile struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	Enabled     bool   `json:"enabled"`
	CanLoadTest bool   `json:"can_load_test"`
}

type adminSession struct {
	Token   string
	Profile adminProfile
}

func (a *application) handleAdmin(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/admin/assets/admin.js":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(mustReadEmbedded("admin.js"))
	case "/admin/login":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(mustReadEmbedded("admin-login.html"))
	case "/admin/password-update":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(mustReadEmbedded("password-update.html"))
	case "/admin/assets/password-update.js":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(mustReadEmbedded("password-update.js"))
	case "/admin":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		if _, err := a.requireAdmin(r); errors.Is(err, errAdminSessionRequired) {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		} else if err != nil {
			http.Error(w, "Admin service is temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(mustReadEmbedded("admin.html"))
	case "/admin/api/login":
		a.handleAdminLogin(w, r)
	case "/admin/api/password-update":
		a.handlePasswordUpdate(w, r)
	case "/admin/api/logout":
		a.handleAdminLogout(w, r)
	case "/admin/api/session":
		a.handleAdminSession(w, r)
	case "/admin/api/metrics":
		a.handleAdminMetrics(w, r)
	case "/admin/api/access":
		a.handleAdminAccess(w, r)
	case "/admin/api/load-test":
		a.handleLoadSimulation(w, r)
	default:
		http.NotFound(w, r)
	}
}

func mustReadEmbedded(name string) []byte {
	value, err := adminFiles.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("read embedded admin asset %q: %v", name, err))
	}
	return value
}

func (a *application) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !a.sameOrigin(r) {
		http.Error(w, "Origin check failed", http.StatusForbidden)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeOneJSON(r.Body, &payload); err != nil || len(payload.Email) > 320 || len(payload.Password) > 1024 || strings.TrimSpace(payload.Email) == "" || payload.Password == "" {
		http.Error(w, "Enter a valid email address and password", http.StatusBadRequest)
		return
	}
	body, _ := json.Marshal(map[string]string{"email": strings.TrimSpace(payload.Email), "password": payload.Password})
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	response, status, err := a.authRequest(ctx, http.MethodPost, "/auth/v1/token?grant_type=password", body, a.anonKey, a.anonKey, "application/json")
	if err != nil || status < 200 || status >= 300 {
		http.Error(w, "Sign-in failed. Check the credentials and try again.", http.StatusUnauthorized)
		return
	}
	var result struct {
		AccessToken string   `json:"access_token"`
		ExpiresIn   float64  `json:"expires_in"`
		User        authUser `json:"user"`
	}
	if err := json.Unmarshal(response, &result); err != nil || result.AccessToken == "" || result.User.ID == "" {
		log.Printf("Supabase returned an incomplete sign-in response")
		http.Error(w, "Unable to complete sign-in", http.StatusBadGateway)
		return
	}
	profile, err := a.profileForUser(ctx, result.User.ID)
	if errors.Is(err, errAdminProfileNotFound) || (err == nil && (!profile.Enabled || (profile.Role != "owner" && profile.Role != "admin"))) {
		http.Error(w, "This account is not authorized for the operations panel", http.StatusForbidden)
		return
	}
	if err != nil {
		log.Printf("verify Vercel admin access failed: %v", err)
		http.Error(w, "Unable to verify admin access", http.StatusServiceUnavailable)
		return
	}
	maxAge := int(result.ExpiresIn)
	if maxAge <= 0 || maxAge > 3600 {
		maxAge = 3600
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminCookieName, Value: result.AccessToken, Path: adminCookiePath,
		HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteStrictMode,
		MaxAge: maxAge,
	})
	writeJSON(w, http.StatusOK, map[string]string{"username": profile.Username, "role": profile.Role})
}

func (a *application) handlePasswordUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !a.sameOrigin(r) {
		http.Error(w, "Origin check failed", http.StatusForbidden)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	var payload struct {
		AccessToken string `json:"access_token"`
		Password    string `json:"password"`
	}
	if err := decodeOneJSON(r.Body, &payload); err != nil || len(payload.AccessToken) > 4096 ||
		len(payload.Password) < 8 || len(payload.Password) > 1024 {
		http.Error(w, "Invalid password update request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	userBody, status, err := a.authRequest(ctx, http.MethodGet, "/auth/v1/user", nil, a.anonKey, payload.AccessToken, "")
	if err != nil || status != http.StatusOK {
		http.Error(w, "Password link is invalid or expired", http.StatusUnauthorized)
		return
	}
	var user authUser
	if err := json.Unmarshal(userBody, &user); err != nil || user.ID == "" {
		http.Error(w, "Unable to verify password link", http.StatusBadGateway)
		return
	}
	profile, err := a.profileForUser(ctx, user.ID)
	if errors.Is(err, errAdminProfileNotFound) || (err == nil && (!profile.Enabled || (profile.Role != "owner" && profile.Role != "admin"))) {
		http.Error(w, "This account is not authorized for the operations panel", http.StatusForbidden)
		return
	}
	if err != nil {
		log.Printf("verify account for password update failed: %v", err)
		http.Error(w, "Unable to verify password link", http.StatusServiceUnavailable)
		return
	}
	updateBody, _ := json.Marshal(map[string]string{"password": payload.Password})
	_, status, err = a.authRequest(ctx, http.MethodPut, "/auth/v1/user", updateBody, a.anonKey, payload.AccessToken, "application/json")
	if err != nil {
		log.Printf("Supabase password update request failed: %v", err)
		http.Error(w, "Unable to update password", http.StatusBadGateway)
		return
	}
	if status < 200 || status >= 300 {
		http.Error(w, "Password was rejected by the configured Auth policy", http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *application) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !a.sameOrigin(r) {
		http.Error(w, "Origin check failed", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: adminCookieName, Value: "", Path: adminCookiePath,
		HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteStrictMode,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *application) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	session, err := a.requireAdmin(r)
	if err != nil {
		writeAdminAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":      session.Profile.Username,
		"role":          session.Profile.Role,
		"is_owner":      session.Profile.Role == "owner",
		"can_load_test": session.Profile.Role == "owner" || session.Profile.CanLoadTest,
	})
}

func (a *application) requireAdmin(r *http.Request) (adminSession, error) {
	cookie, err := r.Cookie(adminCookieName)
	if err != nil || cookie.Value == "" || len(cookie.Value) > 4096 {
		return adminSession{}, errAdminSessionRequired
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	body, status, err := a.authRequest(ctx, http.MethodGet, "/auth/v1/user", nil, a.anonKey, cookie.Value, "")
	if err != nil {
		return adminSession{}, &adminBackendError{err: fmt.Errorf("verify Supabase session: %w", err)}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return adminSession{}, errAdminSessionRequired
	}
	if status != http.StatusOK {
		return adminSession{}, &adminBackendError{err: fmt.Errorf("Supabase session check returned status %d", status)}
	}
	var user authUser
	if err := json.Unmarshal(body, &user); err != nil || user.ID == "" {
		return adminSession{}, &adminBackendError{err: errors.New("Supabase returned an invalid user")}
	}
	profile, err := a.profileForUser(ctx, user.ID)
	if errors.Is(err, errAdminProfileNotFound) {
		return adminSession{}, errAdminSessionRequired
	}
	if err != nil {
		return adminSession{}, err
	}
	if !profile.Enabled || (profile.Role != "owner" && profile.Role != "admin") {
		return adminSession{}, errAdminSessionRequired
	}
	return adminSession{Token: cookie.Value, Profile: profile}, nil
}

func (a *application) profileForUser(ctx context.Context, userID string) (adminProfile, error) {
	query := url.Values{}
	query.Set("select", "user_id,username,role,enabled,can_load_test")
	query.Set("user_id", "eq."+userID)
	query.Set("limit", "1")
	body, status, err := a.supabase(ctx, http.MethodGet, "/rest/v1/admin_access?"+query.Encode(), nil, true, "")
	if err != nil || status != http.StatusOK {
		return adminProfile{}, &adminBackendError{err: responseError("load admin access", status, err)}
	}
	var profiles []adminProfile
	if err := json.Unmarshal(body, &profiles); err != nil {
		return adminProfile{}, &adminBackendError{err: fmt.Errorf("decode admin access: %w", err)}
	}
	if len(profiles) == 0 {
		return adminProfile{}, errAdminProfileNotFound
	}
	if len(profiles) != 1 {
		return adminProfile{}, &adminBackendError{err: errors.New("Supabase returned multiple admin profiles")}
	}
	return profiles[0], nil
}

func writeAdminAuthError(w http.ResponseWriter, err error) {
	var backendError *adminBackendError
	if errors.As(err, &backendError) {
		http.Error(w, "Admin service is temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	http.Error(w, "Sign-in required", http.StatusUnauthorized)
}

func responseError(action string, status int, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s returned status %d", action, status)
}

func (a *application) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, err := a.requireAdmin(r); err != nil {
		writeAdminAuthError(w, err)
		return
	}
	end := time.Now().UTC().Truncate(time.Minute)
	start := end.Add(-time.Hour)
	query := url.Values{}
	query.Set("select", "bucket_start,request_count,error_count,total_latency_ms,comment_count,photo_like_count,comment_like_count")
	query.Set("bucket_start", "gte."+start.Format(time.RFC3339))
	query.Set("order", "bucket_start.asc")
	query.Set("limit", "61")
	body, status, err := a.supabase(r.Context(), http.MethodGet, "/rest/v1/usage_minutes?"+query.Encode(), nil, true, "")
	if err != nil || status != http.StatusOK {
		log.Printf("load Vercel metrics failed (status %d): %v", status, err)
		http.Error(w, "Unable to load application usage history", http.StatusBadGateway)
		return
	}
	var buckets []usageBucket
	if err := json.Unmarshal(body, &buckets); err != nil {
		log.Printf("decode Vercel metrics failed: %v", err)
		http.Error(w, "Unable to load application usage history", http.StatusBadGateway)
		return
	}
	points := make([]usagePoint, 0, 61)
	for cursor := start; !cursor.After(end); cursor = cursor.Add(time.Minute) {
		points = append(points, usagePoint{Timestamp: cursor.UnixMilli()})
	}
	index := make(map[int64]int, len(points))
	for i := range points {
		index[points[i].Timestamp/1000] = i
	}
	for _, bucket := range buckets {
		pointIndex, ok := index[bucket.BucketStart.Unix()]
		if !ok {
			continue
		}
		points[pointIndex].Requests = bucket.RequestCount
		points[pointIndex].Errors = bucket.ErrorCount
		points[pointIndex].Latency = averageLatency(bucket)
		points[pointIndex].Comments = bucket.CommentCount
		points[pointIndex].PhotoLikes = bucket.PhotoLikeCount
		points[pointIndex].CommentLikes = bucket.CommentLikeCount
	}
	runs, err := a.simulationRuns(r.Context(), end.Add(-time.Hour), 100)
	if err != nil {
		log.Printf("load simulated infrastructure history failed: %v", err)
		http.Error(w, "Unable to load simulated infrastructure history", http.StatusBadGateway)
		return
	}
	simulation := simulatedInfrastructureAt(time.Now().UTC(), runs)
	writeJSON(w, http.StatusOK, map[string]any{
		"collected_at":       time.Now().UTC(),
		"range_start":        start,
		"source":             "real application-level measurements; infrastructure figures below are simulated",
		"points":             points,
		"simulation":         simulation,
		"simulation_history": simulatedInfrastructureHistory(time.Now().UTC(), runs),
	})
}

type usageBucket struct {
	BucketStart      time.Time `json:"bucket_start"`
	RequestCount     int       `json:"request_count"`
	ErrorCount       int       `json:"error_count"`
	TotalLatencyMS   int64     `json:"total_latency_ms"`
	CommentCount     int       `json:"comment_count"`
	PhotoLikeCount   int       `json:"photo_like_count"`
	CommentLikeCount int       `json:"comment_like_count"`
}

type usagePoint struct {
	Timestamp    int64 `json:"timestamp"`
	Requests     int   `json:"requests"`
	Errors       int   `json:"errors"`
	Latency      int64 `json:"latency_ms"`
	Comments     int   `json:"comments"`
	PhotoLikes   int   `json:"photo_likes"`
	CommentLikes int   `json:"comment_likes"`
}

type simulationRun struct {
	ID            int64     `json:"id"`
	ActorUsername string    `json:"actor_username"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	DurationMS    int       `json:"duration_ms"`
	SimulatedVUs  int       `json:"simulated_vus"`
	Result        string    `json:"result"`
}

type simulationRunView struct {
	simulationRun
	State             string `json:"state"`
	SecondsRemaining  int    `json:"seconds_remaining"`
	CooldownRemaining int    `json:"cooldown_remaining"`
}

type simulatedInfrastructurePoint struct {
	Timestamp         int64 `json:"timestamp"`
	RequestsPerSecond int   `json:"requests_per_second"`
	CPUPercent        int   `json:"cpu_percent"`
	MemoryMB          int   `json:"memory_mb"`
	Replicas          int   `json:"replicas"`
}

func averageLatency(bucket usageBucket) int64 {
	if bucket.RequestCount == 0 {
		return 0
	}
	return bucket.TotalLatencyMS / int64(bucket.RequestCount)
}

func (a *application) simulationRuns(ctx context.Context, since time.Time, limit int) ([]simulationRun, error) {
	query := url.Values{}
	query.Set("select", "id,actor_username,started_at,finished_at,duration_ms,simulated_vus,result")
	query.Set("started_at", "gte."+since.UTC().Format(time.RFC3339))
	query.Set("order", "started_at.desc")
	query.Set("limit", strconv.Itoa(limit))
	body, status, err := a.supabase(ctx, http.MethodGet, "/rest/v1/demo_load_runs?"+query.Encode(), nil, true, "")
	if err != nil || status != http.StatusOK {
		return nil, responseError("load simulated runs", status, err)
	}
	var runs []simulationRun
	if err := json.Unmarshal(body, &runs); err != nil {
		return nil, fmt.Errorf("decode simulated runs: %w", err)
	}
	return runs, nil
}

func simulationRunStatus(run simulationRun, now time.Time) simulationRunView {
	view := simulationRunView{simulationRun: run, State: "completed"}
	remaining := run.FinishedAt.Sub(now)
	if remaining > 0 {
		view.State = "running"
		view.SecondsRemaining = int((remaining + time.Second - 1) / time.Second)
	}
	cooldown := run.StartedAt.Add(120 * time.Second).Sub(now)
	if cooldown > 0 {
		view.CooldownRemaining = int((cooldown + time.Second - 1) / time.Second)
	}
	return view
}

func simulatedInfrastructureAt(at time.Time, runs []simulationRun) map[string]any {
	minute := at.Unix() / 60
	cpu := 18 + int((minute*7)%7)
	memory := 256 + int((minute%3)*16)
	replicas := 1
	requestsPerSecond := 0
	autoscaling := "SIMULATED HPA: stable"
	loading := false
	for _, run := range runs {
		elapsed := at.Sub(run.StartedAt)
		if elapsed < 0 {
			continue
		}
		switch {
		case elapsed < 20*time.Second:
			cpu, memory, replicas, requestsPerSecond, autoscaling, loading = 78, 352, 1, 12, "SIMULATED HPA: scale-up pending", true
		case elapsed < 60*time.Second:
			cpu, memory, replicas, requestsPerSecond, autoscaling, loading = 69, 448, 2, 28, "SIMULATED HPA: scaling out", true
		case elapsed < 90*time.Second:
			cpu, memory, replicas, requestsPerSecond, autoscaling, loading = 52, 512, 3, 40, "SIMULATED HPA: target capacity reached", true
		case elapsed < 120*time.Second:
			cpu, memory, replicas, requestsPerSecond, autoscaling = 31, 384, 2, 4, "SIMULATED HPA: cooling down"
		}
		if elapsed < 120*time.Second {
			break
		}
	}
	alerts := 0
	if cpu >= 75 {
		alerts = 1
	}
	virtualUsers := 0
	if loading {
		virtualUsers = 10
	}
	return map[string]any{
		"label":               "SIMULATED — no Vercel or Kubernetes load generated",
		"cpu_percent":         cpu,
		"memory_mb":           memory,
		"replicas":            replicas,
		"requests_per_second": requestsPerSecond,
		"autoscaling":         autoscaling,
		"alerts":              alerts,
		"load_active":         loading,
		"virtual_users":       virtualUsers,
	}
}

func simulatedInfrastructureHistory(now time.Time, runs []simulationRun) []simulatedInfrastructurePoint {
	end := now.UTC().Truncate(time.Minute)
	start := end.Add(-time.Hour)
	points := make([]simulatedInfrastructurePoint, 0, 61)
	for at := start; !at.After(end); at = at.Add(time.Minute) {
		metrics := simulatedInfrastructureAt(at, runs)
		points = append(points, simulatedInfrastructurePoint{
			Timestamp:         at.UnixMilli(),
			RequestsPerSecond: metrics["requests_per_second"].(int),
			CPUPercent:        metrics["cpu_percent"].(int),
			MemoryMB:          metrics["memory_mb"].(int),
			Replicas:          metrics["replicas"].(int),
		})
	}
	return points
}

func (a *application) handleAdminAccess(w http.ResponseWriter, r *http.Request) {
	session, err := a.requireAdmin(r)
	if err != nil {
		writeAdminAuthError(w, err)
		return
	}
	if session.Profile.Role != "owner" {
		http.Error(w, "Owner permission required", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		query := url.Values{}
		query.Set("select", "user_id,username,role,enabled,can_load_test")
		query.Set("role", "eq.admin")
		query.Set("order", "username.asc")
		body, status, err := a.supabase(r.Context(), http.MethodGet, "/rest/v1/admin_access?"+query.Encode(), nil, true, "")
		if err != nil || status != http.StatusOK {
			log.Printf("load Vercel admin access list failed (status %d): %v", status, err)
			http.Error(w, "Unable to load admin access", http.StatusBadGateway)
			return
		}
		var admins []adminProfile
		if err := json.Unmarshal(body, &admins); err != nil || !hasAllSeedAdmins(admins) {
			log.Printf("Vercel admin access list is incomplete or invalid")
			http.Error(w, "Admin account setup is incomplete", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(body)
	case http.MethodPost:
		if !a.sameOrigin(r) {
			http.Error(w, "Origin check failed", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		var payload struct {
			Username string `json:"username"`
			Action   string `json:"action"`
			Enabled  *bool  `json:"enabled"`
		}
		if err := decodeOneJSON(r.Body, &payload); err != nil {
			http.Error(w, "Invalid access request", http.StatusBadRequest)
			return
		}
		if !isSeedAdmin(payload.Username) {
			http.Error(w, "Unknown admin username", http.StatusBadRequest)
			return
		}
		query := url.Values{}
		query.Set("username", "eq."+payload.Username)
		switch payload.Action {
		case "account", "load_test":
			if payload.Enabled == nil {
				http.Error(w, "An enabled value is required", http.StatusBadRequest)
				return
			}
			field := "enabled"
			if payload.Action == "load_test" {
				field = "can_load_test"
			}
			body, _ := json.Marshal(map[string]bool{field: *payload.Enabled})
			response, status, err := a.supabase(r.Context(), http.MethodPatch, "/rest/v1/admin_access?"+query.Encode(), body, true, "application/json")
			if err != nil || status < 200 || status >= 300 {
				log.Printf("update Vercel admin access failed (status %d): %v", status, err)
				http.Error(w, "Unable to update admin access", http.StatusBadGateway)
				return
			}
			if strings.TrimSpace(string(response)) == "[]" {
				http.Error(w, "Admin account is not configured", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "password_reset":
			if err := a.sendPasswordReset(r.Context(), payload.Username, a.appOrigin+"/admin/password-update"); err != nil {
				log.Printf("send Vercel password reset failed: %v", err)
				http.Error(w, "Unable to start password reset", http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "Unsupported access action", http.StatusBadRequest)
		}
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func isSeedAdmin(username string) bool {
	switch username {
	case "drsura.dev", "admin2.dev", "admin3.dev":
		return true
	default:
		return false
	}
}

func hasAllSeedAdmins(admins []adminProfile) bool {
	if len(admins) != 3 {
		return false
	}
	found := make(map[string]bool, len(admins))
	for _, admin := range admins {
		if !isSeedAdmin(admin.Username) || admin.Role != "admin" || found[admin.Username] {
			return false
		}
		found[admin.Username] = true
	}
	return len(found) == 3
}

func (a *application) sendPasswordReset(ctx context.Context, username, redirectURL string) error {
	query := url.Values{}
	query.Set("select", "user_id")
	query.Set("username", "eq."+username)
	query.Set("role", "eq.admin")
	body, status, err := a.supabase(ctx, http.MethodGet, "/rest/v1/admin_access?"+query.Encode(), nil, true, "")
	if err != nil || status != http.StatusOK {
		return responseError("look up admin account", status, err)
	}
	var profiles []adminProfile
	if err := json.Unmarshal(body, &profiles); err != nil || len(profiles) != 1 {
		return errors.New("admin account is not configured")
	}
	userBody, status, err := a.authRequest(ctx, http.MethodGet, "/auth/v1/admin/users/"+url.PathEscape(profiles[0].UserID), nil, a.serviceKey, a.serviceKey, "")
	if err != nil || status != http.StatusOK {
		return responseError("look up admin email", status, err)
	}
	var user struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(userBody, &user); err != nil || user.Email == "" {
		return errors.New("admin recovery email is unavailable")
	}
	payload, _ := json.Marshal(map[string]string{"email": user.Email})
	redirect := url.Values{}
	redirect.Set("redirect_to", redirectURL)
	_, status, err = a.authRequest(ctx, http.MethodPost, "/auth/v1/recover?"+redirect.Encode(), payload, a.anonKey, a.anonKey, "application/json")
	if err != nil || status < 200 || status >= 300 {
		return responseError("request provider recovery email", status, err)
	}
	return nil
}

func (a *application) handleLoadSimulation(w http.ResponseWriter, r *http.Request) {
	session, err := a.requireAdmin(r)
	if err != nil {
		writeAdminAuthError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		runs, err := a.simulationRuns(r.Context(), time.Now().UTC().Add(-30*24*time.Hour), 10)
		if err != nil {
			log.Printf("load demo simulation history failed: %v", err)
			http.Error(w, "Unable to load demo simulation history", http.StatusBadGateway)
			return
		}
		history := make([]simulationRunView, 0, len(runs))
		for _, run := range runs {
			history = append(history, simulationRunStatus(run, time.Now().UTC()))
		}
		writeJSON(w, http.StatusOK, history)
	case http.MethodPost:
		if !a.sameOrigin(r) {
			http.Error(w, "Origin check failed", http.StatusForbidden)
			return
		}
		if session.Profile.Role != "owner" && !session.Profile.CanLoadTest {
			http.Error(w, "Demo simulation permission required", http.StatusForbidden)
			return
		}
		if !a.allowAction(w, r, session.Profile.UserID, "demo_simulation", 1) {
			return
		}
		payload, _ := json.Marshal(map[string]string{"p_actor_username": session.Profile.Username})
		body, status, err := a.supabase(r.Context(), http.MethodPost, "/rest/v1/rpc/run_demo_simulation", payload, true, "application/json")
		if err != nil || status < 200 || status >= 300 {
			if bytes.Contains(body, []byte("cooldown")) {
				http.Error(w, "A demo simulation is cooling down; wait before running it again", http.StatusTooManyRequests)
				return
			}
			log.Printf("run bounded demo simulation failed (status %d): %v", status, err)
			http.Error(w, "Unable to run demo simulation", http.StatusBadGateway)
			return
		}
		var run simulationRun
		if err := json.Unmarshal(body, &run); err != nil || run.ID == 0 || run.SimulatedVUs != 10 {
			log.Printf("Supabase returned an invalid simulated run record")
			http.Error(w, "Unable to start demo simulation", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusAccepted, simulationRunStatus(run, time.Now().UTC()))
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (a *application) authRequest(ctx context.Context, method, path string, body []byte, apiKey, bearer, contentType string) ([]byte, int, error) {
	endpoint, err := url.Parse(a.supabaseURL + path)
	if err != nil {
		return nil, 0, fmt.Errorf("parse Supabase Auth endpoint: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create Supabase Auth request: %w", err)
	}
	request.Header.Set("apikey", apiKey)
	request.Header.Set("Authorization", "Bearer "+bearer)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := a.client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("request Supabase Auth: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read Supabase Auth response: %w", err)
	}
	return responseBody, response.StatusCode, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write Vercel JSON response failed: %v", err)
	}
}
