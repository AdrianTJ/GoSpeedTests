// Package docs embeds the OpenAPI specification into the binary so the
// daemon can serve /openapi.yaml (and the Swagger UI that loads it) from any
// working directory. Serving the file from disk broke /docs with a 404
// whenever gostd was started outside the repo root.
package docs

import _ "embed"

// OpenAPISpec is the embedded contents of openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
