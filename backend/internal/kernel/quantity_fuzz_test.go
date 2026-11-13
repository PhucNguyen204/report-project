package kernel

import (
	"math"
	"testing"
)

func FuzzQuantityCanonicalRoundTrip(f *testing.F) {
	for _, seed := range []struct {
		text string
		raw  int64
	}{
		{"0.01", 10_000}, {"-1", -1_000_000}, {"0.333333", 333_333},
		{"-9223372036854.775808", math.MinInt64}, {"9223372036854.775807", math.MaxInt64},
		{"1e9", 0}, {"", -1}, {"0.0000001", 1},
	} {
		f.Add(seed.text, seed.raw)
	}
	f.Fuzz(func(t *testing.T, text string, raw int64) {
		q := Quantity(raw)
		got, err := ParseQuantity(q.String())
		if err != nil || got != q {
			t.Fatalf("canonical roundtrip: raw=%d got=%d err=%v", raw, got, err)
		}
		if len(text) > 256 {
			return
		}
		parsed, err := ParseQuantity(text)
		if err != nil {
			return
		}
		again, err := ParseQuantity(parsed.String())
		if err != nil || again != parsed {
			t.Fatalf("text roundtrip: %q got=%d want=%d err=%v", text, again, parsed, err)
		}
	})
}
