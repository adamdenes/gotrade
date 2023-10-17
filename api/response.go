package api

import (
	"compress/gzip"
	"net/http"
)

// Define a custom ResponseWriter interface
type ResponseWriter interface {
	Write([]byte) (int, error)
	Header() http.Header
	WriteHeader(int)
}

// GzipResponseWriter is an implementation of the ResponseWriter interface
type GzipResponseWriter struct {
	Writer     http.ResponseWriter
	GzipWriter *gzip.Writer
}

func (gw *GzipResponseWriter) Write(data []byte) (int, error) {
	return gw.GzipWriter.Write(data)
}

func (gw *GzipResponseWriter) Header() http.Header {
	return gw.Writer.Header()
}

func (gw *GzipResponseWriter) WriteHeader(statusCode int) {
	gw.Writer.WriteHeader(statusCode)
}
