package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	db                *sql.DB
	podName           = os.Getenv("POD_NAME")
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests processed",
		},
		[]string{"path", "status"},
	)
)

func init() {
	prometheus.MustRegister(httpRequestsTotal)
	if podName == "" {
		podName = "local-pod"
	}
}

type Photo struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	ImageURL   string `json:"image_url"`
	LikesCount int    `json:"likes_count"`
}

type CommentPayload struct {
	Author  string `json:"author"`
	Message string `json:"message"`
}

func simulateWorkload() {
	for i := 0; i < 500000; i++ {
		_ = math.Sqrt(float64(i)) * math.Sin(float64(i))
	}
}

func main() {
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPass, dbName)

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Printf("Warning: DB connection deferred: %v", err)
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/feed", handleFeed)
	http.HandleFunc("/api/photos/", handlePhotoActions)

	log.Println("Server listening on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	httpRequestsTotal.WithLabelValues("/", "200").Inc()

	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, struct{ PodName string }{PodName: podName})
}

func handleFeed(w http.ResponseWriter, r *http.Request) {
	simulateWorkload()
	httpRequestsTotal.WithLabelValues("/api/feed", "200").Inc()

	rows, err := db.Query("SELECT id, title, image_url, likes_count FROM photos ORDER BY id ASC")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var photos []Photo
	for rows.Next() {
		var p Photo
		if err := rows.Scan(&p.ID, &p.Title, &p.ImageURL, &p.LikesCount); err == nil {
			photos = append(photos, p)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(photos)
}

func handlePhotoActions(w http.ResponseWriter, r *http.Request) {
	simulateWorkload()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	photoID, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "Invalid photo ID", http.StatusBadRequest)
		return
	}

	action := parts[3]
	if action == "like" && r.Method == http.MethodPost {
		httpRequestsTotal.WithLabelValues("/api/photos/like", "200").Inc()
		var newLikes int
		err := db.QueryRow("UPDATE photos SET likes_count = likes_count + 1 WHERE id = $1 RETURNING likes_count", photoID).Scan(&newLikes)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"likes": newLikes})
		return
	}

	if action == "comment" && r.Method == http.MethodPost {
		httpRequestsTotal.WithLabelValues("/api/photos/comment", "200").Inc()
		var payload CommentPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, err := db.Exec("INSERT INTO comments (photo_id, author, message) VALUES ($1, $2, $3)", photoID, payload.Author, payload.Message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		return
	}

	http.Error(w, "Action not found", http.StatusNotFound)
}