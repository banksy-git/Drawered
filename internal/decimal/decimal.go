// Package decimal provides exact fixed-point quantities and costs.
//
// Quantities are held in thousandths of a unit (Milli) and costs in
// millionths of a currency unit (Micro). Neither type ever passes through
// floating point.
package decimal

import (
    "errors"
    "strconv"
    "strings"
)

// ErrInvalid is returned when a value cannot be parsed.
var ErrInvalid = errors.New("invalid decimal")

// ErrPrecision is returned when a value has more decimal places than allowed.
var ErrPrecision = errors.New("too many decimal places")

// parseScaled parses a plain decimal literal (no exponent) into an integer
// scaled by 10^scale.
func parseScaled(s string, scale int) (int64, error) {
    s = strings.TrimSpace(s)
    if s == "" {
        return 0, ErrInvalid
    }
    neg := false
    if s[0] == '-' || s[0] == '+' {
        neg = s[0] == '-'
        s = s[1:]
    }
    intPart, fracPart, hasDot := strings.Cut(s, ".")
    if intPart == "" && (!hasDot || fracPart == "") {
        return 0, ErrInvalid
    }
    for _, c := range intPart + fracPart {
        if c < '0' || c > '9' {
            return 0, ErrInvalid
        }
    }
    fracPart = strings.TrimRight(fracPart, "0")
    if len(fracPart) > scale {
        return 0, ErrPrecision
    }
    fracPart += strings.Repeat("0", scale-len(fracPart))
    if intPart == "" {
        intPart = "0"
    }
    if len(intPart) > 18-scale {
        return 0, ErrInvalid
    }
    v, err := strconv.ParseInt(intPart+fracPart, 10, 64)
    if err != nil {
        return 0, ErrInvalid
    }
    if neg {
        v = -v
    }
    return v, nil
}

// formatScaled renders a scaled integer as a minimal decimal string.
func formatScaled(v int64, scale int) string {
    neg := v < 0
    if neg {
        v = -v
    }
    s := strconv.FormatInt(v, 10)
    if len(s) <= scale {
        s = strings.Repeat("0", scale-len(s)+1) + s
    }
    intPart, fracPart := s[:len(s)-scale], strings.TrimRight(s[len(s)-scale:], "0")
    out := intPart
    if fracPart != "" {
        out += "." + fracPart
    }
    if neg {
        out = "-" + out
    }
    return out
}

func unmarshalScaled(b []byte, scale int) (int64, error) {
    s := string(b)
    if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
        s = s[1 : len(s)-1]
    }
    return parseScaled(s, scale)
}

// Milli is a quantity in thousandths of a unit of measure.
type Milli int64

// MilliScale is the number of decimal places a Milli holds.
const MilliScale = 3

// One is a quantity of exactly one unit.
const One Milli = 1000

// ParseMilli parses a decimal string such as "1.25" into a Milli.
func ParseMilli(s string) (Milli, error) {
    v, err := parseScaled(s, MilliScale)
    return Milli(v), err
}

func (m Milli) String() string { return formatScaled(int64(m), MilliScale) }

// IsWhole reports whether the quantity is a whole number of units.
func (m Milli) IsWhole() bool { return m%One == 0 }

func (m Milli) MarshalJSON() ([]byte, error) { return []byte(m.String()), nil }

func (m *Milli) UnmarshalJSON(b []byte) error {
    v, err := unmarshalScaled(b, MilliScale)
    if err != nil {
        return err
    }
    *m = Milli(v)
    return nil
}

// Micro is a monetary amount in millionths of a currency unit.
type Micro int64

// MicroScale is the number of decimal places a Micro holds.
const MicroScale = 6

// ParseMicro parses a decimal string such as "0.0023" into a Micro.
func ParseMicro(s string) (Micro, error) {
    v, err := parseScaled(s, MicroScale)
    return Micro(v), err
}

func (m Micro) String() string { return formatScaled(int64(m), MicroScale) }

func (m Micro) MarshalJSON() ([]byte, error) { return []byte(m.String()), nil }

func (m *Micro) UnmarshalJSON(b []byte) error {
    v, err := unmarshalScaled(b, MicroScale)
    if err != nil {
        return err
    }
    *m = Micro(v)
    return nil
}

// MulMilli multiplies a unit cost by a quantity, rounding half away from
// zero to the nearest millionth.
func MulMilli(cost Micro, q Milli) Micro {
    p := int64(cost) * int64(q)
    if p >= 0 {
        return Micro((p + 500) / 1000)
    }
    return Micro((p - 500) / 1000)
}
