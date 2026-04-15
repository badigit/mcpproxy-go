package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsToolAllowed_Match(t *testing.T) {
	assert.True(t, isToolAllowed("find_party", []string{"find_party"}))
	assert.True(t, isToolAllowed("find_party", []string{"clean_address", "find_party"}))
}

func TestIsToolAllowed_NoMatch(t *testing.T) {
	assert.False(t, isToolAllowed("clean_address", []string{"find_party"}))
	assert.False(t, isToolAllowed("find_company_by_email", []string{"find_party", "clean_address"}))
}

func TestIsToolAllowed_EmptyList(t *testing.T) {
	// Empty list is handled by the caller (len check), but function itself returns false
	assert.False(t, isToolAllowed("anything", []string{}))
}
