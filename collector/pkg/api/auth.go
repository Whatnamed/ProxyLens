package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

var defaultAllowedOrigins = map[string]bool{
	"tauri://localhost":        true,
	"http://tauri.localhost":   true,
	"https://tauri.localhost":  true,
	"http://localhost:5173":    true,
	"http://127.0.0.1:5173":    true,
	"http://localhost:1420":    true,
	"http://127.0.0.1:1420":    true,
}

// AuthAndCORSMiddleware 校验 Bearer Token 与精确 Origin
func (s *Server) AuthAndCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// 1. CORS 检查 (仅在提供 Origin 时处理)
		if origin != "" {
			if !s.isOriginAllowed(origin) {
				s.writeError(w, http.StatusForbidden, "FORBIDDEN_ORIGIN", "Origin not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
			w.Header().Set("Vary", "Origin")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		// 2. Health 检查免鉴权
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		// 3. Bearer Token 校验
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			s.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or malformed Authorization header")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
			s.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid session token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) isOriginAllowed(origin string) bool {
	if s.allowedOrigins != nil && s.allowedOrigins[origin] {
		return true
	}
	return defaultAllowedOrigins[origin]
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
