package opensearchtools

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompileFilters(t *testing.T) {
	filters, err := compileFilters(nil)
	assert.NoError(t, err)
	assert.Nil(t, filters)

	filters, err = compileFilters([]string{"error", "^warning$", "foo.*bar"})
	assert.NoError(t, err)
	assert.Len(t, filters, 3)
	for _, re := range filters {
		assert.NotNil(t, re)
	}

	_, err = compileFilters([]string{"   "})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	_, err = compileFilters([]string{""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	_, err = compileFilters([]string{"[unclosed"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter regex")

	filters, err = compileFilters([]string{"  error  "})
	assert.NoError(t, err)
	assert.Len(t, filters, 1)
	assert.True(t, filters[0].MatchString("error"))
}

func TestMatchAnyFilter(t *testing.T) {
	assert.True(t, matchAnyFilter(nil, "anything"))
	assert.True(t, matchAnyFilter([]*regexp.Regexp{}, "anything"))

	assert.True(t, matchAnyFilter([]*regexp.Regexp{regexp.MustCompile("error")}, "an error occurred"))
	assert.False(t, matchAnyFilter([]*regexp.Regexp{regexp.MustCompile("error")}, "all good"))

	orFilters := []*regexp.Regexp{regexp.MustCompile("error"), regexp.MustCompile("warn")}
	assert.True(t, matchAnyFilter(orFilters, "warn message"))

	assert.False(t, matchAnyFilter([]*regexp.Regexp{regexp.MustCompile("error")}, "Error"))

	assert.True(t, matchAnyFilter([]*regexp.Regexp{regexp.MustCompile(`a\|b`)}, "a|b"))
}
