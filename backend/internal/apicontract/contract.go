// Package apicontract validates HTTP exchanges against the checked-in contract.
// It is used by contract checks, not in the application's request path.
package apicontract

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const resource = "https://dicho.example/openapi.json"

type Operation struct {
	Method, Path, APICode string
	UseCases              []string
	node                  map[string]any
}
type Contract struct {
	root       map[string]any
	operations []Operation
	compiler   *jsonschema.Compiler
	mu         sync.Mutex
	schemas    map[string]*jsonschema.Schema
}
type localOnly struct{}

func (localOnly) Load(url string) (any, error) {
	return nil, fmt.Errorf("external reference forbidden in offline contract: %s", url)
}

func New(document []byte) (*Contract, error) {
	var root map[string]any
	if err := json.Unmarshal(document, &root); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(fmt.Sprint(root["openapi"]), "3.1.") {
		return nil, fmt.Errorf("OpenAPI3.1 required")
	}
	c := &Contract{root: root, compiler: jsonschema.NewCompiler(), schemas: map[string]*jsonschema.Schema{}}
	c.compiler.DefaultDraft(jsonschema.Draft2020)
	c.compiler.AssertFormat()
	c.compiler.UseLoader(localOnly{})
	if err := c.compiler.AddResource(resource, root); err != nil {
		return nil, err
	}
	components, ok := root["components"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing components")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing schemas")
	}
	for name := range schemas {
		if _, err := c.compiler.Compile(resource + "#/components/schemas/" + name); err != nil {
			return nil, fmt.Errorf("schema %s: %w", name, err)
		}
	}
	if err := c.checkReferences(root); err != nil {
		return nil, err
	}
	paths, ok := root["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing paths")
	}
	for path, item := range paths {
		for method, node := range item.(map[string]any) {
			if !strings.Contains(" get post put patch delete options head ", " "+method+" ") {
				continue
			}
			m := node.(map[string]any)
			op := Operation{Method: strings.ToUpper(method), Path: path, APICode: fmt.Sprint(m["x-api-code"]), node: m}
			for _, uc := range m["x-use-case"].([]any) {
				op.UseCases = append(op.UseCases, uc.(string))
			}
			c.operations = append(c.operations, op)
		}
	}
	sort.Slice(c.operations, func(i, j int) bool {
		a, b := c.operations[i], c.operations[j]
		return a.Path+" "+a.Method < b.Path+" "+b.Method
	})
	return c, nil
}
func (c *Contract) Operations() []Operation { return append([]Operation(nil), c.operations...) }
func (c *Contract) checkReferences(v any) error {
	switch x := v.(type) {
	case map[string]any:
		if ref, ok := x["$ref"].(string); ok {
			if _, err := c.resolve(ref); err != nil {
				return err
			}
		}
		for _, child := range x {
			if err := c.checkReferences(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range x {
			if err := c.checkReferences(child); err != nil {
				return err
			}
		}
	}
	return nil
}
func (c *Contract) resolve(ref string) (map[string]any, error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("nonlocal reference %s", ref)
	}
	var value any = c.root
	for _, p := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		p = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
		m, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid reference %s", ref)
		}
		value, ok = m[p]
		if !ok {
			return nil, fmt.Errorf("missing reference %s", ref)
		}
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reference is not object %s", ref)
	}
	return m, nil
}
func (c *Contract) deref(node map[string]any) (map[string]any, error) {
	if ref, ok := node["$ref"].(string); ok {
		return c.resolve(ref)
	}
	return node, nil
}
func (c *Contract) match(method, path string) (Operation, map[string]string, error) {
	u, err := url.Parse(path)
	if err != nil {
		return Operation{}, nil, err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var found *Operation
	var values map[string]string
	best := -1
	for _, op := range c.operations {
		if op.Method != method {
			continue
		}
		pattern := strings.Split(strings.Trim(op.Path, "/"), "/")
		if len(pattern) != len(parts) {
			continue
		}
		params := map[string]string{}
		score := 0
		ok := true
		for i, p := range pattern {
			if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
				params[strings.Trim(p, "{}")] = parts[i]
			} else if p != parts[i] {
				ok = false
				break
			} else {
				score++
			}
		}
		if ok && score > best {
			copy := op
			found = &copy
			values = params
			best = score
		}
	}
	if found == nil {
		return Operation{}, nil, fmt.Errorf("operation not documented: %s %s", method, u.Path)
	}
	return *found, values, nil
}
func (c *Contract) validate(schema map[string]any, value any) error {
	b, _ := json.Marshal(schema)
	key := fmt.Sprintf("%x", sha256.Sum256(b))
	c.mu.Lock()
	s := c.schemas[key]
	if s == nil {
		uri := "https://dicho.example/contracts/" + key
		wrapper := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "components": c.root["components"], "allOf": []any{schema}}
		if err := c.compiler.AddResource(uri, wrapper); err != nil {
			c.mu.Unlock()
			return err
		}
		var err error
		s, err = c.compiler.Compile(uri)
		if err != nil {
			c.mu.Unlock()
			return err
		}
		c.schemas[key] = s
	}
	c.mu.Unlock()
	return s.Validate(value)
}
func (c *Contract) Request(r *http.Request, body []byte) error {
	op, values, err := c.match(r.Method, r.URL.Path)
	if err != nil {
		return err
	}
	security, public := op.node["security"].([]any)
	if !public || len(security) > 0 {
		if token := r.Header.Get("Authorization"); !strings.HasPrefix(token, "Bearer ") || len(token) <= 7 {
			return fmt.Errorf("Bearer authentication required")
		}
	}
	parameters, _ := op.node["parameters"].([]any)
	for _, raw := range parameters {
		p, err := c.deref(raw.(map[string]any))
		if err != nil {
			return err
		}
		name := p["name"].(string)
		schema := p["schema"].(map[string]any)
		location := p["in"].(string)
		var value string
		var present bool
		switch location {
		case "header":
			value = r.Header.Get(name)
			present = value != ""
		case "path":
			value, present = values[name]
		case "query":
			_, present = r.URL.Query()[name]
			value = r.URL.Query().Get(name)
		}
		required, _ := p["required"].(bool)
		if condition, ok := p["x-required-when"].(string); ok {
			var data map[string]any
			_ = json.Unmarshal(body, &data)
			switch condition {
			case "body.dryRun != true":
				required = data["dryRun"] != true
			case "body.remainingQuantity != null":
				required = data["remainingQuantity"] != nil
			}
		}
		if !present {
			if required {
				return fmt.Errorf("required %s %s absent", location, name)
			}
			continue
		}
		var typed any = value
		switch schema["type"] {
		case "integer":
			n, e := strconv.ParseInt(value, 10, 64)
			if e != nil {
				return e
			}
			typed = float64(n)
		case "number":
			n, e := strconv.ParseFloat(value, 64)
			if e != nil {
				return e
			}
			typed = n
		case "boolean":
			n, e := strconv.ParseBool(value)
			if e != nil {
				return e
			}
			typed = n
		case "array":
			list := []any{}
			for _, v := range r.URL.Query()[name] {
				for _, part := range strings.Split(v, ",") {
					list = append(list, part)
				}
			}
			typed = list
		}
		if err = c.validate(schema, typed); err != nil {
			return fmt.Errorf("%s %s: %w", location, name, err)
		}
	}
	raw, hasBody := op.node["requestBody"].(map[string]any)
	if !hasBody {
		return nil
	}
	rb, err := c.deref(raw)
	if err != nil {
		return err
	}
	required, _ := rb["required"].(bool)
	if len(body) == 0 {
		if required {
			return fmt.Errorf("request body required")
		}
		return nil
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("application/json request required")
	}
	var data any
	if err = json.Unmarshal(body, &data); err != nil {
		return err
	}
	schema := rb["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if err = c.validate(schema, data); err != nil {
		return fmt.Errorf("%s request: %w", op.APICode, err)
	}
	return nil
}
func (c *Contract) Response(method, path string, status int, headers http.Header, body []byte) error {
	op, _, err := c.match(method, path)
	if err != nil {
		return err
	}
	responses := op.node["responses"].(map[string]any)
	raw, ok := responses[strconv.Itoa(status)].(map[string]any)
	if !ok {
		return fmt.Errorf("undocumented response %s status%d", op.APICode, status)
	}
	response, err := c.deref(raw)
	if err != nil {
		return err
	}
	content, ok := response["content"].(map[string]any)
	if !ok {
		if len(body) != 0 {
			return fmt.Errorf("status%d must have empty body", status)
		}
		return nil
	}
	if headers != nil && !strings.HasPrefix(headers.Get("Content-Type"), "application/json") {
		return fmt.Errorf("application/json response required")
	}
	var data any
	if err = json.Unmarshal(body, &data); err != nil {
		return err
	}
	schema := content["application/json"].(map[string]any)["schema"].(map[string]any)
	if err = c.validate(schema, data); err != nil {
		return fmt.Errorf("%s response%d: %w", op.APICode, status, err)
	}
	return nil
}
