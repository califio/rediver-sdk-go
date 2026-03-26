package rediver

import "testing"

func TestPtr_Int(t *testing.T) {
	p := Ptr(42)
	if *p != 42 {
		t.Errorf("expected 42, got %d", *p)
	}
}

func TestPtr_String(t *testing.T) {
	p := Ptr("hello")
	if *p != "hello" {
		t.Errorf("expected 'hello', got %q", *p)
	}
}

func TestPtr_Bool(t *testing.T) {
	p := Ptr(true)
	if !*p {
		t.Error("expected true")
	}
	p2 := Ptr(false)
	if *p2 {
		t.Error("expected false")
	}
}

func TestPtr_Float64(t *testing.T) {
	p := Ptr(3.14)
	if *p != 3.14 {
		t.Errorf("expected 3.14, got %f", *p)
	}
}

func TestPtr_ZeroValues(t *testing.T) {
	// Ptr should work with zero values too (unlike ptrOrNil which returns nil)
	pi := Ptr(0)
	if *pi != 0 {
		t.Errorf("expected 0, got %d", *pi)
	}
	ps := Ptr("")
	if *ps != "" {
		t.Errorf("expected empty string, got %q", *ps)
	}
	pb := Ptr(false)
	if *pb != false {
		t.Error("expected false")
	}
}

func TestPtr_Struct(t *testing.T) {
	type point struct{ X, Y int }
	p := Ptr(point{X: 1, Y: 2})
	if p.X != 1 || p.Y != 2 {
		t.Errorf("expected {1,2}, got %v", *p)
	}
}

func TestPtr_ReturnsDistinctPointers(t *testing.T) {
	a := Ptr(10)
	b := Ptr(10)
	if a == b {
		t.Error("Ptr should return distinct pointers for each call")
	}
}
