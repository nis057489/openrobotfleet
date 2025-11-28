package httpserver

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	mux.HandleFunc("/api/install-agent", s.handleInstallAgent)
	mux.HandleFunc("/api/settings/install-defaults", s.handleInstallDefaults)
	mux.HandleFunc("/api/robots", s.handleListRobots)
	mux.HandleFunc("/api/robots/command/broadcast", s.handleRobotCommandBroadcast)
	mux.HandleFunc("/api/robots/", s.handleRobotSubroutes)
	mux.HandleFunc("/api/scenarios", s.handleScenariosCollection)
	mux.HandleFunc("/api/scenarios/", s.handleScenarioItem)
	mux.HandleFunc("/api/jobs", s.handleListJobs)
	mux.HandleFunc("/api/discovery/scan", s.handleDiscoveryScan)
	mux.HandleFunc("/api/semester/start", s.handleSemesterStart)
	mux.HandleFunc("/api/semester/status", s.handleSemesterStatus)

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
	return mux
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
	if r.Method == http.MethodGet {
		s.Controller.GetRobot(w, r)
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

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

type statusPayload struct {
	Status string `json:"status"`
	TS     string `json:"ts"`
	IP     string `json:"ip"`
	Name   string `json:"name"`
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
		log.Printf("status update from %s: status=%s ip=%s", agentID, payload.Status, payload.IP)
		if err := s.DB.UpsertRobotStatus(context.Background(), agentID, name, payload.IP, payload.Status); err != nil {
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
	respondJSON(w, http.StatusOK, candidates)
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
