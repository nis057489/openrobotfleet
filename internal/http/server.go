package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"time"

	"example.com/turtlebot-fleet/internal/controller"
	"example.com/turtlebot-fleet/internal/db"
	mqttc "example.com/turtlebot-fleet/internal/mqtt"
	"example.com/turtlebot-fleet/internal/scan"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Server struct {
	DB         *db.DB
	MQTT       *mqttc.Client
	Controller *controller.Controller
}

func NewServer(dbPath string) (*Server, error) {
	dbConn, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}
	mqttClient := mqttc.NewClient("controller")
	ctrl := controller.New(dbConn, mqttClient)
	s := &Server{DB: dbConn, MQTT: mqttClient, Controller: ctrl}
	go s.subscribeStatusUpdates()
	return s, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/auth/status", s.handleAuthStatus) // Add this line
	mux.HandleFunc("/api/interest", s.handleInterest)

	// Protected routes
	mux.HandleFunc("/api/install-agent", s.handleInstallAgent)
	mux.HandleFunc("/api/settings/install-defaults", s.handleInstallDefaults)
	mux.HandleFunc("/api/settings/system", s.handleSystemConfig)
	mux.HandleFunc("/api/robots", s.handleListRobots)
	mux.HandleFunc("/api/robots/command/broadcast", s.handleRobotCommandBroadcast)
	mux.HandleFunc("/api/robots/identify-all", s.handleIdentifyAll)
	mux.HandleFunc("/api/robots/", s.handleRobotSubroutes)
	mux.HandleFunc("/api/scenarios", s.handleScenariosCollection)
	mux.HandleFunc("/api/scenarios/", s.handleScenarioItem)
	mux.HandleFunc("/api/jobs", s.handleListJobs)
	mux.HandleFunc("/api/discovery/scan", s.handleDiscoveryScan)
	mux.HandleFunc("/api/semester/start", s.handleSemesterStart)
	mux.HandleFunc("/api/semester/status", s.handleSemesterStatus)
	mux.HandleFunc("/api/settings/backup", s.handleBackupDB)
	mux.HandleFunc("/api/settings/restore", s.handleRestoreDB)
	mux.HandleFunc("/api/golden-image", s.handleGoldenImage)
	mux.HandleFunc("/api/golden-image/download", s.handleGoldenImageDownload)
	mux.HandleFunc("/api/agent/download", s.handleAgentDownload)
	mux.HandleFunc("/api/golden-image/build", s.handleGoldenImageBuild)
	mux.HandleFunc("/api/golden-image/status", s.handleGoldenImageStatus)

	webRoot := os.Getenv("WEB_ROOT")
	if webRoot == "" {
		webRoot = "./web/dist"
	}

	fs := http.FileServer(http.Dir(webRoot))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(webRoot, r.URL.Path)
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			http.ServeFile(w, r, filepath.Join(webRoot, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})

	return s.authMiddleware(mux)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow public endpoints
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Check cookie
		cookie, err := r.Cookie("auth_token")
		if err != nil || cookie.Value != "secret-admin-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var creds struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	expected := os.Getenv("ADMIN_PASSWORD")
	if expected == "" {
		expected = "mrs2025" // Default password
	}

	if creds.Password != expected {
		http.Error(w, "Invalid password", http.StatusUnauthorized)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "secret-admin-token",
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	// Log successful login
	ip := r.RemoteAddr
	// If behind a proxy (like Traefik), use X-Forwarded-For
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = fwd
	}
	userAgent := r.Header.Get("User-Agent")

	if err := s.DB.RecordLogin(r.Context(), ip, userAgent); err != nil {
		log.Printf("failed to record login: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	// If we reached here, the middleware already validated the cookie
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"authenticated":true}`))
}

func (s *Server) handleInterest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var payload struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if payload.Email == "" {
		http.Error(w, "Email required", http.StatusBadRequest)
		return
	}

	ip := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = fwd
	}

	if err := s.DB.RecordInterest(r.Context(), payload.Email, ip); err != nil {
		log.Printf("failed to record interest: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) Start() error {
	addr := ":8080"
	if v := os.Getenv("HTTP_ADDR"); v != "" {
		addr = v
	}
	log.Printf("controller listening on %s", addr)
	return http.ListenAndServe(addr, s.routes())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.Controller.Health(w, r)
}

func (s *Server) handleListRobots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.Controller.ListRobots(w, r)
}

func (s *Server) handleRobotSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimSuffix(r.URL.Path, "/")
	if strings.HasSuffix(trimmed, "/install-config") {
		if r.Method != http.MethodPut {
			methodNotAllowed(w)
			return
		}
		s.Controller.UpdateInstallConfig(w, r)
		return
	}
	if strings.HasSuffix(trimmed, "/command") {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.Controller.RobotCommand(w, r)
		return
	}
	if strings.HasSuffix(trimmed, "/tags") {
		if r.Method != http.MethodPut {
			methodNotAllowed(w)
			return
		}
		s.Controller.UpdateRobotTags(w, r)
		return
	}
	if strings.HasSuffix(trimmed, "/terminal") {
		s.Controller.HandleTerminal(w, r)
		return
	}
	if strings.HasSuffix(trimmed, "/upload") {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.Controller.HandleRobotUpload(w, r)
		return
	}
	if r.Method == http.MethodGet {
		s.Controller.GetRobot(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		s.Controller.DeleteRobot(w, r)
		return
	}
	methodNotAllowed(w)
}

func (s *Server) handleRobotCommandBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.Controller.BroadcastCommand(w, r)
}

func (s *Server) handleScenariosCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.Controller.ListScenarios(w, r)
	case http.MethodPost:
		s.Controller.CreateScenario(w, r)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleScenarioItem(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimSuffix(r.URL.Path, "/")
	if strings.HasSuffix(trimmed, "/apply") {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.Controller.ApplyScenario(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.Controller.GetScenario(w, r)
	case http.MethodPut:
		s.Controller.UpdateScenario(w, r)
	case http.MethodDelete:
		s.Controller.DeleteScenario(w, r)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.Controller.ListJobs(w, r)
}

func (s *Server) handleInstallDefaults(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.Controller.GetInstallDefaults(w, r)
	case http.MethodPut:
		s.Controller.UpdateInstallDefaults(w, r)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleInstallAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.Controller.InstallAgent(w, r)
}

func (s *Server) handleSemesterStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.Controller.HandleSemesterStart(w, r)
}

func (s *Server) handleSemesterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.Controller.GetSemesterStatus(w, r)
}

func (s *Server) handleBackupDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if os.Getenv("DEMO_MODE") == "true" {
		respondError(w, http.StatusForbidden, "backup disabled in demo mode")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=controller.db")
	http.ServeFile(w, r, s.DB.Path)
}

func (s *Server) handleRestoreDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if os.Getenv("DEMO_MODE") == "true" {
		respondError(w, http.StatusForbidden, "restore disabled in demo mode")
		return
	}

	file, _, err := r.FormFile("db_file")
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to get file")
		return
	}
	defer file.Close()

	// Close current DB connection to release lock
	if err := s.DB.SQL.Close(); err != nil {
		log.Printf("failed to close db: %v", err)
	}

	// Create new file (overwrite)
	out, err := os.Create(s.DB.Path)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create file")
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	// Re-open DB
	newDB, err := db.Open(s.DB.Path)
	if err != nil {
		log.Printf("failed to reopen db: %v", err)
		os.Exit(1) // Fatal error, let container restart
	}

	// Update the reference
	s.DB.SQL = newDB.SQL

	respondJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

type statusPayload struct {
	Status string `json:"status"`
	TS     string `json:"ts"`
	IP     string `json:"ip"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

func (s *Server) subscribeStatusUpdates() {
	if s.MQTT == nil || s.DB == nil {
		return
	}
	topic := "lab/status/#"
	log.Printf("controller subscribing to %s", topic)
	h := func(_ mqtt.Client, msg mqtt.Message) {
		agentID := parseAgentIDFromTopic(msg.Topic())
		if agentID == "" {
			log.Printf("status: unable to parse agent id from topic %s", msg.Topic())
			return
		}
		var payload statusPayload
		if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
			log.Printf("status: invalid payload for %s: %v", agentID, err)
			return
		}
		name := payload.Name
		if name == "" {
			name = agentID
		}
		log.Printf("status update from %s: status=%s ip=%s type=%s", agentID, payload.Status, payload.IP, payload.Type)
		if err := s.DB.UpsertRobotStatus(context.Background(), agentID, name, payload.IP, payload.Status, payload.Type); err != nil {
			log.Printf("status: failed to upsert robot %s: %v", agentID, err)
		}
	}
	s.MQTT.Subscribe(topic, h)
}

func parseAgentIDFromTopic(topic string) string {
	const prefix = "lab/status/"
	if !strings.HasPrefix(topic, prefix) {
		return ""
	}
	return strings.TrimPrefix(topic, prefix)
}

func (s *Server) handleDiscoveryScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	candidates, err := scan.ScanSubnet()
	if err != nil {
		log.Printf("scan failed: %v", err)
		respondError(w, http.StatusInternalServerError, "scan failed")
		return
	}

	// Enrich with enrollment status
	robots, err := s.DB.ListRobots(r.Context())
	if err != nil {
		log.Printf("failed to list robots for discovery: %v", err)
		// Continue without enrollment info
	}

	knownIPs := make(map[string]bool)
	for _, r := range robots {
		if r.IP != "" {
			knownIPs[r.IP] = true
		}
	}

	type EnrichedCandidate struct {
		scan.Candidate
		Status string `json:"status"` // "enrolled", "unenrolled"
	}

	enriched := make([]EnrichedCandidate, len(candidates))
	for i, c := range candidates {
		status := "unenrolled"
		if knownIPs[c.IP] {
			status = "enrolled"
		}
		enriched[i] = EnrichedCandidate{
			Candidate: c,
			Status:    status,
		}
	}

	// Sort: Unenrolled Pi > Enrolled Pi > Unenrolled Other > Enrolled Other
	// Actually user req:
	// 1. Unenrolled highly likely (Pi)
	// 2. Enrolled (outdated?)
	// 3. All others

	sort.Slice(enriched, func(i, j int) bool {
		a, b := enriched[i], enriched[j]

		aIsPi := a.Manufacturer == "Raspberry Pi"
		bIsPi := b.Manufacturer == "Raspberry Pi"
		aEnrolled := a.Status == "enrolled"
		bEnrolled := b.Status == "enrolled"

		// Priority 1: Unenrolled Pi
		if aIsPi && !aEnrolled {
			if !(bIsPi && !bEnrolled) {
				return true
			}
		} else if bIsPi && !bEnrolled {
			return false
		}

		// Priority 2: Enrolled (any)
		if aEnrolled {
			if !bEnrolled {
				return true
			}
		} else if bEnrolled {
			return false
		}

		// Priority 3: Others (Unenrolled non-Pi)
		// (Implicitly handled by falling through)

		return a.IP < b.IP
	})

	respondJSON(w, http.StatusOK, enriched)
}

func respondJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleGoldenImage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.Controller.GetGoldenImageConfig(w, r)
		return
	}
	if r.Method == http.MethodPut {
		s.Controller.SaveGoldenImageConfig(w, r)
		return
	}
	methodNotAllowed(w)
}

func (s *Server) handleGoldenImageDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.Controller.DownloadGoldenImage(w, r)
}

func (s *Server) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.Controller.DownloadAgentBinary(w, r)
}

func (s *Server) handleGoldenImageBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.Controller.BuildGoldenImage(w, r)
}

func (s *Server) handleGoldenImageStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.Controller.GetBuildStatus(w, r)
}

func (s *Server) handleSystemConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	demoMode := os.Getenv("DEMO_MODE") == "true"
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"demo_mode": demoMode,
	})
}

func (s *Server) handleIdentifyAll(w http.ResponseWriter, r *http.Request) {
	s.Controller.IdentifyAll(w, r)
}
