package midi

import (
	"math"
	"testing"
)

func TestUnit(t *testing.T) {
	tests := []struct {
		in   float64
		want uint32
	}{
		{0, 0}, {1, ValueMax}, {-0.5, 0}, {2, ValueMax},
		{math.NaN(), 0}, {math.Inf(1), ValueMax}, {math.Inf(-1), 0},
	}
	for _, tt := range tests {
		if got := Unit(tt.in); got != tt.want {
			t.Errorf("Unit(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
	if got := To7(Unit(0.5)); got != 64 {
		t.Errorf("To7(Unit(0.5)) = %d, want 64", got)
	}
}

func TestBipolar(t *testing.T) {
	tests := []struct {
		in   float64
		want uint32
	}{
		{0, ValueCenter}, {-1, 0}, {1, ValueMax}, {-3, 0}, {3, ValueMax},
		{math.NaN(), ValueCenter},
	}
	for _, tt := range tests {
		if got := Bipolar(tt.in); got != tt.want {
			t.Errorf("Bipolar(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
	// The 14-bit pitch bend center is 8192; HIDI 1.x sent 8191 here.
	if got := To14(Bipolar(0)); got != 8192 {
		t.Errorf("To14(Bipolar(0)) = %d, want 8192", got)
	}
}

func TestBipolarMonotonic(t *testing.T) {
	prev := Bipolar(-1)
	for i := -999; i <= 1000; i++ {
		v := Bipolar(float64(i) / 1000)
		if v < prev {
			t.Fatalf("Bipolar(%v) = %d is below the previous value %d", float64(i)/1000, v, prev)
		}
		prev = v
	}
}

func TestScaleRoundTrip(t *testing.T) {
	for v := 0; v < 128; v++ {
		if got := To7(From7(uint8(v))); got != uint8(v) {
			t.Errorf("To7(From7(%d)) = %d", v, got)
		}
	}
	for v := 0; v < 1<<14; v++ {
		if got := To14(From14(uint16(v))); got != uint16(v) {
			t.Errorf("To14(From14(%d)) = %d", v, got)
		}
	}
}

func TestScaleUpAnchors(t *testing.T) {
	tests := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"From7 min", From7(0), 0},
		{"From7 center", From7(64), ValueCenter},
		{"From7 max", From7(127), ValueMax},
		{"From14 min", From14(0), 0},
		{"From14 center", From14(8192), ValueCenter},
		{"From14 max", From14(16383), ValueMax},
		{"From7 ignores high bit", From7(0x80 | 64), ValueCenter},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = 0x%08X, want 0x%08X", tt.name, tt.got, tt.want)
		}
	}
}
