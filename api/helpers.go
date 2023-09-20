package api

import (
	"fmt"
	"html/template"
	"net/http"
	"runtime/debug"
)

func (s *Server) serverError(w http.ResponseWriter, err error) {
	trace := fmt.Sprintf("%s\n%s", err.Error(), debug.Stack())
	s.errorLog.Output(2, trace)

	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func (s *Server) clientError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func (s *Server) notFound(w http.ResponseWriter) {
	s.clientError(w, http.StatusNotFound)
}

func (s *Server) render(w http.ResponseWriter, data any) {
	// Parse the HTML template
	ts, err := template.ParseFiles(templateFiles...)
	if err != nil {
		s.errorLog.Println(err.Error())
		// http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		s.serverError(w, err)
		return
	}
	// Render the template
	err = ts.ExecuteTemplate(w, "base", nil)
	if err != nil {
		s.errorLog.Println(err.Error())
		// http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		s.serverError(w, err)
		return
	}
}
