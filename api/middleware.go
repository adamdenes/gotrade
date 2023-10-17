package api

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// RequestIDKey is a custom type used as a key for the request ID in the context.
type RequestIDKey int

const (
	RequestIDContextKey RequestIDKey = iota
)

// Secure headers will act on every request before routing
func (s *Server) secureHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().
			Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self' 'unsafe-inline' unpkg.com;")
		w.Header().Set("Referrer-Policy", "origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "deny")
		w.Header().Set("X-XSS-Protection", "0")

		next.ServeHTTP(w, r)
	})
}

// Request logger
func (s *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.infoLog.Printf("%s - %s %s %s", r.RemoteAddr, r.Proto, r.Method, r.URL.RequestURI())

		next.ServeHTTP(w, r)
	})
}

// "Recover" panics, send server error instead of nothing
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// HTTP server auto closes current connection
				// if 'close' is set in header
				w.Header().Set("Connection", "close")
				s.serverError(w, fmt.Errorf("%s", err))
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// tagRequest is a middleware that adds a unique request ID to the request's context.
func (s *Server) tagRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate a unique request ID (UUID)
		requestID := uuid.New().String()

		// Add the request ID to the request's context
		ctx := context.WithValue(r.Context(), RequestIDContextKey, requestID)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// Create a Pool that contains previously used Writers and
// can create new ones if we run out.
var gzPool = sync.Pool{New: func() any {
	gz, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
	return gz
}}

func (s *Server) gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the client accepts gzip encoding
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") &&
			strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			// Get a Writer from the Pool
			gz := gzPool.Get().(*gzip.Writer)
			// When done, put the Writer back in the Pool
			defer gzPool.Put(gz)
			// We use Reset to set the writer we want to use.
			gz.Reset(w)
			defer gz.Close()

			w.Header().Set("Content-Encoding", "gzip")
			next.ServeHTTP(&GzipResponseWriter{Writer: w, GzipWriter: gz}, r)
		} else {
			next.ServeHTTP(w, r)
		}
	})
}
