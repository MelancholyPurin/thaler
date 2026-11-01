package coded

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_Error_Error(t *testing.T) {
	tcs := []struct {
		name string
		err  *Error
		want string
	}{
		{name: "no arguments", err: New("already_imported", nil), want: "already_imported"},
		{name: "one", err: New("account_not_found", map[string]any{"Name": "Assets:X"}), want: "account_not_found(Name=Assets:X)"},
		{name: "several, in alphabetical order", err: New("currency_mismatch", map[string]any{"B": "USD", "A": "EUR", "N": 3}), want: "currency_mismatch(A=EUR, B=USD, N=3)"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.err

			// when
			got := tc.err.Error()

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Error_Is(t *testing.T) {
	sentinel := New("account_not_found", nil)
	tcs := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{name: "the same error", err: sentinel, target: sentinel, want: true},
		{name: "the same code with arguments", err: New("account_not_found", map[string]any{"Name": "x"}), target: sentinel, want: true},
		{name: "wrapped", err: fmt.Errorf("opening: %w", New("account_not_found", map[string]any{"Name": "x"})), target: sentinel, want: true},
		{name: "another code", err: New("account_exists", nil), target: sentinel},
		{name: "a plain error", err: errors.New("account_not_found"), target: sentinel},
		{name: "a plain target", err: New("account_not_found", nil), target: errors.New("account_not_found")},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.err and tc.target

			// when
			got := errors.Is(tc.err, tc.target)

			// then
			if got != tc.want {
				t.Errorf("errors.Is = %v, want %v", got, tc.want)
			}
		})
	}
}

func Test_In(t *testing.T) {
	a, b := New("a", nil), New("b", map[string]any{"X": 1})
	tcs := []struct {
		name string
		err  error
		want []string
	}{
		{name: "nothing", err: nil},
		{name: "a plain error", err: errors.New("x")},
		{name: "one", err: a, want: []string{"a"}},
		{name: "wrapped twice", err: fmt.Errorf("one: %w", fmt.Errorf("two: %w", a)), want: []string{"a"}},
		{name: "joined", err: errors.Join(a, errors.New("x"), b), want: []string{"a", "b(X=1)"}},
		{name: "wrapped around a join", err: fmt.Errorf("context: %w", errors.Join(a, b)), want: []string{"a", "b(X=1)"}},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.err

			// when
			var got []string
			for _, c := range In(tc.err) {
				got = append(got, c.Error())
			}

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_Code(t *testing.T) {
	tcs := []struct {
		name string
		err  error
		want string
	}{
		{name: "nothing", err: nil},
		{name: "a plain error", err: errors.New("x")},
		{name: "a coded error", err: New("a", nil), want: "a"},
		{name: "wrapped", err: fmt.Errorf("w: %w", New("a", nil)), want: "a"},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// given tc.err

			// when
			got := Code(tc.err)

			// then
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
