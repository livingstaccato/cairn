// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// cairndex.schema.json's "override" definition once declared match as one
// of its own properties, so $ref-ing it straight for defaults: (which has
// no Match field on Override — only Rule.Match) let an editor accept
// defaults: {match: ...} that Load's strict decoder then refuses. These
// read the schema file itself — no JSON-Schema engine, just enough
// encoding/json navigation to keep that specific mistake from coming back.
func readSchema(t *testing.T) map[string]any {
	t.Helper()
	p := filepath.Join("..", "..", "cairndex.schema.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse %s: %v", p, err)
	}
	return doc
}

func schemaObject(t *testing.T, doc map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := doc
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			t.Fatalf("schema has no object at %v (stopped at %q)", path, p)
		}
		cur = next
	}
	return cur
}

func TestSchemaOverrideDoesNotAcceptMatch(t *testing.T) {
	doc := readSchema(t)
	props := schemaObject(t, doc, "definitions", "override", "properties")
	if _, ok := props["match"]; ok {
		t.Error(`definitions.override.properties still declares "match" — ` +
			`defaults: uses $ref: override directly, and Override has no ` +
			`Match field, so this makes the schema accept what Load rejects`)
	}
}

func TestSchemaRuleRequiresAndAcceptsMatch(t *testing.T) {
	doc := readSchema(t)
	rule := schemaObject(t, doc, "definitions", "rule")
	props := schemaObject(t, doc, "definitions", "rule", "properties")
	if _, ok := props["match"]; !ok {
		t.Error(`definitions.rule.properties does not declare "match"`)
	}
	required, _ := rule["required"].([]any)
	found := false
	for _, r := range required {
		if r == "match" {
			found = true
		}
	}
	if !found {
		t.Error(`definitions.rule.required does not list "match"`)
	}
}

// rules: in the root schema has to actually use the closed rule definition
// rather than reopening the same override-plus-match leak a different way.
func TestSchemaRulesItemsReferenceRule(t *testing.T) {
	doc := readSchema(t)
	rules := schemaObject(t, doc, "properties", "rules")
	items, ok := rules["items"].(map[string]any)
	if !ok {
		t.Fatal("properties.rules.items is not an object")
	}
	if ref, _ := items["$ref"].(string); ref != "#/definitions/rule" {
		t.Errorf(`properties.rules.items.$ref = %q, want "#/definitions/rule"`, ref)
	}
}
