package api

import (
	"testing"

	"github.com/adamdenes/gotrade/cmd/rest"
)

func TestBuildURI(t *testing.T) {
	type args struct {
		base  string
		query []string
	}
	tests := []struct {
		name  string
		base  string
		query []string
		want  string
	}{
		{
			name:  "Base with Single Query",
			base:  "https://example.com?",
			query: []string{"foo=bar"},
			want:  "https://example.com?foo=bar",
		},
		{
			name:  "Base with Multiple Queries",
			base:  "https://example.com?",
			query: []string{"foo=bar", "&baz=qux"},
			want:  "https://example.com?foo=bar&baz=qux",
		},
		{
			name:  "Base with Symbol Query",
			base:  "https://example.com?",
			query: []string{"symbol=abc"},
			want:  "https://example.com?symbol=ABC",
		},
		{
			name:  "Base with Mix of Queries",
			base:  "https://example.com?",
			query: []string{"symbol=abc", "&foo=bar"},
			want:  "https://example.com?symbol=ABC&foo=bar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rest.BuildURI(tt.base, tt.query...)
			if result != tt.want {
				t.Errorf("expected %s but got %s", tt.want, result)
			}
		})
	}
}
