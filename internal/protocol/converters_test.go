package protocol

import (
	"math"
	"testing"
)

func TestFromProtoInputAimRotation(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name string
		in   *float64
		want *float64
	}{
		{"unset stays unset", nil, nil},
		{"in range", f(45), f(45)},
		{"normalised", f(-90), f(270)},
		{"NaN dropped", f(math.NaN()), nil},
		{"Inf dropped", f(math.Inf(1)), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromProtoInput(&InputMessage{AimRotation: tt.in}).AimRotation
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("AimRotation = %v, want %v", got, tt.want)
			}
		})
	}
}
