package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

type Quantity int64

const Scale int64 = 1_000_000

func ParseQuantity(s string) (Quantity, error) {
	if s == "" || strings.TrimSpace(s) != s {
		return 0, errors.New("invalid quantity")
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	p := strings.Split(s, ".")
	if len(p) > 2 || p[0] == "" {
		return 0, errors.New("invalid quantity")
	}
	frac := ""
	if len(p) == 2 {
		frac = p[1]
		if frac == "" || len(frac) > 6 {
			return 0, errors.New("quantity precision exceeds 6")
		}
	}
	for _, c := range p[0] + frac {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid quantity")
		}
	}
	n, ok := new(big.Int).SetString(p[0]+frac+strings.Repeat("0", 6-len(frac)), 10)
	if !ok {
		return 0, errors.New("invalid quantity")
	}
	if neg {
		n.Neg(n)
	}
	if !n.IsInt64() {
		return 0, errors.New("quantity overflow")
	}
	return Quantity(n.Int64()), nil
}

func (q Quantity) String() string {
	n := big.NewInt(int64(q))
	neg := n.Sign() < 0
	n.Abs(n)
	a, b := new(big.Int), new(big.Int)
	a.QuoRem(n, big.NewInt(Scale), b)
	s := a.String()
	if b.Sign() != 0 {
		s += "." + strings.TrimRight(fmt.Sprintf("%06d", b.Int64()), "0")
	}
	if neg {
		s = "-" + s
	}
	return s
}

func (q Quantity) Mul(other Quantity) (Quantity, error) {
	n := new(big.Int).Mul(big.NewInt(int64(q)), big.NewInt(int64(other)))
	r := new(big.Int)
	n.QuoRem(n, big.NewInt(Scale), r)
	if r.Sign() != 0 {
		return 0, errors.New("quantity precision loss")
	}
	if !n.IsInt64() {
		return 0, errors.New("quantity overflow")
	}
	return Quantity(n.Int64()), nil
}

func (q Quantity) Div(other Quantity) (Quantity, error) {
	if other == 0 {
		return 0, errors.New("division by zero")
	}
	n := new(big.Int).Mul(big.NewInt(int64(q)), big.NewInt(Scale))
	r := new(big.Int)
	n.QuoRem(n, big.NewInt(int64(other)), r)
	if r.Sign() != 0 {
		return 0, errors.New("quantity precision loss")
	}
	if !n.IsInt64() {
		return 0, errors.New("quantity overflow")
	}
	return Quantity(n.Int64()), nil
}

func (q Quantity) MarshalJSON() ([]byte, error) { return []byte(q.String()), nil }
func (q *Quantity) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(s) > 0 && s[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	}
	v, err := ParseQuantity(s)
	if err == nil {
		*q = v
	}
	return err
}

func (q Quantity) TwoDecimals() bool      { return int64(q)%10_000 == 0 }
func (q Quantity) Value() (string, error) { return q.String(), nil }
func (q *Quantity) Scan(src any) error {
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	case int64:
		s = strconv.FormatInt(v, 10)
	default:
		return fmt.Errorf("unsupported quantity %T", src)
	}
	v, err := ParseQuantity(s)
	if err == nil {
		*q = v
	}
	return err
}

type Money int64
