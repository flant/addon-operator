package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestMergeMap(t *testing.T) {
	for _, tuple := range []struct {
		src      string
		dst      string
		expected string
	}{
		{
			src:      `{}`,
			dst:      `{}`,
			expected: `{}`,
		},
		{
			src:      `{"b":2}`,
			dst:      `{"a":1}`,
			expected: `{"a":1,"b":2}`,
		},
		{
			src:      `{"a":0}`,
			dst:      `{"a":1}`,
			expected: `{"a":0}`,
		},
		{
			src:      `{"a":{       "y":2}}`,
			dst:      `{"a":{"x":1       }}`,
			expected: `{"a":{"x":1, "y":2}}`,
		},
		{
			src:      `{"a":{"x":2}}`,
			dst:      `{"a":{"x":1}}`,
			expected: `{"a":{"x":2}}`,
		},
		{
			src:      `{"a":{       "y":7, "z":8}}`,
			dst:      `{"a":{"x":1, "y":2       }}`,
			expected: `{"a":{"x":1, "y":7, "z":8}}`,
		},
		{
			src:      `{"1": { "b":1, "2": { "3": {         "b":3, "n":[1,2]} }        }}`,
			dst:      `{"1": {        "2": { "3": {"a":"A",        "n":"xxx"} }, "a":3 }}`,
			expected: `{"1": { "b":1, "2": { "3": {"a":"A", "b":3, "n":[1,2]} }, "a":3 }}`,
		},
	} {
		var dst map[string]interface{}
		if err := json.Unmarshal([]byte(tuple.dst), &dst); err != nil {
			t.Error(err)
			continue
		}

		var src map[string]interface{}
		if err := json.Unmarshal([]byte(tuple.src), &src); err != nil {
			t.Error(err)
			continue
		}

		var expected map[string]interface{}
		if err := json.Unmarshal([]byte(tuple.expected), &expected); err != nil {
			t.Error(err)
			continue
		}

		got := mergeMap(dst, src)
		checkMergedValues(t, expected, got)
	}
}

func checkMergedValues(t *testing.T, expected, got map[string]interface{}) {
	expectedBuf, err := json.Marshal(expected)
	if err != nil {
		t.Error(err)
		return
	}
	gotBuf, err := json.Marshal(got)
	if err != nil {
		t.Error(err)
		return
	}
	if bytes.Compare(expectedBuf, gotBuf) != 0 {
		t.Errorf("expected %s, got %s", string(expectedBuf), string(gotBuf))
		return
	}
}

func BenchmarkMapify(b *testing.B) {
	k := 100000

	m := make(map[string]interface{}, k)
	for i := 0; i < k; i++ {
		m[fmt.Sprintf("long%d", i)] = "storyshort"
	}

	c := make(map[string]string, k)
	for i := 0; i < k; i++ {
		c[fmt.Sprintf("long%d", i)] = "storyshort"
	}

	a := make(Values, k)
	for i := 0; i < k; i++ {
		a[fmt.Sprintf("long%d", i)] = "storyshort"
	}

	b.Run("Map interface", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			mapify(m)
		}
	})

	b.Run("Map string", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			mapify(c)
		}
	})

	b.Run("Map Values", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			mapify(a)
		}
	})
}
