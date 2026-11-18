package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	visitorCookieName = "feed_visitor"
	adminCookieName   = "feed_admin"
	visitorCookiePath = "/"
	adminCookiePath   = "/admin"
	maxRequestBytes   = 4096
)

type application struct {
	supabaseURL   string
	appOrigin     string
	anonKey       string
	serviceKey    string
	visitorSecret []byte
	cookieSecure  bool
	client        *http.Client
}

type comment struct {
	ID         int       `json:"id"`
	Author     string    `json:"author"`
	Message    string    `json:"message"`
	LikesCount int       `json:"likes_count"`
	CreatedAt  time.Time `json:"created_at"`
	Liked      bool      `json:"liked"`
}

type visitor struct {
	ID  string
	Key string
}

func Handler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w, r)
	route := strings.TrimSpace(r.URL.Query().Get("route"))
	if route != "" {
		targetURL := *r.URL
		targetURL.Path = route
		targetURL.RawQuery = ""
		r = r.Clone(r.Context())
		r.URL = &targetURL
	}
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	app, err := newApplication()
	if err != nil {
		http.Error(w, "Application configuration is incomplete", http.StatusServiceUnavailable)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		visitor, err := app.ensureVisitor(w, r)
		if err != nil {
			http.Error(w, "Unable to initialize visitor session", http.StatusInternalServerError)
			return
		}
		recorder := &statusRecorder{ResponseWriter: w}
		started := time.Now()
		app.handlePublic(recorder, r, visitor)
		app.recordRequest(r, recorder.status, time.Since(started))
		return
	}

	if strings.HasPrefix(r.URL.Path, "/admin/api/") {
		recorder := &statusRecorder{ResponseWriter: w}
		started := time.Now()
		app.handleAdmin(recorder, r)
		app.recordRequest(r, recorder.status, time.Since(started))
		return
	}
	app.handleAdmin(w, r)
}

func newApplication() (*application, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPABASE_URL")), "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1"))) {
		return nil, errors.New("SUPABASE_URL must be an HTTPS URL")
	}
	if parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("SUPABASE_URL must be an origin without credentials, path, query, or fragment")
	}
	appOrigin := strings.TrimRight(strings.TrimSpace(os.Getenv("APP_BASE_URL")), "/")
	originURL, err := url.Parse(appOrigin)
	if err != nil || originURL.Host == "" || originURL.User != nil || originURL.Path != "" || originURL.RawQuery != "" || originURL.Fragment != "" ||
		(originURL.Scheme != "https" && !(originURL.Scheme == "http" && (originURL.Hostname() == "localhost" || originURL.Hostname() == "127.0.0.1"))) {
		return nil, errors.New("APP_BASE_URL must be an HTTPS origin (HTTP is allowed only for localhost)")
	}
	anonKey := strings.TrimSpace(os.Getenv("SUPABASE_PUBLISHABLE_KEY"))
	serviceKey := strings.TrimSpace(os.Getenv("SUPABASE_SECRET_KEY"))
	if anonKey == "" && serviceKey == "" {
		anonKey = strings.TrimSpace(os.Getenv("SUPABASE_ANON_KEY"))
		serviceKey = strings.TrimSpace(os.Getenv("SUPABASE_SERVICE_ROLE_KEY"))
	} else if anonKey == "" || serviceKey == "" {
		return nil, errors.New("both SUPABASE_PUBLISHABLE_KEY and SUPABASE_SECRET_KEY are required")
	}
	visitorSecret := os.Getenv("VISITOR_HMAC_SECRET")
	if anonKey == "" || serviceKey == "" || len(visitorSecret) < 32 {
		return nil, errors.New("Supabase keys and a 32-byte visitor HMAC secret are required")
	}
	cookieSecure := true
	if value := strings.TrimSpace(os.Getenv("ADMIN_COOKIE_SECURE")); value != "" {
		cookieSecure, err = strconv.ParseBool(value)
		if err != nil {
			return nil, errors.New("ADMIN_COOKIE_SECURE must be true or false")
		}
	}
	if originURL.Scheme == "https" && !cookieSecure {
		return nil, errors.New("ADMIN_COOKIE_SECURE must be true on Vercel")
	}
	return &application{
		supabaseURL: base, appOrigin: appOrigin, anonKey: anonKey, serviceKey: serviceKey,
		visitorSecret: []byte(visitorSecret), cookieSecure: cookieSecure,
		client: &http.Client{Timeout: 6 * time.Second},
	}, nil
}

func setSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; script-src 'self'; connect-src 'self'; img-src 'self' https://images.unsplash.com data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
	w.Header().Set("Cache-Control", "no-store")
}

func (a *application) sameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	expected, err := url.Parse(a.appOrigin)
	if err != nil || r.Host == "" {
		return false
	}
	return strings.EqualFold(origin.Scheme, expected.Scheme) &&
		strings.EqualFold(origin.Host, expected.Host) &&
		strings.EqualFold(r.Host, expected.Host)
}

func (a *application) ensureVisitor(w http.ResponseWriter, r *http.Request) (visitor, error) {
	if cookie, err := r.Cookie(visitorCookieName); err == nil {
		if visitor, ok := a.parseVisitor(cookie.Value); ok {
			return visitor, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return visitor{}, fmt.Errorf("generate visitor id: %w", err)
	}
	id := hex.EncodeToString(raw)
	signed := a.signVisitor(id)
	http.SetCookie(w, &http.Cookie{
		Name: visitorCookieName, Value: signed, Path: visitorCookiePath,
		HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode,
		MaxAge: 60 * 60 * 24 * 365,
	})
	result, ok := a.parseVisitor(signed)
	if !ok {
		return visitor{}, errors.New("generated visitor cookie failed validation")
	}
	return result, nil
}

func (a *application) signVisitor(id string) string {
	mac := hmac.New(sha256.New, a.visitorSecret)
	_, _ = mac.Write([]byte(id))
	return id + "." + hex.EncodeToString(mac.Sum(nil))
}

func (a *application) parseVisitor(value string) (visitor, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[0]) != 64 || len(parts[1]) != 64 {
		return visitor{}, false
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return visitor{}, false
	}
	expected, err := hex.DecodeString(a.signVisitor(parts[0])[65:])
	if err != nil {
		return visitor{}, false
	}
	provided, err := hex.DecodeString(parts[1])
	if err != nil || !hmac.Equal(provided, expected) {
		return visitor{}, false
	}
	mac := hmac.New(sha256.New, a.visitorSecret)
	_, _ = mac.Write([]byte(parts[0]))
	return visitor{ID: parts[0], Key: hex.EncodeToString(mac.Sum(nil))}, true
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (a *application) handlePublic(w http.ResponseWriter, r *http.Request, visitor visitor) {
	switch {
	case r.URL.Path == "/api/feed":
		a.handleFeed(w, r, visitor)
	case strings.HasPrefix(r.URL.Path, "/api/photos/"):
		a.handlePhotoAction(w, r, visitor)
	case strings.HasPrefix(r.URL.Path, "/api/comments/"):
		a.handleCommentAction(w, r, visitor)
	default:
		http.NotFound(w, r)
	}
}

func (a *application) handleFeed(w http.ResponseWriter, r *http.Request, visitor visitor) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	payload, _ := json.Marshal(map[string]string{"p_visitor_key": visitor.Key})
	body, status, err := a.supabase(r.Context(), http.MethodPost, "/rest/v1/rpc/get_feed", payload, true, "application/json")
	if err != nil || status < 200 || status >= 300 {
		log.Printf("load Vercel feed failed (status %d): %v", status, err)
		http.Error(w, "Unable to load the feed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(body)
}

func (a *application) handlePhotoAction(w http.ResponseWriter, r *http.Request, visitor visitor) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "photos" {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil || id < 1 {
		http.Error(w, "Invalid photo id", http.StatusBadRequest)
		return
	}
	switch parts[3] {
	case "like":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !a.allowAction(w, r, visitor.Key, "photo_like", 30) {
			return
		}
		a.callLike(w, r, "like_photo", map[string]any{"p_photo_id": id, "p_visitor_key": visitor.Key})
	case "comments":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		a.getComments(w, r, visitor.Key, id)
	case "comment":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if !a.allowAction(w, r, visitor.Key, "comment", 6) {
			return
		}
		a.createComment(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (a *application) handleCommentAction(w http.ResponseWriter, r *http.Request, visitor visitor) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "comments" || parts[3] != "like" {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil || id < 1 {
		http.Error(w, "Invalid comment id", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !a.allowAction(w, r, visitor.Key, "comment_like", 60) {
		return
	}
	a.callLike(w, r, "like_comment", map[string]any{"p_comment_id": id, "p_visitor_key": visitor.Key})
}

func (a *application) callLike(w http.ResponseWriter, r *http.Request, rpc string, payload any) {
	body, _ := json.Marshal(payload)
	response, status, err := a.supabase(r.Context(), http.MethodPost, "/rest/v1/rpc/"+rpc, body, true, "application/json")
	if err != nil || status < 200 || status >= 300 {
		log.Printf("Vercel like action %s failed (status %d): %v", rpc, status, err)
		http.Error(w, "Unable to update the like", publicDatabaseErrorStatus(response, status, err))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(response)
}

func (a *application) getComments(w http.ResponseWriter, r *http.Request, visitorKey string, photoID int) {
	body, _ := json.Marshal(map[string]any{"p_photo_id": photoID, "p_visitor_key": visitorKey})
	response, status, err := a.supabase(r.Context(), http.MethodPost, "/rest/v1/rpc/get_comments", body, true, "application/json")
	if err != nil || status < 200 || status >= 300 {
		log.Printf("load Vercel comments failed (status %d): %v", status, err)
		http.Error(w, "Unable to load comments", publicDatabaseErrorStatus(response, status, err))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(response)
}

func (a *application) createComment(w http.ResponseWriter, r *http.Request, photoID int) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	var request struct {
		Message string `json:"message"`
	}
	if err := decodeOneJSON(r.Body, &request); err != nil {
		http.Error(w, "Invalid comment request", http.StatusBadRequest)
		return
	}
	request.Message = strings.TrimSpace(request.Message)
	if request.Message == "" || len([]rune(request.Message)) > 2000 {
		http.Error(w, "Comment must contain 1 to 2000 characters", http.StatusBadRequest)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"photo_id": photoID, "author": "Audience User", "message": request.Message,
	})
	body, status, err := a.supabase(r.Context(), http.MethodPost, "/rest/v1/comments?select=id,author,message,likes_count,created_at", payload, true, "application/json")
	if err != nil || status < 200 || status >= 300 {
		log.Printf("create Vercel comment failed (status %d): %v", status, err)
		http.Error(w, "Unable to post the comment", publicDatabaseErrorStatus(body, status, err))
		return
	}
	var comments []comment
	if err := json.Unmarshal(body, &comments); err != nil || len(comments) != 1 {
		log.Printf("Supabase returned an invalid comment response")
		http.Error(w, "Unable to post the comment", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(comments[0])
}

func publicDatabaseErrorStatus(body []byte, status int, requestErr error) int {
	if requestErr != nil {
		return http.StatusBadGateway
	}
	var response struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &response) == nil {
		switch response.Code {
		case "P0002":
			return http.StatusNotFound
		case "22023":
			return http.StatusBadRequest
		case "23503":
			return http.StatusNotFound
		case "42501":
			return http.StatusForbidden
		}
	}
	if status >= 400 && status < 500 {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

func (a *application) allowAction(w http.ResponseWriter, r *http.Request, key, action string, limit int) bool {
	payload, _ := json.Marshal(map[string]any{
		"p_visitor_key": key, "p_action": action, "p_limit": limit, "p_window_seconds": 60,
	})
	body, status, err := a.supabase(r.Context(), http.MethodPost, "/rest/v1/rpc/check_rate_limit", payload, true, "application/json")
	if err != nil || status < 200 || status >= 300 {
		log.Printf("Vercel public rate check failed (status %d): %v", status, err)
		http.Error(w, "Unable to process this action right now", http.StatusServiceUnavailable)
		return false
	}
	var allowed bool
	if err := json.Unmarshal(body, &allowed); err != nil {
		log.Printf("Supabase returned invalid rate-limit response")
		http.Error(w, "Unable to process this action right now", http.StatusServiceUnavailable)
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many actions; try again shortly", http.StatusTooManyRequests)
		return false
	}
	return true
}

func (a *application) recordRequest(r *http.Request, status int, duration time.Duration) {
	if status == 0 {
		status = http.StatusOK
	}
	activity := ""
	if status >= 200 && status < 300 && r.Method == http.MethodPost {
		switch {
		case strings.HasSuffix(r.URL.Path, "/comment"):
			activity = "comment"
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"p_status": status, "p_duration_ms": duration.Milliseconds(), "p_activity": activity,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, code, err := a.supabase(ctx, http.MethodPost, "/rest/v1/rpc/record_app_request", payload, true, "application/json")
	if err != nil || code < 200 || code >= 300 {
		log.Printf("record Vercel app telemetry failed (status %d): %v", code, err)
	}
}

func (a *application) supabase(ctx context.Context, method, path string, body []byte, service bool, contentType string) ([]byte, int, error) {
	endpoint, err := url.Parse(a.supabaseURL + path)
	if err != nil {
		return nil, 0, fmt.Errorf("parse Supabase endpoint: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, fmt.Errorf("create Supabase request: %w", err)
	}
	key := a.anonKey
	if service {
		key = a.serviceKey
	}
	request.Header.Set("apikey", key)
	if !isNewSupabaseAPIKey(key) {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if service {
		request.Header.Set("Prefer", "return=representation")
	}
	response, err := a.client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("request Supabase: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read Supabase response: %w", err)
	}
	return responseBody, response.StatusCode, nil
}

func isNewSupabaseAPIKey(key string) bool {
	return strings.HasPrefix(key, "sb_publishable_") || strings.HasPrefix(key, "sb_secret_")
}

func decodeOneJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("request must contain exactly one JSON value")
	}
	return nil
}

func methodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
