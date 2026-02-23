package main

import "testing"

func TestSelectDuration(t *testing.T) {
	tests := []struct {
		name        string
		override    float64
		manifestDur float64
		ffprobeDur  float64
		want        float64
	}{
		{"override takes precedence", 30.0, 25.0, 28.0, 30.0},
		{"manifest used when no override", 0, 25.0, 28.0, 25.0},
		{"ffprobe fallback", 0, 0, 28.0, 28.0},
		{"default 60s when all zero", 0, 0, 0, 60.0},
		{"negative override ignored", -1, 25.0, 28.0, 25.0},
		{"negative manifest ignored", 0, -1, 28.0, 28.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectDuration(tt.override, tt.manifestDur, tt.ffprobeDur)
			if got != tt.want {
				t.Errorf("selectDuration(%v, %v, %v) = %v, want %v",
					tt.override, tt.manifestDur, tt.ffprobeDur, got, tt.want)
			}
		})
	}
}
