package main

import (
	"fmt"
	"reflect"
	"strings"
)

type jinjaTemplate struct {
	nodes []jinjaNode
}

type jinjaNode interface {
	render(ctx *evalContext, b *strings.Builder) error
}

type jinjaTokenKind int

const (
	tokenText jinjaTokenKind = iota
	tokenVariable
	tokenStatement
)

type jinjaToken struct {
	kind    jinjaTokenKind
	content string
}

func parseJinjaTemplate(src string) (*jinjaTemplate, error) {
	tokens, err := tokenizeJinja(src)
	if err != nil {
		return nil, err
	}
	pos := 0
	nodes, err := parseJinjaNodes(tokens, &pos, nil)
	if err != nil {
		return nil, err
	}
	return &jinjaTemplate{nodes: nodes}, nil
}

func (t *jinjaTemplate) Render(context map[string]interface{}) (string, error) {
	evalCtx := newEvalContext(context)
	var builder strings.Builder
	if err := renderNodes(t.nodes, evalCtx, &builder); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func renderNodes(nodes []jinjaNode, ctx *evalContext, b *strings.Builder) error {
	for _, node := range nodes {
		if err := node.render(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

func tokenizeJinja(src string) ([]jinjaToken, error) {
	tokens := make([]jinjaToken, 0)
	for len(src) > 0 {
		idxVar := strings.Index(src, "{{")
		idxStmt := strings.Index(src, "{%")
		next := -1
		kind := tokenText
		if idxVar != -1 && (idxStmt == -1 || idxVar < idxStmt) {
			next = idxVar
			kind = tokenVariable
		} else if idxStmt != -1 {
			next = idxStmt
			kind = tokenStatement
		}
		if next == -1 {
			tokens = append(tokens, jinjaToken{kind: tokenText, content: src})
			break
		}
		if next > 0 {
			tokens = append(tokens, jinjaToken{kind: tokenText, content: src[:next]})
			src = src[next:]
		}
		switch kind {
		case tokenVariable:
			end := strings.Index(src, "}}")
			if end == -1 {
				return nil, fmt.Errorf("unterminated variable expression")
			}
			content := strings.TrimSpace(src[2:end])
			tokens = append(tokens, jinjaToken{kind: tokenVariable, content: content})
			src = src[end+2:]
		case tokenStatement:
			end := strings.Index(src, "%}")
			if end == -1 {
				return nil, fmt.Errorf("unterminated statement")
			}
			content := strings.TrimSpace(src[2:end])
			tokens = append(tokens, jinjaToken{kind: tokenStatement, content: content})
			src = src[end+2:]
		}
	}
	return tokens, nil
}

func parseJinjaNodes(tokens []jinjaToken, pos *int, stop map[string]bool) ([]jinjaNode, error) {
	nodes := make([]jinjaNode, 0)
	for *pos < len(tokens) {
		token := tokens[*pos]
		switch token.kind {
		case tokenText:
			nodes = append(nodes, &textNode{text: token.content})
			*pos++
		case tokenVariable:
			expr, err := parseValueExpression(token.content)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, &variableNode{expr: expr})
			*pos++
		case tokenStatement:
			keyword := firstWord(token.content)
			if stop != nil && stop[keyword] {
				return nodes, nil
			}
			switch keyword {
			case "if":
				ifNode, err := parseIf(tokens, pos)
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, ifNode)
			case "for":
				forNode, err := parseFor(tokens, pos)
				if err != nil {
					return nil, err
				}
				nodes = append(nodes, forNode)
			case "elif", "else", "endif", "endfor":
				if stop != nil && stop[keyword] {
					return nodes, nil
				}
				return nil, fmt.Errorf("unexpected %s", keyword)
			default:
				return nil, fmt.Errorf("unsupported statement: %s", token.content)
			}
		}
	}
	if stop != nil && len(stop) > 0 {
		expected := make([]string, 0, len(stop))
		for key := range stop {
			expected = append(expected, key)
		}
		return nil, fmt.Errorf("expected one of %s", strings.Join(expected, ", "))
	}
	return nodes, nil
}

func parseIf(tokens []jinjaToken, pos *int) (jinjaNode, error) {
	token := tokens[*pos]
	conditionExpr := strings.TrimSpace(strings.TrimPrefix(token.content, "if"))
	condition, err := parseCondition(conditionExpr)
	if err != nil {
		return nil, err
	}
	*pos++
	body, err := parseJinjaNodes(tokens, pos, map[string]bool{"elif": true, "else": true, "endif": true})
	if err != nil {
		return nil, err
	}
	branches := []ifBranch{{condition: condition, body: body}}
	for *pos < len(tokens) {
		token = tokens[*pos]
		keyword := firstWord(token.content)
		switch keyword {
		case "elif":
			expr := strings.TrimSpace(strings.TrimPrefix(token.content, "elif"))
			cond, err := parseCondition(expr)
			if err != nil {
				return nil, err
			}
			*pos++
			elifBody, err := parseJinjaNodes(tokens, pos, map[string]bool{"elif": true, "else": true, "endif": true})
			if err != nil {
				return nil, err
			}
			branches = append(branches, ifBranch{condition: cond, body: elifBody})
		case "else":
			*pos++
			elseBody, err := parseJinjaNodes(tokens, pos, map[string]bool{"endif": true})
			if err != nil {
				return nil, err
			}
			branches = append(branches, ifBranch{condition: nil, body: elseBody})
			goto endif
		case "endif":
			goto endif
		default:
			return nil, fmt.Errorf("unexpected token in if block: %s", token.content)
		}
	}
endif:
	if *pos >= len(tokens) || firstWord(tokens[*pos].content) != "endif" {
		return nil, fmt.Errorf("if statement not closed with endif")
	}
	*pos++
	return &ifNode{branches: branches}, nil
}

func parseFor(tokens []jinjaToken, pos *int) (jinjaNode, error) {
	token := tokens[*pos]
	fields := strings.Fields(token.content)
	if len(fields) < 4 || fields[0] != "for" || fields[2] != "in" {
		return nil, fmt.Errorf("invalid for statement: %s", token.content)
	}
	varName := fields[1]
	iterableExpr := strings.Join(fields[3:], " ")
	expr, err := parseValueExpression(iterableExpr)
	if err != nil {
		return nil, err
	}
	*pos++
	body, err := parseJinjaNodes(tokens, pos, map[string]bool{"endfor": true})
	if err != nil {
		return nil, err
	}
	if *pos >= len(tokens) || firstWord(tokens[*pos].content) != "endfor" {
		return nil, fmt.Errorf("for statement not closed with endfor")
	}
	*pos++
	return &forNode{variable: varName, iterable: expr, body: body}, nil
}

type textNode struct {
	text string
}

func (n *textNode) render(_ *evalContext, b *strings.Builder) error {
	b.WriteString(n.text)
	return nil
}

type variableNode struct {
	expr *valueExpression
}

func (n *variableNode) render(ctx *evalContext, b *strings.Builder) error {
	value, err := n.expr.Eval(ctx)
	if err != nil {
		return err
	}
	if value == nil {
		return nil
	}
	b.WriteString(fmt.Sprint(value))
	return nil
}

type ifBranch struct {
	condition condition
	body      []jinjaNode
}

type ifNode struct {
	branches []ifBranch
}

func (n *ifNode) render(ctx *evalContext, b *strings.Builder) error {
	for _, branch := range n.branches {
		if branch.condition == nil {
			return renderNodes(branch.body, ctx, b)
		}
		match, err := branch.condition.Eval(ctx)
		if err != nil {
			return err
		}
		if match {
			return renderNodes(branch.body, ctx, b)
		}
	}
	return nil
}

type forNode struct {
	variable string
	iterable *valueExpression
	body     []jinjaNode
}

func (n *forNode) render(ctx *evalContext, b *strings.Builder) error {
	iterableValue, err := n.iterable.Eval(ctx)
	if err != nil {
		return err
	}
	items, err := toIterable(iterableValue)
	if err != nil {
		return err
	}
	for _, item := range items {
		ctx.pushScope()
		ctx.set(n.variable, item)
		if err := renderNodes(n.body, ctx, b); err != nil {
			ctx.popScope()
			return err
		}
		ctx.popScope()
	}
	return nil
}

type filterSpec struct {
	name string
	args []string
}

type valueExpression struct {
	path    []string
	filters []filterSpec
}

func parseValueExpression(input string) (*valueExpression, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return &valueExpression{path: nil}, nil
	}
	segments := strings.Split(input, "|")
	base := strings.TrimSpace(segments[0])
	if base == "" {
		return nil, fmt.Errorf("empty expression")
	}
	path := strings.Split(base, ".")
	for i := range path {
		path[i] = strings.TrimSpace(path[i])
		if path[i] == "" {
			return nil, fmt.Errorf("invalid expression segment in %q", input)
		}
	}
	filters := make([]filterSpec, 0)
	for _, segment := range segments[1:] {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		parts := strings.SplitN(segment, ":", 2)
		name := strings.TrimSpace(parts[0])
		args := []string{}
		if len(parts) == 2 {
			args = append(args, strings.TrimSpace(parts[1]))
		}
		filters = append(filters, filterSpec{name: name, args: args})
	}
	return &valueExpression{path: path, filters: filters}, nil
}

func (e *valueExpression) Eval(ctx *evalContext) (interface{}, error) {
	value, _ := ctx.resolve(e.path)
	for _, filter := range e.filters {
		switch filter.name {
		case "upper":
			value = strings.ToUpper(fmt.Sprint(value))
		case "lower":
			value = strings.ToLower(fmt.Sprint(value))
		case "default":
			fallback := ""
			if len(filter.args) > 0 {
				fallback = trimQuotes(filter.args[0])
			}
			if !toBool(value) {
				value = fallback
			}
		default:
			return nil, fmt.Errorf("unsupported filter: %s", filter.name)
		}
	}
	return value, nil
}

type condition interface {
	Eval(ctx *evalContext) (bool, error)
}

type simpleCondition struct {
	expr *valueExpression
}

func (c *simpleCondition) Eval(ctx *evalContext) (bool, error) {
	value, err := c.expr.Eval(ctx)
	if err != nil {
		return false, err
	}
	return toBool(value), nil
}

type andCondition struct {
	terms []*valueExpression
}

func (c *andCondition) Eval(ctx *evalContext) (bool, error) {
	for _, term := range c.terms {
		value, err := term.Eval(ctx)
		if err != nil {
			return false, err
		}
		if !toBool(value) {
			return false, nil
		}
	}
	return true, nil
}

func parseCondition(input string) (condition, error) {
	tokens := strings.Fields(input)
	if len(tokens) == 0 {
		return &simpleCondition{expr: &valueExpression{}}, nil
	}
	parts := make([]string, 0)
	start := 0
	for i, token := range tokens {
		if token == "and" {
			part := strings.Join(tokens[start:i], " ")
			parts = append(parts, part)
			start = i + 1
		}
	}
	parts = append(parts, strings.Join(tokens[start:], " "))
	if len(parts) == 1 {
		expr, err := parseValueExpression(parts[0])
		if err != nil {
			return nil, err
		}
		return &simpleCondition{expr: expr}, nil
	}
	expressions := make([]*valueExpression, 0, len(parts))
	for _, part := range parts {
		expr, err := parseValueExpression(part)
		if err != nil {
			return nil, err
		}
		expressions = append(expressions, expr)
	}
	return &andCondition{terms: expressions}, nil
}

type evalContext struct {
	scopes []map[string]interface{}
}

func newEvalContext(data map[string]interface{}) *evalContext {
	base := make(map[string]interface{}, len(data))
	for key, value := range data {
		base[key] = value
	}
	return &evalContext{scopes: []map[string]interface{}{base}}
}

func (c *evalContext) pushScope() {
	c.scopes = append(c.scopes, make(map[string]interface{}))
}

func (c *evalContext) popScope() {
	if len(c.scopes) > 1 {
		c.scopes = c.scopes[:len(c.scopes)-1]
	}
}

func (c *evalContext) set(name string, value interface{}) {
	c.scopes[len(c.scopes)-1][name] = value
}

func (c *evalContext) resolve(path []string) (interface{}, bool) {
	if len(path) == 0 {
		return nil, false
	}
	value, ok := c.lookup(path[0])
	if !ok {
		return nil, false
	}
	for _, segment := range path[1:] {
		if value == nil {
			return nil, false
		}
		switch typed := value.(type) {
		case map[string]interface{}:
			var exists bool
			value, exists = typed[segment]
			if !exists {
				return nil, false
			}
		case map[string]string:
			var exists bool
			value, exists = typed[segment]
			if !exists {
				return nil, false
			}
		default:
			rv := reflect.ValueOf(value)
			if rv.Kind() == reflect.Map {
				key := reflect.ValueOf(segment)
				mapValue := rv.MapIndex(key)
				if !mapValue.IsValid() {
					return nil, false
				}
				value = mapValue.Interface()
			} else {
				return nil, false
			}
		}
	}
	return value, true
}

func (c *evalContext) lookup(name string) (interface{}, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if value, ok := c.scopes[i][name]; ok {
			return value, true
		}
	}
	return nil, false
}

func toIterable(value interface{}) ([]interface{}, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []interface{}:
		return typed, nil
	case []string:
		result := make([]interface{}, len(typed))
		for i, v := range typed {
			result[i] = v
		}
		return result, nil
	}
	rv := reflect.ValueOf(value)
	kind := rv.Kind()
	if kind != reflect.Slice && kind != reflect.Array {
		return nil, fmt.Errorf("value %v is not iterable", value)
	}
	result := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		result[i] = rv.Index(i).Interface()
	}
	return result, nil
}

func toBool(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != ""
	case int:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case uint:
		return typed != 0
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	case float32:
		return typed != 0
	case float64:
		return typed != 0
	case []string:
		return len(typed) > 0
	case []interface{}:
		return len(typed) > 0
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	}
	return true
}

func trimQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func firstWord(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
