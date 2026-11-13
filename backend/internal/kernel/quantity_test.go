package kernel

import (
	"encoding/json"
	"testing"
)

func TestQuantityExactArithmetic(t *testing.T) {
	a, err := ParseQuantity("0.1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ParseQuantity("0.2")
	if got := (a + b).String(); got != "0.3" {
		t.Fatalf("got %s", got)
	}
	kg, _ := ParseQuantity("0.001")
	factor, _ := ParseQuantity("1000")
	got, err := kg.Mul(factor)
	if err != nil || got.String() != "1" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestQuantityRejectsLossAndOverflow(t *testing.T) {
	for _, s := range []string{"NaN", "1e99", "0.0000001", "9223372036854775808", "", "1.2.3"} {
		if _, err := ParseQuantity(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	q, _ := ParseQuantity("999999999999")
	if _, err := q.Mul(q); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestQuantityJSONPreservesDecimal(t *testing.T) {
	var q Quantity
	if err := json.Unmarshal([]byte("99999999.99"), &q); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(q)
	if err != nil || string(b) != "99999999.99" {
		t.Fatalf("%s %v", b, err)
	}
	if err := json.Unmarshal([]byte("null"), &q); err == nil {
		t.Fatal("null quantity accepted")
	}
}

func TestQuantityDoesNotTruncateSubscaleAmounts(t *testing.T) {
	a, _ := ParseQuantity("0.000001")
	b, _ := ParseQuantity("0.1")
	if _, err := a.Mul(b); err == nil {
		t.Fatal("silently truncated product")
	}
	three, _ := ParseQuantity("3")
	if _, err := a.Div(three); err == nil {
		t.Fatal("silently truncated quotient")
	}
}

func TestQuantitySignsDivisionAndStorage(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"-0.5", "-0.5"}, {"0", "0"}, {"0001.200000", "1.2"}} {
		q, err := ParseQuantity(tc.input)
		if err != nil || q.String() != tc.want {
			t.Fatalf("%q %v %v", tc.input, q, err)
		}
	}
	a, _ := ParseQuantity("1.5")
	b, _ := ParseQuantity("0.5")
	got, err := a.Div(b)
	if err != nil || got.String() != "3" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err = a.Div(0); err == nil {
		t.Fatal("accepted zero divisor")
	}
	var q Quantity
	for _, src := range []any{[]byte("1.25"), "1.25", int64(1)} {
		if err = q.Scan(src); err != nil {
			t.Fatal(err)
		}
	}
	if err = q.Scan(true); err == nil {
		t.Fatal("accepted bool scan")
	}
	if err = json.Unmarshal([]byte(`"1.25"`), &q); err != nil || q.String() != "1.25" {
		t.Fatalf("%v %v", q, err)
	}
	if !q.TwoDecimals() {
		t.Fatal("lost decimal precision")
	}
	v, err := q.Value()
	if err != nil || v != "1.25" {
		t.Fatalf("%v %v", v, err)
	}
	for _, s := range []string{"-", " 1", "1.", ".1", "1a", "1.2.3"} {
		if _, err = ParseQuantity(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
