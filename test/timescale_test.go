package api

import (
	"testing"

	"github.com/adamdenes/gotrade/internal/storage"
)

func Test_ConvertInterval(t *testing.T) {
	type args struct {
		intervalString string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Convert 1 second",
			args: args{intervalString: "1s"},
			want: "1 second",
		},
		{
			name: "Convert 5 minutes",
			args: args{intervalString: "5m"},
			want: "5 minutes",
		},
		{
			name: "Convert 1 hour",
			args: args{intervalString: "1h"},
			want: "1 hour",
		},
		{
			name: "Convert 3 days",
			args: args{intervalString: "3d"},
			want: "3 days",
		},
		{
			name: "Convert 1 week",
			args: args{intervalString: "1w"},
			want: "1 week",
		},
		{
			name: "Convert 2 months",
			args: args{intervalString: "2M"},
			want: "2 months",
		},
		{
			name: "Convert 15 minutes",
			args: args{intervalString: "15m"},
			want: "15 minutes",
		},
		{
			name: "Convert 12 hours",
			args: args{intervalString: "12h"},
			want: "12 hours",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := storage.ConvertInterval(tt.args.intervalString); got != tt.want {
				t.Errorf("convertInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}
