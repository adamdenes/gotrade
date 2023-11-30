package strategy

import (
	"testing"

	"github.com/markcheno/go-talib"
)

func Test_crossover(t *testing.T) {
	type args struct {
		series1 []float64
		series2 []float64
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "1. Crossing Over (False)",
			args: args{
				series1: []float64{1.0, 2.0, 3.0, 4.0, 5.0},
				series2: []float64{2.0, 3.0, 2.0, 1.0, 0.5},
			},
			want: false,
		},
		{
			name: "2. Crossing Over (False)",
			args: args{
				series1: []float64{5.0, 4.0, 3.0, 2.0, 1.0},
				series2: []float64{2.0, 3.0, 4.0, 5.0, 6.0},
			},
			want: false,
		},
		{
			name: "3. Not Enough Data (False)",
			args: args{
				series1: []float64{1.0, 2.0},
				series2: []float64{3.0, 4.0},
			},
			want: false,
		},
		{
			name: "4. Crossing Over (False)",
			args: args{
				series1: []float64{1.0, 2.0, 3.0, 4.0, 5.0},
				series2: []float64{5.0, 4.0, 3.0, 2.0, 1.0},
			},
			want: false,
		},
		{
			name: "5. Equal Values (False)",
			args: args{
				series1: []float64{1.0, 2.0, 3.0},
				series2: []float64{1.0, 2.0, 3.0},
			},
			want: false,
		},
		{
			name: "6. Crossing Over (True)",
			args: args{
				series1: []float64{22667.209999999995, 23732.66, 21773.97, 23157.070000000003},
				series2: []float64{
					20362.219999999987,
					28171.87,
					28415.290000000005,
					21102.430000000008,
				},
			},
			want: true,
		},
		{
			name: "7. Crossing Over (True)",
			args: args{
				series1: []float64{25792.100000000002, 26027.510000000006, 26906.960000000003},
				series2: []float64{29238.56999999999, 26180.05, 26527.510000000006},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := talib.Crossover(tt.args.series1, tt.args.series2); got != tt.want {
				t.Errorf("crossover() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_crossunder(t *testing.T) {
	type args struct {
		series1 []float64
		series2 []float64
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "1. Crossing Under (True)",
			args: args{
				series1: []float64{1.0, 2.0, 3.0, 4.0, 5.0},
				series2: []float64{5.0, 4.0, 3.0, 2.0, 5.0},
			},
			want: true,
		},
		{
			name: "2. Crossing Under (False)",
			args: args{
				series1: []float64{5.0, 4.0, 3.0, 2.0, 1.0},
				series2: []float64{2.0, 3.0, 4.0, 5.0, 6.0},
			},
			want: false,
		},
		{
			name: "3. Not Enough Data (False)",
			args: args{
				series1: []float64{1.0, 2.0},
				series2: []float64{3.0, 4.0},
			},
			want: false,
		},
		{
			name: "4. Equal Values (False)",
			args: args{
				series1: []float64{1.0, 2.0, 3.0},
				series2: []float64{1.0, 2.0, 3.0},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := talib.Crossunder(tt.args.series1, tt.args.series2); got != tt.want {
				t.Errorf("crossunder() = %v, want %v", got, tt.want)
			}
		})
	}
}
