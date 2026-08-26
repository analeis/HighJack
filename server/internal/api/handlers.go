package api

import (
	"net/http"
	"runtime"

	"github.com/analeis/highjack/server/internal/logging"
	"github.com/analeis/highjack/server/internal/protocol"
	"github.com/analeis/highjack/server/internal/version"
)

// handleHealth reports liveness: the process is up and able to serve.
// It never inspects dependencies; a failing database does not make the
// process unhealthy, it makes it not-ready.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, response{Status: "ok"})
}

// handleReady reports readiness to receive traffic. When any component
// fails its check the endpoint responds 503 so orchestrators can withhold
// or withdraw traffic. Internal details are never exposed beyond a stable
// reason code.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.readiness != nil {
		if err := s.readiness(r.Context()); err != nil {
			logging.FromContext(r.Context()).Warn("readiness check failed", "reason", err.Error())
			writeJSON(w, http.StatusServiceUnavailable, response{
				Status: "unavailable",
				Detail: "dependency_check_failed",
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, response{Status: "ready"})
}

// versionPayload describes the running server build.
type versionPayload struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	GoVersion       string `json:"goVersion"`
	ProtocolVersion string `json:"protocolVersion"`
	SchemaVersion   int    `json:"schemaVersion"`
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, versionPayload{
		Name:            "highjack-server",
		Version:         version.Version,
		Commit:          version.Commit,
		GoVersion:       runtime.Version(),
		ProtocolVersion: protocol.ProtocolVersion,
		SchemaVersion:   protocol.SchemaVersion,
	})
}
