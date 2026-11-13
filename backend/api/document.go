// Package api embeds the versioned HTTP contract served at /openapi.json.
package api

import _ "embed"

// Document contains the complete OpenAPI 3.1 JSON document.
//
//go:embed openapi.json
var Document []byte
