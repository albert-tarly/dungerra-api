package utils

import (
	"math"

	"github.com/albert-tarly/dungerra-api/internal/config"
)

// Collision detection helpers
func CheckRectCollision(x1, y1, w1, h1, x2, y2, w2, h2 float64) bool {
	return x1 < x2+w2 && x1+w1 > x2 && y1 < y2+h2 && y1+h1 > y2
}

func CutLineSegmentBeforeRect(x1, y1, x2, y2, rx, ry, rw, rh float64) (float64, float64) {
	// Cheap reject first: callers test every wall in nine chunks against each
	// moving object, and almost none of them are anywhere near the segment.
	// A segment whose bounding box is strictly apart from the rectangle cannot
	// touch it, so the result is the same as the full test below.
	if math.Max(x1, x2) < rx || math.Min(x1, x2) > rx+rw ||
		math.Max(y1, y2) < ry || math.Min(y1, y2) > ry+rh {
		return x2, y2
	}

	// Liang-Barsky algorithm to find intersection point
	dx := x2 - x1
	dy := y2 - y1

	p := []float64{-dx, dx, -dy, dy}
	q := []float64{x1 - rx, rx + rw - x1, y1 - ry, ry + rh - y1}

	u1, u2 := 0.0, 1.0

	for i := range 4 {
		if p[i] == 0 {
			// Line is parallel to this edge
			if q[i] <= 0 {
				return x2, y2 // No intersection (touching border counts as collision)
			}
		} else {
			t := q[i] / p[i]
			if p[i] < 0 {
				if t >= u2 {
					return x2, y2 // No intersection
				}
				if t >= u1 {
					u1 = t
				}
			} else {
				// Leaving the rectangle
				if t <= u1 {
					return x2, y2 // No intersection
				}
				if t <= u2 {
					u2 = t
				}
			}
		}
	}

	// Return intersection point
	ix := x1 + u1*dx
	iy := y1 + u1*dy
	return ix, iy
}

func CheckLineRectCollision(x1, y1, x2, y2, rx, ry, rw, rh float64) bool {
	ix, iy := CutLineSegmentBeforeRect(x1, y1, x2, y2, rx, ry, rw, rh)
	return !(ix == x2 && iy == y2)
}

func CheckCircleCollision(x1, y1, r1, x2, y2, r2 float64) bool {
	dx := x1 - x2
	dy := y1 - y2
	distance := math.Sqrt(dx*dx + dy*dy)
	return distance < r1+r2
}

func CheckCircleRectCollision(cx, cy, r, rx, ry, rw, rh float64) bool {
	// Find closest point on rectangle to circle
	closestX := math.Max(rx, math.Min(cx, rx+rw))
	closestY := math.Max(ry, math.Min(cy, ry+rh))

	// Calculate distance between circle center and closest point
	dx := cx - closestX
	dy := cy - closestY

	return (dx*dx + dy*dy) < (r * r)
}

// Returns the closest point on the line segment AB to point P
func ClosestPointOnLineSegment(ax, ay, bx, by, px, py float64) (float64, float64) {
	apx := px - ax
	apy := py - ay
	abx := bx - ax
	aby := by - ay

	ab2 := abx*abx + aby*aby
	if ab2 == 0 {
		return ax, ay // a and b are the same point
	}

	ap_ab := apx*abx + apy*aby
	t := ap_ab / ab2

	if t < 0 {
		return ax, ay
	} else if t > 1 {
		return bx, by
	}

	return ax + abx*t, ay + aby*t
}

// NormalizeDegrees maps an angle in degrees to the range [0, 360).
func NormalizeDegrees(angle float64) float64 {
	angle = math.Mod(angle, 360)
	if angle < 0 {
		angle += 360
	}
	if angle >= 360 { // a tiny negative angle + 360 can round up to 360
		angle -= 360
	}
	return angle
}

// RotateTowards turns current toward target along the shorter arc by at most
// maxStep degrees. The result is normalised to [0, 360).
func RotateTowards(current, target, maxStep float64) float64 {
	diff := math.Mod(target-current, 360)
	if diff > 180 {
		diff -= 360
	} else if diff < -180 {
		diff += 360
	}
	if math.Abs(diff) <= maxStep {
		return NormalizeDegrees(target)
	}
	return NormalizeDegrees(current + math.Copysign(maxStep, diff))
}

func ChunkXYFromPosition(posX, posY float64) (int, int) {
	chunkSize := config.ChunkSize
	return int(math.Floor(posX / chunkSize)), int(math.Floor(posY / chunkSize))
}
