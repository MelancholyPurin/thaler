package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/melancholypurin/thaler/internal/ledger"
)

// Every listing takes --json, which prints what the service answered as one JSON
// document instead of a table, for scripts. The document is the service's own type,
// written the same way everywhere:
//   - a struct is an object whose keys are its field names in snake_case (SpentTotal is
//     "spent_total", ScheduleID is "schedule_id"), in the order of the fields;
//   - a list is an array, [] when it is empty, never null;
//   - an amount is {"amount": "-12.50", "currency": "EUR"}, the amount a decimal
//     in text with a point, never a number, so that no digit is lost;
//   - a day is the text YYYY-MM-DD, a month YYYY-MM, a moment RFC 3339 in UTC;
//   - an exchange rate is its exact decimal in text.
// Nothing else is printed in that mode: what a table says in a warning (a currency
// with no rate) is in the data (a "missing" list).

// addJSONFlag gives a listing its --json flag.
func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "print JSON instead of a table, for scripts")
}

// wantsJSON says whether the listing was asked for as JSON.
func wantsJSON(cmd *cobra.Command) bool {
	on, _ := cmd.Flags().GetBool("json")
	return on
}

// printJSON writes v as an indented JSON document and a line feed.
func printJSON(cmd *cobra.Command, v any) error {
	tree, err := jsonTree(reflect.ValueOf(v))
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tree); err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(buf.Bytes())
	return err
}

// member is one key of an object; an object keeps its members in order, which a map
// would not.
type member struct {
	key   string
	value any
}

type object []member

func (o object) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(m.key)
		if err != nil {
			return nil, err
		}
		val, err := marshalNoEscape(m.value)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalNoEscape is json.Marshal without turning < > & into \u escapes.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// jsonTree turns a value of the service into what encoding/json writes the way the
// comment above says.
func jsonTree(v reflect.Value) (any, error) {
	switch v.Kind() {
	case reflect.Invalid:
		return nil, nil
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
		if r, ok := v.Interface().(*big.Rat); ok {
			return ratText(r), nil
		}
		return jsonTree(v.Elem())
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), nil
	case reflect.Slice, reflect.Array:
		out := make([]any, v.Len())
		for i := range out {
			item, err := jsonTree(v.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("cannot write a map keyed by %s as JSON", v.Type().Key())
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		out := make(object, 0, len(keys))
		for _, k := range keys {
			item, err := jsonTree(v.MapIndex(k))
			if err != nil {
				return nil, err
			}
			out = append(out, member{k.String(), item})
		}
		return out, nil
	case reflect.Struct:
		return jsonStruct(v)
	}
	return nil, fmt.Errorf("cannot write a %s as JSON", v.Type())
}

func jsonStruct(v reflect.Value) (any, error) {
	switch x := v.Interface().(type) {
	case ledger.Money:
		return object{{"amount", x.Decimal()}, {"currency", string(x.Currency)}}, nil
	case time.Time:
		return x.UTC().Format(time.RFC3339), nil
	}
	var out object
	for i := range v.NumField() {
		field := v.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		item, err := jsonTree(v.Field(i))
		if err != nil {
			return nil, err
		}
		out = append(out, member{snakeCase(field.Name), item})
	}
	if out == nil {
		out = object{}
	}
	return out, nil
}

// snakeCase writes a Go name the way the keys are: SpentTotal as spent_total, ID as id,
// ScheduleID as schedule_id.
func snakeCase(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if !unicode.IsUpper(prev) || nextLower {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// ratText is an exchange rate as the shortest decimal that is exact: 0.92, not 0.920000.
// A rate is always a decimal, since that is all that can be entered.
func ratText(r *big.Rat) string {
	scaled := new(big.Int)
	for digits := range 40 {
		scaled.Mul(r.Num(), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil))
		if new(big.Int).Rem(scaled, r.Denom()).Sign() == 0 {
			return r.FloatString(digits)
		}
	}
	return r.FloatString(40)
}
