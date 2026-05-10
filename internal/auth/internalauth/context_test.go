package internalauth

import (
	"context"
	"net/http"
	"testing"
)

func TestWithToken_RoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		token     string
		wantToken string
		wantOK    bool
	}{
		{name: "non-empty token round trips", token: "abc.def.ghi", wantToken: "abc.def.ghi", wantOK: true},
		{name: "empty token reports absent", token: "", wantToken: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithToken(context.Background(), tc.token)
			got, ok := TokenFrom(ctx)
			if got != tc.wantToken || ok != tc.wantOK {
				t.Errorf("TokenFrom = (%q, %v), want (%q, %v)", got, ok, tc.wantToken, tc.wantOK)
			}
		})
	}
}

func TestTokenFrom_AbsentByDefault(t *testing.T) {
	if got, ok := TokenFrom(context.Background()); ok {
		t.Errorf("TokenFrom on bare ctx = (%q, true), want (\"\", false)", got)
	}
}

func TestInjectHeader(t *testing.T) {
	cases := []struct {
		name       string
		ctx        context.Context
		wantHeader string
	}{
		{
			name:       "ctx with token sets X-Internal-Auth",
			ctx:        WithToken(context.Background(), "abc.def.ghi"),
			wantHeader: "abc.def.ghi",
		},
		{
			name:       "ctx without token leaves header empty",
			ctx:        context.Background(),
			wantHeader: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			InjectHeader(tc.ctx, h)
			if got := h.Get(HeaderName); got != tc.wantHeader {
				t.Errorf("header = %q, want %q", got, tc.wantHeader)
			}
		})
	}
}
