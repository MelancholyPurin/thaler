package main

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_run(t *testing.T) {
	tcs := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "no language", wantErr: "usage: i18ntodo LANGUAGE (one of [en es de])"},
		{name: "two languages", args: []string{"es", "de"}, wantErr: "usage: i18ntodo LANGUAGE (one of [en es de])"},
		{name: "English is the source", args: []string{"en"}, wantErr: "en is the source language, so its messages are written, not translated"},
		{name: "a language Thaler does not speak", args: []string{"fr"}, wantErr: `unknown language "fr": use one of en, es, de`},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given
			var stdout, stderr bytes.Buffer

			// when
			err := run(tc.args, &stdout, &stderr)

			// then
			got := ""
			if err != nil {
				got = err.Error()
			}
			if diff := cmp.Diff(tc.wantErr, got); diff != "" {
				t.Errorf("error mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
