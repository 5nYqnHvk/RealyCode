package toolguard

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/5nYqnHvk/RelayCode/internal/anthropic"
	"github.com/5nYqnHvk/RelayCode/internal/config"
	"github.com/5nYqnHvk/RelayCode/internal/provider/toolargs"
)

type Registry struct {
	tools   map[string]anthropic.Tool
	aliases map[string]map[string]string
	policy  config.ToolValidationConfig
}

func NewRegistry(tools []anthropic.Tool, passthroughServerTools bool, aliases map[string]map[string]string, policy ...config.ToolValidationConfig) *Registry {
	resolvedPolicy := config.ToolValidationConfig{UnknownTools: "drop", InvalidKnownTools: "warn", MalformedArguments: "repair"}
	if len(policy) > 0 {
		resolvedPolicy = policy[0]
	}
	out := &Registry{tools: map[string]anthropic.Tool{}, aliases: aliases, policy: resolvedPolicy}
	for _, tool := range anthropic.ToolsForUpstream(tools, passthroughServerTools) {
		if tool.Name != "" {
			out.tools[tool.Name] = tool
		}
	}
	return out
}

func (r *Registry) Has(toolName string) bool {
	if r == nil {
		return false
	}
	_, ok := r.tools[toolName]
	return ok
}

func (r *Registry) Policy() config.ToolValidationConfig {
	if r == nil {
		return config.ToolValidationConfig{}
	}
	return r.policy
}

func (r *Registry) Validate(toolName, args string) (string, bool) {
	if r == nil {
		return "", false
	}
	tool, ok := r.tools[toolName]
	if !ok {
		return "", false
	}
	if args == "" {
		args = "{}"
	}
	restored := toolargs.RestoreCompleteArgs(toolName, args, r.aliases)
	var value any
	if err := json.Unmarshal([]byte(restored), &value); err != nil {
		return "", false
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	if len(tool.InputSchema) > 0 {
		stripEmptyOptionalArgs(obj, tool.InputSchema)
	}
	if len(tool.InputSchema) > 0 && !validAgainstSchema(obj, tool.InputSchema) {
		if r.policy.MalformedArguments == "repair" && repairObjectArgs(obj, tool.InputSchema) && validAgainstSchema(obj, tool.InputSchema) {
			cleaned, err := json.Marshal(obj)
			if err != nil {
				return restored, true
			}
			return string(cleaned), true
		}
		return "", false
	}
	cleaned, err := json.Marshal(obj)
	if err != nil {
		return restored, true
	}
	return string(cleaned), true
}

func repairObjectArgs(obj map[string]any, rawSchema json.RawMessage) bool {
	var schema map[string]any
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return false
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return false
	}
	changed := false
	renamed := false
	for prop := range props {
		if _, exists := obj[prop]; exists {
			continue
		}
		candidate, ok := repairKeyCandidate(prop, obj, props)
		if !ok {
			continue
		}
		obj[prop] = obj[candidate]
		delete(obj, candidate)
		changed = true
		renamed = true
	}
	if renamed {
		for key := range obj {
			if _, ok := props[key]; !ok {
				delete(obj, key)
				changed = true
			}
		}
	}
	if changed {
		stripEmptyOptionalArgs(obj, rawSchema)
	}
	return changed
}

func repairKeyCandidate(prop string, obj map[string]any, props map[string]any) (string, bool) {
	want := normalizeKey(prop)
	matches := []string{}
	for key := range obj {
		if _, isKnown := props[key]; isKnown {
			continue
		}
		got := normalizeKey(key)
		if got == want || strings.Contains(want, got) || strings.Contains(got, want) {
			matches = append(matches, key)
		}
	}
	if len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}

func normalizeKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Drop empty optional placeholders emitted by upstream models.
func stripEmptyOptionalArgs(obj map[string]any, rawSchema json.RawMessage) {
	var schema map[string]any
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return
	}
	required := map[string]struct{}{}
	if reqList, ok := schema["required"].([]any); ok {
		for _, item := range reqList {
			if s, ok := item.(string); ok {
				required[s] = struct{}{}
			}
		}
	}
	for name, value := range obj {
		if _, isRequired := required[name]; isRequired {
			continue
		}
		switch v := value.(type) {
		case string:
			if v == "" {
				delete(obj, name)
			}
		case nil:
			delete(obj, name)
		}
	}
}

func validAgainstSchema(value any, raw json.RawMessage) bool {
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return false
	}
	return validateSchemaValue(value, schema)
}

func validateSchemaValue(value any, schema any) bool {
	switch s := schema.(type) {
	case bool:
		return s
	case map[string]any:
		return validateValue(value, s)
	default:
		return false
	}
}

func validateValue(value any, schema map[string]any) bool {
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		matched := false
		for _, item := range enum {
			if jsonEqual(value, item) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if constant, ok := schema["const"]; ok && !jsonEqual(value, constant) {
		return false
	}
	if allOf, ok := schema["allOf"].([]any); ok {
		for _, item := range allOf {
			if !validateSchemaValue(value, item) {
				return false
			}
		}
	}
	if anyOf, ok := schema["anyOf"].([]any); ok && len(anyOf) > 0 {
		matched := false
		for _, item := range anyOf {
			if validateSchemaValue(value, item) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if oneOf, ok := schema["oneOf"].([]any); ok && len(oneOf) > 0 {
		matches := 0
		for _, item := range oneOf {
			if validateSchemaValue(value, item) {
				matches++
			}
		}
		if matches != 1 {
			return false
		}
	}
	if neg, ok := schema["not"]; ok && validateSchemaValue(value, neg) {
		return false
	}
	if typ, ok := schema["type"]; ok && !validateType(value, typ) {
		return false
	}
	if !validateStringConstraints(value, schema) {
		return false
	}
	if !validateNumberConstraints(value, schema) {
		return false
	}
	if !validateArrayConstraints(value, schema) {
		return false
	}
	if !validateObjectConstraints(value, schema) {
		return false
	}
	return true
}

func validateObjectConstraints(value any, schema map[string]any) bool {
	if _, hasProps := schema["properties"]; !hasProps {
		if _, hasRequired := schema["required"]; !hasRequired {
			if _, hasAdditional := schema["additionalProperties"]; !hasAdditional {
				if _, hasMin := schema["minProperties"]; !hasMin {
					if _, hasMax := schema["maxProperties"]; !hasMax {
						return true
					}
				}
			}
		}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return false
	}
	return validateObject(obj, schema)
}

func validateObject(obj map[string]any, schema map[string]any) bool {
	if min, ok := numberKeyword(schema, "minProperties"); ok && float64(len(obj)) < min {
		return false
	}
	if max, ok := numberKeyword(schema, "maxProperties"); ok && float64(len(obj)) > max {
		return false
	}
	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			name := item.(string)
			if _, exists := obj[name]; !exists {
				return false
			}
		}
	}
	props := map[string]any{}
	if rawProps, ok := schema["properties"]; ok {
		var propsOK bool
		props, propsOK = rawProps.(map[string]any)
		if !propsOK {
			return false
		}
	}
	for name, value := range obj {
		propSchema, ok := props[name]
		if ok {
			if !validateSchemaValue(value, propSchema) {
				return false
			}
			continue
		}
		additional, hasAdditional := schema["additionalProperties"]
		if !hasAdditional {
			continue
		}
		switch add := additional.(type) {
		case bool:
			if !add {
				return false
			}
		case map[string]any:
			if !validateSchemaValue(value, add) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validateArrayConstraints(value any, schema map[string]any) bool {
	_, hasItems := schema["items"]
	_, hasMin := schema["minItems"]
	_, hasMax := schema["maxItems"]
	if !hasItems && !hasMin && !hasMax {
		return true
	}
	arr, ok := value.([]any)
	if !ok {
		return false
	}
	if min, ok := numberKeyword(schema, "minItems"); ok && float64(len(arr)) < min {
		return false
	}
	if max, ok := numberKeyword(schema, "maxItems"); ok && float64(len(arr)) > max {
		return false
	}
	if items, ok := schema["items"]; ok {
		for _, item := range arr {
			if !validateSchemaValue(item, items) {
				return false
			}
		}
	}
	return true
}

func validateStringConstraints(value any, schema map[string]any) bool {
	_, hasMin := schema["minLength"]
	_, hasMax := schema["maxLength"]
	_, hasPattern := schema["pattern"]
	if !hasMin && !hasMax && !hasPattern {
		return true
	}
	s, ok := value.(string)
	if !ok {
		return false
	}
	length := utf8.RuneCountInString(s)
	if min, ok := numberKeyword(schema, "minLength"); ok && float64(length) < min {
		return false
	}
	if max, ok := numberKeyword(schema, "maxLength"); ok && float64(length) > max {
		return false
	}
	if pattern, ok := schema["pattern"].(string); ok {
		matched, err := regexp.MatchString(pattern, s)
		if err != nil || !matched {
			return false
		}
	}
	return true
}

func validateNumberConstraints(value any, schema map[string]any) bool {
	_, hasMin := schema["minimum"]
	_, hasMax := schema["maximum"]
	_, hasExclusiveMin := schema["exclusiveMinimum"]
	_, hasExclusiveMax := schema["exclusiveMaximum"]
	_, hasMultiple := schema["multipleOf"]
	if !hasMin && !hasMax && !hasExclusiveMin && !hasExclusiveMax && !hasMultiple {
		return true
	}
	n, ok := value.(float64)
	if !ok {
		return false
	}
	if min, ok := numberKeyword(schema, "minimum"); ok && n < min {
		return false
	}
	if max, ok := numberKeyword(schema, "maximum"); ok && n > max {
		return false
	}
	if min, ok := numberKeyword(schema, "exclusiveMinimum"); ok && n <= min {
		return false
	}
	if max, ok := numberKeyword(schema, "exclusiveMaximum"); ok && n >= max {
		return false
	}
	if minExclusive, ok := schema["exclusiveMinimum"].(bool); ok && minExclusive {
		if min, ok := numberKeyword(schema, "minimum"); ok && n <= min {
			return false
		}
	}
	if maxExclusive, ok := schema["exclusiveMaximum"].(bool); ok && maxExclusive {
		if max, ok := numberKeyword(schema, "maximum"); ok && n >= max {
			return false
		}
	}
	if multipleOf, ok := numberKeyword(schema, "multipleOf"); ok {
		if multipleOf <= 0 {
			return false
		}
		quotient := n / multipleOf
		if math.Abs(quotient-math.Round(quotient)) > 1e-9 {
			return false
		}
	}
	return true
}

func numberKeyword(schema map[string]any, key string) (float64, bool) {
	value, ok := schema[key]
	if !ok {
		return 0, false
	}
	n, ok := value.(float64)
	return n, ok
}

func validateType(value any, typ any) bool {
	switch t := typ.(type) {
	case string:
		return validateSingleType(value, t)
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && validateSingleType(value, s) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func validateSingleType(value any, typ string) bool {
	switch typ {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		f, ok := value.(float64)
		return ok && math.Trunc(f) == f
	case "null":
		return value == nil
	default:
		return false
	}
}

func jsonEqual(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}
