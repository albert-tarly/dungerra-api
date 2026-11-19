package utils

import (
	"math"
	"testing"
)

func TestCheckRectCollision(t *testing.T) {
	tests := []struct {
		name     string
		x1, y1   float64
		w1, h1   float64
		x2, y2   float64
		w2, h2   float64
		expected bool
	}{
		{
			name: "overlapping rectangles",
			x1:   0, y1: 0, w1: 10, h1: 10,
			x2: 5, y2: 5, w2: 10, h2: 10,
			expected: true,
		},
		{
			name: "non-overlapping rectangles",
			x1:   0, y1: 0, w1: 10, h1: 10,
			x2: 20, y2: 20, w2: 10, h2: 10,
			expected: false,
		},
		{
			name: "touching edges",
			x1:   0, y1: 0, w1: 10, h1: 10,
			x2: 10, y2: 0, w2: 10, h2: 10,
			expected: false,
		},
		{
			name: "one inside another",
			x1:   0, y1: 0, w1: 20, h1: 20,
			x2: 5, y2: 5, w2: 5, h2: 5,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckRectCollision(tt.x1, tt.y1, tt.w1, tt.h1, tt.x2, tt.y2, tt.w2, tt.h2)
			if result != tt.expected {
				t.Errorf("CheckRectCollision() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCheckCircleCollision(t *testing.T) {
	tests := []struct {
		name     string
		x1, y1   float64
		r1       float64
		x2, y2   float64
		r2       float64
		expected bool
	}{
		{
			name: "overlapping circles",
			x1:   0, y1: 0, r1: 5,
			x2: 3, y2: 4, r2: 5,
			expected: true,
		},
		{
			name: "non-overlapping circles",
			x1:   0, y1: 0, r1: 5,
			x2: 20, y2: 20, r2: 5,
			expected: false,
		},
		{
			name: "touching circles",
			x1:   0, y1: 0, r1: 5,
			x2: 10, y2: 0, r2: 5,
			expected: false,
		},
		{
			name: "one inside another",
			x1:   0, y1: 0, r1: 10,
			x2: 0, y2: 0, r2: 5,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckCircleCollision(tt.x1, tt.y1, tt.r1, tt.x2, tt.y2, tt.r2)
			if result != tt.expected {
				t.Errorf("CheckCircleCollision() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCheckCircleRectCollision(t *testing.T) {
	tests := []struct {
		name     string
		cx, cy   float64
		r        float64
		rx, ry   float64
		rw, rh   float64
		expected bool
	}{
		{
			name: "circle overlaps rectangle",
			cx:   5, cy: 5, r: 3,
			rx: 0, ry: 0, rw: 10, rh: 10,
			expected: true,
		},
		{
			name: "circle outside rectangle",
			cx:   20, cy: 20, r: 3,
			rx: 0, ry: 0, rw: 10, rh: 10,
			expected: false,
		},
		{
			name: "circle touches rectangle corner",
			cx:   0, cy: 0, r: 5,
			rx: 10, ry: 0, rw: 10, rh: 10,
			expected: false,
		},
		{
			name: "circle inside rectangle",
			cx:   5, cy: 5, r: 2,
			rx: 0, ry: 0, rw: 10, rh: 10,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckCircleRectCollision(tt.cx, tt.cy, tt.r, tt.rx, tt.ry, tt.rw, tt.rh)
			if result != tt.expected {
				t.Errorf("CheckCircleRectCollision() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCutLineSegmentBeforeRect(t *testing.T) {
	tests := []struct {
		name      string
		x1, y1    float64
		x2, y2    float64
		rx, ry    float64
		rw, rh    float64
		expectedX float64
		expectedY float64
		shouldCut bool
	}{
		{
			name: "line intersects rectangle",
			x1:   0, y1: 5,
			x2: 20, y2: 5,
			rx: 10, ry: 0, rw: 10, rh: 10,
			expectedX: 10,
			expectedY: 5,
			shouldCut: true,
		},
		{
			name: "line misses rectangle",
			x1:   0, y1: 0,
			x2: 5, y2: 0,
			rx: 10, ry: 10, rw: 10, rh: 10,
			expectedX: 5,
			expectedY: 0,
			shouldCut: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix, iy := CutLineSegmentBeforeRect(tt.x1, tt.y1, tt.x2, tt.y2, tt.rx, tt.ry, tt.rw, tt.rh)

			// Allow small floating point errors
			epsilon := 1e-9
			if math.Abs(ix-tt.expectedX) > epsilon || math.Abs(iy-tt.expectedY) > epsilon {
				t.Errorf("CutLineSegmentBeforeRect() = (%v, %v), want (%v, %v)", ix, iy, tt.expectedX, tt.expectedY)
			}
		})
	}
}

func TestCheckLineRectCollision(t *testing.T) {
	tests := []struct {
		name     string
		x1, y1   float64
		x2, y2   float64
		rx, ry   float64
		rw, rh   float64
		expected bool
	}{
		{
			name: "line intersects rectangle",
			x1:   0, y1: 5,
			x2: 20, y2: 5,
			rx: 10, ry: 0, rw: 10, rh: 10,
			expected: true,
		},
		{
			name: "line misses rectangle",
			x1:   0, y1: 0,
			x2: 5, y2: 0,
			rx: 10, ry: 10, rw: 10, rh: 10,
			expected: false,
		},
		{
			name: "line starts inside rectangle",
			x1:   12, y1: 5,
			x2: 20, y2: 5,
			rx: 10, ry: 0, rw: 10, rh: 10,
			expected: true, // Line does intersect when starting inside
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckLineRectCollision(tt.x1, tt.y1, tt.x2, tt.y2, tt.rx, tt.ry, tt.rw, tt.rh)
			if result != tt.expected {
				t.Errorf("CheckLineRectCollision() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestClosestPointOnLineSegment(t *testing.T) {
	tests := []struct {
		name      string
		ax, ay    float64
		bx, by    float64
		px, py    float64
		expectedX float64
		expectedY float64
	}{
		{
			name: "point projects onto segment",
			ax:   0, ay: 0,
			bx: 10, by: 0,
			px: 5, py: 5,
			expectedX: 5, expectedY: 0,
		},
		{
			name: "point closest to endpoint a",
			ax:   0, ay: 0,
			bx: 10, by: 0,
			px: -5, py: 5,
			expectedX: 0, expectedY: 0,
		},
		{
			name: "point closest to endpoint b",
			ax:   0, ay: 0,
			bx: 10, by: 0,
			px: 15, py: 5,
			expectedX: 10, expectedY: 0,
		},
		{
			name: "a and b are same point",
			ax:   5, ay: 5,
			bx: 5, by: 5,
			px: 10, py: 10,
			expectedX: 5, expectedY: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultX, resultY := ClosestPointOnLineSegment(tt.ax, tt.ay, tt.bx, tt.by, tt.px, tt.py)
			epsilon := 1e-9
			if math.Abs(resultX-tt.expectedX) > epsilon || math.Abs(resultY-tt.expectedY) > epsilon {
				t.Errorf("ClosestPointOnLineSegment() = (%v, %v), want (%v, %v)", resultX, resultY, tt.expectedX, tt.expectedY)
			}
		})
	}
}

func TestNormalizeDegrees(t *testing.T) {
	tests := []struct{ in, want float64 }{
		{0, 0}, {359.5, 359.5}, {360, 0}, {720 + 45, 45}, {-90, 270}, {-360, 0}, {-1e-15, 0},
	}
	for _, tt := range tests {
		got := NormalizeDegrees(tt.in)
		if got < 0 || got >= 360 || math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("NormalizeDegrees(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRotateTowards(t *testing.T) {
	tests := []struct {
		name                     string
		current, target, maxStep float64
		want                     float64
	}{
		{"reaches target within step", 10, 30, 24, 30},
		{"capped clockwise", 0, 90, 24, 24},
		{"capped counter-clockwise", 90, 0, 24, 66},
		{"shorter arc across 0", 350, 10, 5, 355},
		{"shorter arc across 0 reversed", 10, 350, 5, 5},
		{"wraps below 0", 2, 300, 24, 338},
		{"already there", 123, 123, 24, 123},
		{"unnormalised target", 0, -90, 24, 336},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RotateTowards(tt.current, tt.target, tt.maxStep)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("RotateTowards(%v, %v, %v) = %v, want %v", tt.current, tt.target, tt.maxStep, got, tt.want)
			}
		})
	}
}
