package web

import (
	"embed"
	"html/template"
	"net/http"
	"time"
)

//go:embed templates/*.html
var templatesFS embed.FS

type Server struct {
	tmpl *template.Template
}

type StatusView struct {
	GeneratedAt time.Time
}

func NewServer() (*Server, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/status.html")
	if err != nil {
		return nil, err
	}

	return &Server{tmpl: tmpl}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleStatus)
	return mux
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	view := StatusView{GeneratedAt: time.Now()}
	if err := s.tmpl.Execute(w, view); err != nil {
		http.Error(w, "template render error", http.StatusInternalServerError)
	}
}
