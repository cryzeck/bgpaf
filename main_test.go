package main

import (
	"encoding/json"
	"testing"
)

func TestCreatePolicy(t *testing.T) {
	input := []string{
		"172.20.0.0/14",
		"fd00::/8",
		"2000::/8",
	}

	lis := CreatePolicies(input)

	s, _ := json.Marshal(lis)
	t.Logf("%s", s)
}
