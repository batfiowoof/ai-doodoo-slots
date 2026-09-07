package social

import "testing"

func TestComma(t *testing.T) {
	cases := map[int64]string{
		0:         "0",
		7:         "7",
		999:       "999",
		1000:      "1,000",
		1234567:   "1,234,567",
		-1234567:  "-1,234,567",
		100000000: "100,000,000",
	}
	for in, want := range cases {
		if got := comma(in); got != want {
			t.Errorf("comma(%d) = %q, want %q", in, got, want)
		}
	}
}
