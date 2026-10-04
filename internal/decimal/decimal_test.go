package decimal

import (
    "encoding/json"
    "testing"
)

func TestParseMilli(t *testing.T) {
    cases := []struct {
        in   string
        want Milli
        err  bool
    }{
        {"1", 1000, false},
        {"1.25", 1250, false},
        {"0.001", 1, false},
        {".5", 500, false},
        {"-2.5", -2500, false},
        {"1.2500", 1250, false},
        {"0.0001", 0, true},
        {"1e3", 0, true},
        {"", 0, true},
        {".", 0, true},
        {"abc", 0, true},
    }
    for _, c := range cases {
        got, err := ParseMilli(c.in)
        if (err != nil) != c.err || (!c.err && got != c.want) {
            t.Errorf("ParseMilli(%q) = %v, %v", c.in, got, err)
        }
    }
}

func TestFormat(t *testing.T) {
    for v, want := range map[Milli]string{0: "0", 1000: "1", 1250: "1.25", 1: "0.001", -500: "-0.5"} {
        if got := v.String(); got != want {
            t.Errorf("%d: got %q want %q", v, got, want)
        }
    }
    if got := Micro(2300).String(); got != "0.0023" {
        t.Errorf("micro %q", got)
    }
}

func TestJSON(t *testing.T) {
    var v struct {
        A Milli `json:"a"`
        B Milli `json:"b"`
    }
    if err := json.Unmarshal([]byte(`{"a": 2.5, "b": "3"}`), &v); err != nil {
        t.Fatal(err)
    }
    if v.A != 2500 || v.B != 3000 {
        t.Fatalf("%+v", v)
    }
    b, _ := json.Marshal(v)
    if string(b) != `{"a":2.5,"b":3}` {
        t.Fatalf("%s", b)
    }
}

func TestMulMilli(t *testing.T) {
    if got := MulMilli(2300, 100000); got != 230000 {
        t.Fatalf("%d", got)
    }
    if got := MulMilli(1, 500); got != 1 {
        t.Fatalf("rounding %d", got)
    }
}
