package routes

import (
	"html/template"
	"log"
	"net/http"
)

func NewRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", indexHandler)

	return mux
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	// Send 404 if destination is not `/`
	if r.URL.Path != "/" {
		http.Error(w, "404 Page not found", http.StatusNotFound)
		return
	}

	templateFiles := []string{
		"./web/templates/base.tmpl.html",
		"./web/templates/pages/chart.tmpl.html",
		"./web/templates/partials/script.tmpl.html",
	}

	// Parse the HTML template
	ts, err := template.ParseFiles(templateFiles...)
	if err != nil {
		log.Println(err.Error())
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Render the template
	err = ts.ExecuteTemplate(w, "base", nil)
	if err != nil {
		log.Println(err.Error())
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
}
