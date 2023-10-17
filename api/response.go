package api

import (
	"compress/gzip"
	"net/http"
)

// WrappedResponseWriter is an implementation of the ResponseWriter interface
type WrappedResponseWriter struct {
	Writer     http.ResponseWriter
	GzipWriter *gzip.Writer
}

func (wr *WrappedResponseWriter) Write(data []byte) (int, error) {
	return wr.GzipWriter.Write(data)
}

func (wr *WrappedResponseWriter) Header() http.Header {
	return wr.Writer.Header()
}

func (wr *WrappedResponseWriter) WriteHeader(statusCode int) {
	wr.Writer.WriteHeader(statusCode)
}

func (wr *WrappedResponseWriter) Flush() {
	wr.GzipWriter.Flush()
	wr.GzipWriter.Close()
}
