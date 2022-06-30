package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRawStringLen(t *testing.T) {
	for i, tc := range []struct {
		input    string
		expected int
	}{
		{input: "", expected: 0},
		{input: "a", expected: 1},
		{input: "a\033", expected: 2},
		{input: "a\033[", expected: 3},
		{input: "a\033[2", expected: 4},
		{input: "a\033[2A", expected: 1},
		{input: "a\033[2Aa", expected: 2},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {

			l := rawStringLen(tc.input)
			assert.Equal(t, tc.expected, l)
		})

	}

}
